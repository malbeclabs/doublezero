-- Link Issue Events View
-- Unified view of all link issue events from multiple sources:
-- 1. status_change: Link status transitions (activated -> soft-drained, etc.)
-- 2. isis_delay_override_soft_drain: ISIS delay override set to 1s (effective soft-drain)
-- 3. packet_loss: Periods of significant packet loss
-- 4. missing_telemetry: Gaps in telemetry (no samples received)
-- 5. sla_breach: Latency exceeding committed RTT threshold
--
-- Filter by event_type and apply thresholds at query time.
-- Limited to last 90 days for performance.
--
-- Timestamp precision:
-- - status_change, isis_delay_override_soft_drain: Precise (from history table snapshot_ts)
-- - packet_loss, missing_telemetry, sla_breach: Hourly granularity

CREATE OR REPLACE VIEW dz_link_issue_events
AS
WITH
-- Time boundary for performance
lookback AS (
    SELECT now() - INTERVAL 90 DAY AS min_ts
),

-- 1. STATUS CHANGES: Compute transitions from history, then find recovery time
-- First compute raw transitions with LAG
link_transitions AS (
    SELECT
        lh.pk AS link_pk,
        lh.code AS link_code,
        lh.status AS new_status,
        lh.snapshot_ts AS transition_ts,
        lh.side_a_pk,
        lh.side_z_pk,
        LAG(lh.status) OVER (PARTITION BY lh.pk ORDER BY lh.snapshot_ts) AS previous_status
    FROM dim_dz_links_history lh
    CROSS JOIN lookback
    WHERE lh.is_deleted = 0
      AND lh.snapshot_ts >= lookback.min_ts
),
-- Add metro info and filter to actual status changes
status_transitions AS (
    SELECT
        t.link_pk,
        t.link_code,
        t.previous_status,
        t.new_status,
        t.transition_ts,
        ma.code AS side_a_metro,
        mz.code AS side_z_metro
    FROM link_transitions t
    JOIN dz_devices_current da ON t.side_a_pk = da.pk
    JOIN dz_devices_current dz ON t.side_z_pk = dz.pk
    JOIN dz_metros_current ma ON da.metro_pk = ma.pk
    JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
    WHERE t.previous_status IS NOT NULL
      AND t.previous_status != t.new_status
),
-- Use LEAD to find when status returns to activated (recovery time)
status_transitions_with_recovery AS (
    SELECT
        t.link_pk,
        t.link_code,
        t.transition_ts,
        t.previous_status,
        t.new_status,
        t.side_a_metro,
        t.side_z_metro,
        lead(t.transition_ts) OVER (
            PARTITION BY t.link_pk
            ORDER BY t.transition_ts
        ) AS next_transition_ts,
        lead(t.new_status) OVER (
            PARTITION BY t.link_pk
            ORDER BY t.transition_ts
        ) AS next_status
    FROM status_transitions t
),
status_events AS (
    SELECT
        link_pk,
        link_code,
        'status_change' AS event_type,
        transition_ts AS start_ts,
        -- end_ts is the next transition if it's back to activated
        CASE WHEN next_status = 'activated' THEN next_transition_ts ELSE NULL END AS end_ts,
        previous_status,
        new_status,
        side_a_metro,
        side_z_metro,
        CAST(NULL AS Nullable(Float64)) AS loss_pct,
        CAST(NULL AS Nullable(Float64)) AS latency_us,
        CAST(NULL AS Nullable(Float64)) AS committed_rtt_us,
        CAST(NULL AS Nullable(Float64)) AS overage_pct,
        CAST(NULL AS Nullable(Int64)) AS gap_minutes
    FROM status_transitions_with_recovery
    WHERE previous_status = 'activated'  -- Only outage starts
),

-- 2. ISIS DELAY OVERRIDE: Detect when isis_delay_override_ns transitions to 1s (effective soft-drain)
isis_transitions AS (
    SELECT
        lh.pk AS link_pk,
        lh.code AS link_code,
        lh.isis_delay_override_ns AS new_isis_delay,
        lh.snapshot_ts AS transition_ts,
        lh.side_a_pk,
        lh.side_z_pk,
        LAG(lh.isis_delay_override_ns) OVER (PARTITION BY lh.pk ORDER BY lh.snapshot_ts) AS previous_isis_delay
    FROM dim_dz_links_history lh
    CROSS JOIN lookback
    WHERE lh.is_deleted = 0
      AND lh.snapshot_ts >= lookback.min_ts
),
isis_transitions_filtered AS (
    SELECT
        t.link_pk,
        t.link_code,
        t.previous_isis_delay,
        t.new_isis_delay,
        t.transition_ts,
        ma.code AS side_a_metro,
        mz.code AS side_z_metro
    FROM isis_transitions t
    JOIN dz_devices_current da ON t.side_a_pk = da.pk
    JOIN dz_devices_current dz ON t.side_z_pk = dz.pk
    JOIN dz_metros_current ma ON da.metro_pk = ma.pk
    JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
    WHERE t.previous_isis_delay IS NOT NULL
      AND t.previous_isis_delay != t.new_isis_delay
),
isis_transitions_with_recovery AS (
    SELECT
        t.link_pk,
        t.link_code,
        t.transition_ts,
        t.previous_isis_delay,
        t.new_isis_delay,
        t.side_a_metro,
        t.side_z_metro,
        lead(t.transition_ts) OVER (
            PARTITION BY t.link_pk
            ORDER BY t.transition_ts
        ) AS next_transition_ts,
        lead(t.new_isis_delay) OVER (
            PARTITION BY t.link_pk
            ORDER BY t.transition_ts
        ) AS next_isis_delay
    FROM isis_transitions_filtered t
),
isis_delay_events AS (
    SELECT
        link_pk,
        link_code,
        'isis_delay_override_soft_drain' AS event_type,
        transition_ts AS start_ts,
        -- end_ts is when isis_delay returns to 0 (or other non-1s value)
        CASE WHEN next_isis_delay != 1000000000 THEN next_transition_ts ELSE NULL END AS end_ts,
        '' AS previous_status,
        '' AS new_status,
        side_a_metro,
        side_z_metro,
        CAST(NULL AS Nullable(Float64)) AS loss_pct,
        CAST(NULL AS Nullable(Float64)) AS latency_us,
        CAST(NULL AS Nullable(Float64)) AS committed_rtt_us,
        CAST(NULL AS Nullable(Float64)) AS overage_pct,
        CAST(NULL AS Nullable(Int64)) AS gap_minutes
    FROM isis_transitions_with_recovery
    WHERE new_isis_delay = 1000000000  -- 1 second in nanoseconds = effective soft-drain
      AND (previous_isis_delay IS NULL OR previous_isis_delay != 1000000000)  -- Was not already soft-drained
),

-- 3. PACKET LOSS: Hourly aggregates with loss above threshold
-- Use array aggregation to find recovery times without correlated subqueries
hourly_loss AS (
    SELECT
        f.link_pk,
        toStartOfHour(f.event_ts) AS hour_ts,
        countIf(f.loss = true) AS loss_count,
        count() AS total_count,
        countIf(f.loss = true) * 100.0 / count() AS loss_pct
    FROM fact_dz_device_link_latency f
    CROSS JOIN lookback
    WHERE f.event_ts >= lookback.min_ts
      AND f.link_pk != ''
    GROUP BY f.link_pk, toStartOfHour(f.event_ts)
),
-- Collect recovery timestamps per link for lookup
loss_recovery_lookup AS (
    SELECT
        link_pk,
        groupArray(hour_ts) AS healthy_hours
    FROM hourly_loss
    WHERE loss_pct < 0.1
    GROUP BY link_pk
),
-- Find outage starts with lag calculation
loss_with_state AS (
    SELECT
        h.link_pk,
        h.hour_ts,
        h.loss_pct,
        lagInFrame(h.loss_pct, 1, 0) OVER (PARTITION BY h.link_pk ORDER BY h.hour_ts) AS prev_loss_pct
    FROM hourly_loss h
),
packet_loss_events AS (
    SELECT
        l.link_pk AS link_pk,
        lk.code AS link_code,
        'packet_loss' AS event_type,
        l.hour_ts AS start_ts,
        -- Find first healthy hour after start_ts using array lookup
        -- arrayFirst returns epoch (1970-01-01) when no match found, convert to NULL
        nullIf(arrayFirst(x -> x > l.hour_ts, COALESCE(r.healthy_hours, [])), toDateTime64('1970-01-01 00:00:00', 3)) AS end_ts,
        '' AS previous_status,
        '' AS new_status,
        COALESCE(ma.code, '') AS side_a_metro,
        COALESCE(mz.code, '') AS side_z_metro,
        l.loss_pct AS loss_pct,
        CAST(NULL AS Nullable(Float64)) AS latency_us,
        CAST(NULL AS Nullable(Float64)) AS committed_rtt_us,
        CAST(NULL AS Nullable(Float64)) AS overage_pct,
        CAST(NULL AS Nullable(Int64)) AS gap_minutes
    FROM loss_with_state l
    LEFT JOIN loss_recovery_lookup r ON l.link_pk = r.link_pk
    JOIN dz_links_current lk ON l.link_pk = lk.pk
    LEFT JOIN dz_devices_current da ON lk.side_a_pk = da.pk
    LEFT JOIN dz_devices_current dz ON lk.side_z_pk = dz.pk
    LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
    LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
    WHERE l.loss_pct >= 0.1  -- Loss threshold
      AND l.prev_loss_pct < 0.1  -- Previous hour was OK (start of loss period)
),

-- 4. MISSING TELEMETRY: Gaps in telemetry >= 120 minutes
link_activity AS (
    SELECT
        f.link_pk,
        toStartOfHour(f.event_ts) AS hour_ts,
        MIN(f.event_ts) AS first_sample,
        MAX(f.event_ts) AS last_sample
    FROM fact_dz_device_link_latency f
    CROSS JOIN lookback
    WHERE f.event_ts >= lookback.min_ts
      AND f.link_pk != ''
    GROUP BY f.link_pk, toStartOfHour(f.event_ts)
),
activity_with_prev AS (
    SELECT
        a.link_pk,
        a.hour_ts,
        a.first_sample,
        a.last_sample,
        lagInFrame(a.last_sample, 1, toDateTime64('1970-01-01 00:00:00', 3)) OVER (PARTITION BY a.link_pk ORDER BY a.hour_ts) AS prev_last_sample
    FROM link_activity a
),
missing_telemetry_events AS (
    SELECT
        a.link_pk,
        lk.code AS link_code,
        'missing_telemetry' AS event_type,
        a.prev_last_sample AS start_ts,
        a.first_sample AS end_ts,
        '' AS previous_status,
        '' AS new_status,
        COALESCE(ma.code, '') AS side_a_metro,
        COALESCE(mz.code, '') AS side_z_metro,
        CAST(NULL AS Nullable(Float64)) AS loss_pct,
        CAST(NULL AS Nullable(Float64)) AS latency_us,
        CAST(NULL AS Nullable(Float64)) AS committed_rtt_us,
        CAST(NULL AS Nullable(Float64)) AS overage_pct,
        dateDiff('minute', a.prev_last_sample, a.first_sample) AS gap_minutes
    FROM activity_with_prev a
    JOIN dz_links_current lk ON a.link_pk = lk.pk
    LEFT JOIN dz_devices_current da ON lk.side_a_pk = da.pk
    LEFT JOIN dz_devices_current dz ON lk.side_z_pk = dz.pk
    LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
    LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
    WHERE a.prev_last_sample > toDateTime64('1970-01-01 00:00:00', 3)
      AND dateDiff('minute', a.prev_last_sample, a.first_sample) >= 120  -- 2 hours to avoid false positives from hourly sampling
),

-- 5. SLA BREACH: Hourly latency exceeding committed RTT
hourly_latency AS (
    SELECT
        f.link_pk,
        toStartOfHour(f.event_ts) AS hour_ts,
        avg(f.rtt_us) AS avg_latency_us,
        quantile(0.95)(f.rtt_us) AS p95_latency_us
    FROM fact_dz_device_link_latency f
    CROSS JOIN lookback
    WHERE f.event_ts >= lookback.min_ts
      AND f.link_pk != ''
      AND f.loss = false
      AND f.rtt_us > 0
    GROUP BY f.link_pk, toStartOfHour(f.event_ts)
),
-- Collect recovery timestamps per link for SLA breach lookup
sla_recovery_lookup AS (
    SELECT
        h.link_pk,
        groupArray(h.hour_ts) AS healthy_hours
    FROM hourly_latency h
    JOIN dz_links_current lk ON h.link_pk = lk.pk
    WHERE lk.committed_rtt_ns > 0
      AND ((h.avg_latency_us - (lk.committed_rtt_ns / 1000.0)) / (lk.committed_rtt_ns / 1000.0)) * 100 < 20
    GROUP BY h.link_pk
),
latency_with_state AS (
    SELECT
        h.link_pk,
        h.hour_ts,
        h.avg_latency_us,
        h.p95_latency_us,
        lk.code AS link_code,
        lk.committed_rtt_ns,
        lk.committed_rtt_ns / 1000.0 AS committed_rtt_us,
        COALESCE(ma.code, '') AS side_a_metro,
        COALESCE(mz.code, '') AS side_z_metro,
        CASE
            WHEN lk.committed_rtt_ns > 0
            THEN ((h.avg_latency_us - (lk.committed_rtt_ns / 1000.0)) / (lk.committed_rtt_ns / 1000.0)) * 100
            ELSE 0
        END AS overage_pct,
        lagInFrame(
            CASE
                WHEN lk.committed_rtt_ns > 0
                THEN ((h.avg_latency_us - (lk.committed_rtt_ns / 1000.0)) / (lk.committed_rtt_ns / 1000.0)) * 100
                ELSE 0
            END, 1, 0
        ) OVER (PARTITION BY h.link_pk ORDER BY h.hour_ts) AS prev_overage_pct
    FROM hourly_latency h
    JOIN dz_links_current lk ON h.link_pk = lk.pk
    LEFT JOIN dz_devices_current da ON lk.side_a_pk = da.pk
    LEFT JOIN dz_devices_current dz ON lk.side_z_pk = dz.pk
    LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
    LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
    WHERE lk.committed_rtt_ns > 0
),
sla_breach_events AS (
    SELECT
        l.link_pk AS link_pk,
        l.link_code AS link_code,
        'sla_breach' AS event_type,
        l.hour_ts AS start_ts,
        -- Find first healthy hour after start_ts using array lookup
        -- arrayFirst returns epoch (1970-01-01) when no match found, convert to NULL
        nullIf(arrayFirst(x -> x > l.hour_ts, COALESCE(r.healthy_hours, [])), toDateTime64('1970-01-01 00:00:00', 3)) AS end_ts,
        '' AS previous_status,
        '' AS new_status,
        l.side_a_metro AS side_a_metro,
        l.side_z_metro AS side_z_metro,
        CAST(NULL AS Nullable(Float64)) AS loss_pct,
        l.avg_latency_us AS latency_us,
        l.committed_rtt_us AS committed_rtt_us,
        l.overage_pct AS overage_pct,
        CAST(NULL AS Nullable(Int64)) AS gap_minutes
    FROM latency_with_state l
    LEFT JOIN sla_recovery_lookup r ON l.link_pk = r.link_pk
    WHERE l.overage_pct >= 20 AND l.prev_overage_pct < 20
)

-- UNION all event types
SELECT
    link_pk,
    link_code,
    event_type,
    start_ts,
    end_ts,
    CASE
        WHEN end_ts IS NOT NULL THEN dateDiff('minute', start_ts, end_ts)
        ELSE NULL
    END AS duration_minutes,
    CASE WHEN end_ts IS NULL THEN true ELSE false END AS is_ongoing,
    previous_status,
    new_status,
    side_a_metro,
    side_z_metro,
    loss_pct,
    latency_us,
    committed_rtt_us,
    overage_pct,
    gap_minutes
FROM status_events

UNION ALL

SELECT
    link_pk,
    link_code,
    event_type,
    start_ts,
    end_ts,
    CASE
        WHEN end_ts IS NOT NULL THEN dateDiff('minute', start_ts, end_ts)
        ELSE NULL
    END AS duration_minutes,
    CASE WHEN end_ts IS NULL THEN true ELSE false END AS is_ongoing,
    previous_status,
    new_status,
    side_a_metro,
    side_z_metro,
    loss_pct,
    latency_us,
    committed_rtt_us,
    overage_pct,
    gap_minutes
FROM isis_delay_events

UNION ALL

SELECT
    link_pk,
    link_code,
    event_type,
    start_ts,
    end_ts,
    CASE
        WHEN end_ts IS NOT NULL THEN dateDiff('minute', start_ts, end_ts)
        ELSE NULL
    END AS duration_minutes,
    CASE WHEN end_ts IS NULL THEN true ELSE false END AS is_ongoing,
    previous_status,
    new_status,
    side_a_metro,
    side_z_metro,
    loss_pct,
    latency_us,
    committed_rtt_us,
    overage_pct,
    gap_minutes
FROM packet_loss_events

UNION ALL

SELECT
    link_pk,
    link_code,
    event_type,
    start_ts,
    end_ts,
    gap_minutes AS duration_minutes,
    false AS is_ongoing,
    previous_status,
    new_status,
    side_a_metro,
    side_z_metro,
    loss_pct,
    latency_us,
    committed_rtt_us,
    overage_pct,
    gap_minutes
FROM missing_telemetry_events

UNION ALL

SELECT
    link_pk,
    link_code,
    event_type,
    start_ts,
    end_ts,
    CASE
        WHEN end_ts IS NOT NULL THEN dateDiff('minute', start_ts, end_ts)
        ELSE NULL
    END AS duration_minutes,
    CASE WHEN end_ts IS NULL THEN true ELSE false END AS is_ongoing,
    previous_status,
    new_status,
    side_a_metro,
    side_z_metro,
    loss_pct,
    latency_us,
    committed_rtt_us,
    overage_pct,
    gap_minutes
FROM sla_breach_events;

-- Drop old view name if it exists (for migration from old name)
DROP VIEW IF EXISTS dz_link_outage_events;
