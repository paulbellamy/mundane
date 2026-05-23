# Examples

Runnable, offline workflows that show off what `mundane` is for: crash, then
re-invoke and resume. None of them need network access or secrets — the
"expensive" work is faked with local stubs that log only when they actually
run, so a resumed run visibly skips what it already did.

| Example | Runtime | Shows off |
|---------|---------|-----------|
| [`etl-typescript/`](./etl-typescript) | TypeScript | Crash mid-pipeline; resume without re-extracting or re-transforming. Indexed step names for a per-record loop. |
| [`ai-pipeline-python/`](./ai-pipeline-python) | Python | Each LLM call is a cached step — crash and resume without re-paying for completed calls. |
| [`docker-volume/`](./docker-volume) | Shell + Docker | The task file lives on a volume, so it survives container deletion. An interrupted `nap` resumes its remainder. |

Each directory has its own README with copy-pasteable commands. Across all
three, the move is the same: run it, kill it partway (a `CRASH_AT` env var or
`docker compose down`), run it again, and watch the finished work replay from
the SQLite file instead of re-executing.

Inspect any task file with the CLI (`make build-go` from the repo root):

```sh
go/mundane-bin steps  task.db    # one row per step, with status
go/mundane-bin get    task.db <name>
go/mundane-bin status task.db
```
