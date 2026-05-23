#!/bin/sh
# Shell-level integration tests for the mundane CLI's eval-init pattern.
# Verifies the emitted init script, step/nap helpers, and the __step/__nap
# subprocess paths under a real POSIX sh.
set -u

MUNDANE="${MUNDANE_BIN:-$(dirname "$0")/../../go/mundane-bin}"
if [ ! -x "$MUNDANE" ]; then
    echo "error: mundane binary not found at $MUNDANE (build first or set MUNDANE_BIN)" >&2
    exit 1
fi

WORKDIR=$(mktemp -d)
PASSED=0
FAILED=0

trap 'rm -rf "$WORKDIR"' EXIT

_t() { printf '  %s ... ' "$1"; }
_pass() { printf 'ok\n'; PASSED=$((PASSED + 1)); }
_fail() { printf 'FAIL\n    %s\n' "$1" >&2; FAILED=$((FAILED + 1)); }

# ---------- happy path: step cached on re-run ----------
test_step_cached() {
    _t "step output cached on re-run"
    db="$WORKDIR/three.db"
    rm -f "$db"

    out=$(sh -c "eval \"\$('$MUNDANE' init '$db')\"
step a -- echo first")
    [ "$out" = "first" ] || { _fail "first run output: <$out>"; return; }

    # Re-run: body would print "changed" but cache wins.
    out=$(sh -c "eval \"\$('$MUNDANE' init '$db')\"
step a -- echo changed")
    [ "$out" = "first" ] || { _fail "cache miss on rerun: <$out>"; return; }

    _pass
}
test_step_cached

# ---------- duplicate name raises ----------
test_duplicate_name() {
    _t "duplicate step name in one body exits 2"
    db="$WORKDIR/dup.db"
    rm -f "$db"
    set +e
    sh -c "eval \"\$('$MUNDANE' init '$db')\"
step x -- echo a
step x -- echo b" >/dev/null 2>&1
    rc=$?
    set -e
    [ "$rc" = "2" ] || { _fail "expected exit 2, got $rc"; return; }
    _pass
}
test_duplicate_name

# ---------- lock contention ----------
test_lock() {
    _t "second concurrent init exits 75"
    db="$WORKDIR/lock.db"
    rm -f "$db"

    (sh -c "eval \"\$('$MUNDANE' init '$db')\"
nap a 1s") &
    pid=$!
    sleep 0.2

    set +e
    sh -c "eval \"\$('$MUNDANE' init '$db')\"" >/dev/null 2>&1
    rc=$?
    set -e
    wait $pid
    [ "$rc" = "75" ] || { _fail "expected exit 75, got $rc"; return; }
    _pass
}
test_lock

# ---------- nap persists wake_at, resumes near-instantly ----------
test_nap_persist() {
    _t "nap persists wake_at; second run is instant"
    db="$WORKDIR/nap.db"
    rm -f "$db"

    sh -c "eval \"\$('$MUNDANE' init '$db')\"
nap a 50ms"

    t0=$(date +%s)
    sh -c "eval \"\$('$MUNDANE' init '$db')\"
nap a 10s"
    t1=$(date +%s)
    e=$((t1 - t0))
    [ "$e" -lt 2 ] || { _fail "second nap should be near-instant: ${e}s"; return; }
    _pass
}
test_nap_persist

# ---------- step failure marks row failed and exits with cmd code ----------
test_step_failure() {
    _t "step failure marks row failed and propagates exit code"
    db="$WORKDIR/fail.db"
    rm -f "$db"

    set +e
    sh -c "eval \"\$('$MUNDANE' init '$db')\"
step bad -- sh -c 'exit 5'" >/dev/null 2>&1
    rc=$?
    set -e
    [ "$rc" = "5" ] || { _fail "expected exit 5, got $rc"; return; }

    # Inspect: row should be 'failed'.
    st=$("$MUNDANE" steps "$db" | awk '/^[0-9]+/ && $2=="bad" { print $4 }')
    [ "$st" = "failed" ] || { _fail "row status: <$st> expected failed"; return; }
    _pass
}
test_step_failure

# ---------- invalid name rejected ----------
test_bad_name() {
    _t "invalid step name rejected"
    db="$WORKDIR/badname.db"
    rm -f "$db"
    set +e
    sh -c "eval \"\$('$MUNDANE' init '$db')\"
step 'bad name' -- echo hi" >/dev/null 2>&1
    rc=$?
    set -e
    [ "$rc" = "2" ] || { _fail "expected exit 2, got $rc"; return; }
    _pass
}
test_bad_name

# ---------- status/steps/get subcommands ----------
test_inspect() {
    _t "status/steps/get subcommands"
    db="$WORKDIR/insp.db"
    rm -f "$db"
    sh -c "eval \"\$('$MUNDANE' init '$db')\"
step a -- echo hello" >/dev/null

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
    [ "$out" = "hello" ] || { _fail "get result: <$out>"; return; }
    _pass
}
test_inspect

# ---------- binary stdout (NUL bytes) round-trips as raw bytes ----------
test_binary() {
    _t "step stores binary stdout (NUL bytes) verbatim"
    db="$WORKDIR/bin.db"
    rm -f "$db"

    sh -c "eval \"\$('$MUNDANE' init '$db')\"
step raw -- printf 'a\\000b\\001c'" > "$WORKDIR/bin.out" 2>/dev/null

    size=$(wc -c < "$WORKDIR/bin.out")
    [ "$size" = "5" ] || { _fail "wrong byte count: $size"; return; }

    expected_hex="6100620163"
    actual_hex=$(od -An -tx1 < "$WORKDIR/bin.out" | tr -d ' \n')
    [ "$actual_hex" = "$expected_hex" ] || {
        _fail "binary mismatch: got $actual_hex, want $expected_hex"; return; }
    _pass
}
test_binary

# ---------- schema mismatch rejected ----------
test_schema_mismatch() {
    _t "schema_version != 1 is rejected"
    db="$WORKDIR/badschema.db"
    rm -f "$db"
    # Use Python's sqlite3 module to seed (no sqlite3 CLI required).
    python3 -c "
import sqlite3
c = sqlite3.connect('$db')
c.execute('CREATE TABLE mundane_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)')
c.execute(\"INSERT INTO mundane_meta VALUES ('schema_version', '99')\")
c.commit(); c.close()
"
    set +e
    sh -c "eval \"\$('$MUNDANE' init '$db')\"" >/dev/null 2>&1
    rc=$?
    set -e
    [ "$rc" = "2" ] || { _fail "expected exit 2, got $rc"; return; }
    _pass
}
test_schema_mismatch

# ---------- summary ----------
printf '\n%d passed, %d failed\n' "$PASSED" "$FAILED"
[ "$FAILED" = "0" ] || exit 1
