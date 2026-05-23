# mundane (Python)

See [`../SPEC.md`](../SPEC.md) for the cross-runtime contract.

```python
import mundane

def workflow(ctx):
    user = ctx.step("fetch", lambda: {"name": "alice"})
    ctx.sleep("cool-off", "100ms")
    ctx.step("notify", lambda: f"hi {user['name']}")

mundane.run("task.db", workflow)
```

An async variant is available: `await mundane.arun(path, async_workflow)`
with `await ctx.astep(...)` / `await ctx.asleep(...)`.

## Implementation notes

- **Stdlib only.** No third-party dependencies — `sqlite3`, `fcntl`
  (for `flock`), `json`, and `uuid` from the standard library.
