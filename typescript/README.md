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
- **No redundant SQLite locking.** The DB is opened with the `unix-none`
  VFS (`file:…?vfs=unix-none`), which turns off SQLite's own file locking.
  Our `flock` already guarantees a single writer for the whole run, so
  SQLite's locking is redundant — and on platforms where SQLite locks via
  `flock(2)` (e.g. macOS) it would collide with ours on the same file and
  deadlock. `unix-none` leaves our flock as the sole authority.
- **DB binding.** Built on `node-sqlite3`, wrapped in a thin promise layer
  (see `src/db.ts`).
