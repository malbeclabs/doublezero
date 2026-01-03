-- Add ipdv_us (IPDV/jitter) column to telemetry latency fact tables
-- IPDV (Inter-Packet Delay Variation) is calculated as the absolute difference
-- between consecutive RTT measurements for the same circuit.
--
-- IMPORTANT:
-- 1. Backup your data before running this migration
-- 2. Test on a non-production database first
-- 3. This migration adds a nullable BIGINT column (NULL for first sample or loss)

-- ============================================================================
-- dz_device_link_latency_samples_raw: Add ipdv_us column
-- ============================================================================

ALTER TABLE dz_device_link_latency_samples_raw ADD COLUMN ipdv_us BIGINT;

-- ============================================================================
-- dz_internet_metro_latency_samples_raw: Add ipdv_us column
-- ============================================================================

ALTER TABLE dz_internet_metro_latency_samples_raw ADD COLUMN ipdv_us BIGINT;

