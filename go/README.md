# mundane (Go)

The Go module ships two things: an importable SDK (`mundane`) and the
`mundane` CLI (`cmd/mundane`), which is also the shell runtime. See
[`../SPEC.md`](../SPEC.md) for the cross-runtime contract.

## SDK

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

`mundane.Step[T]` is generic: the returned value is JSON-round-tripped on
first write and remarshalled into `T` on cache hits.

## CLI (shell runtime)

```sh
go build -o mundane ./cmd/mundane
eval "$(./mundane init task.db)"
step greeting -- echo hello
```

## Implementation notes

- **Pure-Go SQLite** via `modernc.org/sqlite` — no cgo, so the binary
  cross-compiles to any Go target without a C toolchain.
- **Lock inheritance.** The eval'd init script holds the `flock` in the
  calling shell on fd 9 and exports `MUNDANE_LOCK_FD`. The internal
  `__step` / `__nap` subcommands honor that env var and inherit the
  parent's lock without re-flocking (a fresh `flock` would block).
- **Trailing newlines.** Text-encoded `step` output is captured from
  `os/exec` into a buffer and stored verbatim, preserving trailing
  newlines exactly as the spec requires.
