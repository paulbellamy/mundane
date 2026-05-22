"""CLI entrypoint: `python -m mundane <subcommand> ...`."""

import json
import sys
from typing import List

from . import inspect


def _usage() -> int:
    print(
        "usage: python -m mundane <subcommand> [args]\n"
        "  status <task.db>\n"
        "  steps  <task.db>\n"
        "  get    <task.db> <name>",
        file=sys.stderr,
    )
    return 2


def main(argv: List[str]) -> int:
    if len(argv) < 2:
        return _usage()
    sub = argv[1]
    if sub == "status":
        if len(argv) != 3:
            return _usage()
        st = inspect.status(argv[2])
        print(
            f"task_id={st['task_id']}  steps={st['total_steps']}  "
            f"done={st['done']}  pending={st['pending']}  failed={st['failed']}"
        )
        return 0
    if sub == "steps":
        if len(argv) != 3:
            return _usage()
        rows = inspect.steps(argv[2])
        print(f"{'id':<4} {'name':<30} {'kind':<6} {'status':<8} {'started_at':<28} {'finished_at'}")
        for r in rows:
            print(
                f"{r['id']:<4} {r['name']:<30} {r['kind']:<6} {r['status']:<8} "
                f"{(r['started_at'] or ''):<28} {r['finished_at'] or ''}"
            )
        return 0
    if sub == "get":
        if len(argv) != 4:
            return _usage()
        val = inspect.get_result(argv[2], argv[3])
        if isinstance(val, bytes):
            sys.stdout.buffer.write(val)
        elif isinstance(val, str):
            sys.stdout.write(val)
        else:
            json.dump(val, sys.stdout)
            sys.stdout.write("\n")
        return 0
    return _usage()


if __name__ == "__main__":
    sys.exit(main(sys.argv))
