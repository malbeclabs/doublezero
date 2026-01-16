-- Link Outage Events View
-- Unified view of all link outage/issue events from multiple sources:
-- 1. status_change: Link status transitions (activated -> soft-drained, etc.)
-- 2. packet_loss: Periods of significant packet loss
-- 3. link_dark: Gaps in telemetry (no samples received)
-- 4. sla_breach: Latency exceeding committed RTT threshold
--
-- Filter by event_type and apply thresholds at query time.
-- Limited to last 90 days for performance.

CREATE OR REPLACE VIEW dz_link_outage_events
AS
WITH
-- Time boundary for performance
lookback AS (
    SELECT now() - INTERVAL 90 DAY AS min_ts
),

-- 1. STATUS CHANGES: Use window function to find recovery time
status_transitions_with_recovery AS (
    SELECT
        t.link_pk,
        t.link_code,
        t.transition_ts,
        t.previous_status,
        t.new_status,
        t.side_a_metro,
        t.side_z_metro,
        -- Use LEAD to find the next transition back to activated
        leadInFrame(t.transition_ts) OVER (
            PARTITION BY t.link_pk
            ORDER BY t.transition_ts
        ) AS next_transition_ts,
        leadInFrame(t.new_status) OVER (
            PARTITION BY t.link_pk
            ORDER BY t.transition_ts
        ) AS next_status
    FROM dz_link_status_transitions t
    CROSS JOIN lookback
    WHERE t.transition_ts >= lookback.min_ts
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

-- 2. PACKET LOSS: Hourly aggregates with loss above threshold
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
-- Mark each hour as "in outage" (loss >= 0.1%) or "healthy" (loss < 0.1%)
loss_with_state AS (
    SELECT
        h.*,
        h.loss_pct >= 0.1 AS is_outage,
        lagInFrame(h.loss_pct, 1, 0) OVER (PARTITION BY h.link_pk ORDER BY h.hour_ts) AS prev_loss_pct,
        -- Find the next hour where loss < 0.1% (recovery)
        leadInFrame(
            CASE WHEN h.loss_pct < 0.1 THEN h.hour_ts ELSE NULL END
        ) OVER (PARTITION BY h.link_pk ORDER BY h.hour_ts) AS next_healthy_ts
    FROM hourly_loss h
),
packet_loss_events AS (
    SELECT
        l.link_pk,
        lk.code AS link_code,
        'packet_loss' AS event_type,
        l.hour_ts AS start_ts,
        l.next_healthy_ts AS end_ts,
        '' AS previous_status,
        '' AS new_status,
        COALESCE(ma.code, '') AS side_a_metro,
        COALESCE(mz.code, '') AS side_z_metro,
        l.loss_pct,
        CAST(NULL AS Nullable(Float64)) AS latency_us,
        CAST(NULL AS Nullable(Float64)) AS committed_rtt_us,
        CAST(NULL AS Nullable(Float64)) AS overage_pct,
        CAST(NULL AS Nullable(Int64)) AS gap_minutes
    FROM loss_with_state l
    JOIN dz_links_current lk ON l.link_pk = lk.pk
    LEFT JOIN dz_devices_current da ON lk.side_a_pk = da.pk
    LEFT JOIN dz_devices_current dz ON lk.side_z_pk = dz.pk
    LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
    LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
    WHERE l.loss_pct >= 0.1  -- Loss threshold (warning level)
      AND l.prev_loss_pct < 0.1  -- Previous hour was OK (this is the START of a loss period)
),

-- 3. LINK DARK: Gaps in telemetry > 30 minutes
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
        a.*,
        lagInFrame(a.last_sample, 1, toDateTime64('1970-01-01 00:00:00', 3)) OVER (PARTITION BY a.link_pk ORDER BY a.hour_ts) AS prev_last_sample
    FROM link_activity a
),
link_dark_events AS (
    SELECT
        a.link_pk,
        lk.code AS link_code,
        'link_dark' AS event_type,
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

-- 4. SLA BREACH: Hourly latency exceeding committed RTT
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
latency_with_state AS (
    SELECT
        h.*,
        lk.code AS link_code,
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
        ) OVER (PARTITION BY h.link_pk ORDER BY h.hour_ts) AS prev_overage_pct,
        -- Find next hour where overage < 20% (recovery)
        leadInFrame(
            CASE
                WHEN lk.committed_rtt_ns > 0 AND ((h.avg_latency_us - (lk.committed_rtt_ns / 1000.0)) / (lk.committed_rtt_ns / 1000.0)) * 100 < 20
                THEN h.hour_ts
                ELSE NULL
            END
        ) OVER (PARTITION BY h.link_pk ORDER BY h.hour_ts) AS next_healthy_ts
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
        l.link_pk,
        l.link_code,
        'sla_breach' AS event_type,
        l.hour_ts AS start_ts,
        l.next_healthy_ts AS end_ts,
        '' AS previous_status,
        '' AS new_status,
        l.side_a_metro,
        l.side_z_metro,
        CAST(NULL AS Nullable(Float64)) AS loss_pct,
        l.avg_latency_us AS latency_us,
        l.committed_rtt_us,
        l.overage_pct,
        CAST(NULL AS Nullable(Int64)) AS gap_minutes
    FROM latency_with_state l
    WHERE l.overage_pct >= 20
      AND l.prev_overage_pct < 20
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
FROM link_dark_events

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
