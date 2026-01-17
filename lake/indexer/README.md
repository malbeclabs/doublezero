# Indexer

Background service that continuously synchronizes data from external sources into ClickHouse for analytics.

## Architecture

### Views

The indexer is organized around **Views** - components that own a specific data domain and handle:
- Periodic refresh from source systems
- Transformation into the analytics schema
- Writing to ClickHouse

Each View operates independently with its own refresh interval and can depend on other Views for enrichment (e.g., GeoIP view depends on Serviceability view for device IPs).

| View | Source | Description |
|------|--------|-------------|
| **Serviceability** | Solana (DZ program) | Network topology: devices, metros, links, contributors, users |
| **Telemetry Latency** | Solana (DZ program) | Latency measurements between devices and to internet endpoints |
| **Telemetry Usage** | InfluxDB | Device interface counters (bandwidth utilization) |
| **Solana** | Solana (mainnet) | Validator stakes, vote accounts, leader slots |
| **GeoIP** | MaxMind + other Views | IP geolocation enrichment for devices and validators |

### Stores vs Views

Each data domain has two components:

- **Store**: Low-level ClickHouse operations (read/write dimension or fact data)
- **View**: Orchestrates refresh cycle, source fetching, transformation, and uses Store for persistence

Views are stateless and restart-safe - they query ClickHouse for current state on each refresh cycle.

## Data Model

The indexer uses dimensional modeling with two dataset types:

### Dimensions (Type 2 SCD)

Slowly-changing dimension tables that track entity state over time:

- `dim_<name>_current` - Latest state of each entity (materialized view)
- `dim_<name>_history` - Full history with `snapshot_ts` timestamps
- `dim_<name>_staging` - Incoming writes before deduplication

Each dimension row includes:
- `entity_id` - Hash of primary key columns
- `snapshot_ts` - When this state was observed
- `attrs_hash` - Hash of attribute columns for change detection
- `is_deleted` - Soft delete flag

### Facts

Time-series event tables for metrics and measurements:

- `fact_<name>` - Append-only event stream
- Partitioned by time for efficient range queries
- Deduplication via ReplacingMergeTree

### Change Detection

Dimensions use content-based change detection:
1. Fetch current state from source
2. Compute `attrs_hash` of attribute columns
3. Compare against latest `attrs_hash` in ClickHouse
4. Only write if hash differs (actual change)

This prevents duplicate history entries when source data hasn't changed.

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

## Package Structure

```
lake/indexer/
├── cmd/indexer/          # Main entrypoint
├── migrations/           # ClickHouse schema migrations (goose)
└── pkg/
    ├── clickhouse/       # ClickHouse client and migrations
    │   ├── dataset/      # Generic dimension/fact table operations
    │   └── testing/      # Test helpers for ClickHouse containers
    ├── dz/
    │   ├── serviceability/   # Network topology view
    │   └── telemetry/
    │       ├── latency/      # Latency measurements view
    │       └── usage/        # Interface counters view
    ├── geoip/            # IP geolocation view
    ├── sol/              # Solana validator view
    ├── indexer/          # View orchestration
    ├── server/           # HTTP server (health, metrics)
    └── metrics/          # Prometheus metrics
```

## Configuration

### Flags

| Flag | Description |
|------|-------------|
| `--dz-env` | DZ ledger environment (devnet, testnet, mainnet-beta) |
| `--solana-env` | Solana environment: devnet, testnet, mainnet-beta (determines Solana mainnet RPC URL) |
| `--clickhouse-addr` | ClickHouse server address (host:port) |
| `--clickhouse-database` | ClickHouse database name |
| `--clickhouse-username` | ClickHouse username |
| `--clickhouse-password` | ClickHouse password |
| `--clickhouse-secure` | Enable TLS for ClickHouse Cloud |
| `--geoip-city-db-path` | Path to MaxMind GeoIP2 City database |
| `--geoip-asn-db-path` | Path to MaxMind GeoIP2 ASN database |

### Environment Variables

| Variable | Description |
|----------|-------------|
| `DZ_ENV` | DZ ledger environment (overrides `--dz-env` flag) |
| `DZ_LEDGER_RPC_URL` | Override the default DZ ledger RPC URL for the environment |
| `CLICKHOUSE_ADDR_TCP` | ClickHouse server address (overrides flag) |
| `CLICKHOUSE_DATABASE` | Database name (overrides flag) |
| `CLICKHOUSE_USERNAME` | Username (overrides flag) |
| `CLICKHOUSE_PASSWORD` | Password (overrides flag) |
| `CLICKHOUSE_SECURE` | Set to "true" to enable TLS |
| `GEOIP_CITY_DB_PATH` | Path to MaxMind GeoIP2 City database |
| `GEOIP_ASN_DB_PATH` | Path to MaxMind GeoIP2 ASN database |
| `INFLUX_URL` | InfluxDB server URL (optional, enables usage view) |
| `INFLUX_TOKEN` | InfluxDB auth token |
| `INFLUX_BUCKET` | InfluxDB bucket name |

## Migrations

Schema migrations are managed with goose and embedded in the binary. They run automatically on startup.

Migrations live in `migrations/` and follow the naming convention `YYYYMMDDHHMMSS_description.sql`.

## Admin CLI

The `lake/admin` CLI provides maintenance operations:

- `reset-db` - Drop and recreate all tables
- `backfill-device-link-latency` - Backfill device link latency from historical data
- `backfill-internet-metro-latency` - Backfill internet metro latency from historical data
- `backfill-device-interface-counters` - Backfill usage metrics from InfluxDB
