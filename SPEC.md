# Mundane — Spec v1.1

A tiny durable-execution library. One workflow run is one SQLite file. Crash,
re-invoke, resume. No daemon, no broker, no scheduler — bring your own cron.

Four runtimes ship from the same repo: a single `mundane` CLI binary (Go;
also exposes a Go SDK), a TypeScript package (`mundane-sdk`), and a
Python package (`mundane-sdk`). The shell UX is the same binary used via
`eval "$(mundane init <db>)"`. All four read and write the same on-disk
format and interoperate row-for-row.

**v1.1 changes from v1**:
- §5 auto-disambig (`#N`) removed. Duplicate step names in one task body
  are a hard error.
- §6 bash interface is now `eval "$(mundane init <db>)"` + `step`/`nap`
  helpers in the caller's shell (formerly `mundane run <db> -- <body>`).
- §2 `encoding` collapsed from {json, text, b64, epoch} to {json, bytes};
  the `--b64` flag is gone (the shell step is binary-safe via raw BLOB).
- Inspection (`status`/`steps`/`get`) is CLI-only — the SDKs no longer
  wrap it. The `mundane` binary is the single inspection surface.
- §10 Go interface added (4th peer runtime).

---

## 1. Model

A **task** is a single SQLite file at a caller-chosen path. The file *is* the
task; the path *is* the identity. If two invocations open the same file they
resume the same task. If they open different files they are different tasks.
Mundane does not canonicalize, resolve, or otherwise interpret the path —
opening "the same file twice" is the caller's responsibility.

A task is a sequence of **steps**. A step has a caller-supplied name (unique
within one task body, §5) and produces a JSON result. Step results are
durable: once a step has committed a result, every subsequent attempt of
the task returns that result without re-executing the body.

A task **completes** when the runtime body returns normally. A task **fails**
when the body throws / exits nonzero with no in-flight retry strategy left
(the runtime itself has none — retry is "invoke again"). A task is **in
progress** in all other cases, including "the process crashed mid-step".

There is no scheduler. There is no "task is running" state machine separate
from the file lock (§4). A task that is not currently held by a live process
is, by definition, eligible to resume on the next invocation.

## 2. Storage

One file per task. SQLite, `journal_mode=DELETE` (no `-wal`/`-shm` siblings —
the file is the whole artifact, safe to `cp`, `mv`, `rm`, ship in a tarball).

Schema, pinned to v1. Opening a file whose `meta.schema_version` does not
equal `1` is a hard error; mundane v1 will not migrate, downgrade, or guess.

```sql
CREATE TABLE mundane_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
-- rows: ('schema_version','1'), ('task_id','<uuid>'), ('created_at','<iso>')

CREATE TABLE mundane_steps (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT NOT NULL,            -- caller-supplied; unique per task
  kind         TEXT NOT NULL,            -- 'step' | 'sleep'
  encoding     TEXT NOT NULL,            -- 'json' | 'bytes'
  result       BLOB,                     -- payload per encoding; NULL while pending
  status       TEXT NOT NULL,            -- 'pending' | 'done' | 'failed'
  error        TEXT,                     -- failure message when status='failed'
  started_at   TEXT NOT NULL,
  finished_at  TEXT,
  UNIQUE(name)
);

CREATE INDEX mundane_steps_status ON mundane_steps(status);
```

Bootstrap is idempotent:

```sql
BEGIN IMMEDIATE;
CREATE TABLE IF NOT EXISTS mundane_meta (...);
CREATE TABLE IF NOT EXISTS mundane_steps (...);
INSERT OR IGNORE INTO mundane_meta VALUES ('schema_version','1');
INSERT OR IGNORE INTO mundane_meta VALUES ('task_id', <new uuid>);
INSERT OR IGNORE INTO mundane_meta VALUES ('created_at', <iso now>);
COMMIT;
```

A row is "cached" iff `status='done'`. `pending` rows exist only inside the
short window of a step body executing; on crash they remain `pending` and the
next invocation re-runs them (steps must be idempotent — that is the entire
contract the caller signs in exchange for the durability guarantee).

`encoding` semantics (v1.1 collapsed the former four-value set to two):
- `json`: `result` is UTF-8 JSON text. The Go/TS/Python SDK steps use this;
  the runtime calls `JSON.parse` / `json.loads` / `json.Unmarshal` on read.
  `sleep` rows also use `json` — the payload is an integer wake-time in ms
  since epoch, stored as a JSON number, so a resumed invocation can compute
  "how much longer" without re-parsing a duration argument (`kind='sleep'`
  distinguishes these from value steps).
- `bytes`: `result` is a raw BLOB stored verbatim. The shell `step` uses
  this for command stdout — binary-safe (NUL bytes, no trailing-newline
  munging) without a base64 layer. `mundane get` emits it unchanged.

## 3. Concurrency

One writer per file, ever. On open the runtime acquires an exclusive
`flock(LOCK_EX | LOCK_NB)` on the SQLite file's fd. If the lock is held by
another live process the runtime exits **fail-fast** with code `75`
(`EX_TEMPFAIL`) and a one-line message on stderr. Cron, supervisor, or
whatever invoked us is responsible for trying again later — mundane does not
queue, retry, or wait.

The lock is released by the OS when the process exits. There is no "stale
lock" state: if a process holding the lock dies, the kernel drops it. There is
nothing to clean up.

Multiple *readers* (`mundane status`, `sqlite3 task.db .dump`, etc.) are
fine — they don't take the write lock. They will see a consistent snapshot
between writer transactions.

## 4. Identity & resumption

On open:

1. `flock` (§3). On failure: exit 75.
2. Bootstrap schema (§2). Idempotent.
3. Read all rows from `mundane_steps` into an in-memory map keyed by `name`.
4. Run the user body. Each `step(name, …)` / `sleep(name, …)` call:
   - looks up `name` in the map;
   - if `status='done'`: return the decoded `result` synchronously;
   - if `status='pending'` or absent: execute the body, write the result,
     return.

Cache misses are appended in caller order; the `id` column gives a stable
sequence. The cache is read once at open and updated in-place on writes; we
do not re-query between steps.

## 5. Step names & idempotency keys

Step names must match `^[A-Za-z0-9][A-Za-z0-9._-]*$` (strict ASCII identifier
with `.`, `_`, `-`). No spaces, colons, slashes, unicode. The runtime
rejects an invalid name with a hard error before touching the DB.

**Names must be unique within one task body.** Calling `step` or `sleep`
twice with the same name in one invocation is a hard error
(`DuplicateStepError`; exit code 2 from the CLI). Each runtime keeps a
per-run "seen" set; on resume the set starts empty, so the first call to
a name in a resumed body cache-hits the existing row as usual — only a
*second* call within the *same* body errors.

If you need many similar steps (e.g. inside a loop), embed an index or
key in the name: `step "process-$i"`, `ctx.step(f"process-{i}", …)`.

The on-disk **idempotency key** is `task_id ":" name`. Because names cannot
contain `:` and `task_id` is a UUID, the key is unambiguous.

## 6. Bash interface

Distribution: a single `mundane` Go binary. The shell UX is the well-known
`eval "$(tool init)"` pattern: at the top of any POSIX shell script, run

```sh
eval "$(mundane init <task.db>)"
```

which opens the DB on a file descriptor, takes the flock, bootstraps the
schema, and defines `step` / `nap` shell functions in the caller's shell.
The lock is held until the calling shell exits.

```sh
#!/bin/sh
eval "$(mundane init task.db)"
step fetch -- curl -fsS https://api/x
nap  cool 5m
step binary -- ./produce-bytes
step notify -- ./send-email.sh
```

The emitted helpers:

- `step NAME -- CMD [ARGS...]` — run `CMD` once; cache stdout as `bytes`
  (raw, binary-safe — NUL bytes and trailing newlines preserved). Stdout
  of the cached or fresh run is re-emitted to *our* stdout. Stderr is
  passed through live and not cached. Nonzero exit from `CMD` marks the
  step `failed` and exits the calling shell with that exit code.
- `nap NAME DURATION` — sleep `DURATION` (e.g. `30s`, `5m`, `2h`). First
  call computes `wake_at = now + DURATION`, writes a `sleep`/`json` row, then
  sleeps `wake_at - now`. Resumption sleeps the remaining time.
  SIGINT/SIGTERM during the sleep kills the process; the row persists;
  the next invocation resumes.

Exit codes:
- `0`: body completed.
- `1`: a non-step error (I/O, etc.).
- `2`: usage error, invalid name, duplicate step name, schema mismatch.
- `75`: another process holds the lock.
- Any other code: propagated verbatim from a failing `step` command.

Auxiliary subcommands (all read-only, no lock contention; no `eval`
needed):
- `mundane status <task.db>` — one-line summary.
- `mundane steps  <task.db>` — table of `id name kind status started finished`.
- `mundane get    <task.db> <name>` — print a cached result (decoded).

Internal subcommands (called by the emitted `step`/`nap` helpers; refuse
to run without `MUNDANE_LOCK_FD` in the environment):
- `mundane __bootstrap <task.db>`
- `mundane __step <task.db> <name> -- CMD [args...]`
- `mundane __nap  <task.db> <name> <duration>`

## 7. TypeScript interface

Package: `mundane-sdk`. Runtime deps: `node-sqlite3` and `fs-ext`. The DB is
opened with the `unix-none` VFS so SQLite does no file locking of its own — the
`flock` (§3) is the sole writer lock, which also avoids colliding with SQLite's
own `flock`-based locking on platforms like macOS.

```ts
import { run } from "mundane-sdk";

await run("task.db", async (ctx) => {
  const user = await ctx.step("fetch-user", async () => {
    return await fetch(`https://api/users/${id}`).then(r => r.json());
  });

  await ctx.sleep("cool-off", "5m");

  await ctx.step("notify", async () => {
    await sendEmail(user.email, "hi");
    return null;
  });
});
```

`ctx.step(name, fn)` — runs `fn` on cache miss, returns its (JSON-round-
tripped) result. On cache hit, returns the cached value without calling `fn`.

`ctx.sleep(name, duration)` — `duration` is a string (`"5m"`, `"2h"`, `"30s"`)
or a number of milliseconds.

JSON contract: the value returned from `fn` must satisfy
`deepEqual(x, JSON.parse(JSON.stringify(x)))`. The runtime performs that
check on the first write (only — not on subsequent reads) and throws
`SerializationError` with the offending key path if it fails. This
catches `Date`, `undefined`, `BigInt`, `Map`, `Set`, functions, and circular
refs at the call site instead of silently corrupting the cache.

`run` returns the body's return value. Lock contention throws
`LockedError` (no retry — the caller decides). Calling `ctx.step` or
`ctx.sleep` twice with the same name in one body throws
`DuplicateStepError`.

## 8. Python interface

Package: `mundane`. Stdlib only (`sqlite3`, `fcntl`, `json`, `uuid`).

```py
import mundane

def workflow(ctx):
    user = ctx.step("fetch-user", lambda: requests.get(url).json())
    ctx.sleep("cool-off", "5m")
    ctx.step("notify", lambda: send_email(user["email"], "hi"))

mundane.run("task.db", workflow)
```

Same shape as TS: `ctx.step(name, fn)`, `ctx.sleep(name, duration)`. No
decorators. The first-write JSON round-trip check raises
`mundane.SerializationError`; lock contention raises `mundane.LockedError`;
duplicate step name within one body raises `mundane.DuplicateStepError`.

Async variant: `await mundane.arun("task.db", async_workflow)` with
`await ctx.astep(...)` / `await ctx.asleep(...)`. The DB operations remain
sync under the hood — async just lets the user `fn` be a coroutine.

## 9. Operational notes

- **Cron**: invoke the same shell script every N minutes; rely on `flock`
  to make overlapping invocations harmless (the second exits 75) and on
  the cache to make a successful invocation that re-runs immediately a
  no-op.
- **Signals**: SIGINT/SIGTERM during a step body interrupt the body; the
  `pending` row is left behind; next invocation re-runs that step. Steps
  must be idempotent. This is restated here because it is the single
  contract the caller cannot avoid.
- **Long sleeps**: `nap 24h` really blocks the process for 24h. If that
  is the wrong tradeoff for your deployment, decompose into shorter naps
  or exit-and-recron yourself — v1 does not provide an "exit and
  reschedule" primitive.
- **Backups**: copy the file. Done.
- **Inspection**: `sqlite3 task.db .dump` is a supported interface. The
  two table names are stable across v1.

## 10. Go interface

Module: `github.com/paulbellamy/mundane/go` — importable as `mundane`.
Single dependency on `modernc.org/sqlite` (pure-Go SQLite; no cgo).

```go
import "github.com/paulbellamy/mundane/go/mundane"

err := mundane.Run("task.db", func(ctx *mundane.Ctx) error {
    user, err := mundane.Step(ctx, "fetch", func() (User, error) {
        return fetchUser(id)
    })
    if err != nil { return err }
    if err := ctx.Sleep("cool", "5m"); err != nil { return err }
    _, err = mundane.Step(ctx, "notify", func() (struct{}, error) {
        return struct{}{}, sendEmail(user.Email, "hi")
    })
    return err
})
```

`mundane.Step[T]` is generic: the typed value the user fn returns is
JSON-round-tripped on first write and remarshalled into `T` on cache hits.
`ctx.Sleep(name, duration)` mirrors the bash/Py/TS shape.

JSON contract: the value returned from `fn` must survive a round-trip
through its own type `T` — `marshal(v)` equals `marshal(unmarshal[T](marshal(v)))`.
Round-tripping through `T` (rather than `any`) preserves struct field order
and `int64` precision; decoding into `any` would sort map keys and route
integers through `float64`, rejecting ordinary values. The runtime performs
that check on the first write and returns `*SerializationError` if it fails.

Lock contention returns `*LockedError`. Duplicate step name returns
`*DuplicateStepError`. Invalid name returns `*InvalidNameError`.
Wrong schema returns `*SchemaError`. A step body's error is wrapped
in `*StepFailedError` (which unwraps to the original).

The same module also ships the CLI in `cmd/mundane`; the binary is the
canonical bash UX (`mundane init`).

## 11. Non-goals (v1)

- Multiple writers per file.
- Cross-file dependencies / fan-out / signals between tasks.
- Schema migrations across major versions.
- Built-in retry, backoff, or timeout policies.
- A daemon, scheduler, broker, UI, or HTTP surface.
- Custom serializers, encrypted-at-rest payloads, or pluggable storage.

Anything in this list is welcome as a layer *on top of* mundane v1, in a
separate package. The core stays small on purpose.
