PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=
CLIPPY_FLAGS=-- -Dclippy::all -D warnings

.PHONY: test
test:
	cargo test --manifest-path ./doublezero/Cargo.toml
	go test -race -v -coverprofile coverage.out ./doublezerod/...

.PHONY: lint
lint:
	cargo clippy --manifest-path ./doublezero/Cargo.toml $(CLIPPY_FLAGS)
	golangci-lint run -c ../.golangci.yaml

.PHONY: build
build:
	cargo build -v $(LDFLAGS) --release --manifest-path ./doublezero/Cargo.toml
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o ./doublezerod/bin/doublezerod ./doublezerod/cmd/doublezerod/main.go
