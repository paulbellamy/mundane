/**
 * Read-only inspection helpers. Do not take the write lock.
 */

import Database from "better-sqlite3";
import { SCHEMA_VERSION } from "./schema";
import { MundaneSchemaError } from "./errors";

function openRO(path: string): Database.Database {
  return new Database(path, { readonly: true, fileMustExist: true });
}

function checkSchema(db: Database.Database): void {
  const row = db
    .prepare("SELECT value FROM mundane_meta WHERE key='schema_version'")
    .get() as { value?: string } | undefined;
  if (!row || row.value !== SCHEMA_VERSION) {
    throw new MundaneSchemaError(
      `schema_version is ${JSON.stringify(row?.value)}, expected "${SCHEMA_VERSION}"`,
    );
  }
}

export interface Status {
  path: string;
  task_id: string | undefined;
  created_at: string | undefined;
  total_steps: number;
  done: number;
  pending: number;
  failed: number;
}

export function status(path: string): Status {
  const db = openRO(path);
  try {
    checkSchema(db);
    const meta = db.prepare("SELECT key, value FROM mundane_meta").all() as {
      key: string;
      value: string;
    }[];
    const byKey = new Map(meta.map((r) => [r.key, r.value]));
    const counts = db
      .prepare("SELECT status, COUNT(*) AS c FROM mundane_steps GROUP BY status")
      .all() as { status: string; c: number }[];
    const byStatus = new Map(counts.map((r) => [r.status, r.c]));
    const total = (byStatus.get("done") ?? 0) +
      (byStatus.get("pending") ?? 0) +
      (byStatus.get("failed") ?? 0);
    return {
      path,
      task_id: byKey.get("task_id"),
      created_at: byKey.get("created_at"),
      total_steps: total,
      done: byStatus.get("done") ?? 0,
      pending: byStatus.get("pending") ?? 0,
      failed: byStatus.get("failed") ?? 0,
    };
  } finally {
    db.close();
  }
}

export interface StepInfo {
  id: number;
  name: string;
  kind: string;
  encoding: string;
  status: string;
  started_at: string;
  finished_at: string | null;
  error: string | null;
}

export function steps(path: string): StepInfo[] {
  const db = openRO(path);
  try {
    checkSchema(db);
    return db
      .prepare(
        "SELECT id, name, kind, encoding, status, started_at, finished_at, error " +
          "FROM mundane_steps ORDER BY id",
      )
      .all() as StepInfo[];
  } finally {
    db.close();
  }
}

export function getResult(path: string, name: string): unknown {
  const db = openRO(path);
  try {
    checkSchema(db);
    const row = db
      .prepare(
        "SELECT encoding, result, status FROM mundane_steps WHERE name = ?",
      )
      .get(name) as
      | { encoding: string; result: Buffer | string | null; status: string }
      | undefined;
    if (!row) throw new Error(`no step named ${JSON.stringify(name)}`);
    if (row.status !== "done") {
      throw new Error(`step ${JSON.stringify(name)} is ${row.status}, not done`);
    }
    if (row.result === null) return null;
    const text =
      typeof row.result === "string" ? row.result : row.result.toString("utf8");
    switch (row.encoding) {
      case "json":
        return JSON.parse(text);
      case "text":
        return text;
      case "b64":
        return Buffer.from(text, "base64");
      case "epoch":
        return Number(text);
      default:
        throw new Error(`unknown encoding: ${row.encoding}`);
    }
  } finally {
    db.close();
  }
}
