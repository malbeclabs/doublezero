-- Remove users_count column from dz_devices SCD tables
-- This column is no longer needed in the lake schema
--
-- IMPORTANT:
-- 1. Backup your data before running this migration
-- 2. Test on a non-production database first
-- 3. This migration drops the users_count column from both current and history tables

-- ============================================================================
-- dz_devices: Drop users_count column
-- ============================================================================

-- Drop column from current table
ALTER TABLE dz_devices_current DROP COLUMN users_count;

-- Drop column from history table
ALTER TABLE dz_devices_history DROP COLUMN users_count;

