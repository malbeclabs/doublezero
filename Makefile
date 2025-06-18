# -----------------------------------------------------------------------------
# Combined targets
# -----------------------------------------------------------------------------
.PHONY: build
build: go-build rust-build

.PHONY: lint
lint: go-lint rust-lint

.PHONY: fmt
fmt: rust-fmt go-fmt

.PHONY: test
test: go-test rust-test

.PHONY: ci
ci: build lint test


# -----------------------------------------------------------------------------
# Go targets
# -----------------------------------------------------------------------------
.PHONY: go-build
go-build:
	CGO_ENABLED=0 go build -v ./...

.PHONY: go-lint
go-lint:
	golangci-lint run -c ./.golangci.yaml

.PHONY: go-fmt
go-fmt:
	go fmt ./...

.PHONY: go-test
go-test:
	go test -exec "sudo -E" -race -v ./...
	$(if $(findstring nocontainertest,$(MAKECMDGOALS)),,$(MAKE) go-container-test)

.PHONY: nocontainertest
nocontainertest:
	@:

.PHONY: go-fuzz
go-fuzz:
	cd tools/twamp && $(MAKE) fuzz

# NOTE: The naming of `tools/e2e-test` is confusing. It's not running the `./e2e` tests, but rather
# the containerized tests in `client/doublezerod`. This package should be renamed to something
# like `tools/container-test` at some point.
.PHONY: go-container-test
go-container-test:
	go run tools/e2e-test/main.go -v

.PHONY: go-ci
go-ci: go-build go-lint go-test go-fuzz


# -----------------------------------------------------------------------------
# Rust targets
# -----------------------------------------------------------------------------
.PHONY: rust-build
rust-build:
	cargo build -v --workspace

.PHONY: rust-lint
rust-lint: rust-fmt-check
	cargo clippy --workspace --all-features --all-targets -- -Dclippy::all -Dwarnings

.PHONY: rust-fmt
rust-fmt:
	rustup component add rustfmt --toolchain nightly
	cargo +nightly fmt --all -- --config imports_granularity=Crate

.PHONY: rust-fmt-check
rust-fmt-check:
	@rustup component add rustfmt --toolchain nightly
	@cargo +nightly fmt --all -- --check --config imports_granularity=Crate || (echo "Formatting check failed. Please run 'make fmt' to fix formatting issues." && exit 1)

.PHONY: rust-test
rust-test:
	cargo test --workspace --all-features

.PHONY: rust-ci
rust-ci: rust-build rust-lint rust-test


# -----------------------------------------------------------------------------
# E2E targets
# -----------------------------------------------------------------------------
.PHONY: e2e-test
e2e-test:
	cd e2e && $(MAKE) test $(if $(parallel),parallel=$(parallel)) $(if $(run),run=$(run))

.PHONY: e2e-build
e2e-build:
	cd e2e && $(MAKE) build
