NETWORK ?= mainnet-beta

# Validate NETWORK parameter
ifneq ($(NETWORK),mainnet-beta)
ifneq ($(NETWORK),development)
    $(error NETWORK must be either "mainnet-beta" or "development". Got "$(NETWORK)")
endif
endif

ifeq ($(NETWORK),mainnet-beta)
    CARGO_FEATURES = entrypoint
else
    CARGO_FEATURES = development,entrypoint
endif

.PHONY: clean
clean:
	rm -rf artifacts-* test-ledger
	cargo clean

.PHONY: build-sbf
build-sbf:
	cd programs/passport && cargo build-sbf --features $(CARGO_FEATURES)
	cd programs/revenue-distribution && cargo build-sbf --features $(CARGO_FEATURES)

.PHONY: build-cli
build-cli:
	cargo build --release --bin doublezero-passport-admin --bin doublezero-revenue-distribution-admin

artifacts-$(NETWORK): build-sbf
	@if [ ! -d "$@" ]; then \
		mkdir -p "$@" && \
		cp target/deploy/*.so "$@/"; \
	else \
		echo "$@ already exists"; \
		exit 1; \
	fi

.PHONY: build-artifacts
build-artifacts: artifacts-$(NETWORK)

.PHONY: build-sbf-mock
build-sbf-mock:
	cd mock/swap-sol-2z && cargo build-sbf --features $(CARGO_FEATURES)

.PHONY: test-sbf
test-sbf: build-sbf-mock
	cargo test-sbf --features $(CARGO_FEATURES)

.PHONY: test-lib
test-lib:
	cargo test --lib --features development,offchain

.PHONY: lint
lint:
	cargo fmt --check
	cargo clippy --all-features --all-targets -- -Dwarnings

.PHONY: doc
doc:
	cargo doc --all-features --no-deps --document-private-items

.PHONY: examples
examples:
	cargo run --example log_output
	cargo run --example log_output --features tracing
	cargo run --example get_blocks --features tracing -- -ut