# Docker volume — a task that outlives the container

A `mundane` task is a single SQLite file, so durability across container
restarts is just "put the file on a volume." This example runs a shell
workflow whose task lives at `/data/task.db` on a named Docker volume. Destroy
and recreate the container mid-run and the task resumes — because the file
never lived in the container's writable layer.

The workflow steps are fake (`echo` + `sleep`), so nothing here needs network
access or secrets.

## Run it

```sh
cd examples/docker-volume
docker compose build
docker compose run --rm workflow
#   >> opening task: /data/task.db
#   [extract] pulled 1000 rows
#   ... (then a 30s `nap cooldown`)
```

## Kill it mid-run, then resume

While the run is paused in the `cooldown` nap, interrupt it — `Ctrl-C`, or
from another terminal:

```sh
docker compose down          # stops & REMOVES the container (keeps the volume)
```

Then re-invoke. The container is brand new, but the task file on the volume is
not:

```sh
docker compose run --rm workflow
#   >> opening task: /data/task.db
#   [extract] pulled 1000 rows          <- replayed from cache, instant
#   ... cooldown resumes only the SECONDS THAT WERE LEFT, not a fresh 30s ...
#   [transform] cleaned + deduped
#   [load] wrote to the warehouse
#   >> workflow complete
```

`extract` re-emits its cached stdout immediately (no re-run), and `nap` sleeps
only the remaining time it persisted before the container died.

## Inspect or reset

```sh
# Peek at the durable state from a throwaway container on the same volume:
docker compose run --rm --entrypoint mundane workflow steps /data/task.db

# Start over — `-v` also removes the volume (deletes the task file):
docker compose down -v
```

## Why this works

- The file *is* the task (SPEC §1): identity is the path, and the path points
  at the volume, not the container.
- `journal_mode=DELETE` means there are no `-wal`/`-shm` siblings to lose — the
  whole artifact is one file, safe to keep on a volume, `cp`, or ship.
- The `flock` is held only for the life of the process inside the container; a
  removed container drops the lock automatically (SPEC §3), so the next run
  acquires it cleanly with nothing to clean up.
