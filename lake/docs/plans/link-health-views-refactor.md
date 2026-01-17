# Link Health Views Refactor

## Problem

The `dz_link_issue_events` view is unreliable, especially for "current" status queries:
- Complex 480-line view trying to unify 5 different event types
- `is_ongoing` flag for "current" state is unreliable
- Different event types have different timestamp precisions (exact vs hourly)
- Complex recovery detection logic
- Agent struggles to get clear answers about link health

## Solution

Replace `dz_link_issue_events` with two simpler, focused views:

### 1. `dz_links_health_current`

Shows current health state of each link with simple boolean flags. Use for "what links have issues right now" questions.

**Columns:**
- `pk`, `code`, `status`, `isis_delay_override_ns`
- `committed_rtt_ns`, `bandwidth_bps`
- `side_a_metro`, `side_z_metro`
- `is_status_degraded` - Link status is not 'activated'
- `is_isis_soft_drained` - ISIS delay override set to 1s
- `loss_pct` - Packet loss percentage (last hour)
- `has_packet_loss` - Loss >= 1% in last hour
- `avg_rtt_us`, `p95_rtt_us` - Latency metrics (last hour)
- `exceeds_committed_rtt` - Avg latency exceeds committed RTT
- `last_sample_ts` - Last telemetry timestamp
- `is_dark` - No telemetry in last 2 hours

### 2. `dz_link_status_changes`

Shows all status transitions for links. Use for "when did link X go down" or "link status history" questions.

**Columns:**
- `link_pk`, `link_code`
- `previous_status`, `new_status`
- `changed_ts`
- `side_a_metro`, `side_z_metro`

### For Packet Loss / Latency History

Query the raw `fact_dz_device_link_latency` table directly with time filters. More reliable than complex event detection.

## Implementation Steps

### 1. Create Migration File

Create `migrations/0008_link_health_views.sql`:

```sql
-- Drop the old complex view
DROP VIEW IF EXISTS dz_link_issue_events;

-- Create dz_links_health_current
CREATE OR REPLACE VIEW dz_links_health_current
AS
WITH recent_latency AS (
    SELECT
        link_pk,
        COUNT(*) AS sample_count,
        countIf(loss = true) * 100.0 / COUNT(*) AS loss_pct,
        avgIf(rtt_us, loss = false AND rtt_us > 0) AS avg_rtt_us,
        quantileIf(0.95)(rtt_us, loss = false AND rtt_us > 0) AS p95_rtt_us,
        max(event_ts) AS last_sample_ts
    FROM fact_dz_device_link_latency
    WHERE event_ts >= now() - INTERVAL 1 HOUR
      AND link_pk != ''
    GROUP BY link_pk
)
SELECT
    l.pk,
    l.code,
    l.status,
    l.isis_delay_override_ns,
    l.committed_rtt_ns,
    l.bandwidth_bps,
    ma.code AS side_a_metro,
    mz.code AS side_z_metro,
    l.status != 'activated' AS is_status_degraded,
    l.isis_delay_override_ns = 1000000000 AS is_isis_soft_drained,
    COALESCE(rl.loss_pct, 0) AS loss_pct,
    COALESCE(rl.loss_pct, 0) >= 1 AS has_packet_loss,
    COALESCE(rl.avg_rtt_us, 0) AS avg_rtt_us,
    COALESCE(rl.p95_rtt_us, 0) AS p95_rtt_us,
    CASE
        WHEN l.committed_rtt_ns > 0 AND COALESCE(rl.avg_rtt_us, 0) > (l.committed_rtt_ns / 1000.0)
        THEN true ELSE false
    END AS exceeds_committed_rtt,
    rl.last_sample_ts,
    CASE
        WHEN rl.last_sample_ts IS NULL THEN true
        WHEN rl.last_sample_ts < now() - INTERVAL 2 HOUR THEN true
        ELSE false
    END AS is_dark
FROM dz_links_current l
LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
LEFT JOIN recent_latency rl ON l.pk = rl.link_pk;

-- Create dz_link_status_changes
CREATE OR REPLACE VIEW dz_link_status_changes
AS
WITH transitions AS (
    SELECT
        lh.pk AS link_pk,
        lh.code AS link_code,
        lh.status AS new_status,
        lh.snapshot_ts AS changed_ts,
        LAG(lh.status) OVER (PARTITION BY lh.pk ORDER BY lh.snapshot_ts) AS previous_status
    FROM dim_dz_links_history lh
    WHERE lh.is_deleted = 0
)
SELECT
    t.link_pk,
    t.link_code,
    t.previous_status,
    t.new_status,
    t.changed_ts,
    ma.code AS side_a_metro,
    mz.code AS side_z_metro
FROM transitions t
JOIN dz_links_current l ON t.link_pk = l.pk
LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
WHERE t.previous_status IS NOT NULL
  AND t.previous_status != t.new_status;
```

### 2. Add Goose Migration

Also add to `indexer/db/clickhouse/migrations/20250117000008_link_health_views.sql` with goose annotations (`-- +goose Up`, `-- +goose StatementBegin`, etc.)

### 3. Update SQL_CONTEXT.md

Replace `dz_link_issue_events` references with new views:

```markdown
| `dz_links_health_current` | Current link health (status, packet loss, latency vs committed, is_dark) |
| `dz_link_status_changes` | Link status transitions with timestamps (previous_status, new_status, changed_ts) |
```

Update "Link Issue Detection" section to "Link Health & Status" with new examples.

### 4. Update Evals

Update `evals/link_outages_detection_test.go` to use new views in validation queries.

### 5. Update Documentation

- `agent/README.md` - Update pre-built views table
- `docs/domain/link-issues.md` - Update or archive

## Files to Change

1. `migrations/0008_link_health_views.sql` - New migration
2. `indexer/db/clickhouse/migrations/20250117000008_link_health_views.sql` - Goose migration
3. `agent/pkg/workflow/prompts/SQL_CONTEXT.md` - Update agent context
4. `agent/evals/link_outages_detection_test.go` - Update validation queries
5. `agent/README.md` - Update docs
6. `migrations/0005_link_issue_events_view.sql` - Can be deleted (migration 8 drops the view)

## Testing

Run evals after changes:
```bash
./scripts/run-evals.sh -f 'LinkOutages' -s  # Short mode first
./scripts/run-evals.sh -f 'LinkOutages'      # Full test
./scripts/run-evals.sh -f 'NetworkHealth'    # Also affected
```
