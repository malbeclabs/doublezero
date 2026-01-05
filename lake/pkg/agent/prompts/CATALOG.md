# Catalog Reference

## Overview

The DoubleZero catalog contains datasets and views organized by domain. This catalog information is embedded in your system prompt. Prefer views and query templates over raw datasets when possible.

## Dataset Types

### SCD2 (Slowly Changing Dimension Type 2) Tables

SCD2 tables track historical changes with validity windows:
- `{table}_current`: Current state snapshot
- `{table}_history`: Historical versions with validity windows

**Querying SCD2 tables:**
- Current state: `valid_from <= CURRENT_TIMESTAMP AND (valid_to IS NULL OR valid_to > CURRENT_TIMESTAMP)`
- Point-in-time: `valid_from <= T AND (valid_to IS NULL OR valid_to > T)`
- Always query `{table}_current` for current state unless doing historical analysis

**Tracked entities:**
- Devices, Links, Users, Contributors, Metros
- Solana vote accounts, Gossip nodes
- GeoIP records

### Fact Tables

Append-only time-series data:
- **Always require time filters** - never run unscoped queries
- Use: `WHERE time >= $__timeFrom() AND time <= $__timeTo()`
- Never use `date_trunc()` in WHERE clauses (prevents partition pruning)

**Fact tables:**
- `dz_device_link_latency_samples_raw`: RTT probe measurements
- `dz_internet_metro_latency_samples_raw`: Internet baseline measurements
- `dz_device_iface_usage_raw`: Interface utilization counters
- `solana_vote_account_activity_raw`: Vote account activity
- `solana_block_production_raw`: Block production metrics

## Key Datasets

### DoubleZero Network

**dz_contributors**: Device and link operators
- Type: SCD2
- Tables: `dz_contributors_current`, `dz_contributors_history`
- Grain: 1 row per contributor code
- Join: `dz_devices_current.contributor_pk = dz_contributors_current.pk`

**dz_devices**: Hardware switches and routers
- Type: SCD2
- Tables: `dz_devices_current`, `dz_devices_history`
- Grain: 1 row per device code
- Key fields: `code` (use for reporting), `status`, `device_type`, `metro_pk`
- **CRITICAL**: Always use `device.code`, never PK/host (host is internal only)
- Status values: pending, activated, suspended, deleted, rejected, soft-drained, hard-drained

**dz_metros**: Geographic regions (exchanges)
- Type: SCD2
- Tables: `dz_metros_current`, `dz_metros_history`
- Grain: 1 row per metro code
- Key fields: `code`, `name`, `longitude`, `latitude`
- Metro format for reporting: **ORIGIN → TARGET** (e.g., "nyc → lon")

**dz_links**: Connections between devices
- Type: SCD2
- Tables: `dz_links_current`, `dz_links_history`
- Grain: 1 row per link code
- Key fields: `link_type` (WAN/DZX), `status`, `committed_rtt_ns`, `committed_jitter_ns`, `bandwidth_bps`
- **CRITICAL**: link_type = 'WAN' → inter-metro (compare vs Internet), link_type = 'DZX' → intra-metro (do NOT compare)
- Drain signaling: `isis_delay_override_ns = 1000000000` means soft-drained
- Status must be 'activated' for most analysis

**dz_users**: Connected sessions
- Type: SCD2
- Tables: `dz_users_current`, `dz_users_history`
- Grain: 1 row per user pubkey (latest state)
- **CRITICAL**: Status value is `'activated'` (not `'active'`)
- Being "on dz" or "connected to dz" means the user exists in the `dz_users` dataset (independent of status filtering)
- Active users: `status = 'activated' AND dz_ip IS NOT NULL`
- Exclude QA/test: `owner_pk != 'DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan'`
- **CRITICAL**: tunnel_id is NOT globally unique - must use composite key (device_pk, tunnel_id)
- **CRITICAL**: User pk (pubkey) is NOT stable - it changes after disconnects/reconnects. Only (owner_pk, client_ip) is the stable identifier for users.
- Use `dz_active_users_view` for user counts & telemetry

**dz_device_link_latency_samples**: RTT probe measurements
- Type: fact
- Tables: `dz_device_link_latency_samples_raw`
- Grain: 1 sample per link × direction × timestamp
- **CRITICAL**: Time filter REQUIRED
- Loss indicated by `rtt_us = 0` (use `WHERE rtt_us > 0` for latency stats)
- Samples in microseconds (µs); committed values in nanoseconds (ns)
- Bi-directional: (A→B, link) and (B→A, link) are both valid
- Only compare DZ WAN links (link_type = 'WAN') to Internet metro pairs

**dz_internet_metro_latency_samples**: Internet baseline
- Type: fact
- Tables: `dz_internet_metro_latency_samples_raw`
- Grain: 1 sample per metro pair × timestamp
- **CRITICAL**: Time filter REQUIRED
- Public Internet baseline for comparison
- Compare only against DZ WAN links (exclude DZX)
- Internet metro telemetry has no loss signal

**dz_device_iface_usage**: Interface utilization
- Type: fact
- Tables: `dz_device_iface_usage_raw`
- Grain: 1 sample per interface × timestamp
- **CRITICAL**: Time filter REQUIRED
- Values are deltas (counter wrap handled automatically)
- Errors/discards recorded on both link sides (A and Z)
- `carrier_transitions = link flaps`
- `user_tunnel_id` extracted from interface name (e.g., Tunnel501 → 501)
- **Directionality**: All `in_*` and `out_*` metrics are from the **device's perspective**:
  - `in_*` = packets/bytes coming INTO the device interface
  - `out_*` = packets/bytes going OUT of the device interface
- **Users connected to devices**: Users/subscribers are connected downstream to devices via tunnels. When a user consumes traffic, the device sends it OUT to the user (device's `out_*` metrics). When a user sends traffic, the device receives it IN from the user (device's `in_*` metrics).
- **Multicast traffic on tunnel interfaces (CRITICAL)**:
  - **`in_multicast_pkts_delta` and `out_multicast_pkts_delta` are NOT reliable on tunnel interfaces** - these fields will be empty/NULL in practice for tunnel interfaces
  - **For multicast traffic on tunnel interfaces, you MUST use `in_pkts_delta` and `out_pkts_delta` instead** - there is no way to get multicast-only usage metrics on tunnel interfaces
  - **Multicast subscribers**: Users/subscribers consuming multicast traffic on tunnel interfaces are identified by filtering for tunnel interfaces (where `user_tunnel_id IS NOT NULL`) and aggregating `out_octets_delta` and `out_pkts_delta` by user (via `user_tunnel_id` join to `dz_users_current`)
  - **Note**: On physical interfaces (non-tunnel), `in_multicast_pkts_delta` and `out_multicast_pkts_delta` may be available, but for tunnel interfaces (which is where users connect), these fields are unreliable and should not be used

### Solana

**Solana Node Types (CRITICAL DISTINCTION):**
- **Gossip nodes** (`solana_gossip_nodes`): All Solana network participants that participate in the gossip protocol for network discovery and communication. These include both validators and non-validator nodes (e.g., RPC nodes, archive nodes, etc.).
- **Validators** (`solana_vote_accounts`): A subset of gossip nodes that participate in consensus by voting on blocks and producing blocks. Validators have vote accounts and can be assigned leader slots. **Not all gossip nodes are validators** - many gossip nodes are non-validator participants (RPC nodes, etc.).
- **Key relationship**: Validators are gossip nodes with vote accounts. A validator has both a `node_pubkey` (gossip identity) and a `vote_pubkey` (validator identity). The `vote_pubkey` is the stable identifier for validators.

**solana_gossip_nodes**: Network participants
- Type: SCD2
- Tables: `solana_gossip_nodes_current`, `solana_gossip_nodes_history`
- Grain: 1 row per node pubkey (latest state)
- Current state: `valid_to IS NULL` in history table
- **CRITICAL**: Contains ALL Solana network participants (validators AND non-validator nodes like RPC nodes)
- **Not all gossip nodes are validators** - only those with entries in `solana_vote_accounts` are validators
- Use this table to find all nodes on the network, but join to `solana_vote_accounts` to identify which are validators

**solana_vote_accounts**: Validators
- Type: SCD2
- Tables: `solana_vote_accounts_current`, `solana_vote_accounts_history`
- Grain: 1 row per vote_pubkey (latest state)
- **CRITICAL**: Validator identity = vote_pubkey (stable). NOT IP. NOT dz user pk.
- **CRITICAL**: This table contains ONLY validators (nodes that participate in consensus). Gossip-only nodes (non-validators) are NOT in this table.
- Staked validator: `epoch_vote_account = true AND activated_stake_lamports > 0`
- Convert lamports to SOL: `lamports / 1e9`
- node_pubkey (gossip identity) can change; vote_pubkey is stable
- **Recent disconnections**: To find validators that disconnected recently, query `solana_vote_accounts_history` where `valid_to IS NOT NULL` and `valid_to >= CURRENT_TIMESTAMP - INTERVAL '1 day'` to find validators that disconnected in the past day

**solana_leader_schedule**: Leader schedule for epochs
- Type: SCD2
- Tables: `solana_leader_schedule_current`, `solana_leader_schedule_history`
- Grain: 1 row per node × epoch
- Use for analyzing validator leadership and slot assignments
- Use solana_leader_schedule_vs_production_current view to compare schedule vs production

**solana_vote_account_activity**: Vote account activity
- Type: fact
- Tables: `solana_vote_account_activity_raw`
- Grain: 1 sample per vote_pubkey × minute
- **CRITICAL**: Time filter REQUIRED
- Sampled every minute from getVoteAccounts and getSlot
- Derived metrics: Vote lag, Root lag, Credits delta

**solana_block_production**: Block production metrics
- Type: fact
- Tables: `solana_block_production_raw`
- Grain: 1 row per leader × epoch (cumulative)
- **CRITICAL**: Time filter REQUIRED
- Sampled hourly from getBlockProduction
- Cumulative values: leader_slots_assigned_cum, blocks_produced_cum
- Use solana_block_production_delta view for hourly deltas

### GeoIP

**geoip_records**: IP geolocation and ASN
- Type: SCD2
- Tables: `geoip_records_current`, `geoip_records_history`
- Grain: 1 row per IP address
- Use for IP geolocation and ASN analysis
- Accuracy limitations - inform users when using geoip-derived data

## Key Views

### DoubleZero Views

**dz_active_users_view**: Active user counts & telemetry (excludes QA/test users)

**dz_links_current_health**: Link health classification with operational flags

**dz_link_telemetry**: Sample-level link telemetry with jitter

**dz_link_telemetry_with_committed**: Link telemetry with committed metrics and violation flags

**dz_device_iface_health**: Device interface health (errors, discards, carrier transitions)
- Fields: `time`, `device_pk`, `host`, `intf`, `link_pk`, `link_side`, `in_errors`, `out_errors`, `in_discards`, `out_discards`, `carrier_transitions`
- Filter by `link_pk IS NOT NULL` for link interfaces, `link_pk IS NULL` for non-link interfaces

**dz_link_traffic**: Link-level traffic aggregation (sums both sides A and Z)

**dz_device_traffic**: Device-level traffic aggregation

**dz_user_device_traffic**: User-level traffic aggregation (uses ASOF JOIN for temporal matching)

**dz_metro_to_metro_latency**: Metro-to-metro latency aggregation from device samples

### Solana Views

**solana_gossip_at_ip_now**: Currently active gossip nodes (all gossip nodes with IPs, not filtered by DZ)

**solana_validators_connected_now**: Validators connected through DZ (validator-grain, safe for rollups)
- Use this view to count validators "on dz" or "connected to dz"
- Returns only validators that are currently connected through DZ
- Use this view to calculate **total connected stake** (sum of `activated_stake_lamports` for all validators in this view, converted to SOL)

**solana_validators_connected_now_connections**: Validator connections (connection-grain bridge)
- Connection-level view showing validator-to-DZ-user mappings
- Use `solana_validators_connected_now` for counts (validator-grain)

**Counting Solana nodes "on dz" or "connected to dz":**
- **Validators on DZ**: Use `solana_validators_connected_now` - this view already filters for validators connected through DZ
- **Gossip nodes on DZ**: Join `solana_gossip_at_ip_now` with `dz_users_current` on `dz_ip = gossip_ip` where `dz_users_current.status = 'activated'` and `dz_users_current.dz_ip IS NOT NULL`
- **CRITICAL**: "on dz" means currently connected (exists in `dz_users_current` with matching `dz_ip`), not historical connections
- Do NOT count all nodes from `solana_gossip_nodes_current` or `solana_vote_accounts_current` - these include nodes not on DZ

**solana_validator_dz_connection_events**: Validator connection/disconnection event log
- Type: view (event log)
- Grain: 1 row per connection or disconnection event
- Fields: `vote_pubkey`, `node_pubkey`, `dz_ip`, `event_time`, `event_type` ('dz_connected' or 'dz_disconnected'), `activated_stake_sol`, `epoch`, `owner_pk`, `client_ip`
- **CRITICAL**: This is the PRIMARY data source for determining when validators connected or disconnected from DZ. Always use this view for connection/disconnection queries, not SCD2 snapshot comparisons.
- **CRITICAL**: The view may create multiple connection events for the same validator when their stake changes (because the underlying overlap view creates separate records for each vote account record). When querying for "which validators connected during time window T", use `solana_validator_dz_first_connection_events` instead (see below), which already filters to only the first connection per validator.

**solana_validator_dz_first_connection_events**: First connection event per validator (deduplicated)
- Type: view (event log, deduplicated)
- Grain: 1 row per validator's first connection event only
- Fields: Same as `solana_validator_dz_connection_events`
- **PREFERRED**: Use this view when querying for "which validators connected during time window T" - it already filters to only the first connection event per validator, eliminating the need for complex CTEs. This view shows only the actual first connection time for each validator, not spurious connection events created when stake changes.
- Query pattern:
```sql
SELECT vote_pubkey, node_pubkey, dz_ip, event_time, activated_stake_sol, owner_pk, client_ip
FROM solana_validator_dz_first_connection_events
WHERE event_time >= T1
  AND event_time <= T2
ORDER BY event_time
```
- **CRITICAL**: Connection events and stake changes are DIFFERENT things:
  - A validator can connect to DZ without stake (or with stake)
  - A validator already connected to DZ can receive stake delegations (stake increases without a connection event)
  - A stake share increase can be caused by: (1) new validators connecting, (2) existing validators receiving stake, (3) validators disconnecting (reducing total network stake), or any combination
  - **Never assume a stake increase means new validators connected** - always query connection events separately

**Solana Validator Identity and IP Association (CRITICAL):**
- **Association mechanism**: The association from DZ to Solana validators is via `dz_users_current.dz_ip = solana_gossip_nodes_current.gossip_ip` (or `dz_users_history.dz_ip = solana_gossip_nodes_history.gossip_ip` for historical queries)
- **vote_pubkey is the stable validator identifier**: Always use `vote_pubkey` to identify validators (e.g., "vote1", "vote2")
- **IP addresses are important**: When reporting on Solana validators (especially those on DZ), ALWAYS include both `vote_pubkey` AND the IP address (`gossip_ip` or `dz_ip`) in the response
- **gossip_ip can change**: Validators can change their `gossip_ip` associated with their `node_pubkey` over time. When reporting disconnections or connections, always report the IP that was associated at the time of the event
- **For connection queries**: Use `solana_validator_dz_connection_events` view with `event_type = 'dz_connected'` and filter by `event_time >= T1 AND event_time <= T2` for the time window of interest. This will show which validators actually connected during that window. **CRITICAL**: Do not infer connections from stake changes - always query connection events directly.
- **For disconnection queries**: Use `solana_validator_dz_connection_events` view with `event_type = 'dz_disconnected'` and filter by `event_time >= CURRENT_TIMESTAMP - INTERVAL '24 hours'`. **CRITICAL**: Only report validators that disconnected and have NOT reconnected. **CRITICAL**: You MUST check the FULL event timeline for each validator, not just the most recent event. A validator whose most recent event is a disconnection may have reconnected earlier and then disconnected again. To determine if a validator is currently disconnected:
  1. Find all disconnection events (`event_type = 'dz_disconnected'`) for the validator in the time window
  2. For EACH disconnection event, check if there is a subsequent reconnection event (`event_type = 'dz_connected'`) with `event_time > disconnect_event_time` for the same `vote_pubkey`
  3. Only include validators where the MOST RECENT disconnection event has NO subsequent reconnection event
  4. If a validator reconnected after its most recent disconnection, exclude it from the results (it is no longer disconnected)
- **CRITICAL**: Never conflate "most recent event is a disconnection" with "remains disconnected". Always verify the full event sequence to determine current connection status
- **CRITICAL**: Never conflate stake changes with connection changes. If asked "which validators connected when stake increased", query `solana_validator_dz_connection_events` for connection events in that time window, not stake snapshots.

## Join Patterns

### Standard Joins

- **FK → PK only**: Join foreign keys to primary keys
- Pattern: `{table}_current.{fk}_pk = {target}_current.pk`
- Never join on other columns unless explicitly documented

### Temporal Joins

- **ASOF JOIN**: For temporal matching (e.g., user traffic to user history)
- **Time-windowed joins**: For sparse or misaligned time-series data (±1 minute window)

### Composite Keys

- **tunnel_id**: NOT globally unique - must use (device_pk, tunnel_id)
- **Device-link circuits**: (origin_device_pk, target_device_pk, link_pk)

## Common Query Patterns

### Metro Pair Comparison

To compare DZ WAN links to Internet:
1. Join device-link samples: `origin_device_pk → dz_devices_current.pk → metro_pk`
2. Join target device: `target_device_pk → dz_devices_current.pk → metro_pk`
3. Match with `dz_internet_metro_latency_samples` for same metro pair
4. Only for `link_type = 'WAN'` links

**When asked to "compare dz to the public internet" or similar:**
- Use the `dz_vs_public_internet_metro_to_metro_named` view for comprehensive comparisons
- Provide metro-to-metro comparisons showing both DZ and Internet performance
- Identify and highlight the **best performing metro pairs** (where DZ shows greatest improvement)
- Identify and highlight the **worst performing metro pairs** (where DZ improvement is smallest or where Internet is competitive)
- Include an **overall summary** comparing both networks across all metro pairs
- Compare **both RTT and jitter** metrics
- **CRITICAL**: Report **both average and p95** (95th percentile) for each metric - you must explicitly calculate and mention p95 values (e.g., "average RTT: 45ms, p95 RTT: 50ms")
- Show specific metro pair names (e.g., "nyc → lon") and numeric values (RTT in ms, jitter in ms)
- Calculate and report improvement percentages or ratios where relevant
- Use aggregate functions like `PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY rtt_us)` to calculate p95 values

### User Traffic Attribution

1. Extract `user_tunnel_id` from interface name
2. Join using composite key: `(device_pk, user_tunnel_id) = (dz_users_current.device_pk, dz_users_current.tunnel_id)`
3. Use ASOF JOIN for temporal matching to user history

### Stake Aggregation

- Always aggregate at `vote_pubkey` grain to avoid double-counting
- Use `solana_validators_connected_now` (validator-grain) for safe rollups
- Do not use connection-grain views for stake sums
- **Terminology**: "Total connected stake" or "connected stake" refers to the total Solana stake (in SOL) of validators currently connected to DZ. This is equivalent to "stake share on DZ" or "DZ stake share" when expressed as a percentage of total network stake.

### Validator Connection Event Queries

**When asked "which validators connected during time window T" or "which validators connected when stake increased":**
- **PREFERRED**: Use `solana_validator_dz_first_connection_events` view - it already filters to only the first connection per validator, making queries simple and correct
- **CRITICAL**: Do NOT infer connections from stake changes or SCD2 snapshot comparisons - always query connection events directly
- **CRITICAL**: Connection events and stake changes are independent - a stake increase can be from new connections OR existing validators receiving stake delegations
- Query pattern (using the preferred view):
```sql
SELECT vote_pubkey, node_pubkey, dz_ip, event_time, activated_stake_sol, owner_pk, client_ip
FROM solana_validator_dz_first_connection_events
WHERE event_time >= T1
  AND event_time <= T2
ORDER BY event_time
```
- Alternative query pattern (if you must use `solana_validator_dz_connection_events` directly):
```sql
WITH first_connections AS (
  SELECT
    vote_pubkey,
    MIN(event_time) AS first_connection_time
  FROM solana_validator_dz_connection_events
  WHERE event_type = 'dz_connected'
  -- DO NOT add time filters here - must find first connection globally
  GROUP BY vote_pubkey
)
SELECT
  e.vote_pubkey,
  e.node_pubkey,
  e.dz_ip,
  e.event_time,
  e.activated_stake_sol,
  e.owner_pk,
  e.client_ip
FROM solana_validator_dz_connection_events e
JOIN first_connections fc
  ON e.vote_pubkey = fc.vote_pubkey
  AND e.event_time = fc.first_connection_time
WHERE e.event_type = 'dz_connected'
  AND e.event_time >= T1  -- Time filter goes HERE, not in the CTE
  AND e.event_time <= T2
ORDER BY e.event_time
```
- **CRITICAL**: The `first_connections` CTE must find the FIRST connection event for each validator GLOBALLY (NO time filters in the CTE). The time window filter must be applied in the final WHERE clause. If you filter the time window in the CTE, you will incorrectly find the minimum within the window rather than the true first connection, which will incorrectly include validators that were already connected before the window. A validator that was already connected before the time window should NOT appear in results, even if they have connection events within the window (those are from stake changes, not actual connections).
- To understand stake changes during a time window, query both:
  1. Connection events: `solana_validator_dz_connection_events` with `event_type = 'dz_connected'` (filtered to first connection per validator)
  2. Stake snapshots: `solana_vote_accounts_history` or `solana_validators_connected_now` at different points in time
- **Never assume**: A stake share increase means new validators connected - always verify with connection events

### Solana Validator Performance Comparison (On DZ vs Off DZ)

**When asked to "compare solana validators on dz vs off dz" or similar:**
- Use `solana_validator_dz_overlaps_windowed` to determine which validators are on DZ vs off DZ
- Compare the following key performance metrics:
  1. **Skip Rate**: Calculate as `SUM(slots_skipped_delta) / SUM(leader_slots_assigned_delta)` from `solana_block_production_delta` for on-DZ vs off-DZ validators
  2. **Vote Latency (Vote Lag)**: Calculate as `AVG(cluster_slot - last_vote_slot)` from `solana_vote_account_activity_raw` for on-DZ vs off-DZ validators (reported in slots)
  3. **Block Produce Rate**: Calculate as `SUM(blocks_produced_delta) / SUM(leader_slots_assigned_delta)` from `solana_block_production_delta` for on-DZ vs off-DZ validators
- Use `solana_block_production_delta` for skip rate and produce rate calculations
- Use `solana_vote_account_activity_raw` for vote lag calculations
- Join with `solana_validator_dz_overlaps_windowed` using `node_pubkey = leader_identity_pubkey` for block production, or `vote_pubkey = vote_account_pubkey` for vote activity
- Filter by time range using `time >= $__timeFrom() AND time <= $__timeTo()`
- Report specific numeric values for each metric (skip rate as percentage, vote lag in slots, produce rate as percentage)
- Include specific vote_pubkeys or node_pubkeys in the comparison when relevant

## Critical Constraints Summary

- **Time filters**: Mandatory on all fact tables
- **Arithmetic**: Cast BIGINT before math, handle unit conversions (µs/ns, lamports/SOL)
- **Keys**: PK always `pk`, FK always `{table}_pk`, join FK → PK only
- **Status**: `status = 'activated'` required for most analysis
- **Reporting**: Use percentages not raw counts, device.code not PK/host
- **Link types**: WAN = inter-metro (compare), DZX = intra-metro (don't compare)
- **Loss**: `rtt_us = 0` indicates loss in device-link telemetry
- **Violations**: RTT/jitter violations require thresholds (2000µs + 1.25× committed)

