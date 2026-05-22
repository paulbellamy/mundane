.PHONY: test test-bash test-python test-ts test-interop build-ts clean

test: test-bash test-python test-ts test-interop

test-bash:
	@echo "=== bash ==="
	@./bash/test/run.sh

test-python:
	@echo "=== python ==="
	@cd python && python3 -m unittest tests.test_basic -v

test-ts: build-ts
	@echo "=== typescript ==="
	@cd typescript && node --test dist/test/basic.test.js

test-interop: build-ts
	@echo "=== interop ==="
	@./interop-tests/run.sh

build-ts:
	@cd typescript && [ -d node_modules ] || npm install --silent
	@cd typescript && npx tsc -p .

clean:
	rm -rf typescript/dist typescript/node_modules
	find . -name '__pycache__' -type d -exec rm -rf {} +
