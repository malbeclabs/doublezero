# SDK Makefile - targets for running examples and tests

# Variables with defaults
sdk_env ?= mainnet-beta
sdk_epoch ?= 0

# -----------------------------------------------------------------------------
# Test targets
# -----------------------------------------------------------------------------
.PHONY: test
test:
	go test ./borsh-incremental/go/...
	go test ./revdist/go/...
	go test ./serviceability/go/...
	go test ./telemetry/go/...
	$(MAKE) test-python
	$(MAKE) test-typescript

.PHONY: test-python
test-python:
	cd borsh-incremental/python && uv run pytest
	cd revdist/python && uv run pytest
	cd serviceability/python && uv run pytest
	cd telemetry/python && uv run pytest

.PHONY: test-typescript
test-typescript:
	bun install
	bunx tsc --noEmit --project borsh-incremental/typescript/tsconfig.json
	bun test --cwd borsh-incremental/typescript
	bunx tsc --noEmit --project revdist/typescript/tsconfig.json
	bun test --cwd revdist/typescript
	bunx tsc --noEmit --project serviceability/typescript/tsconfig.json
	bun test --cwd serviceability/typescript
	bunx tsc --noEmit --project telemetry/typescript/tsconfig.json
	bun test --cwd telemetry/typescript

# -----------------------------------------------------------------------------
# Compat test targets (require network access)
# -----------------------------------------------------------------------------
.PHONY: compat-test
compat-test:
	REVDIST_COMPAT_TEST=1 go test -run TestCompat -v ./revdist/go/...
	$(MAKE) compat-test-python-revdist
	$(MAKE) compat-test-typescript-revdist
	SERVICEABILITY_COMPAT_TEST=1 go test -run TestCompat -v ./serviceability/go/...
	$(MAKE) compat-test-python-serviceability
	$(MAKE) compat-test-typescript-serviceability

.PHONY: compat-test-python-revdist
compat-test-python-revdist:
	cd revdist/python && REVDIST_COMPAT_TEST=1 uv run pytest -k compat -v

.PHONY: compat-test-typescript-revdist
compat-test-typescript-revdist:
	bun install
	REVDIST_COMPAT_TEST=1 bun test --cwd revdist/typescript --grep compat

.PHONY: compat-test-python-serviceability
compat-test-python-serviceability:
	cd serviceability/python && SERVICEABILITY_COMPAT_TEST=1 uv run pytest -k compat -v

.PHONY: compat-test-typescript-serviceability
compat-test-typescript-serviceability:
	bun install
	SERVICEABILITY_COMPAT_TEST=1 bun test --cwd serviceability/typescript --grep compat

# -----------------------------------------------------------------------------
# Serviceability examples
# -----------------------------------------------------------------------------
.PHONY: example-serviceability-go
example-serviceability-go:
	go run ./serviceability/go/examples/fetch --env $(sdk_env)

.PHONY: example-serviceability-python
example-serviceability-python:
	cd serviceability/python && uv run python examples/fetch.py --env $(sdk_env)

.PHONY: example-serviceability-typescript
example-serviceability-typescript:
	bun run serviceability/typescript/examples/fetch.ts --env $(sdk_env)

# -----------------------------------------------------------------------------
# Telemetry examples
# -----------------------------------------------------------------------------
.PHONY: example-telemetry-go
example-telemetry-go:
	go run ./telemetry/go/examples/fetch --env $(sdk_env) --epoch $(sdk_epoch)

.PHONY: example-telemetry-python
example-telemetry-python:
	cd telemetry/python && uv run python examples/fetch.py --env $(sdk_env) --epoch $(sdk_epoch)

.PHONY: example-telemetry-typescript
example-telemetry-typescript:
	bun run telemetry/typescript/examples/fetch.ts --env $(sdk_env)

# -----------------------------------------------------------------------------
# Revdist examples
# -----------------------------------------------------------------------------
.PHONY: example-revdist-go
example-revdist-go:
	go run ./revdist/go/examples/fetch --env $(sdk_env) --epoch $(sdk_epoch)

.PHONY: example-revdist-python
example-revdist-python:
	cd revdist/python && uv run python examples/fetch.py --env $(sdk_env) --epoch $(sdk_epoch)

.PHONY: example-revdist-typescript
example-revdist-typescript:
	bun run revdist/typescript/examples/fetch.ts --env $(sdk_env) --epoch $(sdk_epoch)

# -----------------------------------------------------------------------------
# Run all examples for a specific language
# -----------------------------------------------------------------------------
.PHONY: examples-go
examples-go: example-serviceability-go example-telemetry-go example-revdist-go

.PHONY: examples-python
examples-python: example-serviceability-python example-telemetry-python example-revdist-python

.PHONY: examples-typescript
examples-typescript: example-serviceability-typescript example-telemetry-typescript example-revdist-typescript

# -----------------------------------------------------------------------------
# Version management
# -----------------------------------------------------------------------------
.PHONY: clean
clean:
	rm -rf */typescript/dist */python/dist

.PHONY: bump-version
bump-version:
ifndef version
	$(error version is required. Usage: make bump-version version=0.0.4)
endif
	@echo "Bumping all SDK packages to version $(version)..."
	@for pkg in borsh-incremental serviceability revdist telemetry; do \
		sed -i '' 's/^version = ".*"/version = "$(version)"/' $$pkg/python/pyproject.toml; \
		sed -i '' 's/"version": ".*"/"version": "$(version)"/' $$pkg/typescript/package.json; \
	done
	@echo "Done. New versions:"
	@$(MAKE) --no-print-directory show-versions

.PHONY: show-versions
show-versions:
	@for pkg in borsh-incremental serviceability revdist telemetry; do \
		py_ver=$$(grep '^version = ' $$pkg/python/pyproject.toml | sed 's/version = "//;s/"//'); \
		ts_ver=$$(grep '"version":' $$pkg/typescript/package.json | head -1 | sed 's/.*"version": "//;s/".*//' | tr -d ' '); \
		echo "$$pkg: Python=$$py_ver TypeScript=$$ts_ver"; \
	done

# -----------------------------------------------------------------------------
# Publishing
# -----------------------------------------------------------------------------
.PHONY: publish
publish: publish-python publish-typescript

.PHONY: publish-python
publish-python:
	@echo "Publishing Python packages to PyPI..."
	cd borsh-incremental/python && rm -rf dist && uv build && uv publish
	cd serviceability/python && rm -rf dist && uv build && uv publish
	cd revdist/python && rm -rf dist && uv build && uv publish
	cd telemetry/python && rm -rf dist && uv build && uv publish
	@echo "Done publishing Python packages."

.PHONY: publish-typescript
publish-typescript:
	@echo "Publishing TypeScript packages to npm..."
	cd borsh-incremental/typescript && rm -rf dist && bunx tsc && npm publish --access public
	cd serviceability/typescript && rm -rf dist && bunx tsc && npm publish --access public
	cd revdist/typescript && rm -rf dist && bunx tsc && npm publish --access public
	cd telemetry/typescript && rm -rf dist && bunx tsc && npm publish --access public
	@echo "Done publishing TypeScript packages."
