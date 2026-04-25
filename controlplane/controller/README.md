# DoubleZero Controller

The controller generates device configurations from Solana smart contract state and serves them to agents running on network devices via gRPC.

## Configuration

### ClickHouse Integration

The controller can optionally write GetConfig request metrics to ClickHouse. Configure via environment variables:

```bash
CLICKHOUSE_ADDR=https://your-instance.clickhouse.cloud:8443
CLICKHOUSE_DB=devnet
CLICKHOUSE_USER=controller_devnet
CLICKHOUSE_PASS=your_password
CLICKHOUSE_TLS_DISABLED=false
```

#### Required ClickHouse Permissions

Create the user and grant necessary permissions:

```sql
-- Create user
CREATE USER controller_devnet IDENTIFIED BY 'your_password';

-- Grant permissions
GRANT SELECT, INSERT, CREATE TABLE, SHOW COLUMNS ON devnet.controller_grpc_getconfig_success TO controller_devnet;
GRANT SELECT, INSERT, CREATE TABLE, SHOW COLUMNS ON devnet.controller_agent_versions TO controller_devnet;
```

The controller automatically creates both tables on startup and batch-inserts every 10 seconds.

#### Table Schemas

**GetConfig events** — one row per successful GetConfig poll:

```sql
CREATE TABLE devnet.controller_grpc_getconfig_success (
    timestamp DateTime64(3),
    device_pubkey LowCardinality(String)
) ENGINE = MergeTree
PARTITION BY toYYYYMM(timestamp)
ORDER BY (timestamp, device_pubkey)
```

**Agent versions** — latest agent and controller version per device. Uses `ReplacingMergeTree` so ClickHouse merges rows down to one per device; query with `FINAL` for deduplicated results:

```sql
CREATE TABLE devnet.controller_agent_versions (
    device_pubkey LowCardinality(String),
    updated_at DateTime64(3),
    agent_version LowCardinality(String) DEFAULT '',
    agent_commit LowCardinality(String) DEFAULT '',
    agent_date LowCardinality(String) DEFAULT '',
    controller_version LowCardinality(String) DEFAULT '',
    controller_commit LowCardinality(String) DEFAULT '',
    controller_date LowCardinality(String) DEFAULT ''
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY device_pubkey
```
