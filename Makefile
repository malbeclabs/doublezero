PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=

.PHONY: test
test:
	go test -exec "sudo -E" -race -v ./...
	$(MAKE) test-e2e
	cargo test --workspace --all-features

.PHONY: test-e2e
test-e2e:
	go run tools/e2e-test/main.go
	cd e2e && $(MAKE) test

.PHONY: lint
lint:
	golangci-lint run -c ./.golangci.yaml
	cargo clippy --workspace --all-features --all-targets -- -Dclippy::all -Dwarnings
	$(MAKE) cargo-fmt-check

.PHONY: fmt
fmt:
	rustup component add rustfmt --toolchain nightly
	cargo +nightly fmt --all -- --config imports_granularity=Crate

.PHONY: cargo-fmt-check
cargo-fmt-check:
	@rustup component add rustfmt --toolchain nightly
	@cargo +nightly fmt --all -- --check --config imports_granularity=Crate || (echo "Formatting check failed. Please run 'make fmt' to fix formatting issues." && exit 1)

.PHONY: build
build:
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o ./client/doublezerod/bin/doublezerod ./client/doublezerod/cmd/doublezerod/main.go
	cargo build -v $(LDFLAGS) --workspace

.PHONY: checks
checks: lint test build
