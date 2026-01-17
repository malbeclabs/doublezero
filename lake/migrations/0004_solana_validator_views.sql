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
-- Returns the latest stake/commission values (not values at connection time)
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
),
first_connections AS (
    -- Get first connection time per validator (GROUP BY only immutable identifiers)
    SELECT
        vote_pubkey,
        node_pubkey,
        MIN(connected_ts) AS first_connected_ts,
        MAX(connected_ts) AS last_connected_ts
    FROM connection_events
    GROUP BY vote_pubkey, node_pubkey
),
latest_values AS (
    -- Get latest stake/commission values per validator using row_number
    SELECT
        vote_pubkey,
        node_pubkey,
        owner_pubkey,
        dz_ip,
        activated_stake_lamports,
        commission_percentage,
        ROW_NUMBER() OVER (PARTITION BY vote_pubkey, node_pubkey ORDER BY connected_ts DESC) AS rn
    FROM connection_events
)
SELECT
    fc.vote_pubkey,
    fc.node_pubkey,
    lv.owner_pubkey,
    lv.dz_ip,
    lv.activated_stake_lamports,
    lv.commission_percentage,
    fc.first_connected_ts
FROM first_connections fc
JOIN latest_values lv ON fc.vote_pubkey = lv.vote_pubkey AND fc.node_pubkey = lv.node_pubkey AND lv.rn = 1;

-- solana_validators_off_dz_current
-- Shows validators NOT currently connected to DZ, with their geoip location
-- Useful for regional analysis of validators not yet on the network
CREATE OR REPLACE VIEW solana_validators_off_dz_current
AS
SELECT
    va.vote_pubkey,
    va.node_pubkey,
    va.activated_stake_lamports,
    va.commission_percentage,
    va.epoch,
    gn.gossip_ip,
    geo.city,
    geo.region,
    geo.country,
    geo.country_code
FROM solana_vote_accounts_current va
JOIN solana_gossip_nodes_current gn ON va.node_pubkey = gn.pubkey
LEFT JOIN geoip_records_current geo ON gn.gossip_ip = geo.ip
WHERE va.epoch_vote_account = 'true'
  AND va.activated_stake_lamports > 0
  AND va.vote_pubkey NOT IN (SELECT vote_pubkey FROM solana_validators_on_dz_current);

-- solana_validators_disconnections
-- Shows validators that disconnected from DZ (were connected, now aren't)
-- Includes disconnection timestamp and stake at time of disconnection
-- Useful for analyzing churn and understanding stake share decreases
CREATE OR REPLACE VIEW solana_validators_disconnections
AS
WITH connection_events AS (
    -- Find all times when a validator was connected (user, gossip node, and vote account all exist together)
    SELECT
        va.vote_pubkey,
        va.node_pubkey,
        u.owner_pubkey,
        u.dz_ip,
        u.entity_id AS user_entity_id,
        va.activated_stake_lamports,
        va.commission_percentage,
        GREATEST(u.snapshot_ts, gn.snapshot_ts, va.snapshot_ts) AS connected_ts
    FROM dim_dz_users_history u
    JOIN dim_solana_gossip_nodes_history gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip != ''
    JOIN dim_solana_vote_accounts_history va ON gn.pubkey = va.node_pubkey
    WHERE u.is_deleted = 0 AND u.status = 'activated' AND u.dz_ip != ''
      AND gn.is_deleted = 0
      AND va.is_deleted = 0 AND va.epoch_vote_account = 'true' AND va.activated_stake_lamports > 0
),
disconnection_events AS (
    -- Find when users were deleted (disconnected)
    SELECT
        entity_id AS user_entity_id,
        snapshot_ts AS disconnected_ts
    FROM dim_dz_users_history
    WHERE is_deleted = 1
),
validator_disconnections AS (
    -- Join connection events with disconnection events
    -- A validator disconnected if they had a connection and the user was later deleted
    SELECT
        ce.vote_pubkey,
        ce.node_pubkey,
        ce.owner_pubkey,
        ce.dz_ip,
        ce.activated_stake_lamports,
        ce.commission_percentage,
        ce.connected_ts,
        de.disconnected_ts,
        ROW_NUMBER() OVER (PARTITION BY ce.vote_pubkey, ce.node_pubkey ORDER BY de.disconnected_ts DESC) AS rn
    FROM connection_events ce
    JOIN disconnection_events de ON ce.user_entity_id = de.user_entity_id
    WHERE de.disconnected_ts > ce.connected_ts  -- Disconnection must be after connection
)
SELECT
    vote_pubkey,
    node_pubkey,
    owner_pubkey,
    dz_ip,
    activated_stake_lamports,
    commission_percentage,
    connected_ts,
    disconnected_ts
FROM validator_disconnections
WHERE rn = 1  -- Most recent disconnection per validator
  AND vote_pubkey NOT IN (SELECT vote_pubkey FROM solana_validators_on_dz_current);  -- Exclude currently connected
