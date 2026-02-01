env ?= localnet

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

.PHONY: clean
clean:
	cargo clean


# -----------------------------------------------------------------------------
# Go targets
# -----------------------------------------------------------------------------
.PHONY: go-build
go-build:
	CGO_ENABLED=0 go build -v -tags "qa e2e" ./...
	cd controlplane/s3-uploader && go build -v ./...

.PHONY: go-lint
go-lint:
	golangci-lint run -c ./.golangci.yaml
	cd controlplane/s3-uploader && golangci-lint run

.PHONY: go-fmt
go-fmt:
	go fmt ./...
	cd controlplane/s3-uploader && go fmt ./...

.PHONY: go-test
go-test:
	go test -exec "sudo -E" -race -v ./...
	cd controlplane/s3-uploader && go test -race -v ./...
	$(if $(findstring nocontainertest,$(MAKECMDGOALS)),,$(MAKE) go-container-test)
	$(MAKE) -C client/doublezerod test-faults

.PHONY: nocontainertest
nocontainertest:
	@:

.PHONY: go-fuzz
go-fuzz:
	cd tools/twamp && $(MAKE) fuzz
	cd client/doublezerod && $(MAKE) fuzz

.PHONY: go-container-test
go-container-test:
	go run tools/container-test/main.go -v

.PHONY: go-ci
go-ci: go-build go-lint go-test go-fuzz


# -----------------------------------------------------------------------------
# Rust targets
# -----------------------------------------------------------------------------
.PHONY: rust-build
rust-build: rust-build-programs
	cargo build -v --workspace

.PHONY: rust-build-programs
rust-build-programs:
	cd smartcontract && $(MAKE) build-programs env=$(env)

.PHONY: rust-lint
rust-lint: rust-fmt-check
	@cargo install cargo-hack
	cargo hack clippy --workspace --all-targets --exclude doublezero-telemetry --exclude doublezero-serviceability --exclude doublezero-program-common --exclude doublezero-record -- -Dclippy::all -Dwarnings
	cd smartcontract && $(MAKE) lint-programs

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
	cargo test --workspace --exclude doublezero-telemetry --exclude doublezero-serviceability --exclude doublezero-program-common --exclude doublezero-record --all-features
	cd smartcontract && $(MAKE) test-programs
	$(MAKE) rust-program-accounts-compat

.PHONY: rust-test-programs
rust-test-programs:
	cd smartcontract && $(MAKE) test-programs

.PHONY: rust-validator-test
rust-validator-test:
	bash smartcontract/test/run_record_test.sh

.PHONY: rust-ci
rust-ci: rust-build rust-lint rust-test rust-validator-test

.PHONY: rust-program-accounts-compat
rust-program-accounts-compat:
	cargo run -p doublezero -- accounts -ed --no-output
	cargo run -p doublezero -- accounts -et --no-output
	cargo run -p doublezero -- accounts -em --no-output

# -----------------------------------------------------------------------------
# SDK targets (revenue distribution)
# -----------------------------------------------------------------------------
.PHONY: sdk-test
sdk-test:
	go test ./sdk/revdist/go/...
	$(MAKE) python-test-revdist
	$(MAKE) typescript-test-revdist

.PHONY: python-test-revdist
python-test-revdist:
	cd sdk/revdist/python && uv run pytest

.PHONY: typescript-test-revdist
typescript-test-revdist:
	cd sdk/revdist/typescript && bun install && npx tsc --noEmit && bun test

.PHONY: sdk-compat-test
sdk-compat-test:
	REVDIST_COMPAT_TEST=1 go test -run TestCompat -v ./sdk/revdist/go/...
	$(MAKE) python-compat-test-revdist
	$(MAKE) typescript-compat-test-revdist

.PHONY: python-compat-test-revdist
python-compat-test-revdist:
	cd sdk/revdist/python && REVDIST_COMPAT_TEST=1 uv run pytest -k compat -v

.PHONY: typescript-compat-test-revdist
typescript-compat-test-revdist:
	cd sdk/revdist/typescript && bun install && REVDIST_COMPAT_TEST=1 bun test --grep compat

.PHONY: generate-fixtures
generate-fixtures:
	cd sdk/revdist/testdata/fixtures/generate-fixtures && cargo run

# -----------------------------------------------------------------------------
# E2E targets
# -----------------------------------------------------------------------------
.PHONY: e2e-test
e2e-test:
	cd e2e && $(MAKE) test

.PHONY: e2e-build
e2e-build:
	cd e2e && $(MAKE) build

# -----------------------------------------------------------------------------
# Build programs for specific environments
# -----------------------------------------------------------------------------
.PHONY: build-programs
build-programs:
	$(MAKE) -C smartcontract build-programs env=$(env)

.PHONY: build-programs-localnet
build-programs-localnet:
	$(MAKE) build-programs env=localnet

.PHONY: build-programs-devnet
build-programs-devnet:
	$(MAKE) build-programs env=devnet

.PHONY: build-programs-testnet
build-programs-testnet:
	$(MAKE) build-programs env=testnet

.PHONY: build-programs-mainnet-beta
build-programs-mainnet-beta:
	$(MAKE) build-programs env=mainnet-beta
