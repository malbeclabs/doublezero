package dzsvc

import sqltools "github.com/malbeclabs/doublezero/tools/mcp/internal/tools/sql"

func (v *View) SchemaTool() (*sqltools.SchemaTool, error) {
	return sqltools.NewSchemaTool(sqltools.SchemaToolConfig{
		Logger: v.log,
		DB:     v.cfg.DB,
		Schema: SCHEMA,
	})
}

var SCHEMA = &sqltools.Schema{
	Name:        "doublezero-serviceability",
	Description: "DoubleZero serviceability dataset. Use this dataset for questions about the network structure, contributors, devices, metros, links, and users in the DoubleZero network.",
	Tables: []sqltools.TableInfo{
		{
			Name:        "dz_contributors",
			Description: "Contributors in the DoubleZero network. Each contributor operates one or more devices and links. Join: dz_devices.contributor_pk = dz_contributors.pk, dz_links.contributor_pk = dz_contributors.pk (primary keys are always 'pk')",
			Columns: []sqltools.ColumnInfo{
				{Name: "pk", Type: "VARCHAR", Description: "Primary key. Join: dz_devices.contributor_pk = dz_contributors.pk, dz_links.contributor_pk = dz_contributors.pk (primary keys are always 'pk', not 'contributor_pk')"},
				{Name: "code", Type: "VARCHAR", Description: "Contributor code. Short human readable identifier for the contributor."},
				{Name: "name", Type: "VARCHAR", Description: "Contributor name. Full human readable name for the contributor."},
			},
		},
		{
			Name:        "dz_devices",
			Description: "Network devices. Join: dz_devices.metro_pk = dz_metros.pk, dz_devices.contributor_pk = dz_contributors.pk. Each device is operated by a contributor.",
			Columns: []sqltools.ColumnInfo{
				{Name: "pk", Type: "VARCHAR", Description: "Primary key. Join target: dz_links.side_a_pk = dz_devices.pk, dz_links.side_z_pk = dz_devices.pk"},
				{Name: "status", Type: "VARCHAR", Description: "pending, activated, suspended, deleted, rejected, soft-drained, hard-drained"},
				{Name: "device_type", Type: "VARCHAR", Description: "hybrid, transit, edge"},
				{Name: "code", Type: "VARCHAR", Description: "Device code. Human readable identifier for the device (e.g., la2-dz01, ny5-dz01)"},
				{Name: "public_ip", Type: "VARCHAR", Description: "Public IP address"},
				{Name: "contributor_pk", Type: "VARCHAR", Description: "Join to contributors.pk"},
				{Name: "metro_pk", Type: "VARCHAR", Description: "Foreign key. Join: dz_devices.metro_pk = dz_metros.pk (NOT dz_metros.metro_pk - primary keys are always 'pk')"},
			},
		},
		{
			Name:        "dz_metros",
			Description: "Metro areas (also called exchanges). Join: dz_devices.metro_pk = dz_metros.pk (NOT dz_metros.metro_pk - primary keys are always 'pk'). Available columns: pk, code, name, longitude, latitude. There is NO country column.",
			Columns: []sqltools.ColumnInfo{
				{Name: "pk", Type: "VARCHAR", Description: "Primary key. Join: dz_devices.metro_pk = dz_metros.pk (primary keys are always 'pk', not 'metro_pk')"},
				{Name: "code", Type: "VARCHAR", Description: "Metro code (e.g., nyc, lon, fra)"},
				{Name: "name", Type: "VARCHAR", Description: "Metro name (e.g., New York, London, Frankfurt)"},
				{Name: "longitude", Type: "DOUBLE", Description: "Longitude"},
				{Name: "latitude", Type: "DOUBLE", Description: "Latitude"},
			},
		},
		{
			Name:        "dz_links",
			Description: "Network links connecting 2 devices. Join: dz_links.side_a_pk = dz_devices.pk, dz_links.side_z_pk = dz_devices.pk",
			Columns: []sqltools.ColumnInfo{
				{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
				{Name: "status", Type: "VARCHAR", Description: "pending, activated, suspended, deleted, rejected, requested, hard-drained, soft-drained"},
				{Name: "code", Type: "VARCHAR", Description: "Link code. Human readable identifier for the link (e.g., la2-dz01:ny5-dz01)"},
				{Name: "tunnel_net", Type: "VARCHAR", Description: "Tunnel network CIDR (e.g., 172.16.0.0/31)"},
				{Name: "contributor_pk", Type: "VARCHAR", Description: "Foreign key. Join: dz_links.contributor_pk = dz_contributors.pk (NOT dz_contributors.contributor_pk - primary keys are always 'pk')"},
				{Name: "side_a_pk", Type: "VARCHAR", Description: "Foreign key. Join: dz_links.side_a_pk = dz_devices.pk"},
				{Name: "side_z_pk", Type: "VARCHAR", Description: "Foreign key. Join: dz_links.side_z_pk = dz_devices.pk"},
				{Name: "side_a_iface_name", Type: "VARCHAR", Description: "Interface name on side A"},
				{Name: "side_z_iface_name", Type: "VARCHAR", Description: "Interface name on side Z"},
				{Name: "link_type", Type: "VARCHAR", Description: "WAN or DZX"},
				{Name: "delay_ns", Type: "BIGINT", Description: "Committed delay (nanoseconds)"},
				{Name: "jitter_ns", Type: "BIGINT", Description: "Committed jitter (nanoseconds)"},
				{Name: "bandwidth_bps", Type: "BIGINT", Description: "Link capacity in bits per second (bps). Use for aggregation. Common values: 10000000000 (10Gbps), 100000000000 (100Gbps)"},
			},
		},
		{
			Name:        "dz_users",
			Description: "Users connected to the DoubleZero network via devices. Join: dz_users.device_pk = dz_devices.pk. Some users are Solana validators, but not all users are Solana validators. Join to solana_gossip_nodes.gossip_ip via dz_users.dz_ip to get the gossip node associated with the user.",
			Columns: []sqltools.ColumnInfo{
				{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
				{Name: "owner_pk", Type: "VARCHAR", Description: "Owner public key"},
				{Name: "status", Type: "VARCHAR", Description: "pending, activated, suspended, deleted, rejected, pending_ban, banned, updating"},
				{Name: "kind", Type: "VARCHAR", Description: "Connection type: ibrl, ibrl_with_allocated_ip, edge_filtering, multicast"},
				{Name: "client_ip", Type: "VARCHAR", Description: "Client IP address"},
				{Name: "dz_ip", Type: "VARCHAR", Description: "DoubleZero IP address. Join to solana_gossip_nodes.gossip_ip to get the gossip node associated with the user"},
				{Name: "device_pk", Type: "VARCHAR", Description: "Foreign key. Join: dz_users.device_pk = dz_devices.pk"},
			},
		},
	},
}
