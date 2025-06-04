PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=

.PHONY: test
test:
	go test ./... -race -v -coverprofile coverage.out
	$(MAKE) test-e2e
	cargo test --workspace --all-features

# NOTE: This does not yet run the tests in the ./e2e directory. It only runs the e2e tests in the
# ./client/doublezerod directory for now, until the e2e tests are converted to use the e2e-test tool.
# TODO(snormore): Remove this note when the e2e tests are converted to use the e2e-test tool.
.PHONY: test-e2e
test-e2e:
	go run tools/e2e-test/main.go

.PHONY: lint
lint:
	golangci-lint run -c ./.golangci.yaml
	cargo clippy --workspace --all-features --all-targets -- -Dclippy::all -Dwarnings
	cargo fmt --check --all

.PHONY: build
build:
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o ./client/doublezerod/bin/doublezerod ./client/doublezerod/cmd/doublezerod/main.go
	cargo build -v $(LDFLAGS) --workspace

.PHONY: checks
checks: lint test build
