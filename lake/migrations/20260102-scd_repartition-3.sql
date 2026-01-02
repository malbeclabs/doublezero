-- SCD Table Cleanup Migration
-- This migration drops all temporary tables created during the repartitioning process.
--
-- This migration should be run AFTER:
-- 1. Migration -1 (repartitioning) has completed successfully
-- 2. Migration -2 (rewrite to clean storage names) has completed successfully
-- 3. You have verified that all data is correct and the new tables are working properly
--
-- IMPORTANT:
-- 1. Verify all data is correct before running this cleanup
-- 2. This migration is irreversible - make sure you have backups
-- 3. Test on a non-production database first
-- 4. Run this during a maintenance window
--
-- ⚠️  WARNING: DO NOT RUN THIS ENTIRE MIGRATION AT ONCE ⚠️
-- Run each table section individually in a controlled manner.
-- Verify each table drop completes successfully before proceeding to the next.

-- ============================================================================
-- Drop __old tables (from migration -1)
-- ============================================================================
DROP TABLE IF EXISTS dz_contributors_history__old;
DROP TABLE IF EXISTS dz_contributors_ingest_runs__old;
DROP TABLE IF EXISTS dz_devices_history__old;
DROP TABLE IF EXISTS dz_devices_ingest_runs__old;
DROP TABLE IF EXISTS dz_metros_history__old;
DROP TABLE IF EXISTS dz_metros_ingest_runs__old;
DROP TABLE IF EXISTS dz_links_history__old;
DROP TABLE IF EXISTS dz_links_ingest_runs__old;
DROP TABLE IF EXISTS dz_users_history__old;
DROP TABLE IF EXISTS dz_users_ingest_runs__old;
DROP TABLE IF EXISTS geoip_records_history__old;
DROP TABLE IF EXISTS geoip_records_ingest_runs__old;
DROP TABLE IF EXISTS solana_leader_schedule_history__old;
DROP TABLE IF EXISTS solana_leader_schedule_ingest_runs__old;
DROP TABLE IF EXISTS solana_vote_accounts_history__old;
DROP TABLE IF EXISTS solana_vote_accounts_ingest_runs__old;
DROP TABLE IF EXISTS solana_gossip_nodes_history__old;
DROP TABLE IF EXISTS solana_gossip_nodes_ingest_runs__old;

-- ============================================================================
-- Drop __repart_physical tables (from migration -2)
-- ============================================================================
DROP TABLE IF EXISTS dz_contributors_history__repart_physical;
DROP TABLE IF EXISTS dz_contributors_ingest_runs__repart_physical;
DROP TABLE IF EXISTS dz_devices_history__repart_physical;
DROP TABLE IF EXISTS dz_devices_ingest_runs__repart_physical;
DROP TABLE IF EXISTS dz_metros_history__repart_physical;
DROP TABLE IF EXISTS dz_metros_ingest_runs__repart_physical;
DROP TABLE IF EXISTS dz_links_history__repart_physical;
DROP TABLE IF EXISTS dz_links_ingest_runs__repart_physical;
DROP TABLE IF EXISTS dz_users_history__repart_physical;
DROP TABLE IF EXISTS dz_users_ingest_runs__repart_physical;
DROP TABLE IF EXISTS geoip_records_history__repart_physical;
DROP TABLE IF EXISTS geoip_records_ingest_runs__repart_physical;
DROP TABLE IF EXISTS solana_leader_schedule_history__repart_physical;
DROP TABLE IF EXISTS solana_leader_schedule_ingest_runs__repart_physical;
DROP TABLE IF EXISTS solana_vote_accounts_history__repart_physical;
DROP TABLE IF EXISTS solana_vote_accounts_ingest_runs__repart_physical;
DROP TABLE IF EXISTS solana_gossip_nodes_history__repart_physical;
DROP TABLE IF EXISTS solana_gossip_nodes_ingest_runs__repart_physical;

