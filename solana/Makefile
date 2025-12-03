PASSPORT_PATH = programs/passport/Cargo.toml
REVENUE_DISTRIBUTION_PATH = programs/revenue-distribution/Cargo.toml

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
	cargo build-sbf --features $(CARGO_FEATURES) --manifest-path $(PASSPORT_PATH)
	cargo build-sbf --features $(CARGO_FEATURES) --manifest-path $(REVENUE_DISTRIBUTION_PATH)

artifacts-$(NETWORK):
	DOCKER_BUILDKIT=1 docker build \
	--build-arg NETWORK=${NETWORK} \
	--platform linux/amd64 \
	--output type=local,dest=./artifacts-${NETWORK} \
	.

.PHONY: build-artifacts
build-artifacts: artifacts-$(NETWORK)

.PHONY: build-sbf-mock
build-sbf-mock:
	cargo build-sbf --features $(CARGO_FEATURES) --manifest-path mock/swap-sol-2z/Cargo.toml

.PHONY: test-sbf
test-sbf: build-sbf-mock
	cargo test-sbf --features $(CARGO_FEATURES) --manifest-path $(PASSPORT_PATH)
	cargo test-sbf --features $(CARGO_FEATURES) --manifest-path $(REVENUE_DISTRIBUTION_PATH)

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