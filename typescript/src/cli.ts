#!/usr/bin/env node
/**
 * CLI: `mundane status|steps|get <task.db> [name]`
 *
 * (`run` is library-only in TS — workflow bodies are JS.)
 */

import { status, steps, getResult } from "./inspect";

function usage(): never {
  process.stderr.write(
    "usage: mundane <subcommand>\n" +
      "  status <task.db>\n" +
      "  steps  <task.db>\n" +
      "  get    <task.db> <name>\n",
  );
  process.exit(2);
}

function main(argv: string[]): number {
  if (argv.length < 2) usage();
  const sub = argv[0];
  if (sub === "status") {
    if (argv.length !== 2) usage();
    const s = status(argv[1]);
    process.stdout.write(
      `task_id=${s.task_id}  steps=${s.total_steps}  done=${s.done}  pending=${s.pending}  failed=${s.failed}\n`,
    );
    return 0;
  }
  if (sub === "steps") {
    if (argv.length !== 2) usage();
    const rows = steps(argv[1]);
    process.stdout.write(
      `${"id".padEnd(4)} ${"name".padEnd(30)} ${"kind".padEnd(6)} ${"status".padEnd(8)} ${"started_at".padEnd(28)} finished_at\n`,
    );
    for (const r of rows) {
      process.stdout.write(
        `${String(r.id).padEnd(4)} ${r.name.padEnd(30)} ${r.kind.padEnd(6)} ${r.status.padEnd(8)} ${(r.started_at || "").padEnd(28)} ${r.finished_at || ""}\n`,
      );
    }
    return 0;
  }
  if (sub === "get") {
    if (argv.length !== 3) usage();
    const v = getResult(argv[1], argv[2]);
    if (Buffer.isBuffer(v)) {
      process.stdout.write(v);
    } else if (typeof v === "string") {
      process.stdout.write(v);
    } else {
      process.stdout.write(JSON.stringify(v) + "\n");
    }
    return 0;
  }
  usage();
}

try {
  process.exit(main(process.argv.slice(2)));
} catch (e) {
  const msg = e instanceof Error ? e.message : String(e);
  process.stderr.write(`mundane: ${msg}\n`);
  process.exit(1);
}
