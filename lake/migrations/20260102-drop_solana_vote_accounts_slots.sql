-- Drop last_vote_slot and root_slot from solana_vote_accounts SCD tables
-- This migration removes the last_vote_slot and root_slot columns from both
-- solana_vote_accounts_current and solana_vote_accounts_history tables.
--
-- IMPORTANT:
-- 1. This migration is irreversible - make sure you have backups
-- 2. Test on a non-production database first
-- 3. Run this during a maintenance window
-- 4. After running this migration, the SCD code has been updated to no longer
--    include these columns in the payload

-- ============================================================================
-- Drop columns from solana_vote_accounts_current
-- ============================================================================
ALTER TABLE solana_vote_accounts_current DROP COLUMN IF EXISTS last_vote_slot;
ALTER TABLE solana_vote_accounts_current DROP COLUMN IF EXISTS root_slot;

-- ============================================================================
-- Drop columns from solana_vote_accounts_history
-- ============================================================================
ALTER TABLE solana_vote_accounts_history DROP COLUMN IF EXISTS last_vote_slot;
ALTER TABLE solana_vote_accounts_history DROP COLUMN IF EXISTS root_slot;

