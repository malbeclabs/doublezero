# SQL & Domain Context

This document contains ClickHouse SQL patterns and business rules for the DoubleZero (DZ) network data.

## Database: ClickHouse

### NULL vs Empty String (CRITICAL)
ClickHouse String columns are typically NOT nullable - they use empty string `''` instead of NULL.

**WRONG patterns:**
- `WHERE column IS NOT NULL` - Always true for non-nullable columns
- `WHERE column IS NULL` - Always false for non-nullable columns
- `CASE WHEN column IS NULL THEN ...` - NULL branch never executes
- `COALESCE(column, 'default')` - Never returns default for non-nullable strings

**CORRECT patterns:**
- `WHERE column != ''` - Check for non-empty
- `WHERE column = ''` - Check for empty
- `CASE WHEN column = '' THEN 'default' ELSE column END`
- `if(column = '', 'default', column)`

**LEFT JOIN behavior:**
- Unmatched rows return `''` (empty string) for String columns, not NULL
- **Anti-join (find rows in A with NO match in B):** `WHERE b.key = ''`
- **Keep only matched rows:** `WHERE b.key != ''`

Example anti-join pattern:
```sql
-- Find users that have NO matching device
SELECT u.*
FROM users u
LEFT JOIN devices d ON u.device_pk = d.pk
WHERE d.pk = ''  -- Empty string means no match (NOT "IS NULL"!)
```

### Other ClickHouse Specifics
- **Function names are case-sensitive**: Use lowercase `lag`, `lead`, `row_number` (not `LAG`, `LEAD`, `ROW_NUMBER`)
- Quantiles: `quantile(0.95)(column)` not `PERCENTILE_CONT`
- Date functions: `now()`, `toDate()`, `toDateTime()`
- Intervals: `INTERVAL 24 HOUR`, `INTERVAL 7 DAY`
- Count non-empty: `countIf(column != '')` or `sum(column != '')`

### Ambiguous Column References (CRITICAL)
When joining tables/views that share column names (like `epoch`), ClickHouse may report "ambiguous identifier" errors.

**Problem tables**: `solana_vote_accounts_current` and `solana_validators_on_dz_current` BOTH have an `epoch` column.

**Solution**: Use a CTE to isolate one table's columns before joining:

```sql
-- WRONG: Ambiguous epoch reference
SELECT va.vote_pubkey, va.epoch
FROM solana_vote_accounts_current AS va
LEFT JOIN solana_validators_on_dz_current AS dz ON va.vote_pubkey = dz.vote_pubkey
LEFT JOIN other_table AS ot ON ot.epoch = va.epoch  -- AMBIGUOUS!

-- CORRECT: Isolate the base table in a CTE first
WITH base_validators AS (
    SELECT vote_pubkey, node_pubkey, activated_stake_lamports, epoch
    FROM solana_vote_accounts_current
    WHERE epoch_vote_account = 'true' AND activated_stake_lamports > 0
)
SELECT bv.vote_pubkey, bv.epoch
FROM base_validators AS bv
LEFT JOIN solana_validators_on_dz_current AS dz ON bv.vote_pubkey = dz.vote_pubkey
LEFT JOIN other_table AS ot ON ot.epoch = bv.epoch  -- Now unambiguous
```

### Nested Aggregates (CRITICAL)
**ClickHouse does NOT allow aggregate functions inside other aggregate functions.**

**WRONG patterns:**
- `MAX(GREATEST(MAX(a), MAX(b)))` - nested aggregates
- `SUM(AVG(column))` - nested aggregates

**CORRECT patterns:**
- Use CTEs to compute inner aggregations first
- `GREATEST(MAX(a), MAX(b))` is valid - GREATEST is not an aggregate

```sql
-- WRONG: nested aggregates
SELECT MAX(GREATEST(MAX(in_octets), MAX(out_octets))) FROM ...

-- CORRECT: compute per-group max in CTE, then aggregate
WITH per_window AS (
    SELECT GREATEST(MAX(in_octets), MAX(out_octets)) AS peak
    FROM ... GROUP BY window
)
SELECT MAX(peak) FROM per_window
```

## Pre-Built Views (USE THESE FIRST)

**Always prefer these views over manual joins:**

| View | Use For |
|------|---------|
| `solana_validators_on_dz_current` | Validators currently on DZ (vote_pubkey, node_pubkey, activated_stake_sol, device_code, device_metro_code, connected_ts) |
| `solana_validators_off_dz_current` | Validators NOT on DZ with GeoIP (vote_pubkey, activated_stake_sol, city, country) |
| `solana_validators_performance_current` | **Validator performance metrics** (vote_lag, skip_rate, dz_status, device_code, device_metro_code) - USE FOR COMPARISONS |
| `solana_validators_on_dz_connections` | All connection events with `first_connected_ts`, device_code, device_metro_code |
| `solana_validators_disconnections` | Validators that left DZ (vote_pubkey, activated_stake_sol, device_code, device_metro_code, connected_ts, disconnected_ts) |
| `solana_validators_new_connections` | Recently connected validators with device_code, device_metro_code |
| `dz_links_health_current` | Current link health (status, packet loss, latency vs committed, is_dark) |
| `dz_link_status_changes` | Link status transitions with timestamps (previous_status, new_status, changed_ts) |
| `dz_vs_internet_latency_comparison` | Compare DZ vs public internet latency per metro pair |

### Time Windows
When the question says "recently" or "recent", default to **past 24 hours** unless context suggests otherwise.
- "recently decreased" → filter to `> now() - INTERVAL 24 HOUR`
- "in the past week" → filter to `> now() - INTERVAL 7 DAY`

**For stake share decrease questions:**
1. Query `solana_validators_disconnections` with `WHERE disconnected_ts > now() - INTERVAL 24 HOUR`
2. ALWAYS return individual vote_pubkey values and their stake amounts, not just aggregates

### Validator Performance Comparison (IMPORTANT)
**When comparing validators on DZ vs off DZ**, use `solana_validators_performance_current`:

```sql
-- Compare performance metrics by DZ status
SELECT
    dz_status,
    COUNT(*) AS validator_count,
    ROUND(AVG(avg_vote_lag_slots), 2) AS avg_vote_lag,
    ROUND(AVG(skip_rate_pct), 2) AS avg_skip_rate,
    SUM(activated_stake_sol) AS total_stake_sol
FROM solana_validators_performance_current
WHERE is_delinquent = false
GROUP BY dz_status;

-- Top validators by performance (lowest vote lag)
SELECT vote_pubkey, node_pubkey, dz_status, avg_vote_lag_slots, skip_rate_pct, activated_stake_sol
FROM solana_validators_performance_current
WHERE is_delinquent = false AND avg_vote_lag_slots IS NOT NULL
ORDER BY avg_vote_lag_slots ASC
LIMIT 10;

-- Validators with high skip rate
SELECT vote_pubkey, node_pubkey, dz_status, skip_rate_pct, slots_assigned, blocks_produced
FROM solana_validators_performance_current
WHERE skip_rate_pct > 5
ORDER BY skip_rate_pct DESC;
```

### Common View Queries

```sql
-- Count validators on DZ
SELECT COUNT(*) FROM solana_validators_on_dz_current;

-- Validators that disconnected recently (ALWAYS include vote_pubkey!)
SELECT vote_pubkey, node_pubkey, activated_stake_sol, disconnected_ts
FROM solana_validators_disconnections
WHERE disconnected_ts > now() - INTERVAL 24 HOUR
ORDER BY activated_stake_sol DESC;

-- Stake share on DZ
SELECT
    SUM(CASE WHEN dz.vote_pubkey != '' THEN va.activated_stake_lamports ELSE 0 END) / SUM(va.activated_stake_lamports) * 100 AS stake_share_pct
FROM solana_vote_accounts_current va
LEFT JOIN solana_validators_on_dz_current dz ON va.vote_pubkey = dz.vote_pubkey
WHERE va.activated_stake_lamports > 0;

-- Off-DZ validators in a city
SELECT vote_pubkey, activated_stake_sol, city
FROM solana_validators_off_dz_current
WHERE city = 'Tokyo'
ORDER BY activated_stake_sol DESC LIMIT 10;

-- Links with current issues
SELECT code, status, is_soft_drained, is_hard_drained, is_isis_soft_drained,
       has_packet_loss, loss_pct, exceeds_committed_rtt, is_dark
FROM dz_links_health_current
WHERE is_soft_drained OR is_hard_drained OR is_isis_soft_drained OR has_packet_loss OR exceeds_committed_rtt OR is_dark;

-- Link status changes in past 7 days
SELECT link_code, previous_status, new_status, changed_ts
FROM dz_link_status_changes
WHERE changed_ts > now() - INTERVAL 7 DAY
ORDER BY changed_ts DESC;

-- DZ vs Internet latency comparison
SELECT origin_metro, target_metro,
       dz_avg_rtt_ms, internet_avg_rtt_ms, rtt_improvement_pct,
       dz_avg_jitter_ms, internet_avg_jitter_ms, jitter_improvement_pct
FROM dz_vs_internet_latency_comparison
ORDER BY origin_metro, target_metro;
```

### DZ vs Public Internet Comparison
**When asked to "compare DZ to the public internet"**, use the `dz_vs_internet_latency_comparison` view:
- Shows side-by-side DZ and internet latency for each metro pair
- Includes RTT (round-trip time), jitter, and improvement percentages
- Positive `rtt_improvement_pct` means DZ is faster than internet

**NOT for path questions:** This view shows latency metrics, NOT network paths. For questions about "path", "route", "shortest path", or "how to get from X to Y", use `execute_cypher` instead.

## Business Rules

### Status & State
- **Device status values**: pending, activated, suspended, deleted, rejected, drained
- **User kind values** (in `kind` column):
  - `ibrl` - unicast users (standard IP routing)
  - `ibrl_with_allocated_ip` - unicast with pre-allocated IP
  - `multicast` - multicast subscribers
  - `edge_filtering` - edge filtering users
- **Active user**: `status = 'activated' AND dz_ip != ''`
- **Exclude test user**: `owner_pubkey != 'DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan'`
- **Staked validator**: `epoch_vote_account = 'true' AND activated_stake_lamports > 0` (note: String, not Boolean)
- **Connected stake**: `SUM(activated_stake_lamports)` for validators connected to DZ
- **Stake share**: Percentage of total Solana stake on DZ = `connected_stake / total_network_stake * 100`
- **Soft-drain signal**: `isis_delay_override_ns = 1000000000`
- **Link types**: WAN (inter-metro), DZX (intra-metro)

**For "network state" or "network summary" questions**, include:
1. Device count (total and by status - activated vs drained)
2. Link count (activated links)
3. User count (connected users with `dz_ip != ''`)
4. Metro count

```sql
-- Network state summary queries
SELECT COUNT(*) AS total_devices, countIf(status = 'activated') AS active, countIf(status = 'drained') AS drained FROM dz_devices_current;
SELECT COUNT(*) AS active_links FROM dz_links_current WHERE status = 'activated';
SELECT COUNT(*) AS connected_users FROM dz_users_current WHERE status = 'activated' AND dz_ip != '';
SELECT COUNT(DISTINCT pk) AS metro_count FROM dz_metros_current;

-- Find drained devices (ALWAYS list specific device codes!)
SELECT code, status, metro_pk FROM dz_devices_current WHERE status = 'drained';
```

**For "network health" questions**, check and list:
1. Link issues from `dz_links_health_current` - check is_soft_drained, is_hard_drained, is_isis_soft_drained, has_packet_loss, exceeds_committed_rtt, is_dark
2. Drained devices - **MUST list specific device codes**
3. Interface errors from `fact_dz_device_interface_counters` - include device code and **actual numeric counts**

**CRITICAL: Always include specific identifiers and counts:**
- "tok-dzd1 is drained, chi-dzd1 is drained" (NOT just "2 drained devices")
- "tok-fra-1 has 50% packet loss" (NOT just "packet loss detected")
- "lon-dzd1 has 8 in_errors, 3 discards" (NOT just "interface errors detected")

### Metro Codes (IMPORTANT)
Metro codes are **lowercase 3-letter codes**. Common examples:
- `nyc` (New York), `lon` (London), `tyo` (Tokyo), `sin` (Singapore)
- `sao` (São Paulo), `fra` (Frankfurt), `chi` (Chicago), `lax` (Los Angeles)

**Always use lowercase** when filtering by metro code:
```sql
-- CORRECT
WHERE side_a_metro = 'nyc' OR side_z_metro = 'lon'

-- WRONG (will return 0 rows)
WHERE side_a_metro = 'NYC' OR side_z_metro = 'LON'
```

### Validator Performance Metrics
**When asked to "compare validators on DZ vs off DZ"**, use `solana_validators_performance_current` view.
This view includes vote lag, skip rate, and DZ status pre-calculated.

Key metrics:
- **Vote lag** (`avg_vote_lag_slots`): How far behind the validator is (lower is better)
- **Skip rate** (`skip_rate_pct`): Percentage of missed blocks (lower is better)
- **DZ status** (`dz_status`): 'on_dz' or 'off_dz'

**CRITICAL: Always include specific validator identifiers** (vote_pubkey or node_pubkey):
- "vote1, vote2, vote3 are on DZ with avg vote lag 50 slots"
- NOT: "on-DZ validators average 50 slots" (no identifiers!)

**Note on delinquent validators:** The view already filters out delinquent validators from vote lag calculations.
Delinquent validators have `is_delinquent = true` and NULL vote lag metrics.

```sql
-- Compare on-DZ vs off-DZ performance (USE THIS!)
SELECT dz_status, COUNT(*) AS validators,
       ROUND(AVG(avg_vote_lag_slots), 2) AS avg_vote_lag,
       ROUND(AVG(skip_rate_pct), 2) AS avg_skip_rate
FROM solana_validators_performance_current
WHERE is_delinquent = false
GROUP BY dz_status;

-- Individual validator performance
SELECT vote_pubkey, node_pubkey, dz_status, avg_vote_lag_slots, skip_rate_pct
FROM solana_validators_performance_current
WHERE is_delinquent = false
ORDER BY avg_vote_lag_slots ASC LIMIT 10;
```

### Telemetry Patterns
- **Loss detection**: `loss = true OR rtt_us = 0`
- **For latency stats**: Always filter `WHERE loss = false AND rtt_us > 0`
- **Vote lag**: Calculate as `cluster_slot - last_vote_slot` with `WHERE is_delinquent = false`
- **Link interfaces**: Identified by `link_pk IS NOT NULL`
- **User tunnel interfaces**: Identified by `user_tunnel_id IS NOT NULL`
- **Internet comparison**: Only compare DZ WAN links to internet latency (not DZX)

### Interface Errors & Health
Use `fact_dz_device_interface_counters` for interface-level issues:
- `in_errors_delta`, `out_errors_delta` - Packet errors
- `in_discards_delta`, `out_discards_delta` - Dropped packets
- `carrier_transitions_delta` - Link flaps

```sql
-- Find devices with interface errors (past 24h)
SELECT d.code AS device_code, f.intf,
       SUM(f.in_errors_delta) AS in_errors,
       SUM(f.out_errors_delta) AS out_errors,
       SUM(f.in_discards_delta) AS in_discards,
       SUM(f.out_discards_delta) AS out_discards,
       SUM(f.carrier_transitions_delta) AS carrier_transitions
FROM fact_dz_device_interface_counters f
JOIN dz_devices_current d ON f.device_pk = d.pk
WHERE f.event_ts > now() - INTERVAL 24 HOUR
GROUP BY d.code, f.intf
HAVING in_errors > 0 OR out_errors > 0 OR in_discards > 0 OR out_discards > 0 OR carrier_transitions > 0
ORDER BY in_errors + out_errors DESC;
```

### Bandwidth & Utilization (CRITICAL)
**NEVER combine in and out traffic for utilization calculations.** Network interfaces are full-duplex.

**Bandwidth rate calculation:**
```sql
-- Per-interface rate (when grouping by a specific entity)
SELECT
    entity_id,
    SUM(in_octets_delta) * 8.0 / NULLIF(SUM(delta_duration), 0) AS in_rate_bps,
    SUM(out_octets_delta) * 8.0 / NULLIF(SUM(delta_duration), 0) AS out_rate_bps
FROM fact_dz_device_interface_counters
WHERE event_ts > now() - INTERVAL 1 HOUR
GROUP BY entity_id  -- e.g., link_pk, user_tunnel_id, device_pk
```

**Aggregating rates across multiple interfaces:**
When computing total/aggregate bandwidth across multiple interfaces:
1. First compute per-interface rates (GROUP BY device_pk, intf)
2. Then SUM those rates

```sql
-- CORRECT: Total bandwidth across all interfaces
SELECT SUM(in_rate) AS total_in_bps, SUM(out_rate) AS total_out_bps
FROM (
    SELECT
        SUM(in_octets_delta) * 8.0 / NULLIF(SUM(delta_duration), 0) AS in_rate,
        SUM(out_octets_delta) * 8.0 / NULLIF(SUM(delta_duration), 0) AS out_rate
    FROM fact_dz_device_interface_counters
    WHERE event_ts > now() - INTERVAL 1 HOUR
    GROUP BY device_pk, intf
)

-- WRONG: This divides by sum of all durations, undercounting by N interfaces
SELECT
    SUM(in_octets_delta) * 8.0 / SUM(delta_duration) AS in_rate_bps  -- WRONG!
FROM fact_dz_device_interface_counters
WHERE event_ts > now() - INTERVAL 1 HOUR
```
Use `/ 1e6` for Mbps, `/ 1e9` for Gbps.

**ALWAYS report bandwidth as RATES (Gbps/Mbps), not data volumes (GB/TB):**
- For bandwidth questions, ALWAYS calculate and show the rate in Gbps or Mbps
- Data volumes (GB transferred) may be shown alongside rates but NEVER as the only metric
- Example: "owner3: **16.7 Mbps** avg bandwidth (15 GB total)" - rate is primary, volume is secondary

**Correct utilization calculation:**
```sql
-- Per-link, per-direction utilization
SELECT
    l.code AS link_code,
    SUM(in_octets_delta) * 8 / (l.bandwidth_bps * SUM(delta_duration)) AS in_utilization,
    SUM(out_octets_delta) * 8 / (l.bandwidth_bps * SUM(delta_duration)) AS out_utilization
FROM fact_dz_device_interface_counters f
JOIN dz_links_current l ON f.link_pk = l.pk
WHERE event_ts > now() - INTERVAL 1 HOUR
GROUP BY l.pk, l.code, l.bandwidth_bps
```

**WRONG patterns:**
- `SUM(in_octets + out_octets) / bandwidth` - combines directions
- `SUM(bytes) / (bandwidth_bps * 3600)` - hardcoded time instead of `delta_duration`

### Per-User Traffic (CRITICAL)
To query bandwidth/traffic for **specific users**, join through `user_tunnel_id`:

```sql
-- CORRECT: Join on user_tunnel_id to get per-user traffic with RATE
SELECT
    u.owner_pubkey,
    u.dz_ip,
    SUM(f.in_octets_delta + f.out_octets_delta) AS total_bytes,
    SUM(f.in_octets_delta + f.out_octets_delta) * 8.0 / SUM(f.delta_duration) / 1e6 AS avg_mbps
FROM dz_users_current u
JOIN fact_dz_device_interface_counters f
  ON f.device_pk = u.device_pk
  AND f.user_tunnel_id = u.tunnel_id
WHERE f.intf LIKE 'tunnel%'
  AND f.event_ts > now() - INTERVAL 24 HOUR
GROUP BY u.owner_pubkey, u.dz_ip
ORDER BY total_bytes DESC
```

### History Tables (CRITICAL)
**ALWAYS check the schema for exact table and column names.** Do NOT guess.

History tables follow the pattern `dim_{entity}_history` (e.g., `dim_dz_users_history`).

**History tables use SNAPSHOT pattern:**
- `snapshot_ts` - timestamp when the snapshot was taken
- `is_deleted` - whether the record was deleted at this snapshot
- NO `valid_from`/`valid_to` columns

**To query historical state at a point in time:**
```sql
-- Find records as of 24 hours ago
SELECT * FROM dim_dz_users_history
WHERE snapshot_ts <= now() - INTERVAL 24 HOUR
  AND is_deleted = false
ORDER BY snapshot_ts DESC
LIMIT 1 BY pk  -- Get latest snapshot per entity
```

**WRONG table names** (these do NOT exist):
- `dz_users_history` - use `dim_dz_users_history`
- `solana_vote_accounts_history` - does NOT exist
- `solana_gossip_nodes_history` - does NOT exist

### History Table Deduplication (CRITICAL)
**NEVER GROUP BY attributes that change over time** like `activated_stake_lamports`. Use `row_number()`:

```sql
-- CORRECT: Use row_number to get one row per entity
SELECT vote_pubkey, node_pubkey, activated_stake_lamports, snapshot_ts
FROM (
  SELECT *,
    row_number() OVER (
      PARTITION BY entity_id
      ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC
    ) AS rn
  FROM dim_solana_vote_accounts_history
  WHERE snapshot_ts <= now() - INTERVAL 24 HOUR
    AND is_deleted = 0
)
WHERE rn = 1
```

### DZ-Solana Relationship (IMPORTANT)
Solana validators/nodes connect to DZ as **users**, not directly to devices.

**Entity relationships:**
- `dz_users_current` = Solana validators connected to DZ (each user has a `dz_ip`)
- `dz_devices_current` = DZ network devices (routers/switches)
- Users connect TO devices via `dz_users_current.device_pk = dz_devices_current.pk`

**To find "Solana validators on DZ" or "connected validators":**
1. Start from `dz_users_current` with `status = 'activated'`
2. Join `dz_users_current.dz_ip` to `solana_gossip_nodes_current.gossip_ip`
3. Join gossip to vote accounts: `solana_gossip_nodes_current.pubkey = solana_vote_accounts_current.node_pubkey`

**Use the pre-built views instead:**
```sql
-- Simple: use the pre-built view
SELECT COUNT(*) AS validators_on_dz FROM solana_validators_on_dz_current

-- Or with details:
SELECT vote_pubkey, activated_stake_lamports / 1e9 AS stake_sol, connected_ts
FROM solana_validators_on_dz_current
ORDER BY stake_sol DESC
```

**WRONG patterns:**
- `SELECT COUNT(*) FROM solana_vote_accounts_current` - counts ALL validators, not just those on DZ
- Any query about "connected" validators without including `dz_users_current` in the join

### Validators That Disconnected From DZ

**"Which validators are no longer on DZ?"** (any time):
```sql
SELECT c.vote_pubkey, c.activated_stake_lamports / 1e9 AS stake_sol, c.first_connected_ts
FROM solana_validators_on_dz_connections c
WHERE c.vote_pubkey NOT IN (SELECT vote_pubkey FROM solana_validators_on_dz_current)
ORDER BY stake_sol DESC
```

**"Which validators disconnected RECENTLY?"** (within time window):
Use history tables. **ALWAYS include the disconnection timestamp.**

### Validators That Connected During a Time Window
```sql
-- Validators that first connected between 24 hours ago and 22 hours ago
SELECT vote_pubkey, node_pubkey, activated_stake_lamports / 1e9 AS stake_sol, first_connected_ts
FROM solana_validators_on_dz_connections
WHERE first_connected_ts BETWEEN now() - INTERVAL 24 HOUR AND now() - INTERVAL 22 HOUR
ORDER BY stake_sol DESC
```

### Link Health & Status
**Use `dz_links_health_current` for current state** and **`dz_link_status_changes` for history**.

**`dz_links_health_current` columns:**
| Column | Description |
|--------|-------------|
| `is_soft_drained` | Link status is 'soft-drained' |
| `is_hard_drained` | Link status is 'hard-drained' |
| `is_isis_soft_drained` | ISIS delay override set to 1000ms |
| `has_packet_loss` | Loss >= 1% in last hour |
| `loss_pct` | Packet loss percentage (last hour) |
| `exceeds_committed_rtt` | Avg latency exceeds committed RTT |
| `avg_rtt_us`, `p95_rtt_us` | Latency metrics (last hour) |
| `is_dark` | No telemetry in last 2 hours |

```sql
-- Links with current issues
SELECT code, side_a_metro, side_z_metro, status, loss_pct,
       is_soft_drained, is_hard_drained, is_isis_soft_drained, has_packet_loss, exceeds_committed_rtt, is_dark
FROM dz_links_health_current
WHERE is_soft_drained OR is_hard_drained OR is_isis_soft_drained OR has_packet_loss OR exceeds_committed_rtt OR is_dark;

-- Links with issues in a specific metro
SELECT code, status, loss_pct, is_dark
FROM dz_links_health_current
WHERE (side_a_metro = 'sao' OR side_z_metro = 'sao')
  AND (is_soft_drained OR is_hard_drained OR has_packet_loss OR is_dark);
```

**`dz_link_status_changes` for history:**
```sql
-- When did a link go down?
SELECT link_code, previous_status, new_status, changed_ts
FROM dz_link_status_changes
WHERE link_code = 'nyc-lon-1'
ORDER BY changed_ts DESC;

-- Recent status changes
SELECT link_code, previous_status, new_status, changed_ts, side_a_metro, side_z_metro
FROM dz_link_status_changes
WHERE changed_ts > now() - INTERVAL 48 HOUR
ORDER BY changed_ts DESC;
```

**For historical packet loss / latency** (beyond the last hour), query `fact_dz_device_link_latency` directly:

```sql
-- Historical packet loss by link (last 30 days, hourly buckets)
SELECT
    l.code AS link_code,
    toStartOfHour(f.event_ts) AS hour,
    count(*) AS samples,
    countIf(f.loss = true) AS lost_samples,
    round(countIf(f.loss = true) * 100.0 / count(*), 1) AS loss_pct
FROM fact_dz_device_link_latency f
JOIN dz_links_current l ON f.link_pk = l.pk
WHERE f.event_ts > now() - INTERVAL 30 DAY
  AND f.link_pk != ''
GROUP BY l.code, hour
HAVING loss_pct >= 1  -- Only show hours with packet loss
ORDER BY hour DESC;

-- Packet loss for links in a specific metro
SELECT
    l.code AS link_code,
    ma.code AS side_a_metro,
    mz.code AS side_z_metro,
    toDate(f.event_ts) AS date,
    round(countIf(f.loss = true) * 100.0 / count(*), 1) AS loss_pct
FROM fact_dz_device_link_latency f
JOIN dz_links_current l ON f.link_pk = l.pk
LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
WHERE f.event_ts > now() - INTERVAL 30 DAY
  AND (ma.code = 'sao' OR mz.code = 'sao')
GROUP BY l.code, ma.code, mz.code, date
HAVING loss_pct >= 1
ORDER BY date DESC;
```

**IMPORTANT for "outage" questions**: Check BOTH status changes (`dz_link_status_changes`) AND historical packet loss (`fact_dz_device_link_latency`). A link can have an "outage" due to status change OR due to packet loss.

### Validators by Region/Metro
The pre-built views include `device_code`, `device_metro_code`, and `device_metro_name` columns. Use these for regional analysis:
```sql
-- Validators by metro (simple - use the view!)
SELECT device_metro_code, device_metro_name, COUNT(*) AS validator_count, SUM(activated_stake_sol) AS total_stake_sol
FROM solana_validators_on_dz_current
GROUP BY device_metro_code, device_metro_name
ORDER BY validator_count DESC;

-- Validators by device
SELECT device_code, device_metro_code, COUNT(*) AS validator_count, SUM(activated_stake_sol) AS total_stake_sol
FROM solana_validators_on_dz_current
GROUP BY device_code, device_metro_code
ORDER BY validator_count DESC;

-- Performance by metro
SELECT device_metro_code, dz_status, COUNT(*) AS validators, ROUND(AVG(avg_vote_lag_slots), 2) AS avg_vote_lag
FROM solana_validators_performance_current
WHERE is_delinquent = false AND device_metro_code != ''
GROUP BY device_metro_code, dz_status
ORDER BY device_metro_code;
```

### Off-DZ Validators by GeoIP Region
```sql
SELECT
    va.vote_pubkey,
    va.activated_stake_lamports / 1e9 AS stake_sol,
    gn.gossip_ip,
    geo.city,
    geo.country
FROM solana_gossip_nodes_current gn
JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
LEFT JOIN geoip_records_current geo ON gn.gossip_ip = geo.ip
LEFT JOIN dz_users_current u ON gn.gossip_ip = u.dz_ip AND u.status = 'activated'
WHERE u.pk = ''  -- Anti-join: not on DZ
  AND va.activated_stake_lamports > 0
  AND geo.city = 'Tokyo'
ORDER BY va.activated_stake_lamports DESC
LIMIT 10
```

### Contributors & Links
When asked about **contributors associated with links**, use device contributors on both sides:
```sql
SELECT DISTINCT
    l.code AS link_code,
    side_a_device.contributor_pk AS side_a_contributor,
    side_z_device.contributor_pk AS side_z_contributor
FROM dz_links_current l
JOIN dz_devices_current side_a_device ON l.side_a_pk = side_a_device.pk
JOIN dz_devices_current side_z_device ON l.side_z_pk = side_z_device.pk
```

### Common Joins
- **DZ User to Solana Gossip**: `dz_users_current.dz_ip = solana_gossip_nodes_current.gossip_ip`
- **Gossip to Validator**: `solana_gossip_nodes_current.pubkey = solana_vote_accounts_current.node_pubkey`
- **User to Device**: `dz_users_current.device_pk = dz_devices_current.pk`
- **Device to Metro**: `dz_devices_current.metro_pk = dz_metros_current.pk`
- **Link telemetry**: `fact_dz_device_link_latency.link_pk = dz_links_current.pk`

### Data Ingestion Start (CRITICAL)
The earliest `snapshot_ts` in history tables = **when ingestion began**, NOT when entities were created.

**For "recently connected" questions**: Use comparison approach (connected NOW but NOT X hours ago).
**For "growth since tracking began"**: Use first-appearance approach.

## SQL Guidelines

- Use ClickHouse SQL syntax
- **CRITICAL: Write all SQL on a single line (no line breaks within SQL strings)**
- End queries with semicolons
- Use explicit column names (no SELECT *)
- Include meaningful aliases
- Add ORDER BY for deterministic results
- Use LIMIT for list queries (default 100), but NOT for aggregations
- **ONLY use table and column names from the schema**
- Always report devices/links by `code`, not pk
- **ALWAYS include entity identifiers** (vote_pubkey, node_pubkey, device code, link code) in results
- Always include time filters on fact tables using `event_ts`
