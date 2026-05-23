"""Schema definition shared across mundane runtimes.

The schema is pinned to v1. Opening a file whose meta.schema_version is
not '1' is a hard error.
"""

import re

SCHEMA_VERSION = "1"

# Name regex per spec section 5: ^[A-Za-z0-9][A-Za-z0-9._-]*$
NAME_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._\-]*$")


BOOTSTRAP_SQL = """
PRAGMA journal_mode = DELETE;

CREATE TABLE IF NOT EXISTS mundane_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS mundane_steps (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT NOT NULL,
  kind         TEXT NOT NULL,
  encoding     TEXT NOT NULL,
  result       BLOB,
  status       TEXT NOT NULL,
  error        TEXT,
  started_at   TEXT NOT NULL,
  finished_at  TEXT,
  UNIQUE(name)
);

CREATE INDEX IF NOT EXISTS mundane_steps_status ON mundane_steps(status);
"""


def validate_name(name: str) -> None:
    if not isinstance(name, str) or not NAME_RE.match(name):
        raise ValueError(
            f"invalid step name {name!r}: must match {NAME_RE.pattern}"
        )
