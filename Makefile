.PHONY: test test-go test-sh test-python test-ts test-conformance test-examples \
        build build-go build-ts \
        lint lint-sh lint-python lint-ts lint-go clean

MUNDANE_BIN := go/mundane-bin
export MUNDANE_BIN

test: build test-go test-sh test-python test-ts test-conformance

lint: lint-sh lint-python lint-ts lint-go

build: build-go build-ts

build-go:
	@cd go && go build -o mundane-bin ./cmd/mundane
	@cd go && go build -o conformance-driver ./cmd/conformance

build-ts:
	@cd typescript && [ -d node_modules ] || npm install --silent
	@cd typescript && npx tsc -p .

lint-sh:
	@echo "=== shellcheck ==="
	@shellcheck -s sh install.sh bash/test/run.sh examples/smoke.sh examples/docker-volume/workflow.sh

lint-python:
	@echo "=== ruff ==="
	@cd python && ruff check .

lint-ts: build-ts
	@echo "=== biome ==="
	@cd typescript && npx biome check src/ test/

lint-go:
	@echo "=== go vet ==="
	@cd go && go vet ./...

test-go: build-go
	@echo "=== go ==="
	@cd go && go test ./...

test-sh: build-go
	@echo "=== sh integration ==="
	@./bash/test/run.sh

test-python:
	@echo "=== python ==="
	@cd python && python3 -m unittest tests.test_basic -v

test-ts: build-ts
	@echo "=== typescript ==="
	@cd typescript && node --test dist/test/basic.test.js

test-conformance: build
	@echo "=== conformance (shared harness) ==="
	@python3 conformance/run.py

test-examples: build-go
	@echo "=== examples (smoke) ==="
	@./examples/smoke.sh

clean:
	rm -rf typescript/dist typescript/node_modules go/mundane-bin go/conformance-driver
	find . -name '__pycache__' -type d -exec rm -rf {} +
