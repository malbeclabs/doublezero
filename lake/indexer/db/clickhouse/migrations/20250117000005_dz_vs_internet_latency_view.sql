-- +goose Up

-- DZ vs Public Internet Latency Comparison View
-- Compares DZ network performance against public internet for each metro pair.
-- This makes it easy to answer questions like "compare DZ to the public internet".
--
-- Both DZ and internet latency are aggregated over the past 24 hours by default.
-- Metrics include: avg RTT, p95 RTT, avg jitter, p95 jitter, packet loss (DZ only)
--
-- The improvement_pct shows how much faster DZ is compared to internet
-- (positive = DZ is faster, negative = internet is faster)
--
-- NOTE: Metro pairs are normalized using least/greatest so that both directions
-- (e.g., ams→fra and fra→ams) are combined into a single comparison.

-- +goose StatementBegin
CREATE OR REPLACE VIEW dz_vs_internet_latency_comparison
AS
WITH
-- Time boundary for aggregation (last 24 hours)
lookback AS (
    SELECT now() - INTERVAL 24 HOUR AS min_ts
),

-- DZ latency aggregated by metro pair (normalized direction)
-- Join through links and devices to get metro codes
-- Use least/greatest to normalize direction so both a→z and z→a are combined
dz_latency AS (
    SELECT
        least(ma.code, mz.code) AS metro1,
        greatest(ma.code, mz.code) AS metro2,
        -- Pick name corresponding to the normalized codes
        if(ma.code < mz.code, ma.name, mz.name) AS metro1_name,
        if(ma.code < mz.code, mz.name, ma.name) AS metro2_name,
        round(avg(f.rtt_us) / 1000.0, 2) AS avg_rtt_ms,
        round(quantile(0.95)(f.rtt_us) / 1000.0, 2) AS p95_rtt_ms,
        round(avg(f.ipdv_us) / 1000.0, 2) AS avg_jitter_ms,
        round(quantile(0.95)(f.ipdv_us) / 1000.0, 2) AS p95_jitter_ms,
        round(countIf(f.loss = true) * 100.0 / count(), 2) AS loss_pct,
        count() AS sample_count
    FROM fact_dz_device_link_latency f
    CROSS JOIN lookback
    JOIN dz_links_current l ON f.link_pk = l.pk
    JOIN dz_devices_current da ON l.side_a_pk = da.pk
    JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
    JOIN dz_metros_current ma ON da.metro_pk = ma.pk
    JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
    WHERE f.event_ts >= lookback.min_ts
      AND f.link_pk != ''
      AND ma.code != mz.code  -- Exclude intra-metro links
    GROUP BY metro1, metro2, metro1_name, metro2_name
),

-- Internet latency aggregated by metro pair (normalized direction)
internet_latency AS (
    SELECT
        least(ma.code, mz.code) AS metro1,
        greatest(ma.code, mz.code) AS metro2,
        if(ma.code < mz.code, ma.name, mz.name) AS metro1_name,
        if(ma.code < mz.code, mz.name, ma.name) AS metro2_name,
        round(avg(f.rtt_us) / 1000.0, 2) AS avg_rtt_ms,
        round(quantile(0.95)(f.rtt_us) / 1000.0, 2) AS p95_rtt_ms,
        round(avg(f.ipdv_us) / 1000.0, 2) AS avg_jitter_ms,
        round(quantile(0.95)(f.ipdv_us) / 1000.0, 2) AS p95_jitter_ms,
        count() AS sample_count
    FROM fact_dz_internet_metro_latency f
    CROSS JOIN lookback
    JOIN dz_metros_current ma ON f.origin_metro_pk = ma.pk
    JOIN dz_metros_current mz ON f.target_metro_pk = mz.pk
    WHERE f.event_ts >= lookback.min_ts
      AND ma.code != mz.code  -- Exclude same-metro measurements
    GROUP BY metro1, metro2, metro1_name, metro2_name
)

-- Join DZ and Internet latency for comparison
SELECT
    COALESCE(dz.metro1, inet.metro1) AS origin_metro,
    COALESCE(dz.metro1_name, inet.metro1_name) AS origin_metro_name,
    COALESCE(dz.metro2, inet.metro2) AS target_metro,
    COALESCE(dz.metro2_name, inet.metro2_name) AS target_metro_name,

    -- DZ metrics
    dz.avg_rtt_ms AS dz_avg_rtt_ms,
    dz.p95_rtt_ms AS dz_p95_rtt_ms,
    dz.avg_jitter_ms AS dz_avg_jitter_ms,
    dz.p95_jitter_ms AS dz_p95_jitter_ms,
    dz.loss_pct AS dz_loss_pct,
    dz.sample_count AS dz_sample_count,

    -- Internet metrics
    inet.avg_rtt_ms AS internet_avg_rtt_ms,
    inet.p95_rtt_ms AS internet_p95_rtt_ms,
    inet.avg_jitter_ms AS internet_avg_jitter_ms,
    inet.p95_jitter_ms AS internet_p95_jitter_ms,
    inet.sample_count AS internet_sample_count,

    -- Improvement calculations (positive = DZ is faster)
    CASE
        WHEN inet.avg_rtt_ms > 0 AND dz.avg_rtt_ms > 0
        THEN round((inet.avg_rtt_ms - dz.avg_rtt_ms) / inet.avg_rtt_ms * 100, 1)
        ELSE NULL
    END AS rtt_improvement_pct,

    CASE
        WHEN inet.avg_jitter_ms > 0 AND dz.avg_jitter_ms > 0
        THEN round((inet.avg_jitter_ms - dz.avg_jitter_ms) / inet.avg_jitter_ms * 100, 1)
        ELSE NULL
    END AS jitter_improvement_pct

FROM dz_latency dz
FULL OUTER JOIN internet_latency inet
    ON dz.metro1 = inet.metro1
    AND dz.metro2 = inet.metro2
WHERE dz.sample_count > 0 OR inet.sample_count > 0
ORDER BY origin_metro, target_metro;
-- +goose StatementEnd

-- +goose Down
-- Note: Down migrations would drop views.
-- Since we use CREATE OR REPLACE, re-running up is safe.
