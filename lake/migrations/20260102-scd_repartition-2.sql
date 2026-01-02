-- SCD Table Rewrite Migration
-- This migration cleans up the __repart suffixes in physical table/directory names in storage.
--
-- NOTE: The tables in the catalog are already represented correctly (after migration -1).
-- This migration is specifically about cleaning up the physical storage layer:
-- - Physical table/directory names in S3 currently have __repart suffixes
-- - We need to rewrite the data to use the original table names in storage
-- - This ensures the S3 prefix/directory structure matches the catalog table names
--
-- ⚠️  WARNING: DO NOT RUN THIS ENTIRE MIGRATION AT ONCE ⚠️
-- Run each table section individually in a controlled manner.
-- Verify each table migration completes successfully before proceeding to the next.
--
-- IMPORTANT:
-- 1. This migration should be run AFTER the repartitioning migration (-1)
-- 2. Backup your data before running this migration
-- 3. Test on a non-production database first
-- 4. Run this during a maintenance window as it requires table swaps
-- 5. Stop writers / pause ingestion before running this migration
--
-- Process for each table:
-- 1) Move the current table out of the way (this one still writes under __repart in storage)
-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix to match catalog)
-- 3) Apply partitioning to the new naked table
-- 4) Backfill (rewrite) all data into the new table (writes to correct storage path)
-- 5) (Optional) sanity checks
-- 6) (Optional) keep old table around for rollback; drop later when confident

-- ============================================================================
-- dz_contributors_history: partition on valid_from
-- ============================================================================
-- 0) (Optional but recommended) stop writers / pause ingestion here

-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE dz_contributors_history RENAME TO dz_contributors_history__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE dz_contributors_history AS
SELECT * FROM dz_contributors_history__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE dz_contributors_history
SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

-- 4) Backfill (rewrite) all data into the new table (writes to .../dz_contributors_history/...)
INSERT INTO dz_contributors_history
SELECT * FROM dz_contributors_history__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM dz_contributors_history;
-- SELECT count(*) FROM dz_contributors_history__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE dz_contributors_history__repart_physical;

-- ============================================================================
-- dz_contributors_ingest_runs: partition on started_at
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE dz_contributors_ingest_runs RENAME TO dz_contributors_ingest_runs__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE dz_contributors_ingest_runs AS
SELECT * FROM dz_contributors_ingest_runs__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE dz_contributors_ingest_runs
SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

-- 4) Backfill (rewrite) all data into the new table (writes to .../dz_contributors_ingest_runs/...)
INSERT INTO dz_contributors_ingest_runs
SELECT * FROM dz_contributors_ingest_runs__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM dz_contributors_ingest_runs;
-- SELECT count(*) FROM dz_contributors_ingest_runs__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE dz_contributors_ingest_runs__repart_physical;

-- ============================================================================
-- dz_devices_history: partition on valid_from
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE dz_devices_history RENAME TO dz_devices_history__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE dz_devices_history AS
SELECT * FROM dz_devices_history__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE dz_devices_history
SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

-- 4) Backfill (rewrite) all data into the new table (writes to .../dz_devices_history/...)
INSERT INTO dz_devices_history
SELECT * FROM dz_devices_history__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM dz_devices_history;
-- SELECT count(*) FROM dz_devices_history__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE dz_devices_history__repart_physical;

-- ============================================================================
-- dz_devices_ingest_runs: partition on started_at
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE dz_devices_ingest_runs RENAME TO dz_devices_ingest_runs__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE dz_devices_ingest_runs AS
SELECT * FROM dz_devices_ingest_runs__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE dz_devices_ingest_runs
SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

-- 4) Backfill (rewrite) all data into the new table (writes to .../dz_devices_ingest_runs/...)
INSERT INTO dz_devices_ingest_runs
SELECT * FROM dz_devices_ingest_runs__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM dz_devices_ingest_runs;
-- SELECT count(*) FROM dz_devices_ingest_runs__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE dz_devices_ingest_runs__repart_physical;

-- ============================================================================
-- dz_metros_history: partition on valid_from
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE dz_metros_history RENAME TO dz_metros_history__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE dz_metros_history AS
SELECT * FROM dz_metros_history__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE dz_metros_history
SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

-- 4) Backfill (rewrite) all data into the new table (writes to .../dz_metros_history/...)
INSERT INTO dz_metros_history
SELECT * FROM dz_metros_history__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM dz_metros_history;
-- SELECT count(*) FROM dz_metros_history__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE dz_metros_history__repart_physical;

-- ============================================================================
-- dz_metros_ingest_runs: partition on started_at
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE dz_metros_ingest_runs RENAME TO dz_metros_ingest_runs__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE dz_metros_ingest_runs AS
SELECT * FROM dz_metros_ingest_runs__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE dz_metros_ingest_runs
SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

-- 4) Backfill (rewrite) all data into the new table (writes to .../dz_metros_ingest_runs/...)
INSERT INTO dz_metros_ingest_runs
SELECT * FROM dz_metros_ingest_runs__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM dz_metros_ingest_runs;
-- SELECT count(*) FROM dz_metros_ingest_runs__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE dz_metros_ingest_runs__repart_physical;

-- ============================================================================
-- dz_links_history: partition on valid_from
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE dz_links_history RENAME TO dz_links_history__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE dz_links_history AS
SELECT * FROM dz_links_history__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE dz_links_history
SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

-- 4) Backfill (rewrite) all data into the new table (writes to .../dz_links_history/...)
INSERT INTO dz_links_history
SELECT * FROM dz_links_history__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM dz_links_history;
-- SELECT count(*) FROM dz_links_history__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE dz_links_history__repart_physical;

-- ============================================================================
-- dz_links_ingest_runs: partition on started_at
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE dz_links_ingest_runs RENAME TO dz_links_ingest_runs__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE dz_links_ingest_runs AS
SELECT * FROM dz_links_ingest_runs__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE dz_links_ingest_runs
SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

-- 4) Backfill (rewrite) all data into the new table (writes to .../dz_links_ingest_runs/...)
INSERT INTO dz_links_ingest_runs
SELECT * FROM dz_links_ingest_runs__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM dz_links_ingest_runs;
-- SELECT count(*) FROM dz_links_ingest_runs__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE dz_links_ingest_runs__repart_physical;

-- ============================================================================
-- dz_users_history: partition on valid_from
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE dz_users_history RENAME TO dz_users_history__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE dz_users_history AS
SELECT * FROM dz_users_history__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE dz_users_history
SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

-- 4) Backfill (rewrite) all data into the new table (writes to .../dz_users_history/...)
INSERT INTO dz_users_history
SELECT * FROM dz_users_history__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM dz_users_history;
-- SELECT count(*) FROM dz_users_history__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE dz_users_history__repart_physical;

-- ============================================================================
-- dz_users_ingest_runs: partition on started_at
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE dz_users_ingest_runs RENAME TO dz_users_ingest_runs__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE dz_users_ingest_runs AS
SELECT * FROM dz_users_ingest_runs__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE dz_users_ingest_runs
SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

-- 4) Backfill (rewrite) all data into the new table (writes to .../dz_users_ingest_runs/...)
INSERT INTO dz_users_ingest_runs
SELECT * FROM dz_users_ingest_runs__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM dz_users_ingest_runs;
-- SELECT count(*) FROM dz_users_ingest_runs__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE dz_users_ingest_runs__repart_physical;

-- ============================================================================
-- geoip_records_history: partition on valid_from
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE geoip_records_history RENAME TO geoip_records_history__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE geoip_records_history AS
SELECT * FROM geoip_records_history__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE geoip_records_history
SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

-- 4) Backfill (rewrite) all data into the new table (writes to .../geoip_records_history/...)
INSERT INTO geoip_records_history
SELECT * FROM geoip_records_history__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM geoip_records_history;
-- SELECT count(*) FROM geoip_records_history__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE geoip_records_history__repart_physical;

-- ============================================================================
-- geoip_records_ingest_runs: partition on started_at
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE geoip_records_ingest_runs RENAME TO geoip_records_ingest_runs__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE geoip_records_ingest_runs AS
SELECT * FROM geoip_records_ingest_runs__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE geoip_records_ingest_runs
SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

-- 4) Backfill (rewrite) all data into the new table (writes to .../geoip_records_ingest_runs/...)
INSERT INTO geoip_records_ingest_runs
SELECT * FROM geoip_records_ingest_runs__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM geoip_records_ingest_runs;
-- SELECT count(*) FROM geoip_records_ingest_runs__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE geoip_records_ingest_runs__repart_physical;

-- ============================================================================
-- solana_leader_schedule_history: partition on valid_from
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE solana_leader_schedule_history RENAME TO solana_leader_schedule_history__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE solana_leader_schedule_history AS
SELECT * FROM solana_leader_schedule_history__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE solana_leader_schedule_history
SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

-- 4) Backfill (rewrite) all data into the new table (writes to .../solana_leader_schedule_history/...)
INSERT INTO solana_leader_schedule_history
SELECT * FROM solana_leader_schedule_history__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM solana_leader_schedule_history;
-- SELECT count(*) FROM solana_leader_schedule_history__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE solana_leader_schedule_history__repart_physical;

-- ============================================================================
-- solana_leader_schedule_ingest_runs: partition on started_at
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE solana_leader_schedule_ingest_runs RENAME TO solana_leader_schedule_ingest_runs__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE solana_leader_schedule_ingest_runs AS
SELECT * FROM solana_leader_schedule_ingest_runs__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE solana_leader_schedule_ingest_runs
SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

-- 4) Backfill (rewrite) all data into the new table (writes to .../solana_leader_schedule_ingest_runs/...)
INSERT INTO solana_leader_schedule_ingest_runs
SELECT * FROM solana_leader_schedule_ingest_runs__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM solana_leader_schedule_ingest_runs;
-- SELECT count(*) FROM solana_leader_schedule_ingest_runs__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE solana_leader_schedule_ingest_runs__repart_physical;

-- ============================================================================
-- solana_vote_accounts_history: partition on valid_from
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE solana_vote_accounts_history RENAME TO solana_vote_accounts_history__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE solana_vote_accounts_history AS
SELECT * FROM solana_vote_accounts_history__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE solana_vote_accounts_history
SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

-- 4) Backfill (rewrite) all data into the new table (writes to .../solana_vote_accounts_history/...)
INSERT INTO solana_vote_accounts_history
SELECT * FROM solana_vote_accounts_history__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM solana_vote_accounts_history;
-- SELECT count(*) FROM solana_vote_accounts_history__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE solana_vote_accounts_history__repart_physical;

-- ============================================================================
-- solana_vote_accounts_ingest_runs: partition on started_at
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE solana_vote_accounts_ingest_runs RENAME TO solana_vote_accounts_ingest_runs__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE solana_vote_accounts_ingest_runs AS
SELECT * FROM solana_vote_accounts_ingest_runs__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE solana_vote_accounts_ingest_runs
SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

-- 4) Backfill (rewrite) all data into the new table (writes to .../solana_vote_accounts_ingest_runs/...)
INSERT INTO solana_vote_accounts_ingest_runs
SELECT * FROM solana_vote_accounts_ingest_runs__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM solana_vote_accounts_ingest_runs;
-- SELECT count(*) FROM solana_vote_accounts_ingest_runs__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE solana_vote_accounts_ingest_runs__repart_physical;

-- ============================================================================
-- solana_gossip_nodes_history: partition on valid_from
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE solana_gossip_nodes_history RENAME TO solana_gossip_nodes_history__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE solana_gossip_nodes_history AS
SELECT * FROM solana_gossip_nodes_history__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE solana_gossip_nodes_history
SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from));

-- 4) Backfill (rewrite) all data into the new table (writes to .../solana_gossip_nodes_history/...)
INSERT INTO solana_gossip_nodes_history
SELECT * FROM solana_gossip_nodes_history__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM solana_gossip_nodes_history;
-- SELECT count(*) FROM solana_gossip_nodes_history__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE solana_gossip_nodes_history__repart_physical;

-- ============================================================================
-- solana_gossip_nodes_ingest_runs: partition on started_at
-- ============================================================================
-- 1) Move the current table out of the way (this one still writes under __repart in physical storage)
ALTER TABLE solana_gossip_nodes_ingest_runs RENAME TO solana_gossip_nodes_ingest_runs__repart_physical;

-- 2) Create a brand-new table with the *naked* name (this sets the S3 prefix/directory to match catalog)
CREATE TABLE solana_gossip_nodes_ingest_runs AS
SELECT * FROM solana_gossip_nodes_ingest_runs__repart_physical WHERE false;

-- 3) Apply partitioning to the new naked table
ALTER TABLE solana_gossip_nodes_ingest_runs
SET PARTITIONED BY (year(started_at), month(started_at), day(started_at));

-- 4) Backfill (rewrite) all data into the new table (writes to .../solana_gossip_nodes_ingest_runs/...)
INSERT INTO solana_gossip_nodes_ingest_runs
SELECT * FROM solana_gossip_nodes_ingest_runs__repart_physical;

-- 5) (Optional) sanity checks
-- SELECT count(*) FROM solana_gossip_nodes_ingest_runs;
-- SELECT count(*) FROM solana_gossip_nodes_ingest_runs__repart_physical;

-- 6) (Optional) keep old table around for rollback; drop later when confident
-- DROP TABLE solana_gossip_nodes_ingest_runs__repart_physical;

