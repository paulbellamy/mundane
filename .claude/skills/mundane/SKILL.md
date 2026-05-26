---
name: mundane
description: >-
  Use mundane to make a multi-step script crash-resumable so completed work
  isn't redone on re-run. One workflow run is one SQLite file; wrap expensive
  or side-effecting steps (LLM/API calls, fetches, sends, transforms) so each
  runs at most once and a re-invocation resumes where it left off. Available
  for shell, Go, Python, and TypeScript. TRIGGER when a task is a sequence of
  steps that must survive crashes/restarts, must not re-charge or re-send on
  retry, or needs to pause (nap) and resume later. SKIP for one-shot scripts,
  request/response handlers, fan-out/concurrent writers to one file, sub-second
  latency-critical paths, or anything needing a real queue/scheduler/broker.
---

# mundane

A tiny durable-execution library. **One workflow run is one SQLite file.**
Crash, re-invoke against the same file, resume — no daemon, no broker, no
scheduler (bring your own cron/supervisor). See `SPEC.md` for the full contract.

## Mental model

- The **file path is the task identity.** Re-invoking against the same path
  resumes the same task; a different path is a different task. mundane does not
  canonicalize paths — opening "the same file" is the caller's job.
- A task is a sequence of named **steps**. Once a step commits a result, every
  later attempt returns that cached result *without re-running the body*.
- One writer per file via `flock`. A second concurrent invocation fails fast
  (exit code 75 / `LockedError`) — it does not queue or wait.
- **The contract you sign for durability: steps must be idempotent.** A crash
  mid-step leaves the row `pending`, and the next invocation re-runs it.

## Two primitives

- **step(name, fn)** — run `fn` once on cache miss, cache its JSON result,
  return the cached value on every later attempt. For expensive/side-effecting
  work you want to happen at most once (API call, LLM call, email send, write).
- **sleep / nap(name, duration)** — durable sleep. First call records
  `wake_at = now + duration`; a resume after a crash sleeps only the *remaining*
  time. Durations are strings like `"30s"`, `"5m"`, `"2h"` (or ms as a number).

## Install

- **Go:** `go get github.com/paulbellamy/mundane/go` (pure-Go SQLite, no cgo).
- **Python:** `pip install mundane` (stdlib only, Python 3.8+).
- **TypeScript:** `npm install @mundane/core` (Node 18+; pulls in `node-sqlite3`, `fs-ext`).
- **Shell:** needs the `mundane` CLI binary on `PATH`. Easiest:
  `curl -fsSL https://raw.githubusercontent.com/paulbellamy/mundane/main/install.sh | sh`
  (detects OS/arch, downloads a release binary; `MUNDANE_VERSION` /
  `MUNDANE_INSTALL_DIR` override it). Or build from source:
  `cd go && go build -o mundane ./cmd/mundane` (or `go install
  github.com/paulbellamy/mundane/go/cmd/mundane@latest`).

## How to use it (per runtime)

### Shell
```sh
#!/bin/sh
eval "$(mundane init task.db)"        # flock + bootstrap + define step/nap
step fetch  -- curl -fsS https://api/x   # caches stdout (raw bytes, binary-safe)
nap  cool   5m
step notify -- ./send-email.sh
```
Inspect (read-only, no lock, no eval): `mundane status task.db`,
`mundane steps task.db`, `mundane get task.db fetch`.

### Go
```go
import "github.com/paulbellamy/mundane/go/mundane"

err := mundane.Run("task.db", func(ctx *mundane.Ctx) error {
    user, err := mundane.Step(ctx, "fetch", func() (User, error) {
        return fetchUser(id)
    })
    if err != nil { return err }
    return ctx.Sleep("cool", "5m")
})
```

### Python
```python
import mundane

def workflow(ctx):
    user = ctx.step("fetch", lambda: requests.get(url).json())
    ctx.sleep("cool-off", "5m")
    ctx.step("notify", lambda: send_email(user["email"], "hi"))

mundane.run("task.db", workflow)
# async: await mundane.arun(path, fn) with await ctx.astep(...) / ctx.asleep(...)
```

### TypeScript
```ts
import { run } from "@mundane/core";

await run("task.db", async (ctx) => {
  const user = await ctx.step("fetch", async () => (await fetch(url)).json());
  await ctx.sleep("cool-off", "5m");
  await ctx.step("notify", async () => { await sendEmail(user.email, "hi"); return null; });
});
```

## Rules that bite

- **Step names** must match `^[A-Za-z0-9][A-Za-z0-9._-]*$` — ASCII identifier
  plus `. _ -`. No spaces, slashes, colons, or unicode.
- **Names must be unique within one body.** Reusing a name in the same run is a
  hard error (`DuplicateStepError`, exit 2). In loops, embed an index:
  `step "process-$i"` / `ctx.step(f"process-{i}", ...)`.
- **Results must JSON round-trip.** Returning `Date`, `undefined`, `BigInt`,
  `Map`/`Set`, functions, or circular refs throws a serialization error on the
  first write (caught at the call site, not silently corrupting the cache).
- A step body's nonzero exit / thrown error marks the step `failed` and
  propagates; "retry" means invoke the file again.

## When NOT to use it

mundane is deliberately small. Reach for something else when you need:

- **A single one-shot command** with no expensive or non-idempotent steps —
  the SQLite file is pure overhead.
- **Request/response or sub-second latency-critical paths** — every step is a
  synchronous SQLite write + flock; it's for batch/background work, not hot loops.
- **Concurrency on one task** — one writer per file, period. No fan-out, no
  parallel steps writing the same file, no cross-task signals/dependencies.
- **A real job queue / scheduler / broker / worker pool** — there is no
  scheduler or "running" state; you supply cron or a supervisor. Long `nap`s
  literally block the process for that whole duration.
- **Built-in retry/backoff/timeout policies** — none exist; retry == re-invoke.
- **Steps that can't be made idempotent**, or results that aren't JSON-serializable.
- **Schema migrations / multiple DB schema versions** — files are pinned to
  schema v1; opening a mismatched file is a hard error.
