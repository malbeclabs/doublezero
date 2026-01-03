-- Fix credits_delta for epoch rollover rows
--
-- Problem: When epoch rollover occurred (epoch N -> epoch N+1), credits_delta
-- was incorrectly set to the absolute credits_epoch_credits value instead of NULL.
-- This migration sets credits_delta to NULL for all epoch rollover rows.
--
-- IMPORTANT:
-- 1. Backup your data before running this migration
-- 2. Test on a non-production database first
-- 3. Run the dry-run query below first to see how many rows will be affected

-- ============================================================================
-- DRY RUN: Count rows that will be fixed
-- ============================================================================
-- Uncomment and run this first to see how many rows will be affected:
--
-- WITH epoch_rollovers AS (
--   SELECT
--     vote_account_pubkey,
--     time,
--     credits_epoch,
--     credits_epoch_credits,
--     credits_delta,
--     LAG(credits_epoch) OVER (
--       PARTITION BY vote_account_pubkey
--       ORDER BY time
--     ) AS prev_epoch
--   FROM solana_vote_account_activity_raw
-- )
-- SELECT
--   COUNT(*) AS rows_to_fix,
--   COUNT(DISTINCT vote_account_pubkey) AS affected_vote_accounts
-- FROM epoch_rollovers
-- WHERE prev_epoch IS NOT NULL
--   AND credits_epoch = prev_epoch + 1
--   AND credits_delta IS NOT NULL;

-- ============================================================================
-- BACKFILL: Set credits_delta to NULL for epoch rollover rows
-- ============================================================================
-- Note: DuckDB supports UPDATE with subqueries containing window functions

UPDATE solana_vote_account_activity_raw
SET credits_delta = NULL
WHERE (vote_account_pubkey, time) IN (
  SELECT
    vote_account_pubkey,
    time
  FROM (
    SELECT
      vote_account_pubkey,
      time,
      credits_epoch,
      LAG(credits_epoch) OVER (
        PARTITION BY vote_account_pubkey
        ORDER BY time
      ) AS prev_epoch,
      credits_delta
    FROM solana_vote_account_activity_raw
  ) AS epoch_rollovers
  WHERE prev_epoch IS NOT NULL
    AND credits_epoch = prev_epoch + 1
    AND credits_delta IS NOT NULL
);

