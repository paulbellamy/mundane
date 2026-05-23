"""Conformance driver (Python). See conformance/run.py.

Usage: python3 driver.py <task.db> <scenario.json>
"""

import json
import sys

import mundane


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: driver.py <task.db> <scenario.json>", file=sys.stderr)
        return 2
    db_path, scn_path = sys.argv[1], sys.argv[2]
    with open(scn_path) as f:
        scenario = json.load(f)

    def wf(ctx):
        for op in scenario["operations"]:
            if op["op"] == "step":
                value = op.get("value")
                ctx.step(op["name"], lambda value=value: value)
            elif op["op"] == "sleep":
                ctx.sleep(op["name"], op["duration"])
            else:
                raise ValueError(f"unknown op: {op['op']}")

    mundane.run(db_path, wf)
    return 0


if __name__ == "__main__":
    sys.exit(main())
