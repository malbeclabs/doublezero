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
	cd borsh-incremental/typescript && bun install && bun tsc --noEmit && bun test
	cd revdist/typescript && bun install && bun tsc --noEmit && bun test
	cd serviceability/typescript && bun install && bun tsc --noEmit && bun test
	cd telemetry/typescript && bun install && bun tsc --noEmit && bun test

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
	cd revdist/typescript && bun install && REVDIST_COMPAT_TEST=1 bun test --grep compat

.PHONY: compat-test-python-serviceability
compat-test-python-serviceability:
	cd serviceability/python && SERVICEABILITY_COMPAT_TEST=1 uv run pytest -k compat -v

.PHONY: compat-test-typescript-serviceability
compat-test-typescript-serviceability:
	cd serviceability/typescript && bun install && SERVICEABILITY_COMPAT_TEST=1 bun test --grep compat

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
	cd serviceability/typescript && bun run examples/fetch.ts --env $(sdk_env)

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
	cd telemetry/typescript && bun run examples/fetch.ts --env $(sdk_env)

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
	cd revdist/typescript && bun run examples/fetch.ts --env $(sdk_env) --epoch $(sdk_epoch)

# -----------------------------------------------------------------------------
# Run all examples for a specific language
# -----------------------------------------------------------------------------
.PHONY: examples-go
examples-go: example-serviceability-go example-telemetry-go example-revdist-go

.PHONY: examples-python
examples-python: example-serviceability-python example-telemetry-python example-revdist-python

.PHONY: examples-typescript
examples-typescript: example-serviceability-typescript example-telemetry-typescript example-revdist-typescript
