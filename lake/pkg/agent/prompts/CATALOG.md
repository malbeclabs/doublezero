# Data Catalog

Datasets and views organized by domain. Prefer views over raw datasets.

---

## Table Types

### SCD2 (Slowly Changing Dimension Type 2)

Track historical changes with validity windows:

- `{table}_current`: Current state snapshot
- `{table}_history`: Historical versions

**Query patterns:**

```sql
-- Current state
SELECT * FROM dz_devices_current

-- Point-in-time
SELECT * FROM dz_devices_history
WHERE valid_from <= T AND (valid_to IS NULL OR valid_to > T)
```

### Fact Tables

Append-only time-series. **Time filters required.**

```sql
SELECT ...
FROM dz_device_link_latency_samples_raw
WHERE time >= $__timeFrom() AND time <= $__timeTo()
```

Never use `date_trunc()` in WHERE (prevents partition pruning).

---

## DoubleZero Network

### dz_contributors

- Type: SCD2
- Grain: 1 row per contributor code
- Join: `dz_devices_current.contributor_pk = dz_contributors_current.pk`

### dz_devices

- Type: SCD2
- Grain: 1 row per device code
- Key fields: `code`, `status`, `device_type`, `metro_pk`
- Reporting: Always use `code`, never `pk` or `host`
- Status values: pending, activated, suspended, deleted, rejected, soft-drained, hard-drained

### dz_metros

- Type: SCD2
- Grain: 1 row per metro code
- Key fields: `code`, `name`, `longitude`, `latitude`
- Format: `nyc → lon`

### dz_links

- Type: SCD2
- Grain: 1 row per link code
- Key fields: `link_type`, `status`, `committed_rtt_ns`, `committed_jitter_ns`, `bandwidth_bps`
- Link types:
  - `WAN` = inter-metro (compare vs Internet)
  - `DZX` = intra-metro (do NOT compare to Internet)
- Drain: `isis_delay_override_ns = 1000000000` means soft-drained

### dz_users

- Type: SCD2
- Grain: 1 row per user pubkey (latest state)
- Active users: `status = 'activated' AND dz_ip IS NOT NULL`
- Exclude QA: `owner_pk != 'DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan'`
- Stable ID: `(owner_pk, client_ip)` — user `pk` changes on reconnect
- Composite key: `(device_pk, tunnel_id)` — tunnel_id not globally unique

### dz_device_link_latency_samples_raw

- Type: Fact
- Grain: 1 sample per link × direction × timestamp
- Loss: `rtt_us = 0`
- Units: samples in µs, committed values in ns
- Bidirectional: (A→B, link) and (B→A, link) both valid

### dz_internet_metro_latency_samples_raw

- Type: Fact
- Grain: 1 sample per metro pair × timestamp
- Public Internet baseline for comparison
- No loss signal

### dz_device_iface_usage_raw

- Type: Fact
- Grain: 1 sample per interface × timestamp
- Values are deltas (counter wrap handled)
- `carrier_transitions` = link flaps
- Directionality from device perspective:
  - `in_*` = into device
  - `out_*` = out of device
- User tunnel: `user_tunnel_id` extracted from interface name
- Multicast on tunnels: Use `in_pkts_delta`/`out_pkts_delta` (multicast-specific fields unreliable)

---

## Solana

### Node Types

| Type         | Table                  | Description                                       |
| ------------ | ---------------------- | ------------------------------------------------- |
| Gossip nodes | `solana_gossip_nodes`  | All network participants (validators + RPC nodes) |
| Validators   | `solana_vote_accounts` | Consensus participants with vote accounts         |

**Key identity**: `vote_pubkey` is stable. `node_pubkey` and IP can change.

### solana_gossip_nodes

- Type: SCD2
- Grain: 1 row per node pubkey
- Contains ALL participants (validators and non-validators)
- Join to `solana_vote_accounts` to identify validators

### solana_vote_accounts

- Type: SCD2
- Grain: 1 row per vote_pubkey
- Contains ONLY validators
- Staked: `epoch_vote_account = true AND activated_stake_lamports > 0`
- Convert: `lamports / 1e9` → SOL

### solana_leader_schedule

- Type: SCD2
- Grain: 1 row per node × epoch
- View: `solana_leader_schedule_vs_production_current`

### solana_vote_account_activity_raw

- Type: Fact
- Grain: 1 sample per vote_pubkey × minute
- Metrics: Vote lag, Root lag, Credits delta

### solana_block_production_raw

- Type: Fact
- Grain: 1 row per leader × epoch (cumulative)
- View: `solana_block_production_delta` for hourly deltas

---

## GeoIP

### geoip_records

- Type: SCD2
- Grain: 1 row per IP
- Mention accuracy limitations when using

---

## ISIS Network Topology

ISIS (Intermediate System to Intermediate System) provides real-time network routing data. Use ISIS tools for topology questions, then correlate with SQL telemetry.

### Tools

| Tool                 | Purpose                         | Returns                              |
| -------------------- | ------------------------------- | ------------------------------------ |
| `isis_refresh`       | Fetch latest topology from S3   | Network summary stats                |
| `isis_get_summary`   | Get cached network statistics   | Router count, link count, health %   |
| `isis_list_routers`  | List all routers                | Array of router summaries            |
| `isis_get_router`    | Get specific router details     | Full router object with neighbors    |
| `isis_get_adjacencies` | Get network graph             | Array of {Source, Dest, Metric}      |

### Router Schema

```json
{
  "Hostname": "DZ-NYC-SW01",
  "RouterID": "172.16.0.1",
  "SystemID": "0000.0000.0001",
  "RouterType": "L2",
  "Area": "49.0001",
  "Location": "NYC",
  "IsOverloaded": false,
  "Neighbors": [
    {"Hostname": "DZ-CHI-SW01", "Metric": 200, "NeighborAddr": "10.0.1.2"}
  ],
  "NodeSID": 101,
  "SRGBBase": 900000,
  "SRGBRange": 65536
}
```

### Adjacency Schema

```json
{"Source": "DZ-NYC-SW01", "Dest": "DZ-CHI-SW01", "Metric": 200}
```

### When to Use

| Question Type              | Use ISIS               | Use SQL                        |
| -------------------------- | ---------------------- | ------------------------------ |
| Path from NYC to LON       | ✓ isis_get_adjacencies | —                              |
| Which routers in Chicago?  | ✓ isis_list_routers    | —                              |
| Latency on NYC-CHI link    | ✓ for topology         | ✓ for telemetry                |
| Errors on DZ-NYC-SW01      | ✓ for device ID        | ✓ dz_device_iface_health       |
| Validators connected now   | —                      | ✓ solana_validators_connected_now |

### Workflow

1. `isis_refresh` — Fetch fresh topology (if stale)
2. `isis_list_routers` or `isis_get_adjacencies` — Get topology context
3. SQL query — Get telemetry/metrics
4. Correlate — Join ISIS topology with SQL results

---

## Key Views

### DoubleZero Views

| View                                         | Purpose                                         |
| -------------------------------------------- | ----------------------------------------------- |
| `dz_active_users_view`                       | User counts & telemetry (excludes QA)           |
| `dz_links_current_health`                    | Link health with operational flags              |
| `dz_link_telemetry`                          | Sample-level link telemetry with jitter         |
| `dz_link_telemetry_with_committed`           | Telemetry with violation flags                  |
| `dz_device_iface_health`                     | Interface errors, discards, carrier transitions |
| `dz_link_traffic`                            | Link-level traffic (sums A and Z sides)         |
| `dz_device_traffic`                          | Device-level traffic                            |
| `dz_user_device_traffic`                     | User-level traffic (ASOF JOIN)                  |
| `dz_metro_to_metro_latency`                  | Metro-to-metro latency aggregation              |
| `dz_vs_public_internet_metro_to_metro_named` | DZ vs Internet comparison                       |

### Solana Views

| View                                          | Purpose                                              |
| --------------------------------------------- | ---------------------------------------------------- |
| `solana_gossip_at_ip_now`                     | Active gossip nodes with IPs                         |
| `solana_validators_connected_now`             | Validators on DZ (validator-grain, safe for rollups) |
| `solana_validators_connected_now_connections` | Connection-grain bridge                              |
| `solana_validator_dz_connection_events`       | Connection/disconnection event log                   |
| `solana_validator_dz_first_connection_events` | First connection per validator (deduplicated)        |
| `solana_validator_dz_overlaps_windowed`       | On-DZ vs off-DZ classification                       |
| `solana_block_production_delta`               | Hourly block production deltas                       |

---

## Column Schemas

**Use these exact column names. Do not invent columns.**

### Base Tables

#### dz_users_current
```
pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, as_of_ts
```

#### dz_devices_current
```
pk, status, device_type, code, public_ip, contributor_pk, metro_pk, max_users, as_of_ts
```

#### dz_links_current
```
pk, status, code, link_type, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name,
committed_rtt_ns, committed_jitter_ns, bandwidth_bps, isis_delay_override_ns, as_of_ts
```

#### dz_metros_current
```
pk, code, name, longitude, latitude, as_of_ts
```

#### solana_gossip_nodes_current
```
pubkey, gossip_ip, gossip_port, tpuquic_ip, tpuquic_port, version, epoch, as_of_ts
```
⚠️ Column is `gossip_ip`, NOT `dz_ip`. Column is `pubkey`, NOT `node_pubkey`.

#### solana_vote_accounts_current
```
vote_pubkey, node_pubkey, epoch, epoch_vote_account, activated_stake_lamports, commission_percentage, as_of_ts
```

#### dz_device_iface_usage_raw (Fact table)
```
time, device_pk, host, intf, user_tunnel_id, link_pk, link_side,
in_octets_delta, out_octets_delta, in_pkts_delta, out_pkts_delta,
in_errors_delta, out_errors_delta, in_discards_delta, out_discards_delta,
carrier_transitions_delta, in_multicast_pkts_delta, out_multicast_pkts_delta, delta_duration
```
⚠️ NO `owner_pk` column. NO `in_bytes_delta`. Use `in_octets_delta`. Column is `intf`, NOT `iface_name`.

#### solana_vote_account_activity_raw (Fact table)
```
time, vote_account_pubkey, node_identity_pubkey, root_slot, last_vote_slot, cluster_slot,
is_delinquent, credits_delta, activated_stake_lamports, activated_stake_sol, commission
```

#### solana_block_production_raw (Fact table)
```
epoch, time, leader_identity_pubkey, leader_slots_assigned_cum, blocks_produced_cum
```

### Key Views

#### dz_device_iface_health
```
time, device_pk, host, intf, link_pk, link_side,
in_errors, out_errors, in_discards, out_discards, carrier_transitions
```
⚠️ Column is `intf`, NOT `iface_name`. NO `_delta` suffix in this view.

#### dz_user_device_traffic
```
time, device_pk, device_code, user_tunnel_id, user_pk,
total_octets_delta, throughput_bps, in_throughput_bps, out_throughput_bps, total_pkts_delta
```
⚠️ `user_pk` joins to `dz_users_history.pk`. For `owner_pk`/`client_ip`, join to dz_users_history.

#### dz_vs_public_internet_metro_to_metro_named
```
time, origin_metro_pk, origin_metro_code, origin_metro_name,
target_metro_pk, target_metro_code, target_metro_name, data_provider,
dz_rtt_us, dz_is_loss, dz_jitter_us, internet_rtt_us, internet_jitter_us,
rtt_improvement_us, rtt_ratio_dz_over_internet, jitter_improvement_us, jitter_ratio_dz_over_internet
```

#### dz_link_health
```
time, link_pk, link_code, status, is_soft_drained, is_hard_drained, link_type,
rtt_us, loss, jitter_ipdv_us, committed_rtt_us, rtt_minus_committed_us,
side_a_device_pk, side_a_intf, side_z_device_pk, side_z_intf,
side_a_in_errors, side_a_out_errors, side_a_carrier_transitions,
side_z_in_errors, side_z_out_errors, side_z_carrier_transitions
```

#### solana_block_production_delta
```
epoch, time, leader_identity_pubkey,
leader_slots_assigned_cum, blocks_produced_cum, slots_skipped_cum,
leader_slots_assigned_delta, blocks_produced_delta, slots_skipped_delta, produce_rate_cum
```

#### solana_validators_connected_now
```
vote_pubkey, node_pubkey, epoch, commission_percentage, activated_stake_lamports, activated_stake_sol
```

#### solana_validator_dz_connection_events
```
vote_pubkey, node_pubkey, dz_user_pk, owner_pk, client_ip, dz_ip, device_pk,
event_time, event_type, epoch, activated_stake_sol, commission_percentage, event_end_marker
```
`event_type` values: `'dz_connected'` or `'dz_disconnected'`

#### solana_validator_dz_overlaps_windowed
```
vote_pubkey, node_pubkey, dz_user_pk, owner_pk, client_ip, device_pk, dz_ip,
overlap_start, overlap_end, epoch, activated_stake_lamports, activated_stake_sol, commission_percentage
```

---

## Query Patterns

### Join Rules

- FK → PK only: `{table}_pk = {target}.pk`
- Composite keys: `(device_pk, tunnel_id)` for user tunnels
- ASOF JOIN for temporal matching

### Metro Pair Comparison

Use `dz_vs_public_internet_metro_to_metro_named` view, or:

1. Join samples: `origin_device_pk` → `dz_devices_current.pk` → `metro_pk`
2. Join target: `target_device_pk` → `dz_devices_current.pk` → `metro_pk`
3. Match with `dz_internet_metro_latency_samples` for same metro pair
4. Only `link_type = 'WAN'`

Report both avg and p95:

```sql
PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY rtt_us)
```

### User Traffic Attribution

1. Extract `user_tunnel_id` from interface name
2. Join: `(device_pk, user_tunnel_id) = (dz_users_current.device_pk, dz_users_current.tunnel_id)`
3. Use ASOF JOIN for temporal matching

### Stake Aggregation

- Aggregate at `vote_pubkey` grain to avoid double-counting
- Use `solana_validators_connected_now` for safe rollups
- "Connected stake" = sum of `activated_stake_lamports` / 1e9

### Validator Connection Queries

Use `solana_validator_dz_first_connection_events`:

```sql
SELECT vote_pubkey, node_pubkey, dz_ip, event_time, activated_stake_sol, owner_pk, client_ip
FROM solana_validator_dz_first_connection_events
WHERE event_time >= $__timeFrom()
  AND event_time <= $__timeTo()
ORDER BY event_time
```

**Key rules:**

- Connection events ≠ stake changes (independent)
- Never infer connections from stake snapshots
- For disconnections, verify full event timeline (check for subsequent reconnection)

### Validator Performance Comparison (On-DZ vs Off-DZ)

Use `solana_validator_dz_overlaps_windowed` to classify, then:

| Metric       | Calculation                                                     | Source                             |
| ------------ | --------------------------------------------------------------- | ---------------------------------- |
| Skip Rate    | `SUM(slots_skipped_delta) / SUM(leader_slots_assigned_delta)`   | `solana_block_production_delta`    |
| Vote Lag     | `AVG(cluster_slot - last_vote_slot)`                            | `solana_vote_account_activity_raw` |
| Produce Rate | `SUM(blocks_produced_delta) / SUM(leader_slots_assigned_delta)` | `solana_block_production_delta`    |

---

## Quick Reference

| Domain     | Current State                  | Stable ID               | Reporting ID             |
| ---------- | ------------------------------ | ----------------------- | ------------------------ |
| Devices    | `dz_devices_current`           | `pk`                    | `code`                   |
| Links      | `dz_links_current`             | `pk`                    | `code`                   |
| Users      | `dz_users_current`             | `(owner_pk, client_ip)` | `owner_pk` + `client_ip` |
| Validators | `solana_vote_accounts_current` | `vote_pubkey`           | `vote_pubkey` + IP       |
| Gossip     | `solana_gossip_nodes_current`  | `node_pubkey`           | —                        |
