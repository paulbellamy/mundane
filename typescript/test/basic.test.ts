/* Basic tests for @mundane/core */

import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

import Database from "better-sqlite3";
import {
  MundaneDuplicateStepError,
  MundaneLockedError,
  MundaneSerializationError,
  run,
} from "../src/index";

// Inspection lives in the CLI now; tests read on-disk state directly.
function readSteps(path: string): { name: string; encoding: string; status: string }[] {
  const db = new Database(path, { readonly: true });
  try {
    return db.prepare("SELECT name, encoding, status FROM mundane_steps ORDER BY id").all() as {
      name: string;
      encoding: string;
      status: string;
    }[];
  } finally {
    db.close();
  }
}

function readResult(path: string, name: string): unknown {
  const db = new Database(path, { readonly: true });
  try {
    const row = db.prepare("SELECT result, encoding FROM mundane_steps WHERE name = ?").get(name) as
      | { result: Buffer | string; encoding: string }
      | undefined;
    if (!row) throw new Error(`no step ${name}`);
    const text = typeof row.result === "string" ? row.result : row.result.toString("utf8");
    return row.encoding === "json" ? JSON.parse(text) : text;
  } finally {
    db.close();
  }
}

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
    const names = readSteps(path).map((s) => s.name);
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

test("failed step re-runs on next invocation", async () => {
  const { path, cleanup } = newDb();
  try {
    await assert.rejects(
      run(path, async (ctx: any) => {
        await ctx.step("s", async () => {
          throw new Error("boom");
        });
      }),
    );
    // A failed step is not cached; it must re-run.
    const calls: string[] = [];
    const r = await run(path, async (ctx: any) =>
      ctx.step("s", async () => {
        calls.push("s");
        return 7;
      }),
    );
    assert.equal(r, 7);
    assert.deepEqual(calls, ["s"]);
  } finally {
    cleanup();
  }
});

test("failed row is reset to pending during re-run", async () => {
  const { path, cleanup } = newDb();
  try {
    await assert.rejects(
      run(path, async (ctx: any) => {
        await ctx.step("s", async () => {
          throw new Error("boom");
        });
      }),
    );
    let midStatus = "";
    let midError: string | null = "stale";
    await run(path, async (ctx: any) =>
      ctx.step("s", async () => {
        const db = new Database(path, { readonly: true });
        const row = db.prepare("SELECT status, error FROM mundane_steps WHERE name='s'").get() as {
          status: string;
          error: string | null;
        };
        db.close();
        midStatus = row.status;
        midError = row.error;
        return 7;
      }),
    );
    assert.equal(midStatus, "pending");
    assert.equal(midError, null);
  } finally {
    cleanup();
  }
});

test("pending step re-runs on resume", async () => {
  const { path, cleanup } = newDb();
  try {
    // Bootstrap, then leave a pending row behind (simulating a crash mid-step).
    await run(path, async () => {});
    const db = new Database(path);
    db.prepare(
      "INSERT INTO mundane_steps (name, kind, encoding, result, status, started_at) " +
        "VALUES ('s', 'step', 'json', NULL, 'pending', ?)",
    ).run(new Date().toISOString());
    db.close();

    const calls: string[] = [];
    const r = await run(path, async (ctx: any) =>
      ctx.step("s", async () => {
        calls.push("s");
        return 5;
      }),
    );
    assert.equal(r, 5);
    assert.deepEqual(calls, ["s"]);
  } finally {
    cleanup();
  }
});

test("steps are committed and decode round-trips", async () => {
  const { path, cleanup } = newDb();
  try {
    await run(path, async (ctx: any) => {
      await ctx.step("a", async () => ({ x: 1 }));
      await ctx.step("b", async () => "hello");
    });
    const done = readSteps(path).filter((s) => s.status === "done");
    assert.equal(done.length, 2);
    assert.deepEqual(readResult(path, "a"), { x: 1 });
    assert.equal(readResult(path, "b"), "hello");
  } finally {
    cleanup();
  }
});
