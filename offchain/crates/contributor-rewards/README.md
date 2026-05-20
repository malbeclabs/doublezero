# contributor-rewards

An off-chain rewards calculation system for the DoubleZero network that uses Shapley values to ensure fair distribution of rewards based on network contributions.

## Overview

- Fetches on-chain serviceability and telemetry data from DZ Ledger
- Processes network performance metrics (latency, jitter, packet loss)
- Calculates fair reward distributions using Shapley values
- Generates a Merkle root for on-chain verification

This ensures that network participants are rewarded proportionally to their actual contribution to network performance and reliability.

## Reward calculation methodology

For each DoubleZero epoch, contributor rewards are calculated by turning the observed network state into a `network-shapley` model, computing each contributor's marginal value, and publishing the resulting reward shares for on-chain distribution.

The rewarder prepares the following inputs:

- **Devices:** serviceability devices are mapped to Shapley device IDs by metro and assigned to their contributor owners. Device edge capacity comes from physical interface bandwidth.
- **Private links:** activated DoubleZero links are modeled with measured P95 latency, bandwidth, and observed uptime. The `network-shapley` library applies the uptime penalty when it converts private links into effective capacity.
- **Public links:** public internet telemetry is aggregated into city-pair latencies. These links form the public-internet counterfactual against which DoubleZero paths are valued.
- **Demands:** demand rows describe traffic that should be served between cities, including receiver count, traffic per receiver, priority/value, traffic kind, and whether the demand is multicast.
- **City weights:** per-city weights are derived from Solana leader-schedule slot counts for validators in each metro. Per-city Shapley results are aggregated using these weights.

There are two demand classes:

1. **Validator-to-validator IBRL traffic.** IBRL demands are generated between validator metros, excluding same-city pairs. The receiver count is the destination city's validator count, traffic per receiver comes from `demand.traffic`, and the priority/value input is `demand.priority`. In production this priority is set to **$20**. This gives validator-to-validator traffic non-zero value even though it generates no immediate revenue, unlike multicast shreds, which generate roughly $30-100 per seat. The value reflects non-monetary protocol benefits such as validator acquisition and a foundation for future business models.
2. **Multicast shred traffic.** Shred demands are generated from validator metros to metros with multicast subscribers and non-zero metro prices. The receiver count is the subscriber count, traffic per receiver comes from `demand.traffic`, and priority comes from the destination metro price.

For each source city, the rewarder runs `network-shapley` with the full network topology and that city's demands. `network-shapley` validates and consolidates the inputs, adds public-internet nodes and crossovers between private and public paths, builds a flow optimization problem, and solves that problem for every coalition of contributors. Each contributor's Shapley value is its average marginal improvement across coalitions. The rewarder then aggregates per-city values using the city weights, normalizes them into final proportions, converts those proportions into fixed-point reward shares, stores the Shapley output, and posts a Merkle root for distribution.

### Public internet counterfactual

All measured public latencies are multiplied by **1.25** to better reflect actual public latencies for meaningful traffic loads. In the current system, public latencies are measured using single-packet pings. In practice, validators send 50-150 Mbps of traffic, which would encounter queuing over the public internet at such volumes, so single-packet pings are measured too low for the true public internet counterfactual. DoubleZero does not face the same problem on its own links because those links are dedicated.

The protocol cannot yet reliably estimate public internet latencies for large traffic loads directly given the costs of doing so, so it extrapolates from individual pings using a multiplier. We model the loaded-to-baseline latency multiplier on a public-internet path as an independent M/M/1 queue at each router. The queueing-component multiplier is `1 + p/(1-p)` at bottleneck utilization `p`, where `p` ranges between 0 and 1. Ahmed et al. (ICNP 2017) estimates an implied `p` of between 0.3 and 0.5 based on queueing delays in a 510K-client / 33-IXP CDN study. To be conservative for now in the absence of direct DoubleZero evidence — and given that Koneva et al. (2024) validate the M/M/1 envelope as an upper bound on real IXP packet-size distributions — we use `p = 0.2`, i.e. a 1.25x multiplier.

[1] Ahmed, Shafiq, Bedi, Khakpour. "Peering vs. Transit." ICNP 2017.

[2] Koneva et al. arXiv:2406.16452, 2024.

## Fetching snapshots from S3

Replace `<num>` with DZ Epoch (50, 51, .. etc)

```bash
$ wget https://doublezero-contributor-rewards-mn-beta-snapshots.s3.us-east-1.amazonaws.com/mn-epoch-<num>-snapshot.json
```

## Snapshot Compatibility Matrix

| DZ Epoch Lower Bound | DZ Epoch Upper Bound | Contributor Rewards Version |
| -------------------- | -------------------- | --------------------------- |
| -                    | 48                   | v0.2.2                      |
| 49                   | 109                  | v0.3.5                      |
| 110                  | 138                  | v0.4.1                      |
| 139                  | -                    | v0.5.3                      |

In order to check you can follow these steps (depending on tags/snapshots) as shown in the table above.

```bash
$ git checkout contributor-rewards/v0.2.2
$ just build
$ /target/release/doublezero-contributor-rewards -c mainnet-beta.toml inspect shapley -s mn-epoch-47-snapshot.json -f json --output-file 47.json
```

## Reward Inspection

This prints the record addresses for different accounts off of the DZ ledger

```
$ /target/release/doublezero-contributor-rewards -c mainnet-beta.toml inspect rewards -e 57
```

## Configuration file reference

Copy below and put it in `mainnet-beta.toml`

```toml
# DoubleZero Contributor Rewards - Example Configuration
#
# This file has all available configuration options.
# Copy this file and modify values as needed, or use environment variables (recommended)
#
# Environment variables take precedence over this file.
# Use DZ__ prefix with double underscores for nested values.
# Example: DZ__RPC__DZ_URL=https://api.doublezero.com

# Network Configuration
# Options: devnet, testnet, mainnet-beta, mainnet
network = "mainnet-beta"

# Logging level
# Options: trace, debug, info, warn, error
log_level = "info"

# ========== RPC Configuration ==========
[rpc]
# DoubleZero ledger RPC endpoint
dz_url = "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab"

# Solana read client - for reading chain data (leader schedules, epoch info)
# Typically points to mainnet for production data
solana_read_url = "https://api.mainnet-beta.solana.com"

# Solana write client - for writing rewards data (merkle roots)
# Can point to testnet for testing or mainnet for production
solana_write_url = "https://api.mainnet-beta.solana.com"

# Transaction commitment level
# Options: processed, confirmed, finalized
commitment = "confirmed"

# Rate limit for RPC requests per second
rps_limit = 10

# ========== Shapley Value Parameters ==========
[shapley]
# Base uptime requirement for operators (0.0-1.0)
# Example: 0.98 means 98% uptime required
operator_uptime = 0.98

# Bonus multiplier for contiguous network coverage
# Applied when nodes provide continuous coverage across regions
contiguity_bonus = 5.0

# Multiplier for demand-based rewards
# Increases rewards in high-demand areas
demand_multiplier = 1.2

# ========== Shapley Input Parameters ==========
[input]
# Multiplier applied to public internet latency inputs.
# 1.25 inflates single-packet public latency measurements by 25%.
public_latency_multiplier = 1.25

# ========== Demand Generation Parameters ==========
[demand]
# Traffic per receiver in Gbps, used for both IBRL and shred demands
traffic = 0.15

# Priority for IBRL validator-to-validator demands
# Shred demand priority remains derived from metro price
priority = 20.0

# Demand kind/type values written into Shapley demand rows
kind = 1
shred_kind = 2

# Multicast flags for IBRL and shred demands
multicast_enabled = false
shred_multicast_enabled = true

# ========== Program IDs ==========
[programs]
# DZ Serviceability program ID
serviceability_program_id = "ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv"

# DZ Telemetry program ID
telemetry_program_id = "tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC"

# ========== Record Prefixes ==========
[prefixes]
# Prefixes for organizing DZ records on-chain
device_telemetry = "doublezero_device_telemetry_aggregate"
internet_telemetry = "doublezero_internet_telemetry_aggregate"
contributor_rewards = "dz_contributor_rewards"
reward_input = "dz_reward_input"

# ========== Internet Telemetry Lookback Configuration ==========
[inet_lookback]
# Minimum coverage threshold (0.0-1.0)
# Example: 0.8 means at least 80% of expected links must have data
min_coverage_threshold = 0.8

# Maximum number of epochs to look back when current data is insufficient
max_epochs_lookback = 5

# Minimum samples per link to consider it valid
min_samples_per_link = 20

# Enable lookback accumulator
# When true, combines data from multiple epochs to meet coverage threshold
enable_accumulator = true

# Deduplication window in microseconds
# Samples within this time window are considered duplicates
dedup_window_us = 10000000

# ========== Telemetry Default Handling Configuration ==========
[telemetry_defaults]
# Threshold for missing data (0.0-1.0)
# Example: 0.7 means if >70% of samples are missing, use defaults
missing_data_threshold = 0.7

# Default latency for private links when data is missing (in milliseconds)
# Example: 1000.0 means use 1000ms for circuits with insufficient data
private_default_latency_ms = 1000.0

# Enable previous epoch lookup for public links
# When true, fetches previous epoch's average when current has insufficient data
enable_previous_epoch_lookup = true

# ========== Scheduler Configuration ==========
[scheduler]
# Check interval in seconds (how often to check for new epochs)
interval_seconds = 30

# Path to worker state file for tracking processed epochs
state_file = "./test.state"

# Enable dry run mode (no on-chain writes)
enable_dry_run = true

# snapshot dir
snapshot_dir = "./"

# old setting
max_consecutive_failures = 10

# ========== AWS Configuration (Optional) ==========
[aws]
region = "us-east-1"
bucket = "doublezero-contributor-rewards-mn-beta-snapshots"
access_key_id = "not"
secret_access_key = "required"

# ========== Metrics Configuration (Optional) ==========
[metrics]
# Address to expose metrics endpoint
# Format: "IP:PORT" or "[IPv6]:PORT"
addr = "127.0.0.1:9090"

[slack]
enabled = false
webhook_url = "foo"
channel_id = "bar"
```
