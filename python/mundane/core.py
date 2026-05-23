"""Core implementation: run, ctx.step, ctx.sleep."""

from __future__ import annotations

import asyncio
import json
import sqlite3
import time
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
    id: int
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
    if isinstance(raw, bytes):
        if enc == "json":
            return json.loads(raw.decode("utf-8"))
        if enc == "text":
            return raw.decode("utf-8")
        if enc == "b64":
            import base64
            return base64.b64decode(raw)
        if enc == "epoch":
            return int(raw.decode("utf-8"))
    else:
        # sqlite3 returns str for TEXT-typed reads; we store TEXT often.
        s = raw
        if enc == "json":
            return json.loads(s)
        if enc == "text":
            return s
        if enc == "b64":
            import base64
            return base64.b64decode(s)
        if enc == "epoch":
            return int(s)
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
            "SELECT id, name, kind, encoding, result, status, error "
            "FROM mundane_steps ORDER BY id"
        )
        for row in cur:
            sr = _StepRow(
                id=row[0],
                name=row[1],
                kind=row[2],
                encoding=row[3],
                result=row[4],
                status=row[5],
                error=row[6],
            )
            self.cache[sr.name] = sr

    def _check_seen(self, name: str) -> None:
        if name in self.seen:
            raise DuplicateStepError(name)
        self.seen.add(name)

    def _ensure_pending_row(self, name: str, kind: str, encoding: str) -> _StepRow:
        """Insert a pending row if not present; return the (possibly fresh) row."""
        existing = self.cache.get(name)
        if existing is not None:
            return existing
        now = _iso_now()
        self.conn.execute(
            "INSERT INTO mundane_steps "
            "(name, kind, encoding, result, status, started_at) "
            "VALUES (?, ?, ?, NULL, 'pending', ?)",
            (name, kind, encoding, now),
        )
        self.conn.commit()
        # Re-load row to get id
        cur = self.conn.execute(
            "SELECT id, name, kind, encoding, result, status, error "
            "FROM mundane_steps WHERE name = ?",
            (name,),
        )
        row = cur.fetchone()
        sr = _StepRow(
            id=row[0], name=row[1], kind=row[2], encoding=row[3],
            result=row[4], status=row[5], error=row[6],
        )
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


class Context:
    """The object passed to a workflow body. Provides step() and sleep()."""

    def __init__(self, task: _Task):
        self._task = task

    def step(self, name: str, fn: Callable[[], Any]) -> Any:
        validate_name(name)
        self._task._check_seen(name)
        resolved = name
        row = self._task.cache.get(resolved)
        if row is not None and row.status == "done":
            return _decode_result(row)
        # pending or absent: ensure row, run fn, commit
        self._task._ensure_pending_row(resolved, "step", "json")
        try:
            value = fn()
        except BaseException as e:
            self._task._commit_failed(resolved, repr(e))
            raise StepFailedError(resolved, e) from e
        text = _check_json_roundtrip(value)
        self._task._commit_done(resolved, "json", text)
        return value

    def sleep(self, name: str, duration: Union[str, int, float]) -> None:
        validate_name(name)
        self._task._check_seen(name)
        resolved = name
        if isinstance(duration, str):
            ms = parse_duration_ms(duration)
        elif isinstance(duration, (int, float)):
            ms = int(duration)
        else:
            raise TypeError(
                f"duration must be str or number, got {type(duration).__name__}"
            )

        row = self._task.cache.get(resolved)
        if row is not None and row.status == "done":
            wake_at = _decode_result(row)
            self._sleep_remaining(int(wake_at))
            return
        # absent / pending: compute wake_at, write epoch row, sleep remaining.
        wake_at = _now_ms() + ms
        self._task._ensure_pending_row(resolved, "sleep", "epoch")
        self._task._commit_done(resolved, "epoch", str(wake_at))
        self._sleep_remaining(wake_at)

    def _sleep_remaining(self, wake_at_ms: int) -> None:
        now = _now_ms()
        remaining = wake_at_ms - now
        if remaining > 0:
            time.sleep(remaining / 1000.0)

    # ---- async variants ----

    async def astep(self, name: str, fn: Callable[[], Awaitable[Any]]) -> Any:
        validate_name(name)
        self._task._check_seen(name)
        resolved = name
        row = self._task.cache.get(resolved)
        if row is not None and row.status == "done":
            return _decode_result(row)
        self._task._ensure_pending_row(resolved, "step", "json")
        try:
            value = await fn()
        except BaseException as e:
            self._task._commit_failed(resolved, repr(e))
            raise StepFailedError(resolved, e) from e
        text = _check_json_roundtrip(value)
        self._task._commit_done(resolved, "json", text)
        return value

    async def asleep(self, name: str, duration: Union[str, int, float]) -> None:
        validate_name(name)
        self._task._check_seen(name)
        resolved = name
        if isinstance(duration, str):
            ms = parse_duration_ms(duration)
        elif isinstance(duration, (int, float)):
            ms = int(duration)
        else:
            raise TypeError(
                f"duration must be str or number, got {type(duration).__name__}"
            )

        row = self._task.cache.get(resolved)
        if row is not None and row.status == "done":
            wake_at = int(_decode_result(row))
        else:
            wake_at = _now_ms() + ms
            self._task._ensure_pending_row(resolved, "sleep", "epoch")
            self._task._commit_done(resolved, "epoch", str(wake_at))
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

    try:
        conn = sqlite3.connect(path, isolation_level=None)  # autocommit
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
            now_iso = _iso_now()
            new_uuid = str(uuid.uuid4())
            conn.execute(
                "INSERT OR IGNORE INTO mundane_meta (key, value) VALUES ('schema_version', ?)",
                (SCHEMA_VERSION,),
            )
            conn.execute(
                "INSERT OR IGNORE INTO mundane_meta (key, value) VALUES ('task_id', ?)",
                (new_uuid,),
            )
            conn.execute(
                "INSERT OR IGNORE INTO mundane_meta (key, value) VALUES ('created_at', ?)",
                (now_iso,),
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
        conn.close()
        lock.release()


async def arun(path: str, fn: Callable[[Context], Awaitable[Any]]) -> Any:
    """Async variant of run(). The body can use ctx.astep / ctx.asleep."""
    lock, conn, task = _open_task(path)
    try:
        ctx = Context(task)
        return await fn(ctx)
    finally:
        conn.close()
        lock.release()
