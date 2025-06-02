PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=
CLIPPY_FLAGS=-- -Dclippy::all -D warnings

.PHONY: test
test:
	cargo test --manifest-path ./Cargo.toml

.PHONY: lint
lint:
	cargo clippy --manifest-path ./Cargo.toml $(CLIPPY_FLAGS)

.PHONY: build
build:
	cargo build -v $(LDFLAGS) --manifest-path ./Cargo.toml
