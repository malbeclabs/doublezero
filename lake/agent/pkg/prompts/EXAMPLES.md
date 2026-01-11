# Query Examples

Query patterns for common questions. For schema details, see **CATALOG.md**.

---

## Solana Validator Queries

### ⚠️ Critical: "Connected in the last X" = NEW Connections

When asked "how many validators **connected** in the last day/week/etc":
- This means **newly connected** validators (connection event happened in that period)
- **NOT** the total currently connected
- **ALWAYS use historical comparison** (see "Newly Connected Validators" pattern below)

**Never answer with current count alone** - always compare current state vs historical state.

### Count Validators and Gossip Nodes on DZ

**Question**: "How many validators and gossip nodes are on DZ?"

```sql
SELECT
  -- IMPORTANT: ClickHouse returns '' (not NULL) for unmatched LEFT JOIN String columns
  -- Must use countIf with != '' filter, NOT COUNT with IS NOT NULL
  countIf(DISTINCT va.vote_pubkey, va.vote_pubkey != '') AS validator_count,
  COUNT(DISTINCT gn.pubkey) AS gossip_node_count
FROM dz_users_current u
JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
LEFT JOIN solana_vote_accounts_current va
  ON gn.pubkey = va.node_pubkey AND va.activated_stake_lamports > 0
WHERE u.status = 'activated' AND gn.gossip_ip IS NOT NULL
```

### Validators Currently Connected (List)

**Question**: "Which validators are currently on DZ?"

```sql
SELECT DISTINCT
  va.vote_pubkey,
  gn.gossip_ip,
  va.activated_stake_lamports / 1e9 AS stake_sol
FROM dz_users_current u
JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL
JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
WHERE u.status = 'activated'
  AND va.activated_stake_lamports > 0
ORDER BY stake_sol DESC
```

### Newly Connected Validators (Count and List)

**Question**: "How many validators connected in the last 24 hours?"

Strategy: Compare current state vs historical state 24h ago using NOT IN pattern.

⚠️ **Best Practice**: For count queries, also query the specific entities to list them in the response (especially when count ≤ 10).

```sql
-- Get newly connected validators (both count AND list)
-- Returns vote_pubkey, IP, and stake for each newly connected validator
SELECT
  cv.vote_pubkey,
  cv.gossip_ip,
  cv.activated_stake_lamports / 1e9 AS stake_sol
FROM (
  -- Currently connected validators
  SELECT DISTINCT va.vote_pubkey, gn.gossip_ip, va.activated_stake_lamports
  FROM dz_users_current u
  JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL
  JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
  WHERE u.status = 'activated' AND va.activated_stake_lamports > 0
) cv
WHERE cv.vote_pubkey NOT IN (
  -- Validators connected 24h ago (SCD Type 2 point-in-time reconstruction)
  SELECT DISTINCT va2.vote_pubkey
  FROM (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_dz_users_history
    WHERE snapshot_ts <= now() - INTERVAL 24 HOUR AND is_deleted = 0
  ) u2
  JOIN (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_gossip_nodes_history
    WHERE snapshot_ts <= now() - INTERVAL 24 HOUR AND is_deleted = 0
  ) gn2 ON u2.dz_ip = gn2.gossip_ip AND gn2.gossip_ip IS NOT NULL AND gn2.rn = 1
  JOIN (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_vote_accounts_history
    WHERE snapshot_ts <= now() - INTERVAL 24 HOUR AND is_deleted = 0
  ) va2 ON gn2.pubkey = va2.node_pubkey AND va2.rn = 1
  WHERE u2.rn = 1 AND u2.status = 'activated' AND u2.dz_ip IS NOT NULL
    AND va2.epoch_vote_account = 'true' AND va2.activated_stake_lamports > 0
)
ORDER BY stake_sol DESC
```

**Example response format**:
> 🔗 **Newly Connected Validators (Last 24 Hours)**
>
> **3 validators** connected to DZ:
> - `vote1` (10.0.0.1) - 15,000 SOL stake
> - `vote2` (10.0.0.2) - 1,000 SOL stake
> - `vote5` (10.0.0.5) - 500 SOL stake

⚠️ **For COUNT queries**: Always use historical comparison (above). Do NOT use first-connection events for counts.

### Validators Connected During a Specific Time Window

**Question**: "Which validators connected between T1 and T2?" (e.g., "between 24 hours ago and 22 hours ago")

Strategy: Find validators connected at T2 but NOT connected at T1.

```sql
-- Find validators connected at T2 (22h ago) but NOT connected at T1 (24h ago)
-- This finds validators that connected DURING the window
SELECT DISTINCT v_t2.vote_pubkey
FROM (
  -- Validators connected at T2 (end of window) using SCD Type 2 pattern
  SELECT DISTINCT va.vote_pubkey
  FROM (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_dz_users_history
    WHERE snapshot_ts <= now() - INTERVAL 22 HOUR AND is_deleted = 0
  ) u
  JOIN (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_gossip_nodes_history
    WHERE snapshot_ts <= now() - INTERVAL 22 HOUR AND is_deleted = 0
  ) gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL AND gn.rn = 1
  JOIN (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_vote_accounts_history
    WHERE snapshot_ts <= now() - INTERVAL 22 HOUR AND is_deleted = 0
  ) va ON gn.pubkey = va.node_pubkey AND va.rn = 1
  WHERE u.rn = 1 AND u.status = 'activated' AND u.dz_ip IS NOT NULL
    AND va.epoch_vote_account = 'true' AND va.activated_stake_lamports > 0
) v_t2
WHERE v_t2.vote_pubkey NOT IN (
  -- Validators connected at T1 (start of window)
  SELECT DISTINCT va2.vote_pubkey
  FROM (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_dz_users_history
    WHERE snapshot_ts <= now() - INTERVAL 24 HOUR AND is_deleted = 0
  ) u2
  JOIN (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_gossip_nodes_history
    WHERE snapshot_ts <= now() - INTERVAL 24 HOUR AND is_deleted = 0
  ) gn2 ON u2.dz_ip = gn2.gossip_ip AND gn2.gossip_ip IS NOT NULL AND gn2.rn = 1
  JOIN (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_vote_accounts_history
    WHERE snapshot_ts <= now() - INTERVAL 24 HOUR AND is_deleted = 0
  ) va2 ON gn2.pubkey = va2.node_pubkey AND va2.rn = 1
  WHERE u2.rn = 1 AND u2.status = 'activated' AND u2.dz_ip IS NOT NULL
    AND va2.epoch_vote_account = 'true' AND va2.activated_stake_lamports > 0
)
```

⚠️ **Key pattern**: To find entities that connected DURING a window, find those present at the END (T2) but NOT at the START (T1).

### Disconnected Validators

**Question**: "Which validators disconnected in the last 24 hours?"

Strategy: Find validators connected 24h ago, exclude currently connected, verify disconnection in time window.

```sql
SELECT DISTINCT v24h.vote_pubkey
FROM (
  -- Validators connected 24 hours ago
  SELECT DISTINCT va.vote_pubkey, u.entity_id AS user_entity_id
  FROM (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_dz_users_history
    WHERE snapshot_ts <= now() - INTERVAL 24 HOUR AND is_deleted = 0
  ) u
  JOIN (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_gossip_nodes_history
    WHERE snapshot_ts <= now() - INTERVAL 24 HOUR AND is_deleted = 0
  ) gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL AND gn.rn = 1
  JOIN (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_vote_accounts_history
    WHERE snapshot_ts <= now() - INTERVAL 24 HOUR AND is_deleted = 0
  ) va ON gn.pubkey = va.node_pubkey AND va.rn = 1
  WHERE u.rn = 1 AND u.status = 'activated' AND u.dz_ip IS NOT NULL
    AND va.epoch_vote_account = 'true' AND va.activated_stake_lamports > 0
) v24h
WHERE v24h.vote_pubkey NOT IN (
  -- Exclude validators currently connected
  SELECT DISTINCT va.vote_pubkey
  FROM dz_users_current u
  JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL
  JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
  WHERE u.status = 'activated' AND va.activated_stake_lamports > 0
)
AND v24h.user_entity_id IN (
  -- Verify disconnection happened in the past 24 hours
  SELECT entity_id FROM dim_dz_users_history
  WHERE is_deleted = 1 AND snapshot_ts >= now() - INTERVAL 24 HOUR
)
```

### Total Connected Stake

**Question**: "What is the total stake connected to DZ?"

```sql
SELECT
  SUM(va.activated_stake_lamports) / 1e9 AS total_stake_sol,
  COUNT(DISTINCT va.vote_pubkey) AS validator_count
FROM dz_users_current u
JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL
JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
WHERE u.status = 'activated' AND va.activated_stake_lamports > 0
```

⚠️ Always aggregate at `vote_pubkey` grain to avoid double-counting stake.

### Connection Events vs Stake Changes

⚠️ **Critical distinction**:
- **Connection events**: When a validator connects/disconnects from DZ
- **Stake changes**: When a validator's stake increases/decreases

These are **independent**:
- A validator can connect without stake
- An already-connected validator can receive stake delegations
- A stake increase can be from new connections OR existing validators receiving stake

**Never assume** a stake increase means new validators connected.

---

## User/Subscriber Queries

### ⚠️ Multicast Subscriber Bandwidth

**Question**: "Which multicast subscriber consumes the most bandwidth?"

**Key insight**: `in_multicast_pkts_delta` and `out_multicast_pkts_delta` are **always 0** on tunnel interfaces.
To measure multicast subscriber bandwidth, use total octets (`in_octets_delta` + `out_octets_delta`).

Use the same query as "Highest Bandwidth Subscriber" below.

### Highest Bandwidth Subscriber

**Question**: "Which subscriber consumed the most bandwidth in the last 24 hours?"

```sql
SELECT
  u.owner_pubkey,
  u.client_ip,
  u.dz_ip,
  u.tunnel_id,
  SUM(COALESCE(iface.in_octets_delta, 0) + COALESCE(iface.out_octets_delta, 0)) AS total_bytes,
  SUM(COALESCE(iface.in_octets_delta, 0) + COALESCE(iface.out_octets_delta, 0)) / 1e9 AS total_gb
FROM dz_users_current u
JOIN fact_dz_device_interface_counters iface
  ON u.device_pk = iface.device_pk
  AND iface.user_tunnel_id = u.tunnel_id
  AND iface.intf LIKE 'tunnel%'
WHERE u.status = 'activated'
  AND iface.event_ts >= now() - INTERVAL 24 HOUR
GROUP BY u.owner_pubkey, u.client_ip, u.dz_ip, u.tunnel_id
ORDER BY total_bytes DESC
LIMIT 10
```

⚠️ On tunnel interfaces, `in_multicast_pkts_delta` is NOT reliable - use `in_octets_delta` instead.

### Active User Count

**Question**: "How many active users are connected?"

```sql
SELECT COUNT(*) AS active_users
FROM dz_users_current
WHERE status = 'activated'
  AND dz_ip IS NOT NULL
  AND owner_pubkey != 'DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan'
```

---

## Network Health Queries

### Devices Not Activated

```sql
SELECT code, status, device_type
FROM dz_devices_current
WHERE status != 'activated'
```

### Links Not Activated

```sql
SELECT code, status, link_type
FROM dz_links_current
WHERE status != 'activated'
```

### Links with Packet Loss

```sql
SELECT
  l.code AS link_code,
  d1.code AS origin_device,
  d2.code AS target_device,
  COUNT(*) AS total_samples,
  SUM(CASE WHEN lat.loss = true OR lat.rtt_us = 0 THEN 1 ELSE 0 END) AS lost_samples,
  ROUND(100.0 * SUM(CASE WHEN lat.loss = true OR lat.rtt_us = 0 THEN 1 ELSE 0 END) / COUNT(*), 2) AS loss_pct
FROM fact_dz_device_link_latency lat
JOIN dz_links_current l ON lat.link_pk = l.pk
JOIN dz_devices_current d1 ON lat.origin_device_pk = d1.pk
JOIN dz_devices_current d2 ON lat.target_device_pk = d2.pk
WHERE lat.event_ts >= now() - INTERVAL 24 HOUR
GROUP BY l.code, d1.code, d2.code
HAVING loss_pct > 0
ORDER BY loss_pct DESC
```

### Interface Errors/Discards (Link Interfaces)

```sql
SELECT
  d.code AS device_code,
  iface.intf,
  l.code AS link_code,
  SUM(COALESCE(iface.in_errors_delta, 0)) AS in_errors,
  SUM(COALESCE(iface.out_errors_delta, 0)) AS out_errors,
  SUM(COALESCE(iface.in_discards_delta, 0)) AS in_discards,
  SUM(COALESCE(iface.out_discards_delta, 0)) AS out_discards,
  SUM(COALESCE(iface.carrier_transitions_delta, 0)) AS carrier_transitions
FROM fact_dz_device_interface_counters iface
JOIN dz_devices_current d ON iface.device_pk = d.pk
JOIN dz_links_current l ON iface.link_pk = l.pk
WHERE iface.event_ts >= now() - INTERVAL 24 HOUR
  AND iface.link_pk IS NOT NULL
GROUP BY d.code, iface.intf, l.code
HAVING in_errors > 0 OR out_errors > 0 OR in_discards > 0 OR out_discards > 0 OR carrier_transitions > 0
```

### WAN Link Utilization

```sql
SELECT
  l.code AS link_code,
  l.bandwidth_bps,
  AVG((COALESCE(iface.in_octets_delta, 0) + COALESCE(iface.out_octets_delta, 0)) * 8.0 / NULLIF(iface.delta_duration, 0)) AS avg_throughput_bps,
  100.0 * AVG((COALESCE(iface.in_octets_delta, 0) + COALESCE(iface.out_octets_delta, 0)) * 8.0 / NULLIF(iface.delta_duration, 0)) / NULLIF(l.bandwidth_bps, 0) AS avg_utilization_pct
FROM fact_dz_device_interface_counters iface
JOIN dz_links_current l ON iface.link_pk = l.pk
WHERE iface.event_ts >= now() - INTERVAL 24 HOUR
  AND l.link_type = 'WAN'
  AND iface.link_pk IS NOT NULL
GROUP BY l.code, l.bandwidth_bps
HAVING avg_utilization_pct > 80
ORDER BY avg_utilization_pct DESC
```

---

## Latency Comparison Queries

### DZ vs Public Internet

**Question**: "Compare DZ performance to the public internet"

⚠️ Only compare WAN links (not DZX) to Internet.

```sql
-- DZ WAN latency by metro pair
WITH dz_latency AS (
  SELECT
    m1.code AS origin_metro,
    m2.code AS target_metro,
    AVG(lat.rtt_us) / 1000.0 AS avg_rtt_ms,
    quantile(0.95)(lat.rtt_us) / 1000.0 AS p95_rtt_ms,
    AVG(lat.ipdv_us) / 1000.0 AS avg_jitter_ms,
    quantile(0.95)(lat.ipdv_us) / 1000.0 AS p95_jitter_ms
  FROM fact_dz_device_link_latency lat
  JOIN dz_devices_current d1 ON lat.origin_device_pk = d1.pk
  JOIN dz_devices_current d2 ON lat.target_device_pk = d2.pk
  JOIN dz_metros_current m1 ON d1.metro_pk = m1.pk
  JOIN dz_metros_current m2 ON d2.metro_pk = m2.pk
  JOIN dz_links_current l ON lat.link_pk = l.pk
  WHERE lat.event_ts >= now() - INTERVAL 24 HOUR
    AND l.link_type = 'WAN'
    AND lat.loss = false AND lat.rtt_us > 0
  GROUP BY m1.code, m2.code
),
-- Internet latency by metro pair
internet_latency AS (
  SELECT
    m1.code AS origin_metro,
    m2.code AS target_metro,
    AVG(lat.rtt_us) / 1000.0 AS avg_rtt_ms,
    quantile(0.95)(lat.rtt_us) / 1000.0 AS p95_rtt_ms,
    AVG(lat.ipdv_us) / 1000.0 AS avg_jitter_ms,
    quantile(0.95)(lat.ipdv_us) / 1000.0 AS p95_jitter_ms
  FROM fact_dz_internet_metro_latency lat
  JOIN dz_metros_current m1 ON lat.origin_metro_pk = m1.pk
  JOIN dz_metros_current m2 ON lat.target_metro_pk = m2.pk
  WHERE lat.event_ts >= now() - INTERVAL 24 HOUR
  GROUP BY m1.code, m2.code
)
SELECT
  dz.origin_metro,
  dz.target_metro,
  dz.avg_rtt_ms AS dz_avg_rtt_ms,
  dz.p95_rtt_ms AS dz_p95_rtt_ms,
  inet.avg_rtt_ms AS internet_avg_rtt_ms,
  inet.p95_rtt_ms AS internet_p95_rtt_ms,
  inet.avg_rtt_ms - dz.avg_rtt_ms AS rtt_improvement_ms,
  dz.avg_jitter_ms AS dz_avg_jitter_ms,
  inet.avg_jitter_ms AS internet_avg_jitter_ms
FROM dz_latency dz
JOIN internet_latency inet
  ON dz.origin_metro = inet.origin_metro AND dz.target_metro = inet.target_metro
ORDER BY rtt_improvement_ms DESC
```

---

## Incident & Timeline Queries

**Questions about link timelines, incidents, or drain events require multiple data sources combined chronologically.**

When asked about a link timeline or incident, always gather:
1. **Status/config changes** from `dim_dz_links_history`
2. **Packet loss** from `fact_dz_device_link_latency`
3. **Interface errors, discards, carrier transitions** from `fact_dz_device_interface_counters`

### Link Status History

```sql
SELECT
  snapshot_ts AS event_time,
  status,
  isis_delay_override_ns,
  CASE WHEN isis_delay_override_ns = 1000000000 THEN 'soft-drain signal' ELSE '' END AS drain_signal
FROM dim_dz_links_history
WHERE entity_id = (SELECT entity_id FROM dz_links_current WHERE code = '{link_code}')
ORDER BY snapshot_ts
```

### Link Packet Loss Timeline

```sql
SELECT
  event_ts,
  d1.code AS origin_device,
  d2.code AS target_device,
  CASE WHEN loss = true OR rtt_us = 0 THEN 'LOSS' ELSE 'OK' END AS status,
  rtt_us
FROM fact_dz_device_link_latency lat
JOIN dz_links_current l ON lat.link_pk = l.pk
JOIN dz_devices_current d1 ON lat.origin_device_pk = d1.pk
JOIN dz_devices_current d2 ON lat.target_device_pk = d2.pk
WHERE l.code = '{link_code}'
  AND lat.event_ts >= {start_time}
  AND lat.event_ts <= {end_time}
ORDER BY event_ts
```

### Link Interface Errors Timeline

```sql
SELECT
  event_ts,
  d.code AS device_code,
  iface.intf,
  COALESCE(in_errors_delta, 0) AS in_errors,
  COALESCE(out_errors_delta, 0) AS out_errors,
  COALESCE(in_discards_delta, 0) AS in_discards,
  COALESCE(out_discards_delta, 0) AS out_discards,
  COALESCE(carrier_transitions_delta, 0) AS carrier_transitions
FROM fact_dz_device_interface_counters iface
JOIN dz_links_current l ON iface.link_pk = l.pk
JOIN dz_devices_current d ON iface.device_pk = d.pk
WHERE l.code = '{link_code}'
  AND iface.event_ts >= {start_time}
  AND iface.event_ts <= {end_time}
  AND (in_errors_delta > 0 OR out_errors_delta > 0 OR in_discards_delta > 0
       OR out_discards_delta > 0 OR carrier_transitions_delta > 0)
ORDER BY event_ts
```

⚠️ **Timeline responses should**:
- Present events chronologically with explicit timestamps
- Show elapsed time between key events
- Include packet loss percentages and error counts
- Correlate status changes with telemetry symptoms
- Note when issues started, peaked, and resolved

---

## Validator Performance Comparison

### On-DZ vs Off-DZ Performance

**Question**: "Compare validator performance on DZ vs off DZ"

⚠️ **Key Pattern**: Off-DZ validators are validators with vote accounts that are NOT currently connected to DZ.
- **On DZ**: validators whose gossip_ip matches a dz_user's dz_ip
- **Off DZ**: all other validators with activated stake (use `NOT IN` pattern)

Metrics to compare:
1. **Vote Lag**: `AVG(cluster_slot - last_vote_slot)` - lower is better
2. **Skip Rate**: `1 - (blocks_produced / leader_slots_assigned)` - lower is better (use block production data)
3. **Delinquency**: Count of `is_delinquent = true` samples - lower is better

```sql
-- Step 1: Identify validators on DZ
WITH validators_on_dz AS (
  SELECT DISTINCT va.vote_pubkey, va.node_pubkey
  FROM dz_users_current u
  JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL
  JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
  WHERE u.status = 'activated' AND va.activated_stake_lamports > 0
),
-- Step 2: Identify validators off DZ (have vote accounts but NOT connected to DZ)
validators_off_dz AS (
  SELECT DISTINCT va.vote_pubkey, va.node_pubkey
  FROM solana_vote_accounts_current va
  WHERE va.activated_stake_lamports > 0
    AND va.vote_pubkey NOT IN (SELECT vote_pubkey FROM validators_on_dz)
),
-- Step 3: Combine with labels
all_validators AS (
  SELECT vote_pubkey, node_pubkey, 'On DZ' AS dz_status FROM validators_on_dz
  UNION ALL
  SELECT vote_pubkey, node_pubkey, 'Off DZ' AS dz_status FROM validators_off_dz
)
-- Step 4: Compare performance metrics
SELECT
  v.dz_status,
  COUNT(DISTINCT activity.vote_account_pubkey) AS validator_count,
  SUM(activity.activated_stake_lamports) / 1e9 AS total_stake_sol,
  AVG(activity.cluster_slot - activity.last_vote_slot) AS avg_vote_lag_slots,
  quantile(0.95)(activity.cluster_slot - activity.last_vote_slot) AS p95_vote_lag_slots,
  AVG(activity.credits_delta) AS avg_credits_delta,
  SUM(CASE WHEN activity.is_delinquent = true THEN 1 ELSE 0 END) AS delinquent_samples
FROM fact_solana_vote_account_activity activity
JOIN all_validators v ON activity.vote_account_pubkey = v.vote_pubkey
WHERE activity.event_ts >= now() - INTERVAL 24 HOUR
GROUP BY v.dz_status
ORDER BY v.dz_status
```

⚠️ **Common Mistake**: Using only `LEFT JOIN validators_on_dz` won't find off-DZ validators if they have no activity in the fact table. Always use the explicit `NOT IN` pattern to identify off-DZ validators from `solana_vote_accounts_current`.

### Block Production (Skip Rate) Comparison

```sql
-- Compare block production / skip rate between on-DZ and off-DZ validators
WITH validators_on_dz AS (
  SELECT DISTINCT va.vote_pubkey, va.node_pubkey
  FROM dz_users_current u
  JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL
  JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
  WHERE u.status = 'activated' AND va.activated_stake_lamports > 0
),
validators_off_dz AS (
  SELECT DISTINCT va.vote_pubkey, va.node_pubkey
  FROM solana_vote_accounts_current va
  WHERE va.activated_stake_lamports > 0
    AND va.vote_pubkey NOT IN (SELECT vote_pubkey FROM validators_on_dz)
),
all_validators AS (
  SELECT vote_pubkey, node_pubkey, 'On DZ' AS dz_status FROM validators_on_dz
  UNION ALL
  SELECT vote_pubkey, node_pubkey, 'Off DZ' AS dz_status FROM validators_off_dz
)
SELECT
  v.dz_status,
  COUNT(DISTINCT bp.leader_identity_pubkey) AS validator_count,
  SUM(bp.leader_slots_assigned_cum) AS total_slots_assigned,
  SUM(bp.blocks_produced_cum) AS total_blocks_produced,
  ROUND(100.0 * (1 - SUM(bp.blocks_produced_cum) / NULLIF(SUM(bp.leader_slots_assigned_cum), 0)), 2) AS skip_rate_pct
FROM fact_solana_block_production bp
JOIN all_validators v ON bp.leader_identity_pubkey = v.node_pubkey
WHERE bp.event_ts >= now() - INTERVAL 24 HOUR
GROUP BY v.dz_status
ORDER BY v.dz_status
```

---

## Common Aggregation Patterns

### Link Health Classification

```sql
SELECT
  code,
  status,
  CASE WHEN status = 'activated' THEN 1 ELSE 0 END AS is_operational,
  CASE WHEN status IN ('soft-drained', 'hard-drained') OR isis_delay_override_ns = 1000000000 THEN 1 ELSE 0 END AS is_soft_drained,
  CASE WHEN status = 'hard-drained' THEN 1 ELSE 0 END AS is_hard_drained
FROM dz_links_current
```

### Link Telemetry with Committed Metrics

```sql
SELECT
  l.code,
  lat.rtt_us,
  l.committed_rtt_ns / 1000.0 AS committed_rtt_us,
  lat.rtt_us - (l.committed_rtt_ns / 1000.0) AS rtt_minus_committed_us,
  lat.ipdv_us,
  l.committed_jitter_ns / 1000.0 AS committed_jitter_us,
  -- Significant violation: >= 2000µs AND >= 1.25× committed
  CASE WHEN lat.rtt_us - (l.committed_rtt_ns / 1000.0) >= 2000
        AND lat.rtt_us >= (l.committed_rtt_ns / 1000.0) * 1.25
       THEN 1 ELSE 0 END AS is_significant_rtt_violation
FROM fact_dz_device_link_latency lat
JOIN dz_links_current l ON lat.link_pk = l.pk
WHERE lat.event_ts >= now() - INTERVAL 24 HOUR
  AND lat.loss = false AND lat.rtt_us > 0
```

### Metro-to-Metro Latency

```sql
SELECT
  m1.code AS origin_metro,
  m2.code AS target_metro,
  AVG(lat.rtt_us) / 1000.0 AS avg_rtt_ms,
  quantile(0.95)(lat.rtt_us) / 1000.0 AS p95_rtt_ms
FROM fact_dz_device_link_latency lat
JOIN dz_devices_current d1 ON lat.origin_device_pk = d1.pk
JOIN dz_devices_current d2 ON lat.target_device_pk = d2.pk
JOIN dz_metros_current m1 ON d1.metro_pk = m1.pk
JOIN dz_metros_current m2 ON d2.metro_pk = m2.pk
WHERE lat.event_ts >= now() - INTERVAL 24 HOUR
  AND lat.loss = false AND lat.rtt_us > 0
GROUP BY m1.code, m2.code
ORDER BY avg_rtt_ms
```

---

## Point-in-Time Reconstruction (SCD Type 2)

To reconstruct state at a specific point in time:

```sql
-- Get latest record for each entity at/before timestamp T
SELECT *
FROM (
  SELECT
    *,
    ROW_NUMBER() OVER (
      PARTITION BY entity_id
      ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC
    ) AS rn
  FROM dim_{table}_history
  WHERE snapshot_ts <= {T}        -- Only records at/before T
    AND is_deleted = 0            -- Exclude deleted records
) sub
WHERE rn = 1                      -- Latest per entity
```

Key points:
- Filter `snapshot_ts <= T` to get records at/before the target time
- Filter `is_deleted = 0` to exclude soft-deleted records
- Use `ROW_NUMBER()` with proper ordering to get the latest per entity
- Order by `snapshot_ts DESC, ingested_at DESC, op_id DESC` to handle ties

