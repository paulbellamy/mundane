# @mundane/core (TypeScript)

See [`../SPEC.md`](../SPEC.md) for the cross-runtime contract.

```ts
import { run } from "@mundane/core";

await run("task.db", async (ctx) => {
  const user = await ctx.step("fetch", async () => ({ name: "alice" }));
  await ctx.sleep("cool-off", "100ms");
  await ctx.step("notify", async () => `hi ${user.name}`);
});
```

## Implementation notes

- **Locking.** Node has no built-in `flock` binding, so the runtime
  spawns a tiny `sh` helper that holds the actual
  `flock(LOCK_EX | LOCK_NB)` on the SQLite file's fd and releases it when
  our stdin closes. The lock interoperates with the other runtimes: a
  lock held by one is visible to the rest.
- **Synchronous DB.** Built on `better-sqlite3` (sync), matching the
  sync model of flock + body + commit.
