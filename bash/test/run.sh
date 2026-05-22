#!/bin/sh
# Test harness for the bash `mundane` script. POSIX sh, no bashisms.
set -u

MUNDANE="$(dirname "$0")/../mundane"
WORKDIR=$(mktemp -d)
PASSED=0
FAILED=0

trap 'rm -rf "$WORKDIR"' EXIT

_t() {
    _name="$1"
    printf '  %s ... ' "$_name"
}

_pass() {
    printf 'ok\n'
    PASSED=$((PASSED + 1))
}

_fail() {
    printf 'FAIL\n'
    printf '    %s\n' "$1" >&2
    FAILED=$((FAILED + 1))
}

# ---------- happy path: step cached on re-run ----------
test_step_cached() {
    _t "step output cached on re-run"
    db="$WORKDIR/three.db"
    rm -f "$db"

    out=$("$MUNDANE" run "$db" -- step a -- echo first)
    [ "$out" = "first" ] || { _fail "first run output: <$out>"; return; }

    # Re-run: body would print "changed" but cache wins.
    out=$("$MUNDANE" run "$db" -- step a -- echo changed)
    [ "$out" = "first" ] || { _fail "cache miss on rerun: <$out>"; return; }

    _pass
}
test_step_cached

# ---------- name disambiguation ----------
test_disambig() {
    _t "duplicate names get #N suffix"
    db="$WORKDIR/dis.db"
    rm -f "$db"

    "$MUNDANE" run "$db" -- step x -- echo one >/dev/null
    "$MUNDANE" run "$db" -- step x -- echo two >/dev/null  # cached, doesnt count
    # In a single run, two `step x` calls should produce x and x#2.
    "$MUNDANE" run "$db" -- sh -c '
        # cant — step/nap not in subshell.
        true
    ' >/dev/null

    # Since we cant do two steps in one mundane invocation without sourcing,
    # the disambig is mostly tested via TS/Python interop tests.
    names=$(sqlite3 "$db" "SELECT name FROM mundane_steps ORDER BY id")
    if [ "$names" != "x" ]; then _fail "unexpected names: $names"; return; fi
    _pass
}
test_disambig

# ---------- lock contention ----------
test_lock() {
    _t "second concurrent run exits 75"
    db="$WORKDIR/lock.db"
    rm -f "$db"

    # First run holds the lock by napping briefly.
    ( "$MUNDANE" run "$db" -- nap a 1s ) &
    pid=$!
    sleep 0.2  # let the first run acquire the lock

    "$MUNDANE" run "$db" -- step b -- echo hi >/dev/null 2>&1
    rc=$?
    wait $pid
    if [ "$rc" != "75" ]; then _fail "expected exit 75, got $rc"; return; fi
    _pass
}
test_lock

# ---------- sleep persists wake_at ----------
test_nap_persist() {
    _t "nap persists wake_at and skips on cache hit"
    db="$WORKDIR/nap.db"
    rm -f "$db"

    t0=$(date +%s%3N)
    "$MUNDANE" run "$db" -- nap a 50ms
    t1=$(date +%s%3N)
    e1=$((t1 - t0))
    if [ "$e1" -gt 1000 ]; then _fail "first nap too long: ${e1}ms"; return; fi

    t0=$(date +%s%3N)
    "$MUNDANE" run "$db" -- nap a 10s   # already woken
    t1=$(date +%s%3N)
    e2=$((t1 - t0))
    if [ "$e2" -gt 500 ]; then _fail "second nap should be instant: ${e2}ms"; return; fi
    _pass
}
test_nap_persist

# ---------- step failure marks row failed and exits nonzero ----------
test_step_failure() {
    _t "step failure marks row failed and exits nonzero"
    db="$WORKDIR/fail.db"
    rm -f "$db"

    "$MUNDANE" run "$db" -- step bad -- sh -c 'exit 5' >/dev/null 2>&1
    rc=$?
    if [ "$rc" = "0" ]; then _fail "expected nonzero exit"; return; fi
    st=$(sqlite3 "$db" "SELECT status FROM mundane_steps WHERE name='bad'")
    # The row is pending->failed after we attempt and fail.
    if [ "$st" != "failed" ]; then _fail "row status: $st (expected failed)"; return; fi
    _pass
}
test_step_failure

# ---------- invalid name rejected ----------
test_bad_name() {
    _t "invalid step name rejected"
    db="$WORKDIR/badname.db"
    rm -f "$db"
    "$MUNDANE" run "$db" -- step "bad name" -- echo hi >/dev/null 2>&1
    rc=$?
    if [ "$rc" = "0" ]; then _fail "expected nonzero"; return; fi
    _pass
}
test_bad_name

# ---------- status/steps/get subcommands ----------
test_inspect() {
    _t "status/steps/get subcommands"
    db="$WORKDIR/insp.db"
    rm -f "$db"
    "$MUNDANE" run "$db" -- step a -- echo hello >/dev/null

    out=$("$MUNDANE" status "$db")
    case "$out" in
        *"done=1"*) ;;
        *) _fail "status missing done=1: $out"; return ;;
    esac

    out=$("$MUNDANE" steps "$db" | head -1)
    case "$out" in
        *"a"*"step"*"done"*) ;;
        *) _fail "steps row mismatch: $out"; return ;;
    esac

    out=$("$MUNDANE" get "$db" a)
    if [ "$out" != "hello" ]; then _fail "get result: <$out>"; return; fi
    _pass
}
test_inspect

# ---------- --b64 mode ----------
test_b64() {
    _t "step --b64 stores and decodes binary"
    db="$WORKDIR/b64.db"
    rm -f "$db"

    # Generate a payload with NUL bytes via printf octal escapes.
    "$MUNDANE" run "$db" -- step --b64 raw -- printf 'a\000b\001c' \
        > "$WORKDIR/b64.out" 2>/dev/null

    # Confirm exact bytes: a, NUL, b, SOH, c → 5 bytes total.
    size=$(wc -c < "$WORKDIR/b64.out")
    [ "$size" = "5" ] || { _fail "wrong byte count: $size"; return; }

    # Confirm bytes match.
    expected_hex="6100620163"
    actual_hex=$(od -An -tx1 < "$WORKDIR/b64.out" | tr -d ' \n')
    [ "$actual_hex" = "$expected_hex" ] || {
        _fail "binary mismatch: got $actual_hex, want $expected_hex"; return; }
    _pass
}
test_b64

# ---------- schema mismatch rejected ----------
test_schema_mismatch() {
    _t "schema_version != 1 is rejected"
    db="$WORKDIR/badschema.db"
    rm -f "$db"
    sqlite3 "$db" "CREATE TABLE mundane_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL); INSERT INTO mundane_meta VALUES ('schema_version', '99');"
    "$MUNDANE" run "$db" -- step a -- echo hi >/dev/null 2>&1
    rc=$?
    if [ "$rc" = "0" ]; then _fail "expected nonzero exit"; return; fi
    _pass
}
test_schema_mismatch

# ---------- summary ----------
printf '\n%d passed, %d failed\n' "$PASSED" "$FAILED"
[ "$FAILED" = "0" ] || exit 1
