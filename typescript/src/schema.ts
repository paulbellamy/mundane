/**
 * Schema definition shared across runtimes.
 *
 * The schema is pinned to v1. Opening a file whose meta.schema_version is
 * not "1" is a hard error.
 */

import { randomUUID } from "node:crypto";
import type Database from "better-sqlite3";
import { MundaneSchemaError } from "./errors";

export const SCHEMA_VERSION = "1";

export const CREATE_META = `
CREATE TABLE IF NOT EXISTS mundane_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
)
`;

export const CREATE_STEPS = `
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
)
`;

export const CREATE_INDEX = `
CREATE INDEX IF NOT EXISTS mundane_steps_status ON mundane_steps(status)
`;

export function bootstrap(db: Database.Database): void {
  db.pragma("journal_mode = DELETE");

  // Pre-check: if mundane_meta exists with wrong schema_version, bail before
  // running CREATE INDEX (which assumes columns we only promise at v1).
  const existing = db
    .prepare("SELECT name FROM sqlite_master WHERE type='table' AND name='mundane_meta'")
    .get();
  if (existing) {
    const row = db.prepare("SELECT value FROM mundane_meta WHERE key='schema_version'").get() as
      | { value?: string }
      | undefined;
    if (row && row.value !== SCHEMA_VERSION) {
      throw new MundaneSchemaError(
        `schema_version is ${JSON.stringify(row.value)}, expected "${SCHEMA_VERSION}"`,
      );
    }
  }

  db.exec("BEGIN IMMEDIATE");
  try {
    db.exec(CREATE_META);
    db.exec(CREATE_STEPS);
    db.exec(CREATE_INDEX);
    db.prepare("INSERT OR IGNORE INTO mundane_meta (key, value) VALUES ('schema_version', ?)").run(
      SCHEMA_VERSION,
    );
    db.prepare("INSERT OR IGNORE INTO mundane_meta (key, value) VALUES ('task_id', ?)").run(
      randomUUID(),
    );
    db.prepare("INSERT OR IGNORE INTO mundane_meta (key, value) VALUES ('created_at', ?)").run(
      new Date().toISOString(),
    );
    db.exec("COMMIT");
  } catch (e) {
    try {
      db.exec("ROLLBACK");
    } catch {}
    throw e;
  }

  // Final check.
  const row = db.prepare("SELECT value FROM mundane_meta WHERE key='schema_version'").get() as
    | { value?: string }
    | undefined;
  if (!row || row.value !== SCHEMA_VERSION) {
    throw new MundaneSchemaError(
      `schema_version is ${JSON.stringify(row?.value)}, expected "${SCHEMA_VERSION}"`,
    );
  }
}
