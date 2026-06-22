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

## Metrics

The controller exposes Prometheus metrics on `127.0.0.1:2112/metrics`, including
per-device series labeled by `pubkey` (e.g. `controller_grpc_getconfig_requests_total`).

### Pruning on ledger removal

The controller rebuilds its state cache from on-chain data every 10 seconds. When
a device that was present in the previous cache is gone from the new one (removed
from the ledger), the controller drops that pubkey's series from the per-device
metric vectors. Prometheus can then no longer scrape them, and after a scrape
interval plus the staleness window (~5 minutes) the series go stale and queries
return empty. This prevents a removed device's frozen counter from looking
perpetually "fresh" and keeping the `Network: Device Stopped Calling Controller`
alert firing indefinitely.

### Check-ins from ledger-absent pubkeys

A device removed from the ledger may keep calling `GetConfig` with its old pubkey.
Such requests are rejected with `NotFound`, logged at `WARN`
(`device not found in ledger cache; refusing config ...`), and counted on the
low-cardinality aggregate `controller_grpc_getconfig_unknown_pubkey_total`. This
counter deliberately carries no per-pubkey label: `pubkey` is a caller-supplied
value, so labeling by it would let an unknown caller blow up metric cardinality.
The controller does not register any per-pubkey series for these requests, so a
pruned pubkey is not resurrected by a decommissioned box that is still calling in.
This counter replaces the former per-pubkey `controller_grpc_getconfig_pubkey_errors_total`.

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
