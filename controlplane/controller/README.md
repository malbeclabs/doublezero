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
```

The controller will automatically create the `controller_grpc_getconfig_success` table on startup and batch-insert GetConfig events every 10 seconds.

#### Table Schema

```sql
CREATE TABLE devnet.controller_grpc_getconfig_success (
    timestamp DateTime64(3),
    device_pubkey LowCardinality(String)
) ENGINE = MergeTree
PARTITION BY toYYYYMM(timestamp)
ORDER BY (timestamp, device_pubkey)
```
