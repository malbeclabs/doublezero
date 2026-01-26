-- +goose Up

-- Solana Validator DZ Connection Views
-- These views simplify queries about when validators connected to/disconnected from DZ

-- +goose StatementBegin
-- solana_validators_on_dz_current
-- Shows validators currently connected to DZ with their connection details
-- A validator is "on DZ" when: user (activated, has dz_ip) + gossip node (at that IP) + vote account (for that node) all exist
CREATE OR REPLACE VIEW solana_validators_on_dz_current
AS
SELECT
    va.vote_pubkey AS vote_pubkey,
    va.node_pubkey AS node_pubkey,
    u.owner_pubkey AS owner_pubkey,
    u.dz_ip AS dz_ip,
    u.client_ip AS client_ip,
    u.device_pk AS device_pk,
    d.code AS device_code,
    m.code AS device_metro_code,
    m.name AS device_metro_name,
    va.activated_stake_lamports AS activated_stake_lamports,
    va.activated_stake_lamports / 1000000000.0 AS activated_stake_sol,
    va.commission_percentage AS commission_percentage,
    va.epoch AS epoch,
    -- Connection timestamp is the latest of when each component appeared
    GREATEST(u.snapshot_ts, gn.snapshot_ts, va.snapshot_ts) AS connected_ts
FROM dz_users_current u
JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
LEFT JOIN dz_devices_current d ON u.device_pk = d.pk
LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
WHERE u.status = 'activated'
  AND u.dz_ip != ''
  AND va.epoch_vote_account = 'true'
  AND va.activated_stake_lamports > 0;
-- +goose StatementEnd

-- +goose StatementBegin
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
        u.device_pk,
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
        device_pk,
        activated_stake_lamports,
        commission_percentage,
        ROW_NUMBER() OVER (PARTITION BY vote_pubkey, node_pubkey ORDER BY connected_ts DESC) AS rn
    FROM connection_events
)
SELECT
    fc.vote_pubkey AS vote_pubkey,
    fc.node_pubkey AS node_pubkey,
    lv.owner_pubkey AS owner_pubkey,
    lv.dz_ip AS dz_ip,
    lv.device_pk AS device_pk,
    d.code AS device_code,
    m.code AS device_metro_code,
    m.name AS device_metro_name,
    lv.activated_stake_lamports AS activated_stake_lamports,
    lv.activated_stake_lamports / 1000000000.0 AS activated_stake_sol,
    lv.commission_percentage AS commission_percentage,
    fc.first_connected_ts AS first_connected_ts
FROM first_connections fc
JOIN latest_values lv ON fc.vote_pubkey = lv.vote_pubkey AND fc.node_pubkey = lv.node_pubkey AND lv.rn = 1
LEFT JOIN dz_devices_current d ON lv.device_pk = d.pk
LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk;
-- +goose StatementEnd

-- +goose StatementBegin
-- solana_validators_off_dz_current
-- Shows validators NOT currently connected to DZ, with their geoip location
-- Useful for regional analysis of validators not yet on the network
CREATE OR REPLACE VIEW solana_validators_off_dz_current
AS
SELECT
    va.vote_pubkey AS vote_pubkey,
    va.node_pubkey AS node_pubkey,
    va.activated_stake_lamports AS activated_stake_lamports,
    va.activated_stake_lamports / 1000000000.0 AS activated_stake_sol,
    va.commission_percentage AS commission_percentage,
    va.epoch AS epoch,
    gn.gossip_ip AS gossip_ip,
    geo.city AS city,
    geo.region AS region,
    geo.country AS country,
    geo.country_code AS country_code
FROM solana_vote_accounts_current va
JOIN solana_gossip_nodes_current gn ON va.node_pubkey = gn.pubkey
LEFT JOIN geoip_records_current geo ON gn.gossip_ip = geo.ip
WHERE va.epoch_vote_account = 'true'
  AND va.activated_stake_lamports > 0
  AND va.vote_pubkey NOT IN (SELECT vote_pubkey FROM solana_validators_on_dz_current);
-- +goose StatementEnd

-- +goose StatementBegin
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
        u.device_pk,
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
        ce.device_pk,
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
    vd.vote_pubkey AS vote_pubkey,
    vd.node_pubkey AS node_pubkey,
    vd.owner_pubkey AS owner_pubkey,
    vd.dz_ip AS dz_ip,
    vd.device_pk AS device_pk,
    d.code AS device_code,
    m.code AS device_metro_code,
    m.name AS device_metro_name,
    vd.activated_stake_lamports AS activated_stake_lamports,
    vd.activated_stake_lamports / 1000000000.0 AS activated_stake_sol,
    vd.commission_percentage AS commission_percentage,
    vd.connected_ts AS connected_ts,
    vd.disconnected_ts AS disconnected_ts
FROM validator_disconnections vd
LEFT JOIN dz_devices_current d ON vd.device_pk = d.pk
LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
-- Most recent disconnection per validator, excluding currently connected
WHERE vd.rn = 1
  AND vd.vote_pubkey NOT IN (SELECT vote_pubkey FROM solana_validators_on_dz_current);
-- +goose StatementEnd

-- +goose StatementBegin
-- solana_validators_new_connections
-- Shows validators currently on DZ with their first connection timestamp
-- Useful for finding validators that recently joined DZ (filter by first_connected_ts)
-- Example: SELECT * FROM solana_validators_new_connections WHERE first_connected_ts >= now() - INTERVAL 1 DAY
CREATE OR REPLACE VIEW solana_validators_new_connections
AS
SELECT
    curr.vote_pubkey AS vote_pubkey,
    curr.node_pubkey AS node_pubkey,
    curr.owner_pubkey AS owner_pubkey,
    curr.dz_ip AS dz_ip,
    curr.client_ip AS client_ip,
    curr.device_pk AS device_pk,
    curr.device_code AS device_code,
    curr.device_metro_code AS device_metro_code,
    curr.device_metro_name AS device_metro_name,
    curr.activated_stake_lamports AS activated_stake_lamports,
    curr.activated_stake_sol AS activated_stake_sol,
    curr.commission_percentage AS commission_percentage,
    conn.first_connected_ts AS first_connected_ts
FROM solana_validators_on_dz_current curr
JOIN solana_validators_on_dz_connections conn
  ON curr.vote_pubkey = conn.vote_pubkey
  AND curr.node_pubkey = conn.node_pubkey;
-- +goose StatementEnd

-- +goose Down
-- Note: Down migrations would drop views.
-- Since we use CREATE OR REPLACE, re-running up is safe.
