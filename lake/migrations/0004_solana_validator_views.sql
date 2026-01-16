-- Solana Validator DZ Connection Views
-- These views simplify queries about when validators connected to/disconnected from DZ

-- solana_validators_on_dz_current
-- Shows validators currently connected to DZ with their connection details
-- A validator is "on DZ" when: user (activated, has dz_ip) + gossip node (at that IP) + vote account (for that node) all exist
CREATE OR REPLACE VIEW solana_validators_on_dz_current
AS
SELECT
    va.vote_pubkey,
    va.node_pubkey,
    u.owner_pubkey,
    u.dz_ip,
    u.client_ip,
    u.device_pk,
    va.activated_stake_lamports,
    va.commission_percentage,
    va.epoch,
    -- Connection timestamp is the latest of when each component appeared
    GREATEST(u.snapshot_ts, gn.snapshot_ts, va.snapshot_ts) AS connected_ts
FROM dz_users_current u
JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
WHERE u.status = 'activated'
  AND u.dz_ip != ''
  AND va.epoch_vote_account = 'true'
  AND va.activated_stake_lamports > 0;

-- solana_validators_on_dz_connections
-- Shows all validator connection events (when validators first connected to DZ)
-- Uses history tables to find the earliest time each validator was connected
CREATE OR REPLACE VIEW solana_validators_on_dz_connections
AS
WITH connection_events AS (
    -- Find all times when a validator was connected (user, gossip node, and vote account all exist together)
    -- The connection timestamp is the maximum of the three snapshot_ts values
    SELECT
        va.vote_pubkey,
        va.node_pubkey,
        u.owner_pubkey,
        u.dz_ip,
        va.activated_stake_lamports,
        va.commission_percentage,
        GREATEST(u.snapshot_ts, gn.snapshot_ts, va.snapshot_ts) AS connected_ts
    FROM dim_dz_users_history u
    JOIN dim_solana_gossip_nodes_history gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip != ''
    JOIN dim_solana_vote_accounts_history va ON gn.pubkey = va.node_pubkey
    WHERE u.is_deleted = 0 AND u.status = 'activated' AND u.dz_ip != ''
      AND gn.is_deleted = 0
      AND va.is_deleted = 0 AND va.epoch_vote_account = 'true' AND va.activated_stake_lamports > 0
)
SELECT
    vote_pubkey,
    node_pubkey,
    owner_pubkey,
    dz_ip,
    activated_stake_lamports,
    commission_percentage,
    MIN(connected_ts) AS first_connected_ts
FROM connection_events
GROUP BY vote_pubkey, node_pubkey, owner_pubkey, dz_ip, activated_stake_lamports, commission_percentage;
