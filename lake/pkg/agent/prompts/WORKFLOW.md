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
- For stake share analysis: Query both current validators AND connection events
- For bandwidth: Use `dz_device_iface_usage_raw` with byte-to-GB conversion
- For skip rate: Use `solana_block_production_delta` view

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
