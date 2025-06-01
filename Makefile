PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=

.PHONY: test
test:
	go test ./... -race -v -coverprofile coverage.out
	$(MAKE) test-containerized
	cargo test --all --all-features

.PHONY: test-containerized
test-containerized:
	go tool go-e2e

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
