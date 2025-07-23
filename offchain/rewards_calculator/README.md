# Rewards Calculator

The main orchestrator for the DoubleZero off-chain rewards calculation system.

## Overview

The `rewards_calculator` crate is the primary entry point for calculating fair reward distributions in the DoubleZero network. It orchestrates the entire workflow from data fetching through statistical processing to final Shapley value calculations, producing a Merkle root for on-chain verification.

## Features

- **End-to-End Orchestration**: Coordinates data fetching, metrics processing, and reward calculations
- **CLI Interface**: User-friendly command-line interface with flexible time range options
- **Shapley Value Integration**: Integrates with network-shapley-rs for fair value reward proportions
- **Merkle Tree Generation**: Creates cryptographic proofs for on-chain verification
- **Flexible Time Parsing**: Supports ISO 8601, Unix timestamps, and relative time formats
- **Configurable Logging**: Multiple log levels for different debugging needs

## Architecture

The rewards calculator follows a three-stage pipeline:

1. **Data Fetching**: Retrieves serviceability and telemetry data from Solana
2. **Metrics Processing**: Transforms raw data into statistical metrics
3. **Shapley Calculation**: Computes fair reward distributions based on network contributions

## Module Structure

- `main.rs` - CLI entry point and initialization
- `orchestrator.rs` - Main workflow coordination
- `shapley_handler.rs` - Integration with network-shapley-rs library
- `cli.rs` - Command-line argument parsing
- `util.rs` - Time parsing and utility functions
- `settings.rs` - Configuration management

## Usage

### Basic Command

```bash
./rewards_calculator --before "1 hours ago" --after "49 hours ago"
```

### With Options

```bash
# Custom RPC endpoint
./rewards_calculator --before "2024-01-15T10:00:00Z" --after "2024-01-15T08:00:00Z" \
    --rpc-url https://api.devnet.solana.com

# Debug logging
./rewards_calculator --before "1 hours ago" --after "49 hours ago" \
    --log-level debug

```

### Time Format Examples

- **ISO 8601**: `2024-01-15T10:00:00Z`
- **Unix timestamp**: `1705315200`
- **Relative time**: `2 hours ago`, `1 day ago`, `30 minutes ago`

## Output

The calculator produces:

1. **Reward Proportions**: Fair share for each network participant
2. **Merkle Root**: Cryptographic commitment for on-chain verification
3. **Merkle Leaves**: Individual proofs for participants
4. **Burn Rate**: Epoch-specific rate based on network performance

## Configuration

Configuration follows a hierarchy:

1. Default values (config/default.toml)
2. Environment-specific overrides (config/devnet.toml, config/testnet.toml)
3. Environment variables
4. Command-line arguments

### Key Configuration Options

- **RPC Settings**: URL, commitment level, timeouts
- **Burn Rate**: Coefficient and maximum rate
- **Metrics**: Uptime threshold, percentile bins
- **Cache**: Enable/disable, format preferences

## Dependencies

- `data_fetcher` - On-chain data retrieval
- `metrics_processor` - Statistical analysis
- `network-shapley` - Shapley value calculations
- `svm-hash` - Merkle tree implementation
- `clap` - CLI parsing
- `tokio` - Async runtime

## Error Handling

The calculator provides comprehensive error messages for:

- Invalid time ranges
- RPC connection failures
- Missing or corrupted data
- Configuration errors
- Calculation failures
