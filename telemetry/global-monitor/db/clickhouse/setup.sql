-- One-time manual setup: create the database and user before running the application.
CREATE DATABASE IF NOT EXISTS global_monitor;
CREATE USER IF NOT EXISTS global_monitor IDENTIFIED BY 'changeme';
GRANT SELECT, INSERT, CREATE TABLE, DROP TABLE, ALTER TABLE ON global_monitor.* TO global_monitor;
