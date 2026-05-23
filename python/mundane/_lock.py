"""Exclusive file locking via flock(2).

Per spec section 3: the runtime acquires flock(LOCK_EX | LOCK_NB) on the
SQLite file's fd. On failure, fail-fast with exit code 75 / LockedError.

Note: flock(2) and POSIX record locks (used internally by SQLite) live in
separate lock-spaces on Linux, so an flock on the DB file does not interfere
with SQLite's own locking.
"""

import contextlib
import fcntl
import os
from typing import Optional


class FileLock:
    """An exclusive non-blocking flock held for the lifetime of the object."""

    def __init__(self, path: str):
        self.path = path
        self._fd: Optional[int] = None

    def acquire(self) -> None:
        # Open for read+write, creating if missing. SQLite will also open it
        # later; that's fine.
        fd = os.open(self.path, os.O_RDWR | os.O_CREAT, 0o644)
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            os.close(fd)
            raise
        self._fd = fd

    def release(self) -> None:
        if self._fd is not None:
            with contextlib.suppress(OSError):
                fcntl.flock(self._fd, fcntl.LOCK_UN)
            with contextlib.suppress(OSError):
                os.close(self._fd)
            self._fd = None

    def __enter__(self) -> "FileLock":
        self.acquire()
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        self.release()
