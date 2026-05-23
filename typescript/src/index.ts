/**
 * @mundane/core — tiny durable-execution library.
 *
 * Public API:
 *   run(path, async (ctx) => { ... })
 *   ctx.step(name, async () => json-able)
 *   ctx.sleep(name, "5m" | 5_000)
 *
 * See ../../../SPEC.md (project root) for the full contract.
 */

import Database from "better-sqlite3";
import { parseDurationMs } from "./duration";
import {
  MundaneDuplicateStepError,
  MundaneLockedError,
  MundaneSchemaError,
  MundaneSerializationError,
  MundaneStepFailedError,
} from "./errors";
import { type AcquiredLock, acquireLock } from "./lock";
import { validateName } from "./names";
import { bootstrap } from "./schema";

export {
  MundaneDuplicateStepError,
  MundaneLockedError,
  MundaneSchemaError,
  MundaneSerializationError,
  MundaneStepFailedError,
};

export type Json = null | boolean | number | string | Json[] | { [k: string]: Json };

interface StepRow {
  id: number;
  name: string;
  kind: "step" | "sleep";
  encoding: "json" | "bytes";
  result: Buffer | string | null;
  status: "pending" | "done" | "failed";
  error: string | null;
}

export interface Context {
  step<T = unknown>(name: string, fn: () => Promise<T> | T): Promise<T>;
  sleep(name: string, duration: string | number): Promise<void>;
}

function checkJsonRoundtrip(value: unknown): string {
  let text: string;
  try {
    text = JSON.stringify(value);
  } catch (e) {
    throw new MundaneSerializationError(`value is not JSON-serializable: ${(e as Error).message}`);
  }
  if (text === undefined) {
    // JSON.stringify returns undefined for top-level undefined/function/symbol
    throw new MundaneSerializationError(
      "value is not JSON-serializable (undefined / function / symbol at top level)",
    );
  }
  const decoded = JSON.parse(text);
  const mismatch = deepDiff(value, decoded, "");
  if (mismatch !== null) {
    throw new MundaneSerializationError(
      `value does not round-trip through JSON at ${JSON.stringify(mismatch)}`,
      mismatch,
    );
  }
  return text;
}

function deepDiff(a: unknown, b: unknown, path: string): string | null {
  if (a === b) return null;
  if (a === null || b === null || typeof a !== "object" || typeof b !== "object") {
    // Special-case: NaN/Infinity get encoded as null by JSON, mismatch.
    return path || "(root)";
  }
  if (Array.isArray(a) !== Array.isArray(b)) return path || "(root)";
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) return path || "(root)";
    for (let i = 0; i < a.length; i++) {
      const d = deepDiff(a[i], b[i], `${path}[${i}]`);
      if (d) return d;
    }
    return null;
  }
  // Plain objects: keys must match exactly (catches `undefined` values
  // disappearing, Date/Map/Set turning into "{}", and class instances).
  const aProto = Object.getPrototypeOf(a as object);
  if (aProto !== Object.prototype && aProto !== null) {
    return path || "(root)";
  }
  const aKeys = Object.keys(a as object).sort();
  const bKeys = Object.keys(b as object).sort();
  if (aKeys.length !== bKeys.length) return path || "(root)";
  for (let i = 0; i < aKeys.length; i++) {
    if (aKeys[i] !== bKeys[i]) return path || "(root)";
    const d = deepDiff(
      (a as Record<string, unknown>)[aKeys[i]],
      (b as Record<string, unknown>)[aKeys[i]],
      `${path}.${aKeys[i]}`,
    );
    if (d) return d;
  }
  return null;
}

function decodeResult(row: StepRow): unknown {
  if (row.result === null) return null;
  // better-sqlite3 returns BLOB as Buffer, TEXT as string.
  switch (row.encoding) {
    case "json": {
      const text = typeof row.result === "string" ? row.result : row.result.toString("utf8");
      return JSON.parse(text);
    }
    case "bytes":
      return typeof row.result === "string" ? Buffer.from(row.result, "utf8") : row.result;
    default:
      throw new Error(`unknown encoding: ${row.encoding}`);
  }
}

class TaskState {
  readonly db: Database.Database;
  readonly cache = new Map<string, StepRow>();
  readonly seen = new Set<string>();

  constructor(db: Database.Database) {
    this.db = db;
    this.loadCache();
  }

  checkSeen(name: string): void {
    if (this.seen.has(name)) {
      throw new MundaneDuplicateStepError(name);
    }
    this.seen.add(name);
  }

  private loadCache(): void {
    const rows = this.db
      .prepare(
        "SELECT id, name, kind, encoding, result, status, error " +
          "FROM mundane_steps ORDER BY id",
      )
      .all() as StepRow[];
    for (const row of rows) this.cache.set(row.name, row);
  }

  ensurePendingRow(name: string, kind: "step" | "sleep", encoding: StepRow["encoding"]): StepRow {
    const existing = this.cache.get(name);
    if (existing) {
      // Re-running a leftover pending/failed row (never 'done' on this path):
      // reset it to pending so the on-disk state reflects the retry (SPEC §2).
      this.db
        .prepare(
          "UPDATE mundane_steps SET kind=?, encoding=?, status='pending', result=NULL, error=NULL, finished_at=NULL " +
            "WHERE name=?",
        )
        .run(kind, encoding, name);
      existing.kind = kind;
      existing.encoding = encoding;
      existing.status = "pending";
      existing.result = null;
      existing.error = null;
      return existing;
    }
    const now = new Date().toISOString();
    this.db
      .prepare(
        "INSERT INTO mundane_steps (name, kind, encoding, result, status, started_at) " +
          "VALUES (?, ?, ?, NULL, 'pending', ?)",
      )
      .run(name, kind, encoding, now);
    const row = this.db
      .prepare(
        "SELECT id, name, kind, encoding, result, status, error " +
          "FROM mundane_steps WHERE name = ?",
      )
      .get(name) as StepRow;
    this.cache.set(name, row);
    return row;
  }

  commitDone(name: string, encoding: StepRow["encoding"], result: string | Buffer): void {
    const finished = new Date().toISOString();
    this.db
      .prepare(
        "UPDATE mundane_steps SET status='done', encoding=?, result=?, finished_at=?, error=NULL " +
          "WHERE name=?",
      )
      .run(encoding, result, finished, name);
    const row = this.cache.get(name)!;
    row.status = "done";
    row.encoding = encoding;
    row.result = result;
  }

  commitFailed(name: string, errMsg: string): void {
    const finished = new Date().toISOString();
    this.db
      .prepare("UPDATE mundane_steps SET status='failed', error=?, finished_at=? WHERE name=?")
      .run(errMsg, finished, name);
    const row = this.cache.get(name)!;
    row.status = "failed";
    row.error = errMsg;
  }
}

class ContextImpl implements Context {
  constructor(private readonly task: TaskState) {}

  async step<T>(name: string, fn: () => Promise<T> | T): Promise<T> {
    validateName(name);
    this.task.checkSeen(name);
    const cached = this.task.cache.get(name);
    if (cached && cached.status === "done") {
      return decodeResult(cached) as T;
    }
    this.task.ensurePendingRow(name, "step", "json");
    let value: T;
    try {
      value = await fn();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      this.task.commitFailed(name, msg);
      throw new MundaneStepFailedError(name, e);
    }
    const text = checkJsonRoundtrip(value);
    this.task.commitDone(name, "json", text);
    // Return the round-tripped value so first run and resume agree exactly.
    return JSON.parse(text) as T;
  }

  async sleep(name: string, duration: string | number): Promise<void> {
    validateName(name);
    this.task.checkSeen(name);
    const cached = this.task.cache.get(name);
    let wakeAt: number;
    if (cached && cached.status === "done") {
      // Resume: the duration arg is ignored (SPEC §6), so don't parse it — a
      // now-invalid duration string must not fail an otherwise-no-op resume.
      wakeAt = Number(decodeResult(cached));
    } else {
      wakeAt = Date.now() + parseDurationMs(duration);
      this.task.ensurePendingRow(name, "sleep", "json");
      this.task.commitDone(name, "json", String(wakeAt));
    }
    const remaining = wakeAt - Date.now();
    if (remaining > 0) {
      await new Promise<void>((resolve) => setTimeout(resolve, remaining));
    }
  }
}

export async function run<T>(path: string, fn: (ctx: Context) => Promise<T> | T): Promise<T> {
  let lock: AcquiredLock;
  try {
    lock = await acquireLock(path);
  } catch (e) {
    if (e instanceof MundaneLockedError) throw e;
    throw e;
  }
  let db: Database.Database | null = null;
  try {
    db = new Database(path);
    bootstrap(db);
    const task = new TaskState(db);
    const ctx = new ContextImpl(task);
    return await fn(ctx);
  } finally {
    if (db) {
      try {
        db.close();
      } catch {
        // ignore
      }
    }
    await lock.release();
  }
}
