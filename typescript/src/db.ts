/**
 * Thin promise wrapper over node-sqlite3.
 *
 * The database is opened with the `unix-none` VFS, which disables SQLite's own
 * file locking. We don't need it: mundane already holds an exclusive
 * `flock(LOCK_EX)` on the file for the whole run (see lock.ts), so single-writer
 * is guaranteed externally and visible across all runtimes. Leaving SQLite's
 * locking on would be redundant — and on platforms where SQLite locks via
 * `flock(2)` (e.g. macOS), it would collide with our own flock on the same file
 * and deadlock ("database is locked"). `unix-none` sidesteps that entirely.
 */

import { pathToFileURL } from "node:url";
import * as sqlite3 from "sqlite3";

export interface Db {
  run(sql: string, params?: unknown[]): Promise<void>;
  get<T>(sql: string, params?: unknown[]): Promise<T | undefined>;
  all<T>(sql: string, params?: unknown[]): Promise<T[]>;
  exec(sql: string): Promise<void>;
  close(): Promise<void>;
}

export function openDb(path: string, opts: { readonly?: boolean } = {}): Promise<Db> {
  return new Promise((resolve, reject) => {
    const url = pathToFileURL(path);
    url.searchParams.set("vfs", "unix-none");
    const mode =
      (opts.readonly ? sqlite3.OPEN_READONLY : sqlite3.OPEN_READWRITE | sqlite3.OPEN_CREATE) |
      sqlite3.OPEN_URI;
    const raw = new sqlite3.Database(url.href, mode, (err) => {
      if (err) reject(err);
      else resolve(wrap(raw));
    });
  });
}

function wrap(raw: sqlite3.Database): Db {
  return {
    run: (sql, params = []) =>
      new Promise((resolve, reject) =>
        raw.run(sql, params, (err) => (err ? reject(err) : resolve())),
      ),
    get: <T>(sql: string, params: unknown[] = []) =>
      new Promise<T | undefined>((resolve, reject) =>
        raw.get(sql, params, (err, row) => (err ? reject(err) : resolve(row as T | undefined))),
      ),
    all: <T>(sql: string, params: unknown[] = []) =>
      new Promise<T[]>((resolve, reject) =>
        raw.all(sql, params, (err, rows) => (err ? reject(err) : resolve(rows as T[]))),
      ),
    exec: (sql) =>
      new Promise((resolve, reject) => raw.exec(sql, (err) => (err ? reject(err) : resolve()))),
    close: () =>
      new Promise((resolve, reject) => raw.close((err) => (err ? reject(err) : resolve()))),
  };
}
