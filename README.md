# mundane

A tiny durable-execution library. One workflow run is one SQLite file.
Crash, re-invoke, resume. No daemon, no broker, no scheduler — bring
your own cron.

Inspired by [Absurd](https://github.com/earendil-works/absurd) — same
"durable workflows are absurdly simple" thesis, swapping Postgres for
SQLite so a workflow is one portable file you can `cp`, `mv`, or ship in
a tarball.

Three runtimes ship from this repo and interoperate row-for-row on the
same on-disk format:

| Runtime    | Path           | Distribution               |
|------------|----------------|----------------------------|
| Bash       | `bash/mundane` | single POSIX `sh` script   |
| TypeScript | `typescript/`  | `@mundane/core` npm package |
| Python     | `python/`      | `mundane` pypi package      |

See [`SPEC.md`](./SPEC.md) for the contract.

## Quick start

### Bash

```sh
./bash/mundane run task.db -- step greeting -- echo "hello world"
./bash/mundane status task.db
./bash/mundane get    task.db greeting
```

### Python

```python
import mundane

def workflow(ctx):
    user = ctx.step("fetch", lambda: {"name": "alice"})
    ctx.sleep("cool-off", "100ms")
    ctx.step("notify", lambda: f"hi {user['name']}")

mundane.run("task.db", workflow)
```

### TypeScript

```ts
import { run } from "@mundane/core";

await run("task.db", async (ctx) => {
  const user = await ctx.step("fetch", async () => ({ name: "alice" }));
  await ctx.sleep("cool-off", "100ms");
  await ctx.step("notify", async () => `hi ${user.name}`);
});
```

## Running the tests

```sh
make test       # bash + python + typescript + interop
make lint       # shellcheck on every sh script
```

Or by runtime:

```sh
./bash/test/run.sh                                # bash
cd python && python3 -m unittest tests.test_basic # python
cd typescript && npm install \
  && npx tsc -p . && node --test dist/test/      # typescript
./interop-tests/run.sh                            # cross-runtime
```

CI (GitHub Actions) runs all of the above plus shellcheck on every push
and pull request.

## Implementation notes

- **Locking.** Bash and Python use `flock(2)` directly. Node lacks a
  built-in flock binding, so the TS runtime spawns a tiny `sh` helper that
  holds the actual `flock(LOCK_EX | LOCK_NB)` on the SQLite file's fd and
  releases it when our stdin closes. All three runtimes interoperate: a
  lock held by one is visible to the others.
- **Bash workflows** must use functions or inline step/nap invocations,
  not separate scripts, because POSIX sh does not propagate function
  definitions across `exec`. The most common pattern is one `step` per
  `mundane run` invocation, driven externally by cron. For multi-step
  workflows, source `bash/mundane` to gain `step` and `nap` in your shell.
- **Trailing newlines.** Bash `step` (text encoding) preserves trailing
  newlines exactly per the spec. Result bytes are written via stdin to
  avoid command-substitution truncation.
- **JSON interop.** Values written by TS/Python (`encoding='json'`) are
  read by bash via `json_extract(result, '$')` so a step that returned the
  string `"hello"` in Python reads back as `hello` (not `"hello"`) in bash.
