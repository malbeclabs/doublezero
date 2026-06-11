# DoubleZero Controller

The controller generates device configurations from Solana smart contract state and serves them to agents running on network devices via gRPC.

## Architecture

### Agent-Controller Communication Flow

The controller provides a gRPC endpoint (GetConfig) that returns the device configuration. The agent polls the controller every 5 seconds, but only applies the configuration to the EOS device when it has changed (based on local hash computation) or after a 60-second timeout.

The design includes an optimization to reduce EOS device CPU usage:
- Applying configuration to an Arista EOS device causes the EOS ConfigAgent process CPU to spike
- The agent computes a SHA256 hash of the received config and only applies it when:
  1. The hash differs from the last applied configuration, OR
  2. 60 seconds have elapsed since the last application (as a safety measure)

Here's how the agent uses the endpoint:

```
┌─────────┐                  ┌────────────┐                  ┌─────────┐
│  Agent  │                  │ Controller │                  │   EOS   │
│  main() │                  │ GetConfig()│                  │ Device  │
│         │                  │   (gRPC)   │                  │         │
└────┬────┘                  └─────┬──────┘                  └────┬────┘
     │                             │                              │
     │ Every 5s:                   │                              │
     │                             │                              │
     │ GetBgpNeighbors()           │                              │
     ├────────────────────────────────────────────────────────────►│
     │◄────────────────────────────────────────────────────────────┤
     │ [peer IPs]                  │                              │
     │                             │                              │
     │ GetConfigFromServer()       │                              │
     ├────────────────────────────►│                              │
     │                             │ deduplicateTunnels()         │
     │                             │ renderConfig()               │
     │                             │   (~50KB config text)        │
     │◄────────────────────────────┤                              │
     │ ConfigResponse{config: "..."}                              │
     │                             │                              │
     │ Compute SHA256 hash locally │                              │
     │ Compare with cached hash    │                              │
     │ If changed OR 60s elapsed:  │                              │
     │   AddConfigToDevice(config) │                              │
     ├────────────────────────────────────────────────────────────►│
```

**Key Benefits:**
- **CPU**: EOS device only processes config when it actually changes (or every 60s as safety)
- **Responsiveness**: Still checks for changes every 5 seconds
- **Simplicity**: Single endpoint, agent handles caching logic
- **Safety**: Full config application every 60s ensures eventual consistency

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
