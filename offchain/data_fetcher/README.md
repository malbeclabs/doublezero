# Data Fetcher

A Rust library for fetching on-chain data from the DoubleZero network's Solana programs.

## Overview

The `data_fetcher` crate is responsible for retrieving serviceability and telemetry data from the DoubleZero network's on-chain programs. It provides async interfaces to fetch network topology information (devices, links, operators) and performance telemetry data (latency samples, network metrics).

## Features

- **Serviceability Data Fetching**: Retrieves network topology including devices, links, locations, and operators
- **Telemetry Data Fetching**: Fetches device latency samples and performance metrics
- **Concurrent Fetching**: Uses Tokio for efficient parallel data retrieval
- **Retry Logic**: Built-in retry mechanisms for handling RPC failures
- **Time-based Queries**: Support for fetching data within specific time ranges

## Module Structure

- `fetcher.rs` - Main orchestration module that coordinates data fetching
- `rpc.rs` - Solana RPC client wrapper with retry logic
- `serviceability.rs` - Fetches network topology data from the serviceability program
- `telemetry.rs` - Fetches performance telemetry data
- `types.rs` - Data structures and type definitions
- `settings.rs` - Configuration management

## Usage

```rust
use data_fetcher::{fetcher::fetch_data, settings::Settings};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let settings = Settings::from_env()?;

    // Fetch data for a specific time range (in microseconds)
    let before_us = 1705315200_000_000; // End time
    let after_us = 1705308000_000_000;  // Start time

    let (serviceability_data, telemetry_data) = fetch_data(
        &settings,
        before_us,
        after_us
    ).await?;

    Ok(())
}
```

## Configuration

The module uses environment-based configuration via the `Settings` struct:

- RPC URL configuration
- Program IDs for serviceability and telemetry programs
- Retry and timeout settings
- Logging configuration

## Dependencies

- `solana-client` - Solana RPC client
- `doublezero-serviceability` - Serviceability data structures
- `doublezero-telemetry` - Telemetry data structures
- `tokio` - Async runtime
- `borsh` - Data serialization

## Error Handling

All functions return `anyhow::Result` for comprehensive error handling. Common error scenarios include:

- RPC connection failures
- Account not found errors
- Deserialization errors
- Network timeouts

