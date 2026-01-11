# Catalog Reference

Data dictionary for DoubleZero datasets. For query patterns and examples, see **EXAMPLES.md**.

## Schema Discovery

- **DESCRIBE TABLE**: `DESCRIBE TABLE {table_name}` to see columns and types
- **Ignore `stg_*` tables**: Internal staging tables - never query directly

## Data Types

### SCD2 (Slowly Changing Dimension Type 2)

Track historical changes using snapshot-based design:
- **Current view**: `{table}_current` - latest non-deleted row per entity
- **History table**: `dim_{table}_history` - all historical versions

**Internal columns** (in history tables): `entity_id`, `snapshot_ts`, `ingested_at`, `op_id`, `is_deleted`, `attrs_hash`

**Point-in-time queries**: Filter `dim_{table}_history` where `snapshot_ts <= T AND is_deleted = 0`, use `ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC)` to get latest per entity.

### Fact Tables

Append-only time-series with `fact_*` prefix:
- **Time column**: `event_ts` (DateTime64(3)) - NOT `time`
- **Time filter REQUIRED**: `WHERE event_ts >= ... AND event_ts <= ...`
- Never use `date_trunc()` in WHERE (prevents partition pruning)

---

## DoubleZero Network Datasets

### dz_contributors
Device and link operators.
- **Type**: SCD2 | **Current**: `dz_contributors_current` | **History**: `dim_dz_contributors_history`
- **Grain**: 1 row per contributor code (entity_id)
- **Join**: `dz_devices_current.contributor_pk = dz_contributors_current.pk`

### dz_devices
Hardware switches and routers.
- **Type**: SCD2 | **Current**: `dz_devices_current` | **History**: `dim_dz_devices_history`
- **Grain**: 1 row per device code (entity_id)
- **Key fields**: `code`, `status`, `device_type`, `metro_pk`
- **Status values**: pending, activated, suspended, deleted, rejected, soft-drained, hard-drained
- ⚠️ **Always use `code` for reporting**, never PK/host (host is internal only)

### dz_metros
Geographic regions (exchanges).
- **Type**: SCD2 | **Current**: `dz_metros_current` | **History**: `dim_dz_metros_history`
- **Grain**: 1 row per metro code (entity_id)
- **Key fields**: `code`, `name`, `longitude`, `latitude`
- ⚠️ **Metro format**: ORIGIN → TARGET (e.g., "nyc → lon")

### dz_links
Connections between devices.
- **Type**: SCD2 | **Current**: `dz_links_current` | **History**: `dim_dz_links_history`
- **Grain**: 1 row per link code (entity_id)
- **Key fields**: `link_type`, `status`, `committed_rtt_ns`, `committed_jitter_ns`, `bandwidth_bps`
- **Link types**:
  - `WAN` = inter-metro (compare vs Internet)
  - `DZX` = intra-metro (do NOT compare to Internet)
- **Drain signaling**: `isis_delay_override_ns = 1000000000` means soft-drained
- ⚠️ **Status must be 'activated'** for most analysis

### dz_users
Connected sessions/subscribers.
- **Type**: SCD2 | **Current**: `dz_users_current` | **History**: `dim_dz_users_history`
- **Grain**: 1 row per user pubkey (entity_id)
- **Key fields**: `pk`, `owner_pubkey`, `client_ip`, `dz_ip`, `status`, `device_pk`, `tunnel_id`
- **Active users**: `status = 'activated' AND dz_ip IS NOT NULL`
- **Exclude QA/test**: `owner_pubkey != 'DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan'`
- **"On DZ"**: User exists in `dz_users` with matching `dz_ip`
- ⚠️ **Status is `'activated'`** (not 'active')
- ⚠️ **User pk is NOT stable** - changes after disconnects/reconnects. Use `(owner_pubkey, client_ip)` as stable identifier
- ⚠️ **tunnel_id is NOT globally unique** - use composite key `(device_pk, tunnel_id)`

### fact_dz_device_link_latency
RTT probe measurements.
- **Type**: Fact | **Time column**: `event_ts`
- **Grain**: 1 sample per link × direction × timestamp
- **Key fields**: `origin_device_pk`, `target_device_pk`, `link_pk`, `rtt_us`, `loss`, `ipdv_us`
- **Loss detection**: `loss = true` OR `rtt_us = 0`
- **For latency stats**: `WHERE loss = false AND rtt_us > 0`
- **Units**: Samples in µs; committed values in ns
- **Bidirectional**: (A→B, link) and (B→A, link) both valid for same physical link

### fact_dz_internet_metro_latency
Public Internet baseline for comparison.
- **Type**: Fact | **Time column**: `event_ts`
- **Grain**: 1 sample per metro pair × timestamp
- **Key fields**: `origin_metro_pk`, `target_metro_pk`, `data_provider`, `rtt_us`, `ipdv_us`
- **No loss signal** in Internet metro telemetry
- ⚠️ **Compare only against DZ WAN links** (exclude DZX)

### fact_dz_device_interface_counters
Interface counters.
- **Type**: Fact | **Time column**: `event_ts`
- **Grain**: 1 sample per interface × timestamp
- **Key fields**: `device_pk`, `intf`, `link_pk`, `user_tunnel_id`, `delta_duration`
- **Traffic fields**: `in_octets_delta`, `out_octets_delta`, `in_pkts_delta`, `out_pkts_delta`
- **Error fields**: `in_errors_delta`, `out_errors_delta`, `in_discards_delta`, `out_discards_delta`
- **Link flaps**: `carrier_transitions_delta`
- **Values are deltas** (counter wrap handled automatically)
- **Directionality**: `in_*` = INTO device, `out_*` = OUT of device
- **Link interfaces**: `link_pk IS NOT NULL`
- **User tunnel interfaces**: `user_tunnel_id IS NOT NULL`
- ⚠️ **Multicast on tunnels**: `in_multicast_pkts_delta` and `out_multicast_pkts_delta` are NOT reliable on tunnel interfaces - use `in_octets_delta`/`out_octets_delta` instead

---

## Solana Datasets

### Node Types (Critical Distinction)

- **Gossip nodes** (`solana_gossip_nodes`): ALL Solana network participants (validators + RPC nodes + others)
- **Validators** (`solana_vote_accounts`): Subset of gossip nodes that participate in consensus
- **Key relationship**: Validators have both `node_pubkey` (gossip identity) and `vote_pubkey` (validator identity)
- ⚠️ **`vote_pubkey` is the stable validator identifier**

### solana_gossip_nodes
All Solana network participants.
- **Type**: SCD2 | **Current**: `solana_gossip_nodes_current` | **History**: `dim_solana_gossip_nodes_history`
- **Grain**: 1 row per node pubkey (entity_id)
- **Key fields**: `pubkey`, `gossip_ip`, `gossip_port`, `tpuquic_ip`, `tpuquic_port`, `version`, `epoch`
- ⚠️ **Not all gossip nodes are validators** - join to `solana_vote_accounts` to identify validators

### solana_vote_accounts
Validators only.
- **Type**: SCD2 | **Current**: `solana_vote_accounts_current` | **History**: `dim_solana_vote_accounts_history`
- **Grain**: 1 row per vote_pubkey (entity_id)
- **Key fields**: `vote_pubkey`, `node_pubkey`, `activated_stake_lamports`, `epoch_vote_account`, `commission_percentage`, `epoch`
- **Staked validator**: `epoch_vote_account = 'true' AND activated_stake_lamports > 0`
- **Lamports to SOL**: `lamports / 1e9`
- ⚠️ **`node_pubkey` can change**; `vote_pubkey` is stable
- ⚠️ **`epoch_vote_account` is a String** ('true'/'false'), not Boolean

### solana_leader_schedule
Leader schedule for epochs.
- **Type**: SCD2 | **Current**: `solana_leader_schedule_current` | **History**: `dim_solana_leader_schedule_history`
- **Grain**: 1 row per node × epoch (entity_id)
- **Key fields**: `node_pubkey`, `epoch`, `slots`, `slot_count`

### fact_solana_vote_account_activity
Vote account activity (sampled every minute).
- **Type**: Fact | **Time column**: `event_ts`
- **Grain**: 1 sample per vote_pubkey × minute
- **Key fields**: `vote_account_pubkey`, `node_identity_pubkey`, `epoch`, `root_slot`, `last_vote_slot`, `cluster_slot`, `is_delinquent`, `credits_delta`, `activated_stake_lamports`
- **Vote lag**: `cluster_slot - last_vote_slot`
- **Root lag**: `cluster_slot - root_slot`

### fact_solana_block_production
Block production metrics (sampled hourly, cumulative).
- **Type**: Fact | **Time column**: `event_ts`
- **Grain**: 1 row per leader × epoch (cumulative)
- **Key fields**: `epoch`, `leader_identity_pubkey`, `leader_slots_assigned_cum`, `blocks_produced_cum`
- **For deltas**: Use LAG() window function between consecutive rows per epoch/leader

---

## GeoIP Dataset

### geoip_records
IP geolocation and ASN.
- **Type**: SCD2 | **Current**: `geoip_records_current` | **History**: `dim_geoip_records_history`
- **Grain**: 1 row per IP address (entity_id)
- **Key fields**: `ip`, `country_code`, `country`, `region`, `city`, `latitude`, `longitude`, `asn`, `asn_org`
- ⚠️ **Accuracy limitations** - inform users when using geoip-derived data

---

## Join Patterns

### Standard Joins
- **FK → PK only**: `{table}_current.{fk}_pk = {target}_current.pk`
- Never join on other columns unless explicitly documented

### Composite Keys
- **tunnel_id**: Use `(device_pk, tunnel_id)` - not globally unique
- **Device-link circuits**: `(origin_device_pk, target_device_pk, link_pk)`

### Temporal Joins
- **ASOF JOIN**: For temporal matching (e.g., user traffic to user history)
- **Time-windowed joins**: For sparse/misaligned time-series (±1 minute window)

### DZ to Solana Association
- **Association**: `dz_users.dz_ip = solana_gossip_nodes.gossip_ip`
- **Validator on DZ**: User + Gossip Node + Vote Account all connected via IP and pubkeys

---

## ClickHouse-Specific Behaviors

### LEFT JOIN Returns Empty String, Not NULL

In ClickHouse, unmatched String columns in LEFT JOIN return `''` (empty string), not `NULL`.

- ❌ `WHERE column IS NULL` - won't find unmatched rows
- ✅ `WHERE column = ''` - correct for unmatched rows
- ✅ `countIf(DISTINCT column, column != '')` - exclude empty strings when counting

---

## Critical Constraints Summary

| Constraint | Rule |
|------------|------|
| Time filters | **Required** on all fact tables using `event_ts` |
| Current state | Use `{table}_current` views |
| Status filter | `status = 'activated'` for most analysis |
| Device reporting | Use `code`, never PK/host |
| Link comparisons | Only WAN vs Internet (never DZX) |
| Loss detection | `loss = true` OR `rtt_us = 0` |
| Validator identity | Use `vote_pubkey` (stable) |
| User identity | Use `(owner_pubkey, client_ip)` - pk is NOT stable |
| Tunnel join | Use `(device_pk, tunnel_id)` composite key |
| Unit conversions | µs→ms, ns→µs, lamports→SOL (/1e9) |
