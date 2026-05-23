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

- **Locking.** The runtime opens the SQLite file and takes
  `flock(LOCK_EX | LOCK_NB)` on its fd via the `fs-ext` native binding —
  no subprocess. It is the same `flock(2)` the bash, Go, and Python
  runtimes use, so a lock held by one is visible to the rest, and the
  kernel drops it automatically if the process dies.
- **Synchronous DB.** Built on `better-sqlite3` (sync), matching the
  sync model of flock + body + commit.
