# Metrics Processor

A Rust library for processing raw telemetry data into statistical metrics for the DoubleZero network.

## Overview

The `metrics_processor` crate transforms raw telemetry data fetched from the blockchain into meaningful statistical metrics. It calculates network performance indicators such as round-trip time (RTT), jitter, packet loss, and generates both private (device-to-device) and public (location-to-location) link statistics (TBD).

## Features

- **Statistical Analysis**: Calculates mean, median, percentiles (p95, p99) for latency data
- **Jitter Calculation**: Measures network stability through jitter analysis
- **Packet Loss Detection**: Identifies and quantifies packet loss rates
- **Link Aggregation**: Generates both private and public link statistics
- **Data Storage**: Efficient in-memory data processing using custom data structures
- **Time-based Filtering**: Processes data within specified time ranges

## Module Structure

- `dzd_telemetry_processor.rs` - Core telemetry processing logic
- `data_store.rs` - Data storage and management utilities
- `util.rs` - Helper functions and utilities
- `settings.rs` - Configuration management

## Key Concepts

### Private Links

Direct device-to-device connections with detailed performance metrics:

- Origin and target device codes
- RTT statistics (mean, median, p95, p99)
- Jitter measurements
- Packet loss rates
- Sample counts

### Public Links

Aggregated location-to-location connectivity:

- Location-based grouping
- Combined statistics from multiple devices
- Network-wide performance view

### Metrics Calculated

- **RTT (Round-Trip Time)**: Network latency measurements
- **Jitter**: Variation in latency (network stability indicator)
- **Packet Loss**: Percentage of lost packets
- **Statistical Percentiles**: p95 and p99 for performance analysis

## Usage

```rust
use metrics_processor::dzd_telemetry_processor::{
    process_dzd_telemetry,
    DZDTelemetryStatMap
};
use data_fetcher::types::DeviceLatencySamples;

// Process telemetry data
let stats_map = process_dzd_telemetry(
    &telemetry_data,      // Vec<DeviceLatencySamples>
    &device_pk_to_code,   // Device public key to code mapping
    before_us,            // End timestamp in microseconds
    after_us              // Start timestamp in microseconds
)?;

// Access processed metrics
for (circuit_key, stats) in stats_map.iter() {
    println!("Link: {} -> {}", stats.origin_code, stats.target_code);
    println!("Mean RTT: {}μs", stats.mean);
    println!("Packet Loss: {}%", stats.packet_loss);
}
```

## Dependencies

- `data_fetcher` - For telemetry data types
- `network-shapley` - For Shapley value calculations
- `rust_decimal` - For precise decimal calculations
- `chrono` - For timestamp handling

## Performance Considerations

- Processes large volumes of telemetry data efficiently
- Uses sorted data structures for percentile calculations
- Minimizes memory allocations through pre-sizing collections
- Supports parallel processing where applicable

## Error Handling

The processor handles various edge cases:

- Empty or missing telemetry data
- Time range boundary conditions
- Invalid sample timestamps
- Division by zero in statistics calculations
