"""Basic tests for mundane (Python)."""

import contextlib
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

    def test_duplicate_names_disambiguated(self):
        with TempDB() as path:
            def wf(ctx):
                return [
                    ctx.step("x", lambda: 1),
                    ctx.step("x", lambda: 2),
                    ctx.step("x", lambda: 3),
                ]
            self.assertEqual(mundane.run(path, wf), [1, 2, 3])
            # Inspect: should see x, x#2, x#3
            names = [r["name"] for r in mundane.steps(path)]
            self.assertEqual(names, ["x", "x#2", "x#3"])


class Locking(unittest.TestCase):
    def test_second_process_gets_locked_error(self):
        with TempDB() as path:
            barrier = mp.Event()
            done = mp.Event()

            def hold_lock(p, b, d):
                import mundane as m
                def wf(ctx):
                    ctx.step("a", lambda: 1)
                    b.set()
                    d.wait(timeout=5)
                    return 0
                m.run(p, wf)

            proc = mp.Process(target=hold_lock, args=(path, barrier, done))
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

    def test_sleep_remaining_on_resume(self):
        """If we crash mid-sleep, next run should sleep only the remaining time."""
        with TempDB() as path:
            # First run: writes wake_at = now + 200ms then sleeps the full 200ms.
            # We use 50ms so the test is quick but observable.
            t0 = time.time()
            mundane.run(path, lambda ctx: ctx.sleep("n", "50ms"))
            self.assertGreaterEqual(time.time() - t0, 0.04)


class Inspect(unittest.TestCase):
    def test_status_and_get(self):
        with TempDB() as path:
            def wf(ctx):
                ctx.step("a", lambda: {"x": 1})
                ctx.step("b", lambda: "hello")

            mundane.run(path, wf)
            st = mundane.status(path)
            self.assertEqual(st["done"], 2)
            self.assertEqual(mundane.get_result(path, "a"), {"x": 1})
            self.assertEqual(mundane.get_result(path, "b"), "hello")


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
