-- +goose Up

-- +goose StatementBegin
ALTER TABLE interface_state
    ADD COLUMN IF NOT EXISTS in_fcs_errors UInt64 AFTER out_discards,
    ADD COLUMN IF NOT EXISTS in_unicast_pkts UInt64 AFTER in_fcs_errors,
    ADD COLUMN IF NOT EXISTS in_multicast_pkts UInt64 AFTER in_unicast_pkts,
    ADD COLUMN IF NOT EXISTS in_broadcast_pkts UInt64 AFTER in_multicast_pkts,
    ADD COLUMN IF NOT EXISTS out_unicast_pkts UInt64 AFTER in_broadcast_pkts,
    ADD COLUMN IF NOT EXISTS out_multicast_pkts UInt64 AFTER out_unicast_pkts,
    ADD COLUMN IF NOT EXISTS out_broadcast_pkts UInt64 AFTER out_multicast_pkts;
-- +goose StatementEnd

-- Recreate the latest view so SELECT * surfaces the new columns.
-- +goose StatementBegin
DROP VIEW IF EXISTS interface_state_latest;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE VIEW IF NOT EXISTS interface_state_latest AS
SELECT *
FROM interface_state
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM interface_state
    GROUP BY device_pubkey
);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP VIEW IF EXISTS interface_state_latest;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE interface_state
    DROP COLUMN IF EXISTS out_broadcast_pkts,
    DROP COLUMN IF EXISTS out_multicast_pkts,
    DROP COLUMN IF EXISTS out_unicast_pkts,
    DROP COLUMN IF EXISTS in_broadcast_pkts,
    DROP COLUMN IF EXISTS in_multicast_pkts,
    DROP COLUMN IF EXISTS in_unicast_pkts,
    DROP COLUMN IF EXISTS in_fcs_errors;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE VIEW IF NOT EXISTS interface_state_latest AS
SELECT *
FROM interface_state
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM interface_state
    GROUP BY device_pubkey
);
-- +goose StatementEnd
