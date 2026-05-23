# Examples

Runnable, offline workflows that show off what `mundane` is for: crash, then
re-invoke and resume. None of them need network access or secrets — the
"expensive" work is faked with local stubs that log only when they actually
run, so a resumed run visibly skips what it already did.

| Example | Runtime | Shows off |
|---------|---------|-----------|
| [`etl-typescript/`](./etl-typescript) | TypeScript | Crash mid-pipeline; resume without re-extracting or re-transforming. Indexed step names for a per-record loop. |
| [`ai-pipeline-python/`](./ai-pipeline-python) | Python | Each LLM call is a cached step — crash and resume without re-paying for completed calls. |
| [`onboarding-go/`](./onboarding-go) | Go | A multi-step drip campaign: each send is a step, each gap is a `nap`. Kill it mid-wait and resume without re-sending. |
| [`docker-volume/`](./docker-volume) | Shell + Docker | The task file lives on a volume, so it survives container deletion. An interrupted `nap` resumes its remainder. |

Each directory has its own README with copy-pasteable commands. Across all
three, the move is the same: run it, kill it partway (a `CRASH_AT` env var or
`docker compose down`), run it again, and watch the finished work replay from
the SQLite file instead of re-executing.

## Verifying

Every example is smoke-tested in CI (and locally) — built, then driven through
the crash → resume → all-cached cycle with assertions that completed work
replays instead of re-running:

```sh
make test-examples      # or: ./examples/smoke.sh
```

The `docker-volume` image is additionally built and run on a volume in a
dedicated CI job.

## Inspecting

Inspect any task file with the CLI (`make build-go` from the repo root):

```sh
go/mundane-bin steps  task.db    # one row per step, with status
go/mundane-bin get    task.db <name>
go/mundane-bin status task.db
```
