-- +goose Up

-- Solana Validator Performance Metrics View
-- Combines vote lag and skip rate metrics for all validators, with DZ status
-- Performance data is based on the last 24 hours of activity

-- +goose StatementBegin
-- solana_validators_performance_current
-- Shows all validators with their performance metrics and DZ connection status
-- IMPORTANT: Filters out delinquent validators from vote lag calculations
-- (delinquent validators have stopped voting and would skew averages massively)
CREATE OR REPLACE VIEW solana_validators_performance_current
AS
WITH vote_lag_metrics AS (
    -- Calculate vote lag for non-delinquent validators only
    -- Delinquent validators can have vote lags of millions of slots, skewing averages
    SELECT
        vote_account_pubkey,
        node_identity_pubkey,
        ROUND(AVG(cluster_slot - last_vote_slot), 2) AS avg_vote_lag_slots,
        MIN(cluster_slot - last_vote_slot) AS min_vote_lag_slots,
        MAX(cluster_slot - last_vote_slot) AS max_vote_lag_slots,
        COUNT(*) AS vote_samples
    FROM fact_solana_vote_account_activity
    WHERE event_ts > now() - INTERVAL 24 HOUR
      AND is_delinquent = false
    GROUP BY vote_account_pubkey, node_identity_pubkey
),
skip_rate_metrics AS (
    -- Calculate skip rate from block production data
    SELECT
        leader_identity_pubkey,
        MAX(leader_slots_assigned_cum) AS slots_assigned,
        MAX(blocks_produced_cum) AS blocks_produced,
        ROUND(
            (MAX(leader_slots_assigned_cum) - MAX(blocks_produced_cum)) * 100.0
            / NULLIF(MAX(leader_slots_assigned_cum), 0),
            2
        ) AS skip_rate_pct
    FROM fact_solana_block_production
    WHERE event_ts > now() - INTERVAL 24 HOUR
    GROUP BY leader_identity_pubkey
    HAVING slots_assigned > 0
),
delinquent_status AS (
    -- Get current delinquent status per validator
    SELECT
        vote_account_pubkey,
        node_identity_pubkey,
        argMax(is_delinquent, event_ts) AS is_delinquent
    FROM fact_solana_vote_account_activity
    WHERE event_ts > now() - INTERVAL 24 HOUR
    GROUP BY vote_account_pubkey, node_identity_pubkey
)
SELECT
    va.vote_pubkey AS vote_pubkey,
    va.node_pubkey AS node_pubkey,
    va.activated_stake_lamports AS activated_stake_lamports,
    va.activated_stake_lamports / 1000000000.0 AS activated_stake_sol,
    va.commission_percentage AS commission_percentage,
    -- DZ connection status
    CASE WHEN dz.vote_pubkey != '' THEN 'on_dz' ELSE 'off_dz' END AS dz_status,
    -- DZ device/metro info (NULL if not on DZ)
    dz.device_pk AS device_pk,
    dz.device_code AS device_code,
    dz.device_metro_code AS device_metro_code,
    dz.device_metro_name AS device_metro_name,
    -- Vote lag metrics (NULL if delinquent or no recent activity)
    vl.avg_vote_lag_slots AS avg_vote_lag_slots,
    vl.min_vote_lag_slots AS min_vote_lag_slots,
    vl.max_vote_lag_slots AS max_vote_lag_slots,
    vl.vote_samples AS vote_samples,
    -- Skip rate metrics (NULL if no block production data)
    sr.slots_assigned AS slots_assigned,
    sr.blocks_produced AS blocks_produced,
    sr.skip_rate_pct AS skip_rate_pct,
    -- Delinquent status
    COALESCE(ds.is_delinquent, false) AS is_delinquent
FROM solana_vote_accounts_current va
LEFT JOIN solana_validators_on_dz_current dz ON va.vote_pubkey = dz.vote_pubkey
LEFT JOIN vote_lag_metrics vl ON va.vote_pubkey = vl.vote_account_pubkey AND va.node_pubkey = vl.node_identity_pubkey
LEFT JOIN skip_rate_metrics sr ON va.node_pubkey = sr.leader_identity_pubkey
LEFT JOIN delinquent_status ds ON va.vote_pubkey = ds.vote_account_pubkey AND va.node_pubkey = ds.node_identity_pubkey
WHERE va.epoch_vote_account = 'true'
  AND va.activated_stake_lamports > 0;
-- +goose StatementEnd

-- +goose Down
-- Note: Down migrations would drop views.
-- Since we use CREATE OR REPLACE, re-running up is safe.
