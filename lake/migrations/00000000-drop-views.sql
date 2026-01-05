-- Drop all views

-- Generate with:
-- ```sql
-- SELECT
-- 'DROP VIEW IF EXISTS "' || table_schema || '"."' || table_name || '";' AS sql
-- FROM information_schema.views
-- WHERE table_catalog = 'dzlake'
-- ORDER BY table_schema, table_name;
-- ```

DROP VIEW IF EXISTS "main"."dz_device_connected_validators_now";
DROP VIEW IF EXISTS "main"."dz_device_events";
DROP VIEW IF EXISTS "main"."dz_device_iface_health";
DROP VIEW IF EXISTS "main"."dz_device_traffic";
DROP VIEW IF EXISTS "main"."dz_devices_current_health";
DROP VIEW IF EXISTS "main"."dz_link_events";
DROP VIEW IF EXISTS "main"."dz_link_health";
DROP VIEW IF EXISTS "main"."dz_link_health_core";
DROP VIEW IF EXISTS "main"."dz_link_telemetry";
DROP VIEW IF EXISTS "main"."dz_link_telemetry_with_committed";
DROP VIEW IF EXISTS "main"."dz_link_traffic";
DROP VIEW IF EXISTS "main"."dz_links_current_health";
DROP VIEW IF EXISTS "main"."dz_metro_to_metro_latency";
DROP VIEW IF EXISTS "main"."dz_metros_dim";
DROP VIEW IF EXISTS "main"."dz_network_timeline";
DROP VIEW IF EXISTS "main"."dz_owners_internal";
DROP VIEW IF EXISTS "main"."dz_public_internet_metro_to_metro_latency";
DROP VIEW IF EXISTS "main"."dz_solana_block_production_by_dz_status";
DROP VIEW IF EXISTS "main"."dz_solana_identity_on_dz";
DROP VIEW IF EXISTS "main"."dz_user_device_traffic";
DROP VIEW IF EXISTS "main"."dz_users_active_now";
DROP VIEW IF EXISTS "main"."dz_users_connected_now";
DROP VIEW IF EXISTS "main"."dz_users_identities";
DROP VIEW IF EXISTS "main"."dz_users_internal";
DROP VIEW IF EXISTS "main"."dz_users_intervals";
DROP VIEW IF EXISTS "main"."dz_vs_public_internet_metro_to_metro";
DROP VIEW IF EXISTS "main"."dz_vs_public_internet_metro_to_metro_named";
DROP VIEW IF EXISTS "main"."solana_block_production_current";
DROP VIEW IF EXISTS "main"."solana_block_production_delta";
DROP VIEW IF EXISTS "main"."solana_connected_stake_now_summary";
DROP VIEW IF EXISTS "main"."solana_connected_validator_activity";
DROP VIEW IF EXISTS "main"."solana_gossip_at_ip_now";
DROP VIEW IF EXISTS "main"."solana_gossip_nodes_current_state";
DROP VIEW IF EXISTS "main"."solana_leader_schedule_epoch";
DROP VIEW IF EXISTS "main"."solana_leader_schedule_vs_production_current";
DROP VIEW IF EXISTS "main"."solana_staked_vote_accounts_now";
DROP VIEW IF EXISTS "main"."solana_validator_dz_connection_events";
DROP VIEW IF EXISTS "main"."solana_validator_dz_first_connection_events";
DROP VIEW IF EXISTS "main"."solana_validator_dz_overlaps_strict";
DROP VIEW IF EXISTS "main"."solana_validator_dz_overlaps_windowed";
DROP VIEW IF EXISTS "main"."solana_validator_health";
DROP VIEW IF EXISTS "main"."solana_validators_connected_now";
DROP VIEW IF EXISTS "main"."solana_validators_connected_now_connections";
DROP VIEW IF EXISTS "main"."solana_vote_activity";
DROP VIEW IF EXISTS "main"."solana_vote_activity_current_by_identity";
DROP VIEW IF EXISTS "main"."solana_vote_activity_current_by_vote";
DROP VIEW IF EXISTS "main"."solana_vote_to_identity_current_map";
