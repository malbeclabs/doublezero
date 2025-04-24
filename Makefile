PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD` 
LDFLAGS=

.PHONY: test
test:
	cargo test --manifest-path ./client/doublezero/Cargo.toml
	go test -race -v -coverprofile coverage.out ./client/doublezerod/...
	cargo test --manifest-path ./smartcontract/sdk/rs/Cargo.toml
	cargo test --manifest-path ./smartcontract/programs/dz-sla-program/Cargo.toml
	cargo test --manifest-path ./smartcontract/cli/Cargo.toml
	go test ./sdk/go/

.PHONY: lint
lint:
	cargo clippy --manifest-path ./client/doublezero/Cargo.toml
	golangci-lint run -c ./.golangci.yaml
	cargo clippy --manifest-path ./smartcontract/programs/dz-sla-program/Cargo.toml
	cargo clippy --manifest-path ./smartcontract/sdk/rs/Cargo.toml
	cargo clippy --manifest-path ./smartcontract/cli/Cargo.toml

.PHONY: build
build:
	cargo build -v $(LDFLAGS) --release --manifest-path ./client/doublezero/Cargo.toml
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o ./client/doublezerod/bin/doublezerod ./client/doublezerod/cmd/doublezerod/main.go
	cargo build -v $(LDFLAGS) --manifest-path ./smartcontract/programs/dz-sla-program/Cargo.toml
	cargo build -v $(LDFLAGS) --manifest-path ./smartcontract/sdk/rs/Cargo.toml
	cargo build -v $(LDFLAGS) --manifest-path ./smartcontract/cli/Cargo.toml
