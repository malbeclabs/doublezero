-- One-time manual setup: create databases and writer users before running migrations.
-- Run this as a ClickHouse admin before deploying any telemetry services.

CREATE DATABASE IF NOT EXISTS telemetry_mainnet_beta;
CREATE USER IF NOT EXISTS telemetry_mainnet_beta IDENTIFIED BY 'changeme';
GRANT SELECT, INSERT, CREATE TABLE, DROP TABLE, ALTER TABLE ON telemetry_mainnet_beta.* TO telemetry_mainnet_beta;

CREATE DATABASE IF NOT EXISTS telemetry_testnet;
CREATE USER IF NOT EXISTS telemetry_testnet IDENTIFIED BY 'changeme';
GRANT SELECT, INSERT, CREATE TABLE, DROP TABLE, ALTER TABLE ON telemetry_testnet.* TO telemetry_testnet;

CREATE DATABASE IF NOT EXISTS telemetry_devnet;
CREATE USER IF NOT EXISTS telemetry_devnet IDENTIFIED BY 'changeme';
GRANT SELECT, INSERT, CREATE TABLE, DROP TABLE, ALTER TABLE ON telemetry_devnet.* TO telemetry_devnet;
