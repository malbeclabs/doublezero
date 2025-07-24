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

#### Calculate Rewards

Basic usage with relative time:

```bash
./target/release/rewards_calculator calculate-rewards --before "1 hours ago" --after "49 hours ago"
```

OR

```bash
DZ_ENV=testnet ./target/release/rewards_calculator calculate-rewards --before "1 hours ago" --after "49 hours ago"
```

With specific timestamps:

```bash
./target/release/rewards_calculator calculate-rewards --before "2024-01-15T10:00:00Z" --after "2024-01-15T08:00:00Z"
```

#### Export Demand Data

Export demand matrix and validator information:

```bash
# Set API token for IP enrichment (required)
export DZ__DEMAND_GENERATOR__IP_INFO__API_TOKEN=your_ipinfo_token_here

# Export demand matrix only
./target/release/rewards_calculator export-demand --demand demand.csv

# Export both demand matrix and enriched validators
./target/release/rewards_calculator export-demand --demand demand.csv --enriched-validators validators.csv
```

## Usage

```bash
$ ./target/release/rewards-calculator -h
Off-chain rewards calculation for DoubleZero network

Usage: rewards-calculator [OPTIONS] <COMMAND>

Commands:
  calculate-rewards  Calculate rewards for the given time period
  export-demand      Export demand matrix and enriched validators to CSV files
  help               Print this message or the help of the given subcommand(s)

Options:
  -l, --log-level <LOG_LEVEL>  Override log level (trace, debug, info, warn, error)
  -r, --rpc-url <RPC_URL>      Override RPC URL
  -h, --help                   Print help
  -V, --version                Print version
```

## Configuration

The system uses a hierarchical configuration approach:

1. **Base configuration**: `config/default.toml`
2. **Environment-specific**: `config/devnet.toml`, `config/testnet.toml`
3. **Environment variables**: Override any setting
4. **CLI arguments**: Highest priority

### API Configuration (Demand Generator)

The demand generator requires an ipinfo.io API token for enriching validator data with geographic information.

#### Setting Up API Token

The API token **must** be provided via environment variable for security:

```bash
export DZ__DEMAND_GENERATOR__IP_INFO__API_TOKEN=your_token_here
```

#### Rate Limiting Configuration

The demand generator includes configurable rate limiting to respect API quotas:

| Setting                   | Default | Description                                |
| ------------------------- | ------- | ------------------------------------------ |
| `concurrent_api_requests` | 5       | Maximum concurrent API requests            |
| `max_api_retries`         | 3       | Maximum retry attempts for failed requests |
| `retry_backoff_base_ms`   | 100     | Initial backoff duration in milliseconds   |
| `retry_backoff_max_ms`    | 30000   | Maximum backoff duration (30 seconds)      |
| `rate_limit_multiplier`   | 3       | Multiplier for rate limit errors (429)     |

**Note**: The system implements exponential backoff with jitter to handle rate limits gracefully. If you encounter frequent 429 errors, reduce the `concurrent_api_requests` setting.
