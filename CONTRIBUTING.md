# Contributing to mundane

Thanks for your interest in contributing. mundane ships four runtimes — a Go
CLI/SDK, a Go library, a Python SDK, and a TypeScript SDK — that all read and
write the same on-disk SQLite format. The single most important rule is that
**every runtime stays conformant to [`SPEC.md`](./SPEC.md)**: a workflow file
written by one runtime must resume identically under any other.

## Development setup

You need Go 1.24+, Python 3.8+, and Node 18+ installed.

```sh
make build      # build the Go binaries and compile TypeScript
make test       # go + shell integration + python + typescript + conformance
make lint       # shellcheck + ruff + biome + go vet
```

You can also run a single runtime's suite directly:

```sh
cd go && go test ./...                            # Go SDK
./bash/test/run.sh                                # shell integration (needs the CLI built)
cd python && python3 -m unittest tests.test_basic # Python
cd typescript && npm install \
  && npx tsc -p . && node --test dist/test/       # TypeScript
python3 conformance/run.py                         # cross-runtime conformance
```

## The conformance harness

[`conformance/`](./conformance/) is the shared cross-runtime contract.
Scenarios in [`conformance/scenarios/`](./conformance/scenarios) are replayed by
a thin per-runtime driver and verified through the `mundane` CLI, so every
runtime is held to the same on-disk behavior. If you change behavior in one
runtime, you almost always need to change it in all four and keep the
conformance suite green.

## Submitting changes

1. Open an issue first for anything beyond a small fix, so we can agree on the
   approach before you write code.
2. Keep changes focused; a PR should do one thing.
3. Make sure `make test` and `make lint` both pass locally.
4. If you change the on-disk format or public API, update
   [`SPEC.md`](./SPEC.md) and all four runtimes in the same PR.
5. Add or update tests, including a conformance scenario when behavior changes.

## Reporting bugs

Open an issue with the runtime(s) affected, a minimal reproduction, and the
expected vs. actual behavior. For security issues, see
[`SECURITY.md`](./SECURITY.md) instead — please do not file public issues for
vulnerabilities.
