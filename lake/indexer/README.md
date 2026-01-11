# Indexer

The indexer is a background service that continuously synchronizes data from external sources into ClickHouse for analytics.

## Architecture

### Views

The indexer is organized around **Views** - components that own a specific data domain and handle:
- Periodic refresh from source systems
- Transformation into the analytics schema
- Writing to ClickHouse

Each View operates independently with its own refresh interval and can depend on other Views for enrichment (e.g., GeoIP view depends on Serviceability view for device IPs).

### Current Views

| View | Source | Description |
|------|--------|-------------|
| **Serviceability** | Solana (DZ program) | Network topology: devices, metros, links, contributors, users |
| **Telemetry Latency** | Solana (DZ program) | Latency measurements between devices and to internet endpoints |
| **Telemetry Usage** | InfluxDB | Device interface counters (bandwidth utilization) |
| **Solana** | Solana (mainnet) | Validator stakes, vote accounts, leader slots |
| **GeoIP** | MaxMind + other Views | IP geolocation enrichment for devices and validators |

## Data Model

The indexer uses dimensional modeling with two dataset types:

### Dimensions (Type 2 SCD)

Slowly-changing dimension tables that track entity state over time:

- `dim_<name>_current` - Latest state of each entity
- `dim_<name>_history` - Full history with `snapshot_ts` timestamps
- `dim_<name>_tombstone` - Deleted entities

Each dimension row includes:
- `entity_id` - Hash of primary key columns
- `snapshot_ts` - When this state was observed
- `attrs_hash` - Hash of payload columns for change detection
- `is_deleted` - Soft delete flag

### Facts

Time-series event tables for metrics and measurements:

- `fact_<name>` - Append-only event stream
- Partitioned by time for efficient range queries
- Optional deduplication via ReplacingMergeTree

## Data Flow

```
External Sources          Indexer Views           ClickHouse
─────────────────         ─────────────           ──────────

Solana RPC ──────────────► Serviceability ───────► dim_device_*
(DZ Programs)                    │                 dim_metro_*
                                 │                 dim_link_*
                                 │                 dim_contributor_*
                                 │                 dim_user_*
                                 ▼
Solana RPC ──────────────► Telemetry Latency ───► fact_latency_*
(DZ Programs)

InfluxDB ────────────────► Telemetry Usage ─────► fact_device_interface_counters

Solana RPC ──────────────► Solana ──────────────► dim_validator_*
(mainnet)                                         fact_leader_slot

MaxMind DB ──────────────► GeoIP ───────────────► dim_ip_geo
                                                  (enriches other dims)
```

## Change Detection

Dimensions use content-based change detection:
1. Fetch current state from source
2. Compute `attrs_hash` of payload columns
3. Compare against latest `attrs_hash` in ClickHouse
4. Only write if hash differs (actual change)

This prevents duplicate history entries when source data hasn't changed.

## Stores vs Views

Each data domain has two components:

- **Store**: Low-level ClickHouse operations (read/write dimension or fact data)
- **View**: Orchestrates refresh cycle, source fetching, transformation, and uses Store for persistence

Views are stateless and restart-safe - they query ClickHouse for current state on each refresh cycle.
