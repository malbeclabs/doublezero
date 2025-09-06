# DoubleZero Development Instructions

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

## Working Effectively

### Initial Setup and Dependencies

Bootstrap the development environment:
- Install Rust: `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh`
- Install Go: Download from https://go.dev/doc/install (requires Go 1.24+)
- Install system dependencies: `sudo apt-get update && sudo apt-get install -y libudev-dev pkg-config build-essential`
- Install golangci-lint: `curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin latest`
- Add Go tools to PATH: `export PATH="$(go env GOPATH)/bin:$PATH"`

### Build Commands and Timing

**CRITICAL TIMEOUT VALUES - NEVER CANCEL THESE COMMANDS:**

- `make go-build` -- takes 2-3 minutes. NEVER CANCEL. Set timeout to 10+ minutes.
- `make rust-build` -- takes 9-10 minutes (excluding smart contracts). NEVER CANCEL. Set timeout to 15+ minutes.
- `make build` -- combines both, takes 11-13 minutes. NEVER CANCEL. Set timeout to 20+ minutes.
- Client release build: `cargo build --release` -- takes 10+ minutes. NEVER CANCEL. Set timeout to 15+ minutes.

**Build and test the repository:**
```bash
# Combined build (Go + Rust, excluding smart contracts due to Solana dependency)
make build

# Individual builds
make go-build      # Build Go components: control plane, agent, tools
make rust-build    # Build Rust components: client, admin, SDK (excludes smart contracts)

# Smart contracts (requires Solana tools - see Smart Contract section)
cd smartcontract && make build-programs  # Requires Solana CLI with cargo-build-sbf
```

### Testing and Validation

**CRITICAL TIMEOUT VALUES - NEVER CANCEL THESE COMMANDS:**

- `make go-test` -- takes 3-4 minutes. NEVER CANCEL. Set timeout to 10+ minutes.
- `make rust-test` -- takes 1-2 minutes. NEVER CANCEL. Set timeout to 5+ minutes.
- `make test` -- combines both, takes 4-6 minutes. NEVER CANCEL. Set timeout to 15+ minutes.
- E2E tests: `cd e2e && make test` -- takes 10-20 minutes. NEVER CANCEL. Set timeout to 30+ minutes.

**Run tests:**
```bash
# All tests (Go + Rust, excludes smart contracts and E2E)
make test

# Individual test suites
make go-test nocontainertest  # Go tests excluding container-dependent tests
make rust-test               # Rust tests excluding smart contract tests

# E2E tests (requires Docker)
cd e2e && make test          # Full end-to-end integration tests
```

**VALIDATION SCENARIOS:** After making changes, always run through these validation scenarios:
1. **Build validation**: Run `make build` to ensure all components compile
2. **Test validation**: Run `make test` to ensure functionality is preserved
3. **Lint validation**: Run `make lint` to ensure code quality standards
4. **Client functionality**: Build and test client with `cd client/doublezero && cargo build --release`

### Linting and Formatting

**CRITICAL TIMEOUT VALUES - NEVER CANCEL THESE COMMANDS:**

- `make go-lint` -- takes 1-2 minutes. NEVER CANCEL. Set timeout to 5+ minutes.
- `make rust-lint` -- takes 4-5 minutes. NEVER CANCEL. Set timeout to 10+ minutes.
- `make lint` -- combines both, takes 5-7 minutes. NEVER CANCEL. Set timeout to 15+ minutes.

**Linting and formatting:**
```bash
# Lint all code
make lint           # Go + Rust linting

# Individual linting
make go-lint        # golangci-lint for Go components
make rust-lint      # cargo clippy for Rust components (excludes smart contracts)

# Format code
make fmt            # Format both Go and Rust code
make go-fmt         # Format Go code with gofmt and goimports
make rust-fmt       # Format Rust code with cargo fmt
```

**Always run `make lint` and `make fmt` before committing changes or the CI (.github/workflows/go.yml, .github/workflows/rust.yml) will fail.**

### Smart Contract Development

**Note: Smart contract builds require Solana tools which may not be available in all environments due to network restrictions.**

Install Solana tools:
```bash
# Install Solana CLI (requires network access)
sh -c "$(curl -sSfL https://release.anza.xyz/v2.3.6/install)"
echo "$HOME/.local/share/solana/install/active_release/bin" >> $GITHUB_PATH
export PATH="$HOME/.local/share/solana/install/active_release/bin:$PATH"

# Verify installation
solana --version
cargo build-sbf --version
```

Build smart contracts:
```bash
cd smartcontract
make build-programs env=localnet    # Build for local development
make test-programs                  # Test smart contracts
make lint-programs                  # Lint smart contracts
```

**If Solana tools are not available**, you can still work with the Rust components by excluding smart contract packages:
```bash
cargo build --workspace --exclude doublezero-telemetry --exclude doublezero-serviceability --exclude doublezero-record
```

### Local Development Environment

**Run a local devnet for testing:**
```bash
# Start local devnet with all components
dev/dzctl start

# Add devices and clients for testing
dev/dzctl add-device -v --code=lo-dz1 --location lax --exchange xlax --cyoa-network-host-id 8
dev/dzctl add-client -v --cyoa-network-host-id 100

# Monitor logs
docker logs -f dz-local-activator
docker exec -it dz-local-manager bash

# Clean up
dev/dzctl destroy
```

### Client Installation and Usage

**Build client from source:**
```bash
cd client/doublezero

# Development build
cargo build

# Release build (takes 10+ minutes)
cargo build --release

# Install client
./install.sh  # Equivalent to: cargo install --path .
```

**Client daemon requires special capabilities:**
```bash
# Set required capabilities for network operations
sudo setcap cap_net_raw,cap_net_admin=+ep ./target/release/doublezerod

# Verify capabilities
getcap ./target/release/doublezerod
```

## Common Issues and Solutions

### Build Issues
- **"no such command: build-sbf"**: Solana tools not installed. Either install Solana CLI or exclude smart contracts from build.
- **"libudev not found"**: Install system dependencies with `sudo apt-get install -y libudev-dev pkg-config`
- **golangci-lint version mismatch**: Install latest version with the install script provided above.

### Network-Dependent Tests
- Some tests (especially in `tools/twamp`) may fail in sandboxed environments due to network restrictions. This is expected.
- Container tests (`make go-container-test`) require Docker and may fail without proper setup.

### Smart Contract Limitations
- Smart contract builds require network access to download Solana tools
- If unavailable, focus on Go and non-smart-contract Rust components
- Document when smart contract functionality cannot be validated

## Project Structure

**Key components:**
- `client/doublezero/`: Rust client CLI and daemon
- `controlplane/`: Go control plane components (activator, admin tools)
- `smartcontract/`: Solana smart contracts and SDKs
- `e2e/`: End-to-end integration tests
- `tools/`: Network utilities and testing tools
- `dev/`: Local development environment setup

**Build system:**
- Root `Makefile`: Combined Go + Rust builds and tests
- `smartcontract/Makefile`: Smart contract specific builds
- `e2e/Makefile`: End-to-end testing
- Individual `Cargo.toml` files for Rust components
- `go.mod` for Go module dependencies

## Validation Checklist

Before finalizing any changes:
- [ ] Run `make build` successfully (allow 20+ minutes)
- [ ] Run `make test` successfully (allow 15+ minutes)  
- [ ] Run `make lint` successfully (allow 15+ minutes)
- [ ] Run `make fmt` to ensure consistent formatting
- [ ] Test client build: `cd client/doublezero && cargo build --release` (allow 15+ minutes)
- [ ] If modifying smart contracts: test with `cd smartcontract && make build-programs`
- [ ] For significant changes: run E2E tests with `cd e2e && make test` (allow 30+ minutes)

**Remember:** Build and test times are significant. Always set appropriate timeouts and never cancel long-running commands. The project uses both Go and Rust with complex dependencies that require substantial compilation time.