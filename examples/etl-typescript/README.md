# ETL pipeline (TypeScript) — survive a crash mid-run

`extract → transform → load`, where every stage is a `mundane` step. Kill the
process partway through and re-invoke: finished steps return from the SQLite
file without re-running, so you never re-pull the source or re-transform rows
you already handled.

The "remote source" and "warehouse" are local stubs, so this runs fully
offline. Each stub logs `[ran] …` **only** when it actually executes (a cache
miss) — so a resumed run visibly skips the work it already did.

## Run it

```sh
# Build @mundane/core first — the file: dep resolves to its dist/ output:
(cd ../../typescript && npm install && npx tsc -p .)

npm install        # links ../../typescript as @mundane/core
npm run build

# First run — simulate a crash right before transforming record 3:
CRASH_AT=transform-3 npm start -- etl.db
#   [ran] extract: pulling records from source
#   [ran] transform: ada
#   [ran] transform: babbage
#   Error: simulated crash before transform-3

# Re-invoke against the same file — extract + transform-1/2 are cached:
npm start -- etl.db
#   [ran] transform: lovelace
#   [ran] transform: turing
#   [ran] transform: hopper
#   [ran] load: writing 5 rows to the warehouse
#   >> done: loaded 5 rows

# Run a third time — everything is cached, no [ran] lines at all:
npm start -- etl.db
#   >> done: loaded 5 rows
```

## Inspect the durable state

The `mundane` CLI is the inspection surface (build it with `make build-go`
from the repo root):

```sh
../../go/mundane-bin steps  etl.db   # one row per step, with status
../../go/mundane-bin get    etl.db extract
../../go/mundane-bin status etl.db
```

## The pattern

- One step per unit of work; loop steps get an index in the name
  (`transform-${rec.id}`) so each record is its own durable, idempotent unit.
- Downstream steps (`load`) read values returned by upstream steps. On resume,
  the upstream steps cache-hit and re-supply those values, so the body replays
  deterministically.
- Steps must be idempotent: a crash can leave a step half-done, and the next
  run re-executes it from scratch.
