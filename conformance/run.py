#!/usr/bin/env python3
"""Shared conformance harness.

Scenarios (conformance/scenarios/*.json) declare a sequence of operations and
the expected on-disk state. One thin driver per runtime replays a scenario
against a fresh task DB; this harness then inspects the result *through the
mundane CLI* (the single sanctioned inspection surface) and asserts every
runtime produced the expected state.

Two checks per scenario:
  1. correctness  — each driver alone produces the expected state.
  2. interop      — chaining all drivers on one DB leaves the state stable
                    (each later driver cache-hits what the earlier wrote),
                    which exercises cross-runtime resume + row-for-row interop.

Inspection uses `mundane steps` and `mundane get`; no runtime wraps inspect.
"""

import json
import os
import re
import subprocess
import sys
import tempfile

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MUNDANE = os.environ.get("MUNDANE_BIN", os.path.join(REPO, "go", "mundane-bin"))
GO_DRIVER = os.path.join(REPO, "go", "conformance-driver")
DRIVERS_DIR = os.path.join(REPO, "conformance", "drivers")
SCENARIOS_DIR = os.path.join(REPO, "conformance", "scenarios")

DRIVERS = {
    "go": [GO_DRIVER],
    "python": [sys.executable, os.path.join(DRIVERS_DIR, "driver.py")],
    "ts": ["node", os.path.join(DRIVERS_DIR, "driver.js")],
}

PY_ENV = dict(os.environ, PYTHONPATH=os.path.join(REPO, "python"))


def canon(text: str) -> str:
    return json.dumps(json.loads(text), sort_keys=True)


def parse_steps(db: str) -> dict:
    out = subprocess.run(
        [MUNDANE, "steps", db], capture_output=True, text=True, check=True
    ).stdout
    rows = {}
    for line in out.splitlines():
        if not line.strip():
            continue
        # id  name  kind  status  started_at  finished_at
        fields = re.split(r" {2,}", line.strip())
        if len(fields) < 4:
            continue
        _id, name, kind, status = fields[0], fields[1], fields[2], fields[3]
        rows[name] = (kind, status)
    return rows


def get_value(db: str, name: str) -> str:
    return subprocess.run(
        [MUNDANE, "get", db, name], capture_output=True, text=True, check=True
    ).stdout


def verify(db: str, expect: list) -> None:
    rows = parse_steps(db)
    if len(rows) != len(expect):
        raise AssertionError(f"step count {len(rows)} != expected {len(expect)}: {rows}")
    for e in expect:
        name = e["name"]
        if name not in rows:
            raise AssertionError(f"missing step {name!r}")
        kind, status = rows[name]
        if kind != e["kind"] or status != e["status"]:
            raise AssertionError(
                f"step {name!r}: got ({kind},{status}), want ({e['kind']},{e['status']})"
            )
        if "value" in e:
            got = canon(get_value(db, name))
            want = canon(json.dumps(e["value"]))
            if got != want:
                raise AssertionError(f"step {name!r}: value {got} != {want}")


def run_driver(name: str, db: str, scenario_path: str) -> None:
    cmd = DRIVERS[name] + [db, scenario_path]
    env = PY_ENV if name == "python" else os.environ
    r = subprocess.run(cmd, capture_output=True, text=True, env=env)
    if r.returncode != 0:
        raise AssertionError(f"driver {name} failed ({r.returncode}): {r.stderr.strip()}")


def fresh_db() -> str:
    fd, path = tempfile.mkstemp(suffix=".db")
    os.close(fd)
    os.remove(path)
    return path


def main() -> int:
    if not os.path.exists(MUNDANE):
        print(f"error: mundane binary not found at {MUNDANE}", file=sys.stderr)
        return 1
    if not os.path.exists(GO_DRIVER):
        print(f"error: go conformance driver not found at {GO_DRIVER}", file=sys.stderr)
        return 1

    scenarios = sorted(
        f for f in os.listdir(SCENARIOS_DIR) if f.endswith(".json")
    )
    passed = failed = 0

    for fname in scenarios:
        path = os.path.join(SCENARIOS_DIR, fname)
        with open(path) as f:
            scenario = json.load(f)
        expect = scenario["expect"]

        # 1. correctness: each driver alone.
        for driver in DRIVERS:
            label = f"{fname}: {driver} alone"
            print(f"  {label} ... ", end="")
            db = fresh_db()
            try:
                run_driver(driver, db, path)
                verify(db, expect)
                print("ok")
                passed += 1
            except AssertionError as e:
                print(f"FAIL\n    {e}")
                failed += 1
            finally:
                if os.path.exists(db):
                    os.remove(db)

        # 2. interop + resume: chain all drivers on one DB.
        label = f"{fname}: interop chain (go -> python -> ts)"
        print(f"  {label} ... ", end="")
        db = fresh_db()
        try:
            for driver in ("go", "python", "ts"):
                run_driver(driver, db, path)
                verify(db, expect)  # state stable after each resume
            print("ok")
            passed += 1
        except AssertionError as e:
            print(f"FAIL\n    {e}")
            failed += 1
        finally:
            if os.path.exists(db):
                os.remove(db)

    print(f"\n{passed} passed, {failed} failed")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
