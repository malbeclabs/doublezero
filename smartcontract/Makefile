PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=
CLIPPY_FLAGS=-- -Dclippy::all -D warnings

.PHONY: test
test: test-programs
	cargo test --manifest-path ./sdk/rs/Cargo.toml
	cargo test --manifest-path ./cli/Cargo.toml
	go test ./sdk/go/

.PHONY: test-programs
test-programs: test-sbf
	cargo test --lib -p doublezero-serviceability -p doublezero-telemetry

.PHONY: test-sbf
test-sbf:
	cargo test-sbf

.PHONY: lint
lint: lint-programs
	cargo clippy --manifest-path ./sdk/rs/Cargo.toml $(CLIPPY_FLAGS)
	cargo clippy --manifest-path ./cli/Cargo.toml $(CLIPPY_FLAGS)

.PHONY: lint-programs
lint-programs:
	cargo clippy --all-targets -p doublezero-telemetry -p doublezero-serviceability -- -Dclippy::all -Dwarnings

.PHONY: build
build:
	cargo build $(LDFLAGS) --manifest-path ./programs/doublezero-serviceability/Cargo.toml
	cargo build $(LDFLAGS) --manifest-path ./programs/doublezero-telemetry/Cargo.toml
	cargo build $(LDFLAGS) --manifest-path ./sdk/rs/Cargo.toml
	cargo build $(LDFLAGS) --manifest-path ./cli/Cargo.toml
