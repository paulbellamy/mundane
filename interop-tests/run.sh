#!/bin/sh
# Cross-runtime interop tests: bash, TS, and Python all read/write the same file.
set -u

REPO=$(cd "$(dirname "$0")/.." && pwd)
MUNDANE_BASH="$REPO/bash/mundane"
PYTHONPATH="$REPO/python"; export PYTHONPATH
NODE_CWD="$REPO/typescript"

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

PASSED=0
FAILED=0

_t() { printf '  %s ... ' "$1"; }
_pass() { printf 'ok\n'; PASSED=$((PASSED + 1)); }
_fail() { printf 'FAIL\n    %s\n' "$1" >&2; FAILED=$((FAILED + 1)); }

# ---------- bash writes -> python reads ----------
test_bash_to_python() {
    _t "bash writes, python reads"
    db="$WORKDIR/b2py.db"
    rm -f "$db"

    "$MUNDANE_BASH" run "$db" -- step greeting -- echo "hello from bash" >/dev/null

    out=$(python3 -m mundane get "$db" greeting)
    [ "$out" = "hello from bash" ] || { _fail "python read: <$out>"; return; }
    _pass
}
test_bash_to_python

# ---------- python writes -> bash reads ----------
test_python_to_bash() {
    _t "python writes, bash reads"
    db="$WORKDIR/py2b.db"
    rm -f "$db"

    python3 -c "
import mundane
def wf(ctx):
    ctx.step('a', lambda: {'x': 1, 'y': 'hello'})
mundane.run('$db', wf)
"
    # bash mundane get returns the stored text. For JSON, that's the JSON text.
    out=$("$MUNDANE_BASH" get "$db" a)
    case "$out" in
        *'"x":'*'1'*) ;;
        *) _fail "bash read of json: <$out>"; return ;;
    esac
    _pass
}
test_python_to_bash

# ---------- ts writes -> python reads ----------
test_ts_to_python() {
    _t "ts writes, python reads"
    db="$WORKDIR/ts2py.db"
    rm -f "$db"

    cd "$NODE_CWD"
    node -e "
const { run } = require('./dist/src/index');
(async () => {
  await run('$db', async (ctx) => {
    await ctx.step('greeting', async () => ({ msg: 'hello from ts', n: 42 }));
  });
})();
" >/dev/null
    cd - >/dev/null

    val=$(python3 -c "
import mundane
v = mundane.get_result('$db', 'greeting')
print(v.get('msg'), v.get('n'))
")
    [ "$val" = "hello from ts 42" ] || { _fail "python decoded: <$val>"; return; }
    _pass
}
test_ts_to_python

# ---------- python writes -> ts reads ----------
test_python_to_ts() {
    _t "python writes, ts reads"
    db="$WORKDIR/py2ts.db"
    rm -f "$db"

    python3 -c "
import mundane
def wf(ctx):
    ctx.step('payload', lambda: [1, 2, 3, {'k': 'v'}])
mundane.run('$db', wf)
"
    cd "$NODE_CWD"
    out=$(node -e "
const { getResult } = require('./dist/src/inspect');
const v = getResult('$db', 'payload');
console.log(JSON.stringify(v));
")
    cd - >/dev/null
    [ "$out" = '[1,2,3,{"k":"v"}]' ] || { _fail "ts decoded: <$out>"; return; }
    _pass
}
test_python_to_ts

# ---------- mixed: bash + python + ts all add steps to same task ----------
test_three_way_resume() {
    _t "bash + python + ts all add to same task"
    db="$WORKDIR/three_way.db"
    rm -f "$db"

    "$MUNDANE_BASH" run "$db" -- step step_bash -- echo "from bash" >/dev/null

    python3 -c "
import mundane
def wf(ctx):
    ctx.step('step_py', lambda: 'from python')
mundane.run('$db', wf)
"

    cd "$NODE_CWD"
    node -e "
const { run } = require('./dist/src/index');
(async () => {
  await run('$db', async (ctx) => {
    await ctx.step('step_ts', async () => 'from ts');
  });
})();
" >/dev/null
    cd - >/dev/null

    count=$(sqlite3 "$db" "SELECT COUNT(*) FROM mundane_steps WHERE status='done'")
    [ "$count" = "3" ] || { _fail "expected 3 done steps, got $count"; return; }

    # All three runtimes use the same task_id.
    tid_count=$(sqlite3 "$db" "SELECT COUNT(DISTINCT value) FROM mundane_meta WHERE key='task_id'")
    [ "$tid_count" = "1" ] || { _fail "task_id divergence: $tid_count distinct"; return; }
    _pass
}
test_three_way_resume

# ---------- locking interop: bash holds lock, python sees LockedError ----------
test_lock_interop() {
    _t "bash holds lock, python gets LockedError"
    db="$WORKDIR/lockint.db"
    rm -f "$db"

    ( "$MUNDANE_BASH" run "$db" -- nap a 1s ) &
    pid=$!
    sleep 0.2

    set +e
    python3 -c "
import mundane, sys
try:
    mundane.run('$db', lambda ctx: None)
    print('NO_LOCK')
except mundane.LockedError:
    print('LOCKED')
" > "$WORKDIR/out.txt" 2>&1
    set -e
    wait $pid

    grep -q LOCKED "$WORKDIR/out.txt" || { _fail "python didnt see lock: $(cat $WORKDIR/out.txt)"; return; }
    _pass
}
test_lock_interop

# ---------- locking interop: python holds, ts sees ----------
test_lock_interop_py_ts() {
    _t "python holds lock, ts gets MundaneLockedError"
    db="$WORKDIR/lockpyts.db"
    rm -f "$db"

    python3 -c "
import time
import mundane
def wf(ctx):
    ctx.step('a', lambda: 0)
    time.sleep(1)
mundane.run('$db', wf)
" &
    pid=$!
    sleep 0.3

    cd "$NODE_CWD"
    set +e
    out=$(node -e "
const { run, MundaneLockedError } = require('./dist/src/index');
(async () => {
  try {
    await run('$db', async () => {});
    console.log('NO_LOCK');
  } catch (e) {
    if (e instanceof MundaneLockedError) console.log('LOCKED');
    else console.log('OTHER', e.message);
  }
})();
" 2>&1)
    set -e
    cd - >/dev/null
    wait $pid

    case "$out" in
        *LOCKED*) _pass ;;
        *) _fail "ts didnt see lock: $out" ;;
    esac
}
test_lock_interop_py_ts

# ---------- resume across runtimes ----------
test_resume_across_runtimes() {
    _t "step cached in python is read by bash on resume"
    db="$WORKDIR/resume.db"
    rm -f "$db"

    python3 -c "
import mundane
def wf(ctx):
    ctx.step('fetch', lambda: 'result-from-python')
mundane.run('$db', wf)
"

    # bash second run with a different command for the same step name —
    # cache hit prints the python-stored value.
    out=$("$MUNDANE_BASH" run "$db" -- step fetch -- echo "this should NOT run")
    [ "$out" = "result-from-python" ] || { _fail "bash got <$out>"; return; }
    _pass
}
test_resume_across_runtimes

printf '\n%d passed, %d failed\n' "$PASSED" "$FAILED"
[ "$FAILED" = "0" ] || exit 1
