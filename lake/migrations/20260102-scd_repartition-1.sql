-- SCD Table Repartitioning Migration
-- This migration repartitions existing SCD history and ingest_runs tables
-- to use year/month/day partitioning on valid_from and started_at respectively.
--
-- ⚠️  WARNING: DO NOT RUN THIS ENTIRE MIGRATION AT ONCE ⚠️
-- Run each table section individually in a controlled manner.
-- Verify each table migration completes successfully before proceeding to the next.
--
-- IMPORTANT:
-- 1. Backup your data before running this migration
-- 2. Test on a non-production database first
-- 3. Run this during a maintenance window as it requires table swaps
-- 4. This migration will temporarily create _repart and _old tables
--
-- Process for each table:
-- 1) Create a new table with the same schema (empty)
-- 2) Set partitioning on the new table
-- 3) Backfill (rewrite) everything from old table
-- 4) Swap tables (rename old to _old, rename new to original name)

-- ============================================================================
-- dz_contributors_history: partition on valid_from
-- ============================================================================
CREATE TABLE dz_contributors_history__repart AS
SELECT * FROM dz_contributors_history WHERE false;

ALTER TABLE dz_contributors_history__repart SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

INSERT INTO dz_contributors_history__repart
SELECT * FROM dz_contributors_history;

-- Swap (do this in a controlled window)
ALTER TABLE dz_contributors_history RENAME TO dz_contributors_history__old;
ALTER TABLE dz_contributors_history__repart RENAME TO dz_contributors_history;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE dz_contributors_history__old;

-- ============================================================================
-- dz_contributors_ingest_runs: partition on started_at
-- ============================================================================
CREATE TABLE dz_contributors_ingest_runs__repart AS
SELECT * FROM dz_contributors_ingest_runs WHERE false;

ALTER TABLE dz_contributors_ingest_runs__repart SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

INSERT INTO dz_contributors_ingest_runs__repart
SELECT * FROM dz_contributors_ingest_runs;

-- Swap (do this in a controlled window)
ALTER TABLE dz_contributors_ingest_runs RENAME TO dz_contributors_ingest_runs__old;
ALTER TABLE dz_contributors_ingest_runs__repart RENAME TO dz_contributors_ingest_runs;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE dz_contributors_ingest_runs__old;

-- ============================================================================
-- dz_devices_history: partition on valid_from
-- ============================================================================
CREATE TABLE dz_devices_history__repart AS
SELECT * FROM dz_devices_history WHERE false;

ALTER TABLE dz_devices_history__repart SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

INSERT INTO dz_devices_history__repart
SELECT * FROM dz_devices_history;

-- Swap (do this in a controlled window)
ALTER TABLE dz_devices_history RENAME TO dz_devices_history__old;
ALTER TABLE dz_devices_history__repart RENAME TO dz_devices_history;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE dz_devices_history__old;

-- ============================================================================
-- dz_devices_ingest_runs: partition on started_at
-- ============================================================================
CREATE TABLE dz_devices_ingest_runs__repart AS
SELECT * FROM dz_devices_ingest_runs WHERE false;

ALTER TABLE dz_devices_ingest_runs__repart SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

INSERT INTO dz_devices_ingest_runs__repart
SELECT * FROM dz_devices_ingest_runs;

-- Swap (do this in a controlled window)
ALTER TABLE dz_devices_ingest_runs RENAME TO dz_devices_ingest_runs__old;
ALTER TABLE dz_devices_ingest_runs__repart RENAME TO dz_devices_ingest_runs;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE dz_devices_ingest_runs__old;

-- ============================================================================
-- dz_metros_history: partition on valid_from
-- ============================================================================
CREATE TABLE dz_metros_history__repart AS
SELECT * FROM dz_metros_history WHERE false;

ALTER TABLE dz_metros_history__repart SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

INSERT INTO dz_metros_history__repart
SELECT * FROM dz_metros_history;

-- Swap (do this in a controlled window)
ALTER TABLE dz_metros_history RENAME TO dz_metros_history__old;
ALTER TABLE dz_metros_history__repart RENAME TO dz_metros_history;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE dz_metros_history__old;

-- ============================================================================
-- dz_metros_ingest_runs: partition on started_at
-- ============================================================================
CREATE TABLE dz_metros_ingest_runs__repart AS
SELECT * FROM dz_metros_ingest_runs WHERE false;

ALTER TABLE dz_metros_ingest_runs__repart SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

INSERT INTO dz_metros_ingest_runs__repart
SELECT * FROM dz_metros_ingest_runs;

-- Swap (do this in a controlled window)
ALTER TABLE dz_metros_ingest_runs RENAME TO dz_metros_ingest_runs__old;
ALTER TABLE dz_metros_ingest_runs__repart RENAME TO dz_metros_ingest_runs;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE dz_metros_ingest_runs__old;

-- ============================================================================
-- dz_links_history: partition on valid_from
-- ============================================================================
CREATE TABLE dz_links_history__repart AS
SELECT * FROM dz_links_history WHERE false;

ALTER TABLE dz_links_history__repart SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

INSERT INTO dz_links_history__repart
SELECT * FROM dz_links_history;

-- Swap (do this in a controlled window)
ALTER TABLE dz_links_history RENAME TO dz_links_history__old;
ALTER TABLE dz_links_history__repart RENAME TO dz_links_history;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE dz_links_history__old;

-- ============================================================================
-- dz_links_ingest_runs: partition on started_at
-- ============================================================================
CREATE TABLE dz_links_ingest_runs__repart AS
SELECT * FROM dz_links_ingest_runs WHERE false;

ALTER TABLE dz_links_ingest_runs__repart SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

INSERT INTO dz_links_ingest_runs__repart
SELECT * FROM dz_links_ingest_runs;

-- Swap (do this in a controlled window)
ALTER TABLE dz_links_ingest_runs RENAME TO dz_links_ingest_runs__old;
ALTER TABLE dz_links_ingest_runs__repart RENAME TO dz_links_ingest_runs;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE dz_links_ingest_runs__old;

-- ============================================================================
-- dz_users_history: partition on valid_from
-- ============================================================================
CREATE TABLE dz_users_history__repart AS
SELECT * FROM dz_users_history WHERE false;

ALTER TABLE dz_users_history__repart SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

INSERT INTO dz_users_history__repart
SELECT * FROM dz_users_history;

-- Swap (do this in a controlled window)
ALTER TABLE dz_users_history RENAME TO dz_users_history__old;
ALTER TABLE dz_users_history__repart RENAME TO dz_users_history;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE dz_users_history__old;

-- ============================================================================
-- dz_users_ingest_runs: partition on started_at
-- ============================================================================
CREATE TABLE dz_users_ingest_runs__repart AS
SELECT * FROM dz_users_ingest_runs WHERE false;

ALTER TABLE dz_users_ingest_runs__repart SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

INSERT INTO dz_users_ingest_runs__repart
SELECT * FROM dz_users_ingest_runs;

-- Swap (do this in a controlled window)
ALTER TABLE dz_users_ingest_runs RENAME TO dz_users_ingest_runs__old;
ALTER TABLE dz_users_ingest_runs__repart RENAME TO dz_users_ingest_runs;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE dz_users_ingest_runs__old;

-- ============================================================================
-- geoip_records_history: partition on valid_from
-- ============================================================================
CREATE TABLE geoip_records_history__repart AS
SELECT * FROM geoip_records_history WHERE false;

ALTER TABLE geoip_records_history__repart SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

INSERT INTO geoip_records_history__repart
SELECT * FROM geoip_records_history;

-- Swap (do this in a controlled window)
ALTER TABLE geoip_records_history RENAME TO geoip_records_history__old;
ALTER TABLE geoip_records_history__repart RENAME TO geoip_records_history;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE geoip_records_history__old;

-- ============================================================================
-- geoip_records_ingest_runs: partition on started_at
-- ============================================================================
CREATE TABLE geoip_records_ingest_runs__repart AS
SELECT * FROM geoip_records_ingest_runs WHERE false;

ALTER TABLE geoip_records_ingest_runs__repart SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

INSERT INTO geoip_records_ingest_runs__repart
SELECT * FROM geoip_records_ingest_runs;

-- Swap (do this in a controlled window)
ALTER TABLE geoip_records_ingest_runs RENAME TO geoip_records_ingest_runs__old;
ALTER TABLE geoip_records_ingest_runs__repart RENAME TO geoip_records_ingest_runs;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE geoip_records_ingest_runs__old;

-- ============================================================================
-- solana_leader_schedule_history: partition on valid_from
-- ============================================================================
CREATE TABLE solana_leader_schedule_history__repart AS
SELECT * FROM solana_leader_schedule_history WHERE false;

ALTER TABLE solana_leader_schedule_history__repart SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

INSERT INTO solana_leader_schedule_history__repart
SELECT * FROM solana_leader_schedule_history;

-- Swap (do this in a controlled window)
ALTER TABLE solana_leader_schedule_history RENAME TO solana_leader_schedule_history__old;
ALTER TABLE solana_leader_schedule_history__repart RENAME TO solana_leader_schedule_history;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE solana_leader_schedule_history__old;

-- ============================================================================
-- solana_leader_schedule_ingest_runs: partition on started_at
-- ============================================================================
CREATE TABLE solana_leader_schedule_ingest_runs__repart AS
SELECT * FROM solana_leader_schedule_ingest_runs WHERE false;

ALTER TABLE solana_leader_schedule_ingest_runs__repart SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

INSERT INTO solana_leader_schedule_ingest_runs__repart
SELECT * FROM solana_leader_schedule_ingest_runs;

-- Swap (do this in a controlled window)
ALTER TABLE solana_leader_schedule_ingest_runs RENAME TO solana_leader_schedule_ingest_runs__old;
ALTER TABLE solana_leader_schedule_ingest_runs__repart RENAME TO solana_leader_schedule_ingest_runs;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE solana_leader_schedule_ingest_runs__old;

-- ============================================================================
-- solana_vote_accounts_history: partition on valid_from
-- ============================================================================
CREATE TABLE solana_vote_accounts_history__repart AS
SELECT * FROM solana_vote_accounts_history WHERE false;

ALTER TABLE solana_vote_accounts_history__repart SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

INSERT INTO solana_vote_accounts_history__repart
SELECT * FROM solana_vote_accounts_history;

-- Swap (do this in a controlled window)
ALTER TABLE solana_vote_accounts_history RENAME TO solana_vote_accounts_history__old;
ALTER TABLE solana_vote_accounts_history__repart RENAME TO solana_vote_accounts_history;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE solana_vote_accounts_history__old;

-- ============================================================================
-- solana_vote_accounts_ingest_runs: partition on started_at
-- ============================================================================
CREATE TABLE solana_vote_accounts_ingest_runs__repart AS
SELECT * FROM solana_vote_accounts_ingest_runs WHERE false;

ALTER TABLE solana_vote_accounts_ingest_runs__repart SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

INSERT INTO solana_vote_accounts_ingest_runs__repart
SELECT * FROM solana_vote_accounts_ingest_runs;

-- Swap (do this in a controlled window)
ALTER TABLE solana_vote_accounts_ingest_runs RENAME TO solana_vote_accounts_ingest_runs__old;
ALTER TABLE solana_vote_accounts_ingest_runs__repart RENAME TO solana_vote_accounts_ingest_runs;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE solana_vote_accounts_ingest_runs__old;

-- ============================================================================
-- solana_gossip_nodes_history: partition on valid_from
-- ============================================================================
CREATE TABLE solana_gossip_nodes_history__repart AS
SELECT * FROM solana_gossip_nodes_history WHERE false;

ALTER TABLE solana_gossip_nodes_history__repart SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

INSERT INTO solana_gossip_nodes_history__repart
SELECT * FROM solana_gossip_nodes_history;

-- Swap (do this in a controlled window)
ALTER TABLE solana_gossip_nodes_history RENAME TO solana_gossip_nodes_history__old;
ALTER TABLE solana_gossip_nodes_history__repart RENAME TO solana_gossip_nodes_history;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE solana_gossip_nodes_history__old;

-- ============================================================================
-- solana_gossip_nodes_ingest_runs: partition on started_at
-- ============================================================================
CREATE TABLE solana_gossip_nodes_ingest_runs__repart AS
SELECT * FROM solana_gossip_nodes_ingest_runs WHERE false;

ALTER TABLE solana_gossip_nodes_ingest_runs__repart SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

INSERT INTO solana_gossip_nodes_ingest_runs__repart
SELECT * FROM solana_gossip_nodes_ingest_runs;

-- Swap (do this in a controlled window)
ALTER TABLE solana_gossip_nodes_ingest_runs RENAME TO solana_gossip_nodes_ingest_runs__old;
ALTER TABLE solana_gossip_nodes_ingest_runs__repart RENAME TO solana_gossip_nodes_ingest_runs;

-- (Optional) After verifying the migration worked, you can drop the old table:
-- DROP TABLE solana_gossip_nodes_ingest_runs__old;

