# Export required env
export SERVICEABILITY_PROGRAM_ID := "devnet"

# Default (list of commands)
default:
    just -l

# Run fmt
fmt:
    cargo fmt

# Check fmt
fmt-check:
    cargo fmt --check

# Build (release)
build:
    cargo build --release

# Run clippy
clippy:
    cargo clippy -- -Dclippy::all -D warnings

# Run tests
test:
    cargo nextest run --release

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
