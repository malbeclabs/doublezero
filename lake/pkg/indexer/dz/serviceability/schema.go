package dzsvc

import (
	schematypes "github.com/malbeclabs/doublezero/lake/pkg/indexer/schema"
)

var Schema = &schematypes.Schema{
	Name: "doublezero-serviceability",
	Description: `
Use for DZ network structure and state:
- contributors, devices (DZDs), links, metros, users
- topology, ownership, and operational status
- SCD2 history tables can be used to infer historical activity. Do not report initial ingestion of data as user activity.

Default starting point for questions about the DZ network unless the question is explicitly about performance metrics.

SCD2 TABLE STRUCTURE (CRITICAL):
- All tables in this schema use SCD2 (Slowly Changing Dimension Type 2) pattern:
- {table}_current: One row per primary key with the most recent version. Contains: primary key columns, payload columns, as_of_ts (timestamp of snapshot), row_hash (hash for change detection).
- {table}_history: Append-only historical versions with validity windows. Contains: primary key columns, payload columns, valid_from (timestamp), valid_to (timestamp, NULL for current), row_hash, op (I|U|D), run_id (optional).
- Always query {table}_current for current state. Query {table}_history only for historical analysis or point-in-time queries.
- All joins should use {table}_current tables unless explicitly doing historical analysis.

INTERNAL QA OWNER:
- DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan is an internal QA owner_pk; its activity may reflect testing and must not be used alone to infer user growth or churn. Distinguish it from other users in your responses.

PRIMARY KEY SEMANTICS (CRITICAL):
- pk = primary key (always).
- Stores a public key value, but its semantics are relational.
- Use pk for uniqueness, identity, and joins.
- Only {table}_pk → table.pk defines valid joins.
- Other *_pk (e.g. owner_pk) are public keys, non-unique, and not primary keys unless explicitly stated.
`,
	Tables: []schematypes.TableInfo{
		{
			Name:        "dz_contributors",
			Description: "DZ contributors (operators of devices and links) (SCD2). STRUCTURE: dz_contributors_current (one row per pk with latest version + as_of_ts, row_hash) and dz_contributors_history (historical versions with valid_from, valid_to, op, run_id). USAGE: Always query dz_contributors_current for current state. Joins: dz_devices_current.contributor_pk = dz_contributors_current.pk; dz_links_current.contributor_pk = dz_contributors_current.pk.",
			Columns: []schematypes.ColumnInfo{
				{Name: "as_of_ts", Type: "TIMESTAMP", Description: "Timestamp of the snapshot that produced this row (SCD2, in _current table only)"},
				{Name: "row_hash", Type: "VARCHAR", Description: "Hash of payload columns for change detection (SCD2, in _current table only)"},
				{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
				{Name: "code", Type: "VARCHAR", Description: "Contributor code (short human-readable identifier)"},
				{Name: "name", Type: "VARCHAR", Description: "Contributor name (full human-readable name)"},
			},
		},
		{
			Name:        "dz_devices",
			Description: "DZ network devices (DZDs) (SCD2). STRUCTURE: dz_devices_current (one row per pk with latest version + as_of_ts, row_hash) and dz_devices_history (historical versions with valid_from, valid_to, op, run_id). USAGE: Always query dz_devices_current for current state. Joins: dz_devices_current.metro_pk = dz_metros_current.pk; dz_devices_current.contributor_pk = dz_contributors_current.pk. Link endpoints: dz_links_current.side_a_pk = dz_devices_current.pk; dz_links_current.side_z_pk = dz_devices_current.pk.",
			Columns: []schematypes.ColumnInfo{
				{Name: "as_of_ts", Type: "TIMESTAMP", Description: "Timestamp of the snapshot that produced this row (SCD2, in _current table only)"},
				{Name: "row_hash", Type: "VARCHAR", Description: "Hash of payload columns for change detection (SCD2, in _current table only)"},
				{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
				{Name: "status", Type: "VARCHAR", Description: "pending, activated, suspended, deleted, rejected, soft-drained, hard-drained"},
				{Name: "device_type", Type: "VARCHAR", Description: "hybrid, transit, edge"},
				{Name: "code", Type: "VARCHAR", Description: "Device code (e.g. la2-dz01, ny5-dz01)"},
				{Name: "public_ip", Type: "VARCHAR", Description: "Public IP address"},
				{Name: "contributor_pk", Type: "VARCHAR", Description: "Foreign key → dz_contributors_current.pk"},
				{Name: "metro_pk", Type: "VARCHAR", Description: "Foreign key → dz_metros_current.pk"},
			},
		},
		{
			Name:        "dz_metros",
			Description: "Metro areas (exchanges) (SCD2). STRUCTURE: dz_metros_current (one row per pk with latest version + as_of_ts, row_hash) and dz_metros_history (historical versions with valid_from, valid_to, op, run_id). USAGE: Always query dz_metros_current for current state. Join: dz_devices_current.metro_pk = dz_metros_current.pk. Columns: pk, code, name, longitude, latitude. No country column.",
			Columns: []schematypes.ColumnInfo{
				{Name: "as_of_ts", Type: "TIMESTAMP", Description: "Timestamp of the snapshot that produced this row (SCD2, in _current table only)"},
				{Name: "row_hash", Type: "VARCHAR", Description: "Hash of payload columns for change detection (SCD2, in _current table only)"},
				{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
				{Name: "code", Type: "VARCHAR", Description: "Metro code (e.g. nyc, lon, fra)"},
				{Name: "name", Type: "VARCHAR", Description: "Metro name (e.g. New York, London, Frankfurt)"},
				{Name: "longitude", Type: "DOUBLE", Description: "Longitude"},
				{Name: "latitude", Type: "DOUBLE", Description: "Latitude"},
			},
		},
		{
			Name:        "dz_links",
			Description: "Links connecting two devices (SCD2). STRUCTURE: dz_links_current (one row per pk with latest version + as_of_ts, row_hash) and dz_links_history (historical versions with valid_from, valid_to, op, run_id). USAGE: Always query dz_links_current for current state. Joins: dz_links_current.side_a_pk = dz_devices_current.pk; dz_links_current.side_z_pk = dz_devices_current.pk; dz_links_current.contributor_pk = dz_contributors_current.pk. link_type: WAN (inter-metro) or DZX (intra-metro). For inter-metro analysis, join devices as endpoints and filter da.metro_pk != dz.metro_pk.",
			Columns: []schematypes.ColumnInfo{
				{Name: "as_of_ts", Type: "TIMESTAMP", Description: "Timestamp of the snapshot that produced this row (SCD2, in _current table only)"},
				{Name: "row_hash", Type: "VARCHAR", Description: "Hash of payload columns for change detection (SCD2, in _current table only)"},
				{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
				{Name: "status", Type: "VARCHAR", Description: "pending, activated, suspended, deleted, rejected, requested, hard-drained, soft-drained. activated means the link is operational and available for traffic."},
				{Name: "code", Type: "VARCHAR", Description: "Link code (e.g. la2-dz01:ny5-dz01)"},
				{Name: "tunnel_net", Type: "VARCHAR", Description: "Tunnel network CIDR (e.g. 172.16.0.0/31)"},
				{Name: "contributor_pk", Type: "VARCHAR", Description: "Foreign key → dz_contributors_current.pk"},
				{Name: "side_a_pk", Type: "VARCHAR", Description: "Foreign key → dz_devices_current.pk"},
				{Name: "side_z_pk", Type: "VARCHAR", Description: "Foreign key → dz_devices_current.pk"},
				{Name: "side_a_iface_name", Type: "VARCHAR", Description: "Interface name on side A"},
				{Name: "side_z_iface_name", Type: "VARCHAR", Description: "Interface name on side Z"},
				{Name: "link_type", Type: "VARCHAR", Description: "WAN or DZX"},
				{Name: "committed_rtt_ns", Type: "BIGINT", Description: "Committed RTT (nanoseconds)"},
				{Name: "committed_jitter_ns", Type: "BIGINT", Description: "Committed jitter (nanoseconds)"},
				{Name: "bandwidth_bps", Type: "BIGINT", Description: "Link capacity in bits per second"},
				{Name: "isis_delay_override_ns", Type: "BIGINT", Description: "IS-IS delay metric override (nanoseconds). Interpretation rule: isis_delay_override_ns = 1000000000 means the link is soft-drained (drain signal)."},
			},
		},
		{
			Name:        "dz_users",
			Description: "DZ users connected via devices (SCD2). Also referred to as user connections or sessions; a form of activity when viewed historically. STRUCTURE: dz_users_current (one row per pk with latest version + as_of_ts, row_hash) and dz_users_history (historical versions with valid_from, valid_to, op, run_id). USAGE: Always query dz_users_current for current state. Join: dz_users_current.device_pk = dz_devices_current.pk. Some users map to Solana gossip nodes via dz_users_current.dz_ip = solana_gossip_nodes_current.gossip_ip.",
			Columns: []schematypes.ColumnInfo{
				{Name: "as_of_ts", Type: "TIMESTAMP", Description: "Timestamp of the snapshot that produced this row (SCD2, in _current table only)"},
				{Name: "row_hash", Type: "VARCHAR", Description: "Hash of payload columns for change detection (SCD2, in _current table only)"},
				{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
				{Name: "owner_pk", Type: "VARCHAR", Description: "Owner public key"},
				{Name: "status", Type: "VARCHAR", Description: "pending, activated, suspended, deleted, rejected, pending_ban, banned, updating"},
				{Name: "kind", Type: "VARCHAR", Description: "Connection type: ibrl (IBRL), ibrl_with_allocated_ip (IBRL with allocated IP), edge_filtering (Edge Filtering), multicast (Multicast)"},
				{Name: "client_ip", Type: "VARCHAR", Description: "Client IP address"},
				{Name: "dz_ip", Type: "VARCHAR", Description: "DoubleZero IP address"},
				{Name: "device_pk", Type: "VARCHAR", Description: "Foreign key → dz_devices_current.pk"},
				{Name: "tunnel_id", Type: "INTEGER", Description: "Tunnel identifier (u16)"},
			},
		},
	},
}
