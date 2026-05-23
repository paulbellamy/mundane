"""Read-only inspection helpers (don't take the write lock)."""

from __future__ import annotations

import base64
import json
import sqlite3
from typing import Any, Dict, List

from ._schema import SCHEMA_VERSION


def _open_ro(path: str) -> sqlite3.Connection:
    return sqlite3.connect(f"file:{path}?mode=ro", uri=True)


def _check_schema(conn: sqlite3.Connection) -> None:
    row = conn.execute(
        "SELECT value FROM mundane_meta WHERE key = 'schema_version'"
    ).fetchone()
    if not row or row[0] != SCHEMA_VERSION:
        raise RuntimeError(
            f"unsupported schema version: {row[0] if row else None!r}"
        )


def status(path: str) -> Dict[str, Any]:
    """Return a summary dict for the task at `path`."""
    conn = _open_ro(path)
    try:
        _check_schema(conn)
        meta = dict(conn.execute("SELECT key, value FROM mundane_meta").fetchall())
        counts = conn.execute(
            "SELECT status, COUNT(*) FROM mundane_steps GROUP BY status"
        ).fetchall()
        by_status = {s: c for s, c in counts}
        total = sum(by_status.values())
        return {
            "path": path,
            "task_id": meta.get("task_id"),
            "created_at": meta.get("created_at"),
            "total_steps": total,
            "done": by_status.get("done", 0),
            "pending": by_status.get("pending", 0),
            "failed": by_status.get("failed", 0),
        }
    finally:
        conn.close()


def steps(path: str) -> List[Dict[str, Any]]:
    """Return a list of step rows ordered by id."""
    conn = _open_ro(path)
    try:
        _check_schema(conn)
        rows = conn.execute(
            "SELECT id, name, kind, encoding, status, started_at, finished_at, error "
            "FROM mundane_steps ORDER BY id"
        ).fetchall()
        return [
            {
                "id": r[0], "name": r[1], "kind": r[2], "encoding": r[3],
                "status": r[4], "started_at": r[5], "finished_at": r[6],
                "error": r[7],
            }
            for r in rows
        ]
    finally:
        conn.close()


def get_result(path: str, name: str) -> Any:
    """Return the decoded cached result for the step named `name`."""
    conn = _open_ro(path)
    try:
        _check_schema(conn)
        row = conn.execute(
            "SELECT encoding, result, status FROM mundane_steps WHERE name = ?",
            (name,),
        ).fetchone()
        if row is None:
            raise KeyError(f"no step named {name!r}")
        enc, result, st = row
        if st != "done":
            raise RuntimeError(f"step {name!r} is {st}, not done")
        if result is None:
            return None
        # SQLite may give us bytes or str depending on storage; normalise.
        text = result.decode("utf-8") if isinstance(result, bytes) else result
        if enc == "json":
            return json.loads(text)
        if enc == "text":
            return text
        if enc == "b64":
            return base64.b64decode(text)
        if enc == "epoch":
            return int(text)
        raise RuntimeError(f"unknown encoding: {enc!r}")
    finally:
        conn.close()
