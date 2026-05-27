"""Core implementation: run, ctx.step, ctx.sleep."""

from __future__ import annotations

import asyncio
import json
import sqlite3
import time
import urllib.parse
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any, Awaitable, Callable, Dict, Optional, Set, Union

from ._duration import parse_duration_ms
from ._lock import FileLock
from ._schema import BOOTSTRAP_SQL, SCHEMA_VERSION, validate_name


class LockedError(Exception):
    """Raised when the SQLite file is locked by another live process."""


class SerializationError(Exception):
    """Raised when a step's return value doesn't survive a JSON round-trip."""


class SchemaError(Exception):
    """Raised when meta.schema_version doesn't equal 1."""


class DuplicateStepError(Exception):
    """Raised when the same step name is used twice in one task body."""

    def __init__(self, name: str):
        super().__init__(f"duplicate step name: {name}")
        self.name = name


class StepFailedError(Exception):
    """Raised when a step function raises. Wraps the underlying error."""

    def __init__(self, name: str, original: BaseException):
        super().__init__(f"step {name!r} failed: {original!r}")
        self.name = name
        self.original = original


def _iso_now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")


def _now_ms() -> int:
    return int(time.time() * 1000)


def _duration_to_ms(duration: Union[str, int, float]) -> int:
    if isinstance(duration, str):
        return parse_duration_ms(duration)
    if isinstance(duration, (int, float)):
        return int(duration)
    raise TypeError(f"duration must be str or number, got {type(duration).__name__}")


def _check_json_roundtrip(value: Any) -> str:
    """Validate that value survives JSON.dumps -> JSON.loads -> deep equal.

    Per spec section 7: catches Date, undefined, BigInt, Map, Set, functions,
    and circular refs. Returns the JSON text on success.
    """
    try:
        text = json.dumps(value, allow_nan=False)
    except (TypeError, ValueError) as e:
        raise SerializationError(str(e)) from None

    decoded = json.loads(text)
    if not _deep_equal(value, decoded):
        raise SerializationError(
            "value does not round-trip through JSON "
            "(possibly contains tuples, sets, dates, or other non-JSON types)"
        )
    return text


def _deep_equal(a: Any, b: Any) -> bool:
    """Strict structural equality across JSON-shaped values."""
    if type(a) is not type(b):
        # Allow int/float boundary if numerically equal? No — strict, since
        # JSON.dumps preserves int vs float in Python (json emits 1 vs 1.0).
        return False
    if isinstance(a, dict):
        if a.keys() != b.keys():
            return False
        return all(_deep_equal(a[k], b[k]) for k in a)
    if isinstance(a, list):
        if len(a) != len(b):
            return False
        return all(_deep_equal(x, y) for x, y in zip(a, b))
    return a == b


@dataclass
class _StepRow:
    name: str
    kind: str
    encoding: str
    result: Optional[bytes]
    status: str
    error: Optional[str]


def _decode_result(row: _StepRow) -> Any:
    if row.status != "done":
        raise RuntimeError(
            f"internal: step {row.name!r} not done (status={row.status})"
        )
    enc = row.encoding
    raw = row.result
    if raw is None:
        return None
    # v1.1: only json (structured + sleep wake-times) and bytes (raw payloads).
    if enc == "json":
        text = raw.decode("utf-8") if isinstance(raw, bytes) else raw
        return json.loads(text)
    if enc == "bytes":
        return raw if isinstance(raw, bytes) else raw.encode("utf-8")
    raise RuntimeError(f"unknown encoding: {enc!r}")


class _Task:
    """Per-invocation task state: open connection, cache, seen-name set."""

    def __init__(self, conn: sqlite3.Connection):
        self.conn = conn
        # cache: name -> _StepRow
        self.cache: Dict[str, _StepRow] = {}
        # Per-run set of step names already used; duplicates raise.
        self.seen: Set[str] = set()
        self._load_cache()

    def _load_cache(self) -> None:
        cur = self.conn.execute(
            "SELECT name, kind, encoding, result, status, error "
            "FROM mundane_steps ORDER BY id"
        )
        for row in cur:
            sr = _StepRow(*row)
            self.cache[sr.name] = sr

    def _check_seen(self, name: str) -> None:
        if name in self.seen:
            raise DuplicateStepError(name)
        self.seen.add(name)

    def _ensure_pending_row(self, name: str, kind: str, encoding: str) -> _StepRow:
        """Insert a pending row if not present; return the (possibly fresh) row.

        A leftover pending/failed row (never 'done' on this path) is reset to
        pending so the on-disk state reflects the retry, not a stale failure.
        """
        existing = self.cache.get(name)
        if existing is not None:
            self.conn.execute(
                "UPDATE mundane_steps "
                "SET kind=?, encoding=?, status='pending', result=NULL, error=NULL, finished_at=NULL "
                "WHERE name=?",
                (kind, encoding, name),
            )
            self.conn.commit()
            existing.kind = kind
            existing.encoding = encoding
            existing.status = "pending"
            existing.result = None
            existing.error = None
            return existing
        self.conn.execute(
            "INSERT INTO mundane_steps "
            "(name, kind, encoding, result, status, started_at) "
            "VALUES (?, ?, ?, NULL, 'pending', ?)",
            (name, kind, encoding, _iso_now()),
        )
        self.conn.commit()
        sr = _StepRow(name, kind, encoding, None, "pending", None)
        self.cache[name] = sr
        return sr

    def _commit_done(self, name: str, encoding: str, result: Any) -> None:
        """Mark a step done with the given (already-encoded) result bytes/text."""
        finished = _iso_now()
        self.conn.execute(
            "UPDATE mundane_steps "
            "SET status = 'done', encoding = ?, result = ?, finished_at = ?, error = NULL "
            "WHERE name = ?",
            (encoding, result, finished, name),
        )
        self.conn.commit()
        # Refresh cache
        row = self.cache[name]
        row.status = "done"
        row.encoding = encoding
        row.result = result if isinstance(result, (bytes, bytearray)) else (
            result.encode("utf-8") if isinstance(result, str) else result
        )

    def _commit_failed(self, name: str, error: str) -> None:
        finished = _iso_now()
        self.conn.execute(
            "UPDATE mundane_steps "
            "SET status = 'failed', error = ?, finished_at = ? "
            "WHERE name = ?",
            (error, finished, name),
        )
        self.conn.commit()
        row = self.cache.get(name)
        if row is not None:
            row.status = "failed"
            row.error = error


class Context:
    """The object passed to a workflow body. Provides step() and sleep()."""

    def __init__(self, task: _Task):
        self._task = task

    def _step_begin(self, name: str) -> tuple[bool, Any]:
        """Return (hit, value) on a cached 'done' row; else ensure a pending row
        and return (False, None). The caller runs fn and calls _step_commit."""
        validate_name(name)
        self._task._check_seen(name)
        row = self._task.cache.get(name)
        if row is not None and row.status == "done":
            return True, _decode_result(row)
        self._task._ensure_pending_row(name, "step", "json")
        return False, None

    def _step_commit(self, name: str, value: Any) -> Any:
        text = _check_json_roundtrip(value)
        self._task._commit_done(name, "json", text)
        # Return the round-tripped value so first run and resume agree exactly.
        return json.loads(text)

    def _resolve_wake_at(self, name: str, duration: Union[str, int, float]) -> int:
        """Wake-time (epoch ms) for a sleep: cached on resume, else computed and
        written. On resume the duration arg is ignored (SPEC §6)."""
        validate_name(name)
        self._task._check_seen(name)
        row = self._task.cache.get(name)
        if row is not None and row.status == "done":
            return int(_decode_result(row))
        wake_at = _now_ms() + _duration_to_ms(duration)
        self._task._ensure_pending_row(name, "sleep", "json")
        self._task._commit_done(name, "json", str(wake_at))
        return wake_at

    def step(self, name: str, fn: Callable[[], Any]) -> Any:
        hit, value = self._step_begin(name)
        if hit:
            return value
        try:
            result = fn()
        except Exception as e:
            self._task._commit_failed(name, repr(e))
            raise StepFailedError(name, e) from e
        return self._step_commit(name, result)

    def sleep(self, name: str, duration: Union[str, int, float]) -> None:
        wake_at = self._resolve_wake_at(name, duration)
        remaining = wake_at - _now_ms()
        if remaining > 0:
            time.sleep(remaining / 1000.0)

    # ---- async variants ----

    async def astep(self, name: str, fn: Callable[[], Awaitable[Any]]) -> Any:
        hit, value = self._step_begin(name)
        if hit:
            return value
        try:
            result = await fn()
        except Exception as e:
            self._task._commit_failed(name, repr(e))
            raise StepFailedError(name, e) from e
        return self._step_commit(name, result)

    async def asleep(self, name: str, duration: Union[str, int, float]) -> None:
        wake_at = self._resolve_wake_at(name, duration)
        remaining = wake_at - _now_ms()
        if remaining > 0:
            await asyncio.sleep(remaining / 1000.0)


def _open_task(path: str) -> tuple[FileLock, sqlite3.Connection, _Task]:
    """Acquire lock, open DB, bootstrap schema, build cache. Caller owns cleanup."""
    lock = FileLock(path)
    try:
        lock.acquire()
    except BlockingIOError:
        raise LockedError(
            f"{path}: locked by another process"
        ) from None

    conn: Optional[sqlite3.Connection] = None
    try:
        # vfs=unix-none disables SQLite's own file locking. Mundane already
        # holds an exclusive flock(2) on the file for the whole run, so
        # SQLite's internal locking is redundant — and on macOS, where
        # SQLite's POSIX locks share state with our flock on the same vnode,
        # leaving it on would deadlock against ourselves ("database is
        # locked"). The flock above is the sole writer-lock authority.
        # quote() with safe="/" preserves path separators but escapes ? & %
        # so paths with those characters don't break URI parsing.
        encoded = urllib.parse.quote(path, safe="/")
        conn = sqlite3.connect(
            f"file:{encoded}?vfs=unix-none", uri=True, isolation_level=None
        )
        conn.execute("PRAGMA journal_mode = DELETE")

        # Pre-check: if mundane_meta already exists with a non-1 schema_version,
        # bail before running CREATE INDEX (which references columns we don't
        # promise on other schema versions).
        existing = conn.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name='mundane_meta'"
        ).fetchone()
        if existing:
            row = conn.execute(
                "SELECT value FROM mundane_meta WHERE key='schema_version'"
            ).fetchone()
            if row and row[0] != SCHEMA_VERSION:
                raise SchemaError(
                    f"{path}: schema_version is {row[0]!r}, expected {SCHEMA_VERSION!r}"
                )

        # Bootstrap inside an IMMEDIATE transaction (idempotent).
        conn.execute("BEGIN IMMEDIATE")
        try:
            statements = [s.strip() for s in BOOTSTRAP_SQL.split(";") if s.strip()]
            for stmt in statements:
                conn.execute(stmt)
            for key, value in (
                ("schema_version", SCHEMA_VERSION),
                ("task_id", str(uuid.uuid4())),
                ("created_at", _iso_now()),
            ):
                conn.execute(
                    "INSERT OR IGNORE INTO mundane_meta (key, value) VALUES (?, ?)",
                    (key, value),
                )
            conn.execute("COMMIT")
        except Exception:
            conn.execute("ROLLBACK")
            raise

        # Final schema-version check (covers the fresh-bootstrap path).
        row = conn.execute(
            "SELECT value FROM mundane_meta WHERE key = 'schema_version'"
        ).fetchone()
        if not row or row[0] != SCHEMA_VERSION:
            raise SchemaError(
                f"{path}: schema_version is {row[0] if row else None!r}, expected {SCHEMA_VERSION!r}"
            )

        task = _Task(conn)
        return lock, conn, task
    except Exception:
        if conn is not None:
            conn.close()
        lock.release()
        raise


def run(path: str, fn: Callable[[Context], Any]) -> Any:
    """Run a workflow body against the SQLite file at `path`.

    On cache miss, fn-passed step closures execute; on cache hit they return
    the cached value without re-executing. Returns fn's return value.

    Raises LockedError if another process holds the file lock.
    """
    lock, conn, task = _open_task(path)
    try:
        ctx = Context(task)
        return fn(ctx)
    finally:
        try:
            conn.close()
        finally:
            lock.release()


async def arun(path: str, fn: Callable[[Context], Awaitable[Any]]) -> Any:
    """Async variant of run(). The body can use ctx.astep / ctx.asleep."""
    lock, conn, task = _open_task(path)
    try:
        ctx = Context(task)
        return await fn(ctx)
    finally:
        try:
            conn.close()
        finally:
            lock.release()
