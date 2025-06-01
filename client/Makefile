PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=
CLIPPY_FLAGS=-- -Dclippy::all -D warnings
CARGO_FLAGS?=

.PHONY: test
test:
	$(MAKE) test -C doublezero
	$(MAKE) test -C doublezerod

.PHONY: test-e2e
test-e2e:
	go tool go-e2e -v

.PHONY: lint
lint:
	cargo clippy --manifest-path ./doublezero/Cargo.toml $(CLIPPY_FLAGS)
	golangci-lint run -c ../.golangci.yaml

.PHONY: build
build:
	$(MAKE) lint -C doublezero
	$(MAKE) lint -C doublezerod

.PHONY: build
build:
	$(MAKE) build -C doublezero CARGO_FLAGS="$(CARGO_FLAGS)"
	$(MAKE) build -C doublezerod
