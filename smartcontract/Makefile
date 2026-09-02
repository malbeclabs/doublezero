PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=
CLIPPY_FLAGS=-- -Dclippy::all -Dclippy::unreadable_literal -D warnings
env ?= localnet
# Platform-tools (Cargo/rustc) used by cargo build-sbf / test-sbf. The solana 3.0
# program dependency tree pulls edition2024 crates (cpufeatures 0.3 via blake3),
# which require Cargo >= 1.85; agave's default platform-tools (v1.51) ships Cargo
# 1.84.1 and cannot parse them. v1.54 ships a new enough toolchain.
SBF_TOOLS_VERSION ?= v1.54

UNAME_S := $(shell uname -s)

# On macOS, some Rust crates (e.g. blake3) compile C code and need system headers
# from the macOS SDK. By exporting CC and SDKROOT via xcrun, we ensure Apple’s
# clang is used with the correct SDK path. This avoids 'assert.h not found' errors.
ifeq ($(UNAME_S),Darwin)
  export CC      := $(shell xcrun -find clang)
  export SDKROOT := $(shell xcrun --sdk macosx --show-sdk-path)
endif

.PHONY: test
test: test-programs
	cargo test --manifest-path ./sdk/rs/Cargo.toml
	cargo test --manifest-path ./cli/Cargo.toml
	go test ./sdk/go/

.PHONY: test-programs
test-programs: test-sbf
	cargo test --lib \
		-p doublezero-program-common \
		-p doublezero-record \
		-p doublezero-telemetry \
		-p doublezero-serviceability \
		-p doublezero-geolocation

.PHONY: test-sbf
# Run from each program's directory, the way build-programs below does, rather
# than unscoped from here. Two reasons, and only the first is new:
#
#   1. An unscoped cargo test-sbf resolves the whole workspace against the
#      platform-tools rustc, which reports itself as 1.89.0-dev. The workspace
#      now carries crates declaring a higher minimum, aws-sdk-s3 at 1.94.1 and
#      rustler at 1.91, both reached through the offchain crates. Neither is in
#      any program's dependency graph, but resolution fails before the build.
#   2. A `-p` list avoids that resolve and then fails differently: cargo test-sbf
#      builds the .so for the package in the current directory, so with no
#      package there, solana-program-test finds nothing in target/deploy and
#      every test panics with "Program file data not available".
#
# Serviceability runs before telemetry, whose test helper loads the
# serviceability .so through add_program with no processor.
test-sbf:
	cd programs/doublezero-record && cargo test-sbf --tools-version $(SBF_TOOLS_VERSION)
	cd programs/doublezero-serviceability && cargo test-sbf --tools-version $(SBF_TOOLS_VERSION)
	cd programs/doublezero-telemetry && cargo test-sbf --tools-version $(SBF_TOOLS_VERSION)
	cd programs/doublezero-geolocation && cargo test-sbf --tools-version $(SBF_TOOLS_VERSION)

.PHONY: lint
lint: lint-programs
	cargo clippy --manifest-path ./sdk/rs/Cargo.toml $(CLIPPY_FLAGS)
	cargo clippy --manifest-path ./cli/Cargo.toml $(CLIPPY_FLAGS)

.PHONY: lint-programs
lint-programs:
	cargo clippy --all-targets \
		-p doublezero-program-common \
		-p doublezero-record \
		-p doublezero-telemetry \
		-p doublezero-serviceability \
		-p doublezero-geolocation \
		$(CLIPPY_FLAGS)

.PHONY: build
build: build-programs
	cargo build $(LDFLAGS) --manifest-path ./sdk/rs/Cargo.toml
	cargo build $(LDFLAGS) --manifest-path ./cli/Cargo.toml

.PHONY: build-programs
build-programs:
	cd programs/doublezero-record && cargo build-sbf --tools-version $(SBF_TOOLS_VERSION)
	cd programs/doublezero-serviceability && cargo build-sbf --tools-version $(SBF_TOOLS_VERSION)
	cd programs/doublezero-telemetry && cargo build-sbf --tools-version $(SBF_TOOLS_VERSION) --features $(env)
	cd programs/doublezero-geolocation && cargo build-sbf --tools-version $(SBF_TOOLS_VERSION) --features $(env)
