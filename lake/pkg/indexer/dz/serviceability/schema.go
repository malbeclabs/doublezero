package dzsvc

import schematypes "github.com/malbeclabs/doublezero/lake/pkg/indexer/schema"

var Datasets = []schematypes.Dataset{
	{
		Name:        "dz_contributors",
		DatasetType: schematypes.DatasetTypeSCD2,
		Purpose: `
			DZ contributors are responsible for operating devices and tunnels in the DZ network.
			Contains contributor public keys, codes, and names.
			Use for identifying device and link operators, and contributor-level analysis.
		`,
		Tables: []string{"dz_contributors_current", "dz_contributors_history"},
		Description: `
		USAGE:
		- Always query dz_contributors_current for current state.
		- Joins:
			- dz_devices_current.contributor_pk = dz_contributors_current.pk
			- dz_links_current.contributor_pk = dz_contributors_current.pk

		COLUMNS:
		- pk (VARCHAR): Primary key
		- code (VARCHAR): Contributor code (short human-readable identifier)
		- name (VARCHAR): Contributor name (full human-readable name)

		SCD2 DATA STRUCTURE:
		- {table}_current: Current state of the dataset. One row per dataset entity.
		- {table}_history: Append-only historical versions with validity windows.
		- Always query {table}_current for current state. Query {table}_history only for historical analysis or point-in-time queries.
		- All joins should use {table}_current tables unless explicitly doing historical analysis.
		- {table}_current.as_of_ts is the timestamp of the snapshot that produced this row.
		- {table}_current.row_hash is the hash of the payload columns for change detection.
		- {table}_history.valid_from is the timestamp of the start of the validity window.
		- {table}_history.valid_to is the timestamp of the end of the validity window.
		- {table}_history.op is the operation that produced this row (I|U|D).
		- {table}_history.run_id is the identifier of the ingestion run that produced this row.
		`,
	},
	{
		Name:        "dz_devices",
		DatasetType: schematypes.DatasetTypeSCD2,
		Purpose: `
			DZ devices are the hardware switches and routers in the DZ network.
			Contains device public keys, status, type (hybrid/transit/edge), codes, IP addresses, metro and contributor associations.
			Use for topology analysis, network configuration, and device status monitoring.
		`,
		Tables: []string{"dz_devices_current", "dz_devices_history"},
		Description: `
		USAGE:
		- Always query dz_devices_current for current state.
		- Query status to filter by device operational state (pending, activated, suspended, deleted, rejected, soft-drained, hard-drained).
		- Joins:
			- dz_devices_current.metro_pk = dz_metros_current.pk
			- dz_devices_current.contributor_pk = dz_contributors_current.pk
			- dz_links_current.side_a_pk = dz_devices_current.pk
			- dz_links_current.side_z_pk = dz_devices_current.pk

		COLUMNS:
		- pk (VARCHAR): Primary key
		- status (VARCHAR): pending, activated, suspended, deleted, rejected, soft-drained, hard-drained
		- device_type (VARCHAR): hybrid, transit, edge
		- code (VARCHAR): Device code (e.g. la2-dz01, ny5-dz01)
		- public_ip (VARCHAR): Public IP address
		- contributor_pk (VARCHAR): Foreign key → dz_contributors_current.pk
		- metro_pk (VARCHAR): Foreign key → dz_metros_current.pk
		- max_users (INTEGER): Maximum number of users allowed on the device

		SCD2 DATA STRUCTURE:
		- {table}_current: Current state of the dataset. One row per dataset entity.
		- {table}_history: Append-only historical versions with validity windows.
		- Always query {table}_current for current state. Query {table}_history only for historical analysis or point-in-time queries.
		- All joins should use {table}_current tables unless explicitly doing historical analysis.
		- {table}_current.as_of_ts is the timestamp of the snapshot that produced this row.
		- {table}_current.row_hash is the hash of the payload columns for change detection.
		- {table}_history.valid_from is the timestamp of the start of the validity window.
		- {table}_history.valid_to is the timestamp of the end of the validity window.
		- {table}_history.op is the operation that produced this row (I|U|D).
		- {table}_history.run_id is the identifier of the ingestion run that produced this row.
		`,
	},
	{
		Name:        "dz_metros",
		DatasetType: schematypes.DatasetTypeSCD2,
		Purpose: `
			DZ metros are the geographic regions (exchanges) in the DZ network.
			Contains metro public keys, codes, names, and coordinates (longitude, latitude).
			Use for metro-level analysis, geographic grouping, and location-based queries.
		`,
		Tables: []string{"dz_metros_current", "dz_metros_history"},
		Description: `
		USAGE:
		- Always query dz_metros_current for current state.
		- Joins:
			- dz_devices_current.metro_pk = dz_metros_current.pk

		COLUMNS:
		- pk (VARCHAR): Primary key
		- code (VARCHAR): Metro code (e.g. nyc, lon, fra)
		- name (VARCHAR): Metro name (e.g. New York, London, Frankfurt)
		- longitude (DOUBLE): Longitude
		- latitude (DOUBLE): Latitude

		SCD2 DATA STRUCTURE:
		- {table}_current: Current state of the dataset. One row per dataset entity.
		- {table}_history: Append-only historical versions with validity windows.
		- Always query {table}_current for current state. Query {table}_history only for historical analysis or point-in-time queries.
		- All joins should use {table}_current tables unless explicitly doing historical analysis.
		- {table}_current.as_of_ts is the timestamp of the snapshot that produced this row.
		- {table}_current.row_hash is the hash of the payload columns for change detection.
		- {table}_history.valid_from is the timestamp of the start of the validity window.
		- {table}_history.valid_to is the timestamp of the end of the validity window.
		- {table}_history.op is the operation that produced this row (I|U|D).
		- {table}_history.run_id is the identifier of the ingestion run that produced this row.
		`,
	},
	{
		Name:        "dz_links",
		DatasetType: schematypes.DatasetTypeSCD2,
		Purpose: `
			DZ links are the connections between devices in the DZ network.
			Contains link public keys, status, type (WAN/DZX), tunnel networks, committed RTT/jitter, bandwidth, and device endpoints.
			Use for topology analysis, network configuration, link performance analysis, and inter/intra-metro connectivity.
		`,
		Tables: []string{"dz_links_current", "dz_links_history"},
		Description: `
		USAGE:
		- Always query dz_links_current for current state.
		- Joins:
			- dz_links_current.side_a_pk = dz_devices_current.pk
			- dz_links_current.side_z_pk = dz_devices_current.pk
			- dz_links_current.contributor_pk = dz_contributors_current.pk
		- For inter-metro analysis, join devices as endpoints and filter da.metro_pk != dz.metro_pk.

		COLUMNS:
		- pk (VARCHAR): Primary key
		- status (VARCHAR): pending, activated, suspended, deleted, rejected, requested, hard-drained, soft-drained. activated means the link is operational and available for traffic.
		- code (VARCHAR): Link code (e.g. la2-dz01:ny5-dz01)
		- tunnel_net (VARCHAR): Tunnel network CIDR (e.g. 172.16.0.0/31)
		- contributor_pk (VARCHAR): Foreign key → dz_contributors_current.pk
		- side_a_pk (VARCHAR): Foreign key → dz_devices_current.pk
		- side_z_pk (VARCHAR): Foreign key → dz_devices_current.pk
		- side_a_iface_name (VARCHAR): Interface name on side A
		- side_z_iface_name (VARCHAR): Interface name on side Z
		- link_type (VARCHAR): WAN (inter-metro) or DZX (intra-metro)
		- committed_rtt_ns (BIGINT): Committed RTT (nanoseconds)
		- committed_jitter_ns (BIGINT): Committed jitter (nanoseconds)
		- bandwidth_bps (BIGINT): Link capacity in bits per second
		- isis_delay_override_ns (BIGINT): IS-IS delay metric override (nanoseconds). Interpretation rule: isis_delay_override_ns = 1000000000 means the link is soft-drained (drain signal).

		SCD2 DATA STRUCTURE:
		- {table}_current: Current state of the dataset. One row per dataset entity.
		- {table}_history: Append-only historical versions with validity windows.
		- Always query {table}_current for current state. Query {table}_history only for historical analysis or point-in-time queries.
		- All joins should use {table}_current tables unless explicitly doing historical analysis.
		- {table}_current.as_of_ts is the timestamp of the snapshot that produced this row.
		- {table}_current.row_hash is the hash of the payload columns for change detection.
		- {table}_history.valid_from is the timestamp of the start of the validity window.
		- {table}_history.valid_to is the timestamp of the end of the validity window.
		- {table}_history.op is the operation that produced this row (I|U|D).
		- {table}_history.run_id is the identifier of the ingestion run that produced this row.
		`,
	},
	{
		Name:        "dz_users",
		DatasetType: schematypes.DatasetTypeSCD2,
		Purpose: `
			DZ users are the connected sessions in the DZ network.
			Each user is uniquely identified by their public key (pk), and operated by an entity (owner_pk or client public key).
			Users are connected via devices in the DZ network.
			Use for user-level analysis and user-device relationship analysis.
			Users can be Solana gossip nodes or validators, or other types of users.
			User sessions historical activity is tracked in the dz_users_history table.
		`,
		Tables: []string{"dz_users_current", "dz_users_history"},
		Description: `
		USAGE:
		- Always query dz_users_current for current state.
		- Joins:
			- dz_users_current.device_pk = dz_devices_current.pk
			- dz_users_current.dz_ip = solana_gossip_nodes_current.gossip_ip
			- dz_users_current.client_ip = geoip_records_current.ip

		TERMINOLOGY:
		- Also referred to as user connections or sessions; a form of activity when viewed historically.
		- SCD2 history tables can be used to infer historical activity. Do not report initial ingestion of data as user activity.

		RULES:
		- DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan is an internal QA owner_pk; its activity may reflect testing and must not be used alone to infer user growth or churn. Distinguish it from other users in your responses.

		PRIMARY KEY SEMANTICS:
		- pk = primary key (always).
		- Stores a public key value, but its semantics are relational.
		- Use pk for uniqueness, identity, and joins.
		- Only {table}_pk → table.pk defines valid joins.
		- Other *_pk (e.g. owner_pk) are public keys, non-unique, and not primary keys unless explicitly stated.

		COLUMNS:
		- pk (VARCHAR): Primary key
		- owner_pk (VARCHAR): Owner public key
		- status (VARCHAR): pending, activated, suspended, deleted, rejected, pending_ban, banned, updating
		- kind (VARCHAR): Connection type: ibrl (IBRL), ibrl_with_allocated_ip (IBRL with allocated IP), edge_filtering (Edge Filtering), multicast (Multicast)
		- client_ip (VARCHAR): Client IP address
		- dz_ip (VARCHAR): DoubleZero IP address
		- device_pk (VARCHAR): Foreign key → dz_devices_current.pk
		- tunnel_id (INTEGER): Tunnel identifier (u16)

		SCD2 DATA STRUCTURE:
		- {table}_current: Current state of the dataset. One row per dataset entity.
		- {table}_history: Append-only historical versions with validity windows.
		- Always query {table}_current for current state. Query {table}_history only for historical analysis or point-in-time queries.
		- All joins should use {table}_current tables unless explicitly doing historical analysis.
		- {table}_current.as_of_ts is the timestamp of the snapshot that produced this row.
		- {table}_current.row_hash is the hash of the payload columns for change detection.
		- {table}_history.valid_from is the timestamp of the start of the validity window.
		- {table}_history.valid_to is the timestamp of the end of the validity window.
		- {table}_history.op is the operation that produced this row (I|U|D).
		- {table}_history.run_id is the identifier of the ingestion run that produced this row.
		`,
	},
}
