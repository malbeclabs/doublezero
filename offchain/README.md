# DoubleZero Rewarder

An off-chain rewards calculation system for the DoubleZero network that uses Shapley values to ensure fair distribution of rewards based on network contributions.

## Overview

The DoubleZero Rewarder is a Rust-based system that:

- Fetches on-chain serviceability and telemetry data from Solana
- Processes network performance metrics (latency, jitter, packet loss)
- Calculates fair reward distributions using Shapley values
- Generates a Merkle root for on-chain verification

This ensures that network participants are rewarded proportionally to their actual contribution to network performance and reliability.

## Architecture

The system consists of three main components working in a pipeline:

```

   --------------        -------------------         --------------------
   |Data Fetcher| -----> |Metrics Processor| ----->  |Rewards Calculator|
   --------------        -------------------         --------------------
        │                        │                            │
   Fetch data via             Calculates                   Computes
     Custom RPC            RTT, jitter, loss            Shapley values
```

### Components

- **[data_fetcher](./data_fetcher/README.md)**: Retrieves on-chain data from DoubleZero programs
- **[metrics_processor](./metrics_processor/README.md)**: Transforms raw telemetry into statistical metrics
- **[rewards_calculator](./rewards_calculator/README.md)**: Orchestrates the workflow and computes final rewards

## Quick Start

### Building

```bash
# Clone the repository
git clone https://github.com/malbeclabs/doublezero-rewarder.git
cd doublezero-rewarder

# NOTE: YOU MUST DO THIS TO BUILD FIRST
export SERVICEABILITY_PROGRAM_ID=devnet

# Build release binaries
just build
```

### Running

Basic usage with relative time:

```bash
./target/release/rewards_calculator --before "1 hours ago" --after "49 hours ago"
```

OR

```bash
DZ_ENV=testnet ./target/release/rewards_calculator --before "1 hours ago" --after "49 hours ago"
```

With specific timestamps:

```bash
./target/release/rewards_calculator --before "2024-01-15T10:00:00Z" --after "2024-01-15T08:00:00Z"
```

OR

```bash
DZ_ENV=testnet ./target/release/rewards_calculator --before "2024-01-15T10:00:00Z" --after "2024-01-15T08:00:00Z"
```

## Usage

```bash
$ ./target/release/rewards_calculator -h
Off-chain rewards calculation for DoubleZero network

Usage: rewards_calculator [OPTIONS] --before <BEFORE> --after <AFTER>

Options:
  -l, --log-level <LOG_LEVEL>  Override log level (trace, debug, info, warn, error)
  -r, --rpc-url <RPC_URL>      Override RPC URL
  -h, --help                   Print help
  -V, --version                Print version

Time Range:
  -b, --before <BEFORE>  End timestamp for the rewards period (required) Accepts: ISO 8601 (2024-01-15T10:00:00Z), Unix timestamp (1705315200), or relative time (2 hours ago)
  -a, --after <AFTER>    Start timestamp for the rewards period (required) Accepts: ISO 8601 (2024-01-15T08:00:00Z), Unix timestamp (1705308000), or relative time (4 hours ago)

```

## Configuration

The system uses a hierarchical configuration approach:

1. **Base configuration**: `config/default.toml`
2. **Environment-specific**: `config/devnet.toml`, `config/testnet.toml`
3. **Environment variables**: Override any setting
4. **CLI arguments**: Highest priority

### Key Configuration Options

```toml
# General settings
log_level = "info"

# RPC settings
[rpc]
commitment = "finalized"
timeout_secs = 30
max_retries = 3

# Burn rate configuration
[burn]
coefficient = 1
max_rate = 1000

# Metrics processor
[metrics]
uptime_threshold = 0.95
percentile_bins = [50, 75, 90, 95, 99]
```

## Development

### Commands

All development tasks are managed through `just`:

```bash
$ just
Available recipes:
    build     # Build (release)
    ci        # Run CI pipeline
    clean     # Clean
    clippy    # Run clippy
    cov       # Coverage
    default   # Default (list of commands)
    fmt       # Run fmt
    fmt-check # Check fmt
    test      # Run tests
```

### Project Structure

```
doublezero-rewarder/
├── data_fetcher/       # On-chain data retrieval
├── metrics_processor/  # Statistical analysis
├── rewards_calculator/ # Main orchestrator
├── config/            # Configuration files
├── ai-docs/           # Additional documentation
└── Justfile          # Task definitions
```

### Testing

Run the test suite:

```bash
just test
```

Note: The project uses `cargo nextest` for improved test output and performance.

## How It Works

1. **Data Collection**: The system fetches serviceability (network topology) and telemetry (performance metrics) data from Solana for the specified time range.

2. **Metrics Processing**: Raw telemetry data is processed to calculate:

   - Round-trip time (RTT) statistics
   - Network jitter measurements
   - Packet loss rates
   - Private and public link performance

3. **Shapley Calculation**: Using game theory, the system calculates each participant's marginal contribution to the network's overall performance.

4. **Output Generation**: The system produces:
   - Reward proportions for each participant
   - A Merkle root for on-chain commitment
   - Individual Merkle proofs for verification

### Debug Mode

Enable detailed logging:

```bash
RUST_LOG=trace ./target/release/rewards_calculator --before "1 hours ago" --after "49 hours ago"
```
