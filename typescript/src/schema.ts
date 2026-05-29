/**
 * Schema definition shared across runtimes.
 *
 * The schema is pinned to v1. Opening a file whose meta.schema_version is
 * not "1" is a hard error.
 */

import { randomUUID } from "node:crypto";
import type { Db } from "./db";
import { SchemaError } from "./errors";

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

export async function bootstrap(db: Db): Promise<void> {
  await db.exec("PRAGMA journal_mode = DELETE");

  // Pre-check: if mundane_meta exists with wrong schema_version, bail before
  // running CREATE INDEX (which assumes columns we only promise at v1).
  const existing = await db.get(
    "SELECT name FROM sqlite_master WHERE type='table' AND name='mundane_meta'",
  );
  if (existing) {
    const row = await db.get<{ value?: string }>(
      "SELECT value FROM mundane_meta WHERE key='schema_version'",
    );
    if (row && row.value !== SCHEMA_VERSION) {
      throw new SchemaError(
        `schema_version is ${JSON.stringify(row.value)}, expected "${SCHEMA_VERSION}"`,
      );
    }
  }

  await db.exec("BEGIN IMMEDIATE");
  try {
    await db.exec(CREATE_META);
    await db.exec(CREATE_STEPS);
    await db.exec(CREATE_INDEX);
    for (const [key, value] of [
      ["schema_version", SCHEMA_VERSION],
      ["task_id", randomUUID()],
      ["created_at", new Date().toISOString()],
    ]) {
      await db.run("INSERT OR IGNORE INTO mundane_meta (key, value) VALUES (?, ?)", [key, value]);
    }
    await db.exec("COMMIT");
  } catch (e) {
    try {
      await db.exec("ROLLBACK");
    } catch {}
    throw e;
  }

  // Final check.
  const row = await db.get<{ value?: string }>(
    "SELECT value FROM mundane_meta WHERE key='schema_version'",
  );
  if (!row || row.value !== SCHEMA_VERSION) {
    throw new SchemaError(
      `schema_version is ${JSON.stringify(row?.value)}, expected "${SCHEMA_VERSION}"`,
    );
  }
}
