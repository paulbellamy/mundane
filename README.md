# mundane

A tiny durable-execution library. One workflow run is one SQLite file.
Crash, re-invoke, resume. No daemon, no broker, no scheduler — bring
your own cron.

Inspired by [Absurd](https://github.com/earendil-works/absurd) — same
"durable workflows are absurdly simple" thesis, swapping Postgres for
SQLite so a workflow is one portable file you can `cp`, `mv`, or ship in
a tarball.

Four runtimes ship from this repo and interoperate row-for-row on the
same on-disk format:

| Runtime    | Path                  | Distribution                  |
|------------|-----------------------|-------------------------------|
| Shell (CLI)| `go/cmd/mundane`      | single Go binary; eval-init   |
| Go SDK     | `go/mundane`          | `github.com/paulbellamy/mundane/go` |
| TypeScript | `typescript/`         | `@mundane/core` npm package   |
| Python     | `python/`             | `mundane` pypi package        |

See [`SPEC.md`](./SPEC.md) for the contract.

## Quick start

### Shell

```sh
#!/bin/sh
eval "$(mundane init task.db)"      # flock, bootstrap, define step/nap
step greeting -- echo "hello world"
nap   cool 100ms
step --b64 binary -- ./produce-bytes
```

`mundane status task.db` / `steps` / `get task.db greeting` for inspection.

### Go

```go
import "github.com/paulbellamy/mundane/go/mundane"

err := mundane.Run("task.db", func(ctx *mundane.Ctx) error {
    user, err := mundane.Step(ctx, "fetch", func() (User, error) {
        return fetchUser(id)
    })
    if err != nil { return err }
    return ctx.Sleep("cool", "100ms")
})
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
make build      # Go binary + TS compile
make test       # go + sh integration + python + typescript + interop
make lint       # shellcheck + ruff + biome + go vet
```

Or by runtime:

```sh
cd go && go test ./...                            # Go SDK
./bash/test/run.sh                                # shell integration
cd python && python3 -m unittest tests.test_basic # Python
cd typescript && npm install \
  && npx tsc -p . && node --test dist/test/      # TypeScript
./interop-tests/run.sh                            # cross-runtime
```

## Implementation notes

- **Locking.** Go, Python, and bash (via the Go CLI) use `flock(2)`
  directly. Node lacks a built-in flock binding, so the TS runtime spawns
  a tiny `sh` helper that holds the actual `flock(LOCK_EX | LOCK_NB)` on
  the SQLite file's fd. All four runtimes interoperate: a lock held by
  one is visible to the others.
- **Shell UX.** `eval "$(mundane init <db>)"` opens the DB on a file
  descriptor, takes the flock in the calling shell, and defines `step`
  and `nap` shell functions. The lock auto-releases when the shell
  exits. Internal `__step` / `__nap` subcommands honor `MUNDANE_LOCK_FD`
  and inherit the parent's lock without re-flocking.
- **Trailing newlines.** Shell `step` (text encoding) preserves trailing
  newlines exactly per the spec — the Go binary captures `os/exec`
  stdout to a buffer and stores it verbatim.
- **JSON interop.** Values written by Go/TS/Python (`encoding='json'`)
  are emitted by `mundane get` as raw JSON text, so cross-runtime reads
  work without an extra decode step.
- **Duplicate names.** v1.1 changed `step foo` called twice in one body
  from auto-suffix `foo#2` to a hard error. If you need many similar
  steps, include an index/key in the name: `step "process-$i"`.
