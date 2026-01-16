-- Link Status Transitions View
-- Pre-computes status changes for links with metro information
-- Simplifies queries for link outages, status history, and incident timelines

CREATE OR REPLACE VIEW dz_link_status_transitions
AS
WITH transitions AS (
    SELECT
        lh.pk,
        lh.code,
        lh.status AS new_status,
        lh.snapshot_ts,
        lh.side_a_pk,
        lh.side_z_pk,
        LAG(lh.status) OVER (PARTITION BY lh.pk ORDER BY lh.snapshot_ts) AS previous_status
    FROM dim_dz_links_history lh
    WHERE lh.is_deleted = 0
)
SELECT
    t.pk AS link_pk,
    t.code AS link_code,
    t.previous_status,
    t.new_status,
    t.snapshot_ts AS transition_ts,
    ma.code AS side_a_metro,
    mz.code AS side_z_metro
FROM transitions t
JOIN dz_devices_current da ON t.side_a_pk = da.pk
JOIN dz_devices_current dz ON t.side_z_pk = dz.pk
JOIN dz_metros_current ma ON da.metro_pk = ma.pk
JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
WHERE t.previous_status IS NOT NULL
  AND t.previous_status != t.new_status;
