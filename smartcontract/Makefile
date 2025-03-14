PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD` 
LDFLAGS=

.PHONY: test
test:
	cargo test --manifest-path ./programs/dz-sla-program/Cargo.toml -- --nocapture
	cargo test --manifest-path ./sdk/rs/Cargo.toml
	go test ./sdk/go/

.PHONY: lint
lint:
	cargo clippy --manifest-path ./programs/dz-sla-program/Cargo.toml
	cargo clippy --manifest-path ./sdk/rs/Cargo.toml

.PHONY: build
build:
	cargo build-sbf -v $(LDFLAGS) --manifest-path ./programs/dz-sla-program/Cargo.toml
	cargo build -v $(LDFLAGS) --manifest-path ./sdk/rs/Cargo.toml
