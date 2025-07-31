PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=
CLIPPY_FLAGS=-- -Dclippy::all -D warnings
SERVICEABILITY_PROGRAM_ID ?= local

.PHONY: test
test: test-programs
	cargo test --manifest-path ./sdk/rs/Cargo.toml
	cargo test --manifest-path ./cli/Cargo.toml
	go test ./sdk/go/

.PHONY: test-programs
test-programs: test-sbf
	cargo test --lib \
		-p doublezero-record \
		-p doublezero-telemetry \
		-p doublezero-serviceability

.PHONY: test-sbf
test-sbf:
	SERVICEABILITY_PROGRAM_ID=$(SERVICEABILITY_PROGRAM_ID) \
	cargo test-sbf

.PHONY: lint
lint: lint-programs
	cargo clippy --manifest-path ./sdk/rs/Cargo.toml $(CLIPPY_FLAGS)
	cargo clippy --manifest-path ./cli/Cargo.toml $(CLIPPY_FLAGS)

.PHONY: lint-programs
lint-programs:
	SERVICEABILITY_PROGRAM_ID=$(SERVICEABILITY_PROGRAM_ID) \
	cargo clippy --all-targets \
		-p doublezero-record \
		-p doublezero-telemetry \
		-p doublezero-serviceability \
		$(CLIPPY_FLAGS)

.PHONY: build
build: build-sbf
	cargo build $(LDFLAGS) --manifest-path ./sdk/rs/Cargo.toml
	cargo build $(LDFLAGS) --manifest-path ./cli/Cargo.toml

.PHONY: build-sbf
build-sbf:
	cd programs/doublezero-record && cargo build-sbf
	cd programs/doublezero-serviceability && SERVICEABILITY_PROGRAM_ID=$(SERVICEABILITY_PROGRAM_ID) cargo build-sbf
	cd programs/doublezero-telemetry && SERVICEABILITY_PROGRAM_ID=$(SERVICEABILITY_PROGRAM_ID) cargo build-sbf
