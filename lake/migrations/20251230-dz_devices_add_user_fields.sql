-- Add max_users and users_count columns to dz_devices SCD tables
-- These columns track the maximum allowed users and current user count for each device
--
-- IMPORTANT:
-- 1. Backup your data before running this migration
-- 2. Test on a non-production database first
-- 3. This migration adds new columns with default value 0 for existing rows

-- ============================================================================
-- dz_devices: Add max_users and users_count columns
-- ============================================================================

-- Add columns to current table
ALTER TABLE dz_devices_current ADD COLUMN max_users INTEGER DEFAULT 0;
ALTER TABLE dz_devices_current ADD COLUMN users_count INTEGER DEFAULT 0;

-- Add columns to history table
ALTER TABLE dz_devices_history ADD COLUMN max_users INTEGER DEFAULT 0;
ALTER TABLE dz_devices_history ADD COLUMN users_count INTEGER DEFAULT 0;

-- Update existing rows in current table to ensure they have the default value
-- (This is redundant if DEFAULT works, but ensures consistency)
UPDATE dz_devices_current SET max_users = 0 WHERE max_users IS NULL;
UPDATE dz_devices_current SET users_count = 0 WHERE users_count IS NULL;

-- Update existing rows in history table to ensure they have the default value
UPDATE dz_devices_history SET max_users = 0 WHERE max_users IS NULL;
UPDATE dz_devices_history SET users_count = 0 WHERE users_count IS NULL;

