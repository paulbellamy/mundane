# LLM pipeline (Python) — never pay for the same tokens twice

A content-generation workflow (`outline → draft each section → assemble`)
where every model call is a `mundane` step. Crash mid-run and re-invoke:
completed calls return from the SQLite file, so you only pay for the calls
that didn't finish.

The "model" is a local stub that prints `$$ model call: …` **only** when it
actually runs — so a resumed run shows exactly which (expensive) calls you
were spared. Fully offline, no API key.

## Run it

Put the `mundane` package on your path (it's stdlib-only):

```sh
pip install -e ../../python      # or: export PYTHONPATH=../../python
```

```sh
# First run — crash before drafting the "tradeoffs" section:
CRASH_AT=tradeoffs python3 pipeline.py post.db
#   $$ model call: 'outline a post about durable execution with sql...'
#   $$ model call: 'write the intro section, given outline [a1b2c3d4]...'
#   $$ model call: 'write the how-it-works section, ...'
#   RuntimeError: simulated crash before drafting 'tradeoffs'

# Re-invoke — outline + intro + how-it-works are cached, no $$ for them:
python3 pipeline.py post.db
#   $$ model call: 'write the tradeoffs section, ...'
#   $$ model call: 'write the conclusion section, ...'
#   >> assembled post: 294 chars across 4 sections

# Third run — every call is cached, zero $$ lines:
python3 pipeline.py post.db
#   >> assembled post: 294 chars across 4 sections
```

## Inspect the durable state

```sh
../../go/mundane-bin steps post.db     # status per step (build: make build-go)
../../go/mundane-bin get   post.db outline
```

## The pattern

- One step per model call. The cache key is the step name, so give each
  fan-out call a distinct name (`draft-{section}`).
- A later step (`assemble`) consumes values returned by earlier steps; on
  resume those earlier steps cache-hit and re-supply their values, so the body
  replays deterministically without new calls.
- Async variant: `mundane.arun` with `await ctx.astep(...)` lets the step body
  be a coroutine if your client is async.
