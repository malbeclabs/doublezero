-- +goose Up

-- Link Health Views
-- Replaces dz_link_issue_events with two simpler, focused views:
-- 1. dz_links_health_current: Current health state with boolean flags
-- 2. dz_link_status_changes: Historical status transitions

-- +goose StatementBegin
-- Drop the old complex view
DROP VIEW IF EXISTS dz_link_issue_events;
-- +goose StatementEnd

-- +goose StatementBegin
-- Create dz_links_health_current
-- Shows current health state of each link with simple boolean flags
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
    l.pk AS pk,
    l.code AS code,
    l.status AS status,
    l.isis_delay_override_ns AS isis_delay_override_ns,
    l.committed_rtt_ns AS committed_rtt_ns,
    l.bandwidth_bps AS bandwidth_bps,
    ma.code AS side_a_metro,
    mz.code AS side_z_metro,
    l.status = 'soft-drained' AS is_soft_drained,
    l.status = 'hard-drained' AS is_hard_drained,
    l.isis_delay_override_ns = 1000000000 AS is_isis_soft_drained,
    COALESCE(rl.loss_pct, 0) AS loss_pct,
    COALESCE(rl.loss_pct, 0) >= 1 AS has_packet_loss,
    COALESCE(rl.avg_rtt_us, 0) AS avg_rtt_us,
    COALESCE(rl.p95_rtt_us, 0) AS p95_rtt_us,
    CASE
        WHEN l.committed_rtt_ns > 0 AND COALESCE(rl.avg_rtt_us, 0) > (l.committed_rtt_ns / 1000.0)
        THEN true ELSE false
    END AS exceeds_committed_rtt,
    rl.last_sample_ts AS last_sample_ts,
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
-- +goose StatementEnd

-- +goose StatementBegin
-- Create dz_link_status_changes
-- Shows all status transitions for links (historical)
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
-- +goose StatementEnd

-- +goose Down
-- Note: Down migrations would recreate the old view, but we use CREATE OR REPLACE
-- so re-running up is safe.
