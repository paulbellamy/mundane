/* Basic tests for @mundane/core */

import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

import {
  MundaneDuplicateStepError,
  MundaneLockedError,
  MundaneSerializationError,
  run,
} from "../src/index";
import { getResult, status, steps } from "../src/inspect";

function newDb(): { path: string; cleanup: () => void } {
  const dir = mkdtempSync(join(tmpdir(), "mundane-test-"));
  const path = join(dir, "task.db");
  return {
    path,
    cleanup: () => {
      try {
        rmSync(dir, { recursive: true, force: true });
      } catch {}
    },
  };
}

test("three steps execute once; second run uses cache", async () => {
  const { path, cleanup } = newDb();
  try {
    const calls: string[] = [];
    const wf = async (ctx: any) => {
      const a = await ctx.step("a", async () => {
        calls.push("a");
        return 1;
      });
      const b = await ctx.step("b", async () => {
        calls.push("b");
        return { v: a + 1 };
      });
      return b;
    };

    const r1 = await run(path, wf);
    assert.deepEqual(r1, { v: 2 });
    assert.deepEqual(calls, ["a", "b"]);

    const r2 = await run(path, wf);
    assert.deepEqual(r2, { v: 2 });
    assert.deepEqual(calls, ["a", "b"]); // not re-called
  } finally {
    cleanup();
  }
});

test("step is cached after a body throws", async () => {
  const { path, cleanup } = newDb();
  try {
    await assert.rejects(
      run(path, async (ctx: any) => {
        await ctx.step("a", async () => 42);
        throw new Error("simulated crash");
      }),
    );
    const calls: string[] = [];
    const r = await run(path, async (ctx: any) => {
      const v = await ctx.step("a", async () => {
        calls.push("a");
        return 0;
      });
      const w = await ctx.step("b", async () => {
        calls.push("b");
        return v + 1;
      });
      return w;
    });
    assert.equal(r, 43);
    assert.deepEqual(calls, ["b"]);
  } finally {
    cleanup();
  }
});

test("invalid step name is rejected", async () => {
  const { path, cleanup } = newDb();
  try {
    await assert.rejects(
      run(path, async (ctx: any) => {
        await ctx.step("bad name", async () => 1);
      }),
    );
  } finally {
    cleanup();
  }
});

test("duplicate step name raises MundaneDuplicateStepError", async () => {
  const { path, cleanup } = newDb();
  try {
    await assert.rejects(
      run(path, async (ctx: any) => {
        await ctx.step("x", async () => 1);
        await ctx.step("x", async () => 2);
      }),
      (e: any) => e instanceof MundaneDuplicateStepError && e.stepName === "x",
    );
    // First step still committed before the dup raised.
    const names = steps(path).map((s: any) => s.name);
    assert.deepEqual(names, ["x"]);
  } finally {
    cleanup();
  }
});

test("sleep persists wake_at and resumes", async () => {
  const { path, cleanup } = newDb();
  try {
    const t0 = Date.now();
    await run(path, async (ctx: any) => {
      await ctx.sleep("nap", "30ms");
    });
    const e1 = Date.now() - t0;
    assert.ok(e1 < 500, `first sleep should be quick: ${e1}ms`);
    assert.ok(e1 >= 25, `first sleep should be ~30ms: ${e1}ms`);
    const t1 = Date.now();
    // second run sees the wake_at in the past, returns immediately
    await run(path, async (ctx: any) => {
      await ctx.sleep("nap", "10s");
    });
    const e2 = Date.now() - t1;
    assert.ok(e2 < 200, `second sleep should be near-instant: ${e2}ms`);
  } finally {
    cleanup();
  }
});

test("non-JSON value raises MundaneSerializationError", async () => {
  const { path, cleanup } = newDb();
  try {
    await assert.rejects(
      run(path, async (ctx: any) => {
        await ctx.step("a", async () => new Date());
      }),
      (e: any) => e instanceof MundaneSerializationError,
    );
  } finally {
    cleanup();
  }
});

test("locked task throws MundaneLockedError", async () => {
  const { path, cleanup } = newDb();
  try {
    let unblock!: () => void;
    const blocker = new Promise<void>((r) => {
      unblock = r;
    });

    const first = run(path, async (ctx: any) => {
      await ctx.step("init", async () => 0);
      await blocker;
      return 0;
    });

    // Give the first run time to acquire the lock.
    await new Promise((r) => setTimeout(r, 200));

    await assert.rejects(
      run(path, async () => {}),
      (e: any) => e instanceof MundaneLockedError,
    );

    unblock();
    await first;
  } finally {
    cleanup();
  }
});

test("status, steps, getResult", async () => {
  const { path, cleanup } = newDb();
  try {
    await run(path, async (ctx: any) => {
      await ctx.step("a", async () => ({ x: 1 }));
      await ctx.step("b", async () => "hello");
    });
    const s = status(path);
    assert.equal(s.done, 2);
    assert.deepEqual(getResult(path, "a"), { x: 1 });
    assert.equal(getResult(path, "b"), "hello");
  } finally {
    cleanup();
  }
});
