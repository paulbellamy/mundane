#!/bin/sh
# A mundane workflow whose task file lives on a Docker volume, so it outlives
# the container. Delete/recreate the container mid-run and re-invoke: finished
# steps replay from the volume, and an interrupted `nap` resumes its remainder.
#
# Steps are fakeable (echo + sleep), so this needs no network or secrets.
set -eu

DB="${1:-/data/task.db}"
echo ">> opening task: $DB"

# Sets the flock, bootstraps the schema, and defines `step` / `nap`.
eval "$(mundane init "$DB")"

step extract   -- sh -c 'echo "  [extract] pulled 1000 rows"; sleep 1'

# A long pause you can interrupt: `docker compose down` (or Ctrl-C) during the
# nap kills the container, but the volume — and the persisted wake time —
# survive, so the next run sleeps only the remaining seconds. (COOLDOWN is
# overridable so tests/demos can run fast.)
nap  cooldown  "${COOLDOWN:-30s}"

step transform -- sh -c 'echo "  [transform] cleaned + deduped"; sleep 1'
step load      -- sh -c 'echo "  [load] wrote to the warehouse"; sleep 1'

echo ">> workflow complete"
