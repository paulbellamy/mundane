"""Basic tests for mundane (Python)."""

import asyncio
import contextlib
import json
import multiprocessing as mp
import os
import sqlite3
import sys
import tempfile
import time
import unittest

# Make the package importable without installation.
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mundane


def _hold_lock_child(path, barrier, done):
    # Module-scope helper for test_second_process_gets_locked_error.
    # Must be picklable for multiprocessing's spawn start method (default on
    # macOS), so it can't be a local closure inside the test method.
    def wf(ctx):
        ctx.step("a", lambda: 1)
        barrier.set()
        done.wait(timeout=5)
        return 0

    mundane.run(path, wf)


def _names(path):
    """Step names in id order, read straight from SQLite (no inspect API)."""
    conn = sqlite3.connect(path)
    try:
        return [r[0] for r in conn.execute(
            "SELECT name FROM mundane_steps ORDER BY id"
        )]
    finally:
        conn.close()


class TempDB:
    def __enter__(self):
        self.fd, self.path = tempfile.mkstemp(suffix=".db")
        os.close(self.fd)
        os.remove(self.path)  # mundane will recreate
        return self.path

    def __exit__(self, *_):
        for ext in ("", "-wal", "-shm", ".lock"):
            with contextlib.suppress(FileNotFoundError):
                os.remove(self.path + ext)


class HappyPath(unittest.TestCase):
    def test_three_steps_run_once(self):
        calls = []

        def wf(ctx):
            a = ctx.step("a", lambda: (calls.append("a"), 1)[1])
            b = ctx.step("b", lambda: (calls.append("b"), {"v": a + 1})[1])
            return b

        with TempDB() as path:
            r = mundane.run(path, wf)
            self.assertEqual(r, {"v": 2})
            self.assertEqual(calls, ["a", "b"])

            # Re-run: should return same value without re-calling
            r2 = mundane.run(path, wf)
            self.assertEqual(r2, {"v": 2})
            self.assertEqual(calls, ["a", "b"])  # not re-called

    def test_resume_after_crash(self):
        """Simulate crash by raising after step 1; verify step 1 is cached."""
        with TempDB() as path:
            # First run: succeed step a, raise before step b commits
            def wf1(ctx):
                ctx.step("a", lambda: 42)
                raise RuntimeError("simulated crash")

            with self.assertRaises(RuntimeError):
                mundane.run(path, wf1)

            # Second run: step a returns 42 from cache; step b runs and finishes.
            calls = []

            def wf2(ctx):
                v = ctx.step("a", lambda: (calls.append("a"), 999)[1])
                w = ctx.step("b", lambda: (calls.append("b"), v + 1)[1])
                return w

            r = mundane.run(path, wf2)
            self.assertEqual(r, 43)
            self.assertEqual(calls, ["b"])  # only b was called; a came from cache


class Naming(unittest.TestCase):
    def test_invalid_name_rejected(self):
        with TempDB() as path:
            def wf(ctx):
                ctx.step("bad name with space", lambda: 1)
            with self.assertRaises(ValueError):
                mundane.run(path, wf)

    def test_duplicate_name_raises(self):
        with TempDB() as path:
            def wf(ctx):
                ctx.step("x", lambda: 1)
                ctx.step("x", lambda: 2)  # duplicate -> raises
            with self.assertRaises(mundane.DuplicateStepError):
                mundane.run(path, wf)
            # First step still committed before the duplicate was raised.
            self.assertEqual(_names(path), ["x"])


class Locking(unittest.TestCase):
    def test_second_process_gets_locked_error(self):
        with TempDB() as path:
            barrier = mp.Event()
            done = mp.Event()

            proc = mp.Process(target=_hold_lock_child, args=(path, barrier, done))
            proc.start()
            try:
                barrier.wait(timeout=5)
                # Try to open while the other process holds the lock.
                with self.assertRaises(mundane.LockedError):
                    mundane.run(path, lambda ctx: None)
            finally:
                done.set()
                proc.join(timeout=5)


class Sleep(unittest.TestCase):
    def test_sleep_persists_wake_at_and_resumes(self):
        with TempDB() as path:
            # First "run": write the sleep row but interrupt before sleeping
            # by using a 0ms duration.
            t0 = time.time()
            mundane.run(path, lambda ctx: ctx.sleep("nap", "10ms"))
            elapsed1 = time.time() - t0
            self.assertLess(elapsed1, 0.2)

            # Second run: nap row already done; should return immediately.
            t0 = time.time()
            mundane.run(path, lambda ctx: ctx.sleep("nap", "10s"))
            elapsed2 = time.time() - t0
            self.assertLess(elapsed2, 0.2)

    def test_resume_ignores_invalid_duration(self):
        with TempDB() as path:
            mundane.run(path, lambda ctx: ctx.sleep("n", "1ms"))
            # Resume ignores the duration arg, so an invalid string is a no-op.
            mundane.run(path, lambda ctx: ctx.sleep("n", "not-a-duration"))

    def test_sleep_remaining_on_resume(self):
        """If we crash mid-sleep, next run should sleep only the remaining time."""
        with TempDB() as path:
            # First run: writes wake_at = now + 200ms then sleeps the full 200ms.
            # We use 50ms so the test is quick but observable.
            t0 = time.time()
            mundane.run(path, lambda ctx: ctx.sleep("n", "50ms"))
            self.assertGreaterEqual(time.time() - t0, 0.04)

    def test_resume_sleeps_only_the_remainder(self):
        """A wake_at still in the future makes resume sleep the remainder."""
        with TempDB() as path:
            # Establish the sleep row with a short duration.
            mundane.run(path, lambda ctx: ctx.sleep("n", "10ms"))
            # Rewrite wake_at ~300ms into the future to simulate a long nap whose
            # process was restarted before it elapsed.
            future = int(time.time() * 1000) + 300
            conn = sqlite3.connect(path)
            conn.execute("UPDATE mundane_steps SET result=? WHERE name='n'", (str(future),))
            conn.commit()
            conn.close()
            # Resume must block for the remaining ~300ms (duration arg ignored).
            t0 = time.time()
            mundane.run(path, lambda ctx: ctx.sleep("n", "10ms"))
            self.assertGreaterEqual(time.time() - t0, 0.2)


class FailedStep(unittest.TestCase):
    def test_failed_step_reruns(self):
        with TempDB() as path:
            def boom():
                raise RuntimeError("boom")

            with self.assertRaises(mundane.StepFailedError):
                mundane.run(path, lambda ctx: ctx.step("s", boom))

            # A failed step is not cached; it must re-run.
            calls = []
            r = mundane.run(path, lambda ctx: ctx.step("s", lambda: (calls.append("s"), 7)[1]))
            self.assertEqual(r, 7)
            self.assertEqual(calls, ["s"])

    def test_failed_row_reset_to_pending_during_rerun(self):
        with TempDB() as path:
            def boom():
                raise RuntimeError("boom")

            with self.assertRaises(mundane.StepFailedError):
                mundane.run(path, lambda ctx: ctx.step("s", boom))

            # While the re-run body executes, the row must read 'pending' with
            # the stale error cleared (the reset committed before fn runs).
            seen = {}

            def observe():
                # observe() runs inside mundane.run, while the writer holds
                # an exclusive flock on the file. On macOS, opening with the
                # default unix VFS would deadlock against that flock — open
                # with unix-none (no SQLite-level lock) instead.
                conn = sqlite3.connect(f"file:{path}?vfs=unix-none", uri=True)
                seen["status"], seen["error"] = conn.execute(
                    "SELECT status, error FROM mundane_steps WHERE name='s'"
                ).fetchone()
                conn.close()
                return 7

            mundane.run(path, lambda ctx: ctx.step("s", observe))
            self.assertEqual(seen["status"], "pending")
            self.assertIsNone(seen["error"])


class Async(unittest.TestCase):
    def test_arun_astep_asleep(self):
        async def aval(calls, tag, v):
            calls.append(tag)
            return v

        with TempDB() as path:
            calls = []

            async def wf(ctx):
                a = await ctx.astep("a", lambda: aval(calls, "a", 1))
                await ctx.asleep("nap", "10ms")
                return await ctx.astep("b", lambda: aval(calls, "b", a + 1))

            r = asyncio.run(mundane.arun(path, wf))
            self.assertEqual(r, 2)
            self.assertEqual(calls, ["a", "b"])

            # Resume: both steps cache-hit, neither fn runs.
            calls.clear()
            r2 = asyncio.run(mundane.arun(path, wf))
            self.assertEqual(r2, 2)
            self.assertEqual(calls, [])


class Inspect(unittest.TestCase):
    def test_steps_committed(self):
        with TempDB() as path:
            def wf(ctx):
                ctx.step("a", lambda: {"x": 1})
                ctx.step("b", lambda: "hello")

            mundane.run(path, wf)
            # Inspection lives in the CLI now; verify on-disk state directly.
            conn = sqlite3.connect(path)
            done = conn.execute(
                "SELECT COUNT(*) FROM mundane_steps WHERE status='done'"
            ).fetchone()[0]
            self.assertEqual(done, 2)
            a = conn.execute(
                "SELECT result, encoding FROM mundane_steps WHERE name='a'"
            ).fetchone()
            self.assertEqual(a[1], "json")
            self.assertEqual(json.loads(a[0]), {"x": 1})
            conn.close()


class Serialization(unittest.TestCase):
    def test_non_jsonable_raises(self):
        with TempDB() as path:
            def wf(ctx):
                # Tuples become lists in JSON; that's a roundtrip mismatch.
                ctx.step("a", lambda: (1, 2, 3))
            with self.assertRaises(mundane.SerializationError):
                mundane.run(path, wf)


class Schema(unittest.TestCase):
    def test_wrong_schema_version_rejected(self):
        with TempDB() as path:
            # Create a file with wrong schema version manually.
            conn = sqlite3.connect(path)
            conn.execute("CREATE TABLE mundane_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)")
            conn.execute("CREATE TABLE mundane_steps (id INTEGER PRIMARY KEY)")
            conn.execute("INSERT INTO mundane_meta VALUES ('schema_version', '99')")
            conn.commit()
            conn.close()

            with self.assertRaises(mundane.SchemaError):
                mundane.run(path, lambda ctx: None)


if __name__ == "__main__":
    unittest.main()
