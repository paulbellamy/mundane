.PHONY: test test-go test-sh test-python test-ts test-interop build build-go build-ts \
        lint lint-sh lint-python lint-ts lint-go clean

MUNDANE_BIN := go/mundane-bin
export MUNDANE_BIN

test: build test-go test-sh test-python test-ts test-interop

lint: lint-sh lint-python lint-ts lint-go

build: build-go build-ts

build-go:
	@cd go && go build -o mundane-bin ./cmd/mundane

build-ts:
	@cd typescript && [ -d node_modules ] || npm install --silent
	@cd typescript && npx tsc -p .

lint-sh:
	@echo "=== shellcheck ==="
	@shellcheck -s sh bash/test/run.sh interop-tests/run.sh

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

test-interop: build
	@echo "=== interop ==="
	@./interop-tests/run.sh

clean:
	rm -rf typescript/dist typescript/node_modules go/mundane-bin
	find . -name '__pycache__' -type d -exec rm -rf {} +
