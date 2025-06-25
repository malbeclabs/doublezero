PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=
CLIPPY_FLAGS=-- -Dclippy::all -D warnings

.PHONY: test
test: test-sbf
	cargo test --manifest-path ./sdk/rs/Cargo.toml
	cargo test --manifest-path ./programs/doublezero-serviceability/Cargo.toml
	cargo test --manifest-path ./programs/doublezero-telemetry/Cargo.toml
	cargo test --manifest-path ./cli/Cargo.toml
	go test ./sdk/go/

.PHONY: test-sbf
test-sbf:
	cargo test-sbf --manifest-path ./programs/doublezero-serviceability/Cargo.toml
	cargo test-sbf --manifest-path ./programs/doublezero-telemetry/Cargo.toml

.PHONY: lint
lint:
	cargo clippy --manifest-path ./programs/doublezero-serviceability/Cargo.toml $(CLIPPY_FLAGS)
	cargo clippy --manifest-path ./programs/doublezero-telemetry/Cargo.toml $(CLIPPY_FLAGS)
	cargo clippy --manifest-path ./sdk/rs/Cargo.toml $(CLIPPY_FLAGS)
	cargo clippy --manifest-path ./cli/Cargo.toml $(CLIPPY_FLAGS)

.PHONY: build
build:
	cargo build $(LDFLAGS) --manifest-path ./programs/doublezero-serviceability/Cargo.toml
	cargo build $(LDFLAGS) --manifest-path ./programs/doublezero-telemetry/Cargo.toml
	cargo build $(LDFLAGS) --manifest-path ./sdk/rs/Cargo.toml
	cargo build $(LDFLAGS) --manifest-path ./cli/Cargo.toml
