// Conformance driver (TypeScript/Node). See conformance/run.py.
//
// Usage: node driver.js <task.db> <scenario.json>
// Requires the compiled @mundane/core (run `tsc -p .` in typescript/ first).

const fs = require("node:fs");
const { run } = require("../../typescript/dist/src/index.js");

async function main() {
  const [dbPath, scnPath] = process.argv.slice(2);
  if (!dbPath || !scnPath) {
    console.error("usage: node driver.js <task.db> <scenario.json>");
    process.exit(2);
  }
  const scenario = JSON.parse(fs.readFileSync(scnPath, "utf8"));
  await run(dbPath, async (ctx) => {
    for (const op of scenario.operations) {
      if (op.op === "step") {
        await ctx.step(op.name, async () => op.value ?? null);
      } else if (op.op === "sleep") {
        await ctx.sleep(op.name, op.duration);
      } else {
        throw new Error(`unknown op: ${op.op}`);
      }
    }
  });
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
