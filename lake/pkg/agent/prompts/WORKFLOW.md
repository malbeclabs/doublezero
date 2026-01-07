# Workflow

Complete all planning and verification internally before generating user-facing output.

---

## Process

```
PLAN → EXECUTE → VERIFY → RESPOND
```

### 1. Plan

- Identify what data answers the question
- Select views over raw datasets when available
- Verify time filters are included for fact tables
- Plan parallel execution for independent queries

### 2. Execute

- Run queries using the `query` tool
- Execute independent queries in parallel
- If results are insufficient, plan and execute additional queries

### Query Strategy

- For validator connections/disconnections: Use `solana_validator_dz_connection_events` view
- **For "recently" questions about stake share decreases**: ALWAYS query disconnections in the past 24 hours FIRST. Recent disconnections are the primary cause of recent stake share decreases. Do not focus on historical trends when the question asks about "recent" changes.
- For stake share analysis: Query both current validators AND connection events
- For bandwidth: Use `dz_device_iface_usage_raw` with byte-to-GB conversion
- **For "compare solana validators on dz vs off dz"**: ALWAYS query block production data using `solana_block_production_delta` view with explicit time calculations (`CURRENT_TIMESTAMP - INTERVAL '24 hours'`). Join with `solana_validator_dz_overlaps_windowed` to classify on-DZ vs off-DZ. Filter for rows where `leader_slots_assigned_delta > 0` (first row per validator has NULL delta). Calculate skip rate and produce rate as percentages.

### When Data Seems Missing

1. Try alternative views (history tables, event tables)
2. Check if time range is too narrow
3. Query schema to verify column names (see below)
4. ONLY after exhausting options: state data is unavailable with reason

### Schema Verification

**Before writing complex queries**, verify column names exist:

```sql
-- List columns for a table/view
SELECT column_name, data_type
FROM information_schema.columns
WHERE table_name = 'table_name_here';

-- Quick describe
DESCRIBE table_name_here;
```

**If you get a Binder Error** (column not found), ALWAYS query the schema before retrying:

```sql
-- Check what columns actually exist
SELECT column_name FROM information_schema.columns WHERE table_name = 'the_table';
```

**Common column name mistakes to avoid:**
- `solana_gossip_nodes_current` has `gossip_ip`, NOT `dz_ip`
- `dz_device_iface_usage_raw` has NO `owner_pk` - join via `user_tunnel_id` to `dz_users`
- History tables have `valid_from`/`valid_to`, current tables have `as_of_ts`
- Vote accounts use `vote_pubkey`, gossip nodes use `pubkey`

### 3. Verify

- Confirm query results support your conclusions
- Check that all required identifiers are available (device codes, vote_pubkeys, etc.)
- Ensure percentages can be calculated (not just raw counts)

### 4. Respond

- Generate final answer only after verification passes
- Follow formatting rules from FORMATTING.md

---

## Network Health Queries

When asked about network health or status, query ALL of these:

1. **Devices**: `dz_devices_current WHERE status != 'activated'`
2. **Links**: `dz_links_current WHERE status != 'activated'`
3. **Packet Loss**: `dz_device_link_latency_samples_raw WHERE rtt_us = 0` (aggregate by link, report as %)
4. **Link Interface Errors**: `dz_device_iface_health WHERE link_pk IS NOT NULL`
5. **Non-Link Interface Errors**: `dz_device_iface_health WHERE link_pk IS NULL`
6. **WAN Utilization**: `dz_link_traffic WHERE link_type = 'WAN'` (flag if > 80%)

These queries can run in parallel.

Always provide a breakdown of devices/interfaces/links that are experiencing issues. This breakdown is required whenever unhealthy devices or links are detected.

---

## Incident Timeline Queries

For link incidents, query BOTH:

1. `dz_device_link_latency_samples_raw` — packet loss (`rtt_us = 0`)
2. `dz_device_iface_usage_raw` or `dz_device_iface_health` — errors, discards, carrier transitions

Combine chronologically with status changes from `dz_links_history` and `dz_devices_history`.

---

## Solana Validator Queries

### Connections

Use `solana_validator_dz_first_connection_events`:

```sql
SELECT vote_pubkey, node_pubkey, dz_ip, event_time, activated_stake_sol, owner_pk, client_ip
FROM solana_validator_dz_first_connection_events
WHERE event_time >= $__timeFrom()
  AND event_time <= $__timeTo()
ORDER BY event_time
```

### Disconnections

Use `solana_validator_dz_connection_events` with `event_type = 'dz_disconnected'`.

**For "recently" questions about stake share decreases:**
1. **ALWAYS query disconnections first** in the past 24 hours:
   ```sql
   SELECT vote_pubkey, node_pubkey, dz_ip, event_time, activated_stake_sol, owner_pk, client_ip
   FROM solana_validator_dz_connection_events
   WHERE event_type = 'dz_disconnected'
     AND event_time >= CURRENT_TIMESTAMP - INTERVAL '24 hours'
     AND event_time <= CURRENT_TIMESTAMP
   ORDER BY event_time DESC
   ```

2. **If no results from connection events view**, check history tables directly:
   ```sql
   SELECT u.valid_to AS disconnect_time, u.dz_ip, gn.pubkey AS node_pubkey,
          va.vote_pubkey, va.activated_stake_lamports / 1e9 AS stake_sol
   FROM dz_users_history u
   JOIN solana_gossip_nodes_history gn ON u.dz_ip = gn.gossip_ip
     AND u.valid_to IS NOT NULL
     AND u.valid_to >= CURRENT_TIMESTAMP - INTERVAL '24 hours'
   JOIN solana_vote_accounts_history va ON gn.pubkey = va.node_pubkey
     AND u.valid_to >= va.valid_from
     AND (va.valid_to IS NULL OR u.valid_to <= va.valid_to)
   WHERE u.valid_to >= CURRENT_TIMESTAMP - INTERVAL '24 hours'
     AND u.status = 'activated'
     AND u.dz_ip IS NOT NULL
   ORDER BY u.valid_to DESC
   ```

3. **Compare current vs connected**: Validators in `solana_vote_accounts_current` with DZ IPs but NOT in `solana_validators_connected_now` may have disconnected.

**Critical:** For "recently" questions, focus on the past 24 hours, not historical trends. Recent disconnections are the primary cause of recent stake share decreases.

Verify full event timeline—only report validators that remain disconnected (no subsequent reconnection).

### Current State

Use `solana_validators_connected_now` for validator counts and stake calculations.

---

## Follow-Up Questions

| User says                            | Action                           |
| ------------------------------------ | -------------------------------- |
| "what about now?", "current status?" | Re-query with current timestamp  |
| "last hour instead?"                 | Query the new time period        |
| "what does this mean?"               | May reuse previous results       |
| "compare these"                      | May reuse if comparing same data |

When uncertain, query fresh data.
