# Export required env
export SERVICEABILITY_PROGRAM_ID := "devnet"

# Default (list of commands)
default:
    just -l

# Run fmt
fmt:
    @rustup component add rustfmt --toolchain nightly
    @cargo +nightly fmt --all -- --config imports_granularity=Crate

# Check fmt
fmt-check:
	@rustup component add rustfmt --toolchain nightly
	@cargo +nightly fmt --all -- --check --config imports_granularity=Crate || (echo "Formatting check failed. Please run 'just fmt' to fix formatting issues." && exit 1)

# Build (release)
build:
    cargo build --release

# Run clippy
clippy:
    cargo clippy --all-features -- -Dclippy::all -D warnings

# Run tests
test:
    cargo nextest run

# Clean
clean:
    cargo clean

# Coverage
cov:
    cargo llvm-cov nextest --lcov --output-path lcov.info

# Run CI pipeline
ci:
    @just fmt-check
    @just clippy
    @just test
