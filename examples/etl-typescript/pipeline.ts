/**
 * ETL pipeline (extract -> transform -> load) that survives a crash.
 *
 * Every stage is a mundane step. If the process dies partway through, the
 * next invocation resumes: finished steps return from the SQLite file
 * without re-running, so you never re-extract or re-transform work you
 * already did.
 *
 * Offline + fakeable: the "remote source" and "warehouse" are local stubs.
 * Each stub logs ONLY when it actually executes (a cache miss), so a resumed
 * run visibly skips the work it already completed.
 *
 *   npm install
 *   npm run build
 *
 *   # First run, crash before transforming record 3:
 *   CRASH_AT=transform-3 npm start -- etl.db   # processes 1,2 then dies
 *
 *   # Re-invoke: extract + transform-1/2 are cached, it resumes at 3:
 *   npm start -- etl.db
 *
 *   # Inspect the durable state at any time:
 *   ../../go/mundane-bin steps etl.db
 */

import { run } from "mundane-sdk";

interface SourceRecord {
  id: number;
  name: string;
  balanceCents: number;
}

interface Row {
  id: number;
  name: string;
  balanceUsd: number;
}

// Stand-in for a slow paginated API. In a real pipeline this is the
// expensive, rate-limited bit you really don't want to repeat on a retry.
const SOURCE: SourceRecord[] = [
  { id: 1, name: "ada", balanceCents: 1299 },
  { id: 2, name: "babbage", balanceCents: 4200 },
  { id: 3, name: "lovelace", balanceCents: 999 },
  { id: 4, name: "turing", balanceCents: 31337 },
  { id: 5, name: "hopper", balanceCents: 8080 },
];

const ran = (label: string) => console.log(`  [ran] ${label}`);
const slow = () => new Promise<void>((r) => setTimeout(r, 150));

async function main(): Promise<void> {
  const db = process.argv[2] ?? "etl.db";
  const crashAt = process.env.CRASH_AT;

  await run(db, async (ctx) => {
    const records = await ctx.step("extract", async (): Promise<SourceRecord[]> => {
      ran("extract: pulling records from source");
      await slow();
      return SOURCE;
    });

    const rows: Row[] = [];
    for (const rec of records) {
      const stepName = `transform-${rec.id}`;
      if (crashAt === stepName) {
        throw new Error(`simulated crash before ${stepName}`);
      }
      const row = await ctx.step(stepName, async (): Promise<Row> => {
        ran(`transform: ${rec.name}`);
        await slow();
        return {
          id: rec.id,
          name: rec.name.toUpperCase(),
          balanceUsd: rec.balanceCents / 100,
        };
      });
      rows.push(row);
    }

    const result = await ctx.step("load", async () => {
      ran(`load: writing ${rows.length} rows to the warehouse`);
      await slow();
      return { loaded: rows.length };
    });

    console.log(`>> done: loaded ${result.loaded} rows`);
  });
}

main().catch((err) => {
  console.error(err instanceof Error ? err.message : err);
  process.exit(1);
});
