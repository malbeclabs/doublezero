PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=

.PHONY: test
test:
	go test ./... -race -v -coverprofile coverage.out
	cargo test --all --all-features

.PHONY: lint
lint:
	golangci-lint run -c ./.golangci.yaml
	cargo clippy --all --all-features --all-targets

.PHONY: build
build:
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o ./client/doublezerod/bin/doublezerod ./client/doublezerod/cmd/doublezerod/main.go
	cargo build -v $(LDFLAGS) --all

.PHONY: checks
checks: lint test build
