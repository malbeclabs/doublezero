-- Drop Ingest Runs Tables Migration
-- This migration drops all _ingest_runs tables that are no longer needed.
--
-- IMPORTANT:
-- 1. This migration is irreversible - make sure you have backups
-- 2. Test on a non-production database first
-- 3. Run this during a maintenance window
-- 4. After running this migration, the SCD code has been updated to disable ingest_runs tracking
--
-- ⚠️  WARNING: DO NOT RUN THIS ENTIRE MIGRATION AT ONCE ⚠️
-- Run each table section individually in a controlled manner.
-- Verify each table drop completes successfully before proceeding to the next.

-- ============================================================================
-- Drop all ingest_runs tables
-- ============================================================================
DROP TABLE IF EXISTS dz_contributors_ingest_runs;
DROP TABLE IF EXISTS dz_devices_ingest_runs;
DROP TABLE IF EXISTS dz_metros_ingest_runs;
DROP TABLE IF EXISTS dz_links_ingest_runs;
DROP TABLE IF EXISTS dz_users_ingest_runs;
DROP TABLE IF EXISTS geoip_records_ingest_runs;
DROP TABLE IF EXISTS solana_leader_schedule_ingest_runs;
DROP TABLE IF EXISTS solana_vote_accounts_ingest_runs;
DROP TABLE IF EXISTS solana_gossip_nodes_ingest_runs;

