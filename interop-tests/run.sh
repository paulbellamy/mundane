#!/bin/sh
# Cross-runtime interop: Go (the bash CLI), Python, and TypeScript all read
# and write the same on-disk format.
set -u

REPO=$(cd "$(dirname "$0")/.." && pwd)
MUNDANE="${MUNDANE_BIN:-$REPO/go/mundane-bin}"
if [ ! -x "$MUNDANE" ]; then
    echo "error: mundane binary not found at $MUNDANE (build first or set MUNDANE_BIN)" >&2
    exit 1
fi
PYTHONPATH="$REPO/python"; export PYTHONPATH
NODE_CWD="$REPO/typescript"

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

PASSED=0
FAILED=0

_t() { printf '  %s ... ' "$1"; }
_pass() { printf 'ok\n'; PASSED=$((PASSED + 1)); }
_fail() { printf 'FAIL\n    %s\n' "$1" >&2; FAILED=$((FAILED + 1)); }

# ---------- shell (go-backed CLI) writes -> python reads ----------
test_sh_to_python() {
    _t "shell writes (Go CLI), python reads"
    db="$WORKDIR/sh2py.db"
    rm -f "$db"

    sh -c "eval \"\$('$MUNDANE' init '$db')\"
step greeting -- echo 'hello from shell'" >/dev/null

    out=$(python3 -m mundane get "$db" greeting)
    [ "$out" = "hello from shell" ] || { _fail "python read: <$out>"; return; }
    _pass
}
test_sh_to_python

# ---------- python writes -> shell reads ----------
test_python_to_sh() {
    _t "python writes, shell (Go CLI) reads"
    db="$WORKDIR/py2sh.db"
    rm -f "$db"

    python3 -c "
import mundane
def wf(ctx):
    ctx.step('a', lambda: {'x': 1, 'y': 'hello'})
mundane.run('$db', wf)
"
    out=$("$MUNDANE" get "$db" a)
    case "$out" in
        *'"x":'*'1'*) _pass ;;
        *) _fail "shell read of json: <$out>" ;;
    esac
}
test_python_to_sh

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

# ---------- mixed: shell + python + ts all add steps to same task ----------
test_three_way() {
    _t "shell + python + ts all add to same task"
    db="$WORKDIR/three_way.db"
    rm -f "$db"

    sh -c "eval \"\$('$MUNDANE' init '$db')\"
step step_sh -- echo 'from shell'" >/dev/null

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

    # Count rows via the Go CLI itself.
    count=$("$MUNDANE" steps "$db" | wc -l | tr -d ' ')
    [ "$count" = "3" ] || { _fail "expected 3 done steps, got $count"; return; }

    # All three runtimes share the task_id.
    tid_count=$("$MUNDANE" status "$db" | awk '{
        for (i=1; i<=NF; i++) if (index($i,"task_id=")==1) print substr($i,9)
    }' | sort -u | wc -l)
    [ "$tid_count" = "1" ] || { _fail "task_id divergence: $tid_count distinct"; return; }
    _pass
}
test_three_way

# ---------- locking interop: shell holds, python sees LockedError ----------
test_lock_sh_to_py() {
    _t "shell holds lock, python gets LockedError"
    db="$WORKDIR/lockint.db"
    rm -f "$db"

    (sh -c "eval \"\$('$MUNDANE' init '$db')\"
nap a 1s") &
    pid=$!
    sleep 0.2

    set +e
    out=$(python3 -c "
import mundane
try:
    mundane.run('$db', lambda ctx: None)
    print('NO_LOCK')
except mundane.LockedError:
    print('LOCKED')
" 2>&1)
    set -e
    wait $pid

    case "$out" in
        *LOCKED*) _pass ;;
        *) _fail "python didnt see lock: $out" ;;
    esac
}
test_lock_sh_to_py

# ---------- locking interop: python holds, ts sees ----------
test_lock_py_to_ts() {
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
test_lock_py_to_ts

# ---------- resume across runtimes ----------
test_resume_across_runtimes() {
    _t "step cached in python is read by shell on resume"
    db="$WORKDIR/resume.db"
    rm -f "$db"

    python3 -c "
import mundane
def wf(ctx):
    ctx.step('fetch', lambda: 'result-from-python')
mundane.run('$db', wf)
"

    # Shell second run: 'fetch' is cached as JSON \"result-from-python\".
    # `mundane get` decodes JSON, so we see the bare string.
    out=$("$MUNDANE" get "$db" fetch)
    case "$out" in
        *result-from-python*) _pass ;;
        *) _fail "shell got <$out>" ;;
    esac
}
test_resume_across_runtimes

printf '\n%d passed, %d failed\n' "$PASSED" "$FAILED"
[ "$FAILED" = "0" ] || exit 1
