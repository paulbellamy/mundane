#!/bin/sh
# Smoke-test the example workflows: build each one and drive it through the
# crash -> resume -> all-cached cycle, asserting that completed work replays
# from the SQLite file instead of re-executing.
#
# Covers the three code examples (TS/Python/Go) plus the shell workflow that
# the docker-volume example runs. The Docker image itself is built/run in a
# separate CI job (see .github/workflows/ci.yml) since it needs a daemon.
#
#   ./examples/smoke.sh          # or: make test-examples
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

pass() { printf '  ok: %s\n' "$1"; }
fail() { printf 'SMOKE FAIL: %s\n' "$1" >&2; exit 1; }
has()  { printf '%s' "$1" | grep -qF "$2"; }
ok()   { has "$1" "$2" || fail "$3 (missing: $2)"; }
no()   { has "$1" "$2" && fail "$3 (unexpected: $2)"; return 0; }

echo "== building mundane CLI =="
(cd "$ROOT/go" && go build -o mundane-bin ./cmd/mundane)
MUNDANE="$ROOT/go/mundane-bin"

# ---------------------------------------------------------------- ETL (TS)
echo "== etl-typescript =="
(cd "$ROOT/typescript" && npm install --silent && npx tsc -p .)  # file: dep packs dist/
cd "$ROOT/examples/etl-typescript" || exit 1
npm install --silent
npm run build --silent
db="$WORK/etl.db"
if CRASH_AT=transform-3 node dist/pipeline.js "$db" >/dev/null 2>&1; then
  fail "etl: run 1 should have crashed"
fi
out=$(node dist/pipeline.js "$db")
ok "$out" "done: loaded 5 rows" "etl: resume did not complete"
out=$(node dist/pipeline.js "$db")
ok "$out" "done: loaded 5 rows" "etl: cached run did not complete"
no "$out" "[ran]" "etl: cached run re-executed a step"
pass "etl-typescript"

# ------------------------------------------------------------ AI (Python)
echo "== ai-pipeline-python =="
cd "$ROOT/examples/ai-pipeline-python" || exit 1
export PYTHONPATH="$ROOT/python"
db="$WORK/ai.db"
if CRASH_AT=tradeoffs python3 pipeline.py "$db" >/dev/null 2>&1; then
  fail "ai: run 1 should have crashed"
fi
out=$(python3 pipeline.py "$db")
ok "$out" "assembled post" "ai: resume did not complete"
out=$(python3 pipeline.py "$db")
ok "$out" "assembled post" "ai: cached run did not complete"
no "$out" "model call" "ai: cached run made a fresh model call"
pass "ai-pipeline-python"

# -------------------------------------------------------- onboarding (Go)
echo "== onboarding-go =="
cd "$ROOT/examples/onboarding-go" || exit 1
go build -o /dev/null ./...   # compile check without leaving a binary
db="$WORK/drip.db"
out=$(DRIP_WAIT=50ms go run . "$db")
ok "$out" "drip complete" "onboarding: first run did not complete"
ok "$out" "Welcome to Acme" "onboarding: first run did not send welcome"
out=$(DRIP_WAIT=50ms go run . "$db")
ok "$out" "drip complete" "onboarding: cached run did not complete"
no "$out" "[email]" "onboarding: cached run re-sent mail"
pass "onboarding-go"

# ------------------------------------------ shell workflow (docker-volume)
echo "== docker-volume (shell workflow) =="
mkdir -p "$WORK/bin"
ln -sf "$MUNDANE" "$WORK/bin/mundane"   # the script calls `mundane`, not mundane-bin
db="$WORK/shell.db"
out=$(cd "$ROOT/examples/docker-volume" && PATH="$WORK/bin:$PATH" COOLDOWN=1s sh workflow.sh "$db")
ok "$out" "workflow complete" "shell: first run did not complete"
out=$(cd "$ROOT/examples/docker-volume" && PATH="$WORK/bin:$PATH" COOLDOWN=1s sh workflow.sh "$db")
ok "$out" "workflow complete" "shell: cached run did not complete"
pass "docker-volume (shell workflow)"

echo
echo "all example smoke tests passed"
