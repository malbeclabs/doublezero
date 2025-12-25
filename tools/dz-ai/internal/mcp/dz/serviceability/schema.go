package dzsvc

import sqltools "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/tools/sql"

func (v *View) SchemaTool() (*sqltools.SchemaTool, error) {
	return sqltools.NewSchemaTool(sqltools.SchemaToolConfig{
		Logger: v.log,
		DB:     v.cfg.DB,
		Schema: SCHEMA,
	})
}

var SCHEMA = &sqltools.Schema{
	Name: "doublezero-serviceability",
	Description: `
Use for DZ network structure and state:
- contributors, devices (DZDs), links, metros, users
- topology, ownership, and operational status

Default starting point for questions about the DZ network unless the question is explicitly about performance metrics.
`,
	Tables: []sqltools.TableInfo{
		{
			Name:        "dz_contributors",
			Description: "DZ contributors (operators of devices and links). Joins: dz_devices.contributor_pk = dz_contributors.pk; dz_links.contributor_pk = dz_contributors.pk.",
			Columns: []sqltools.ColumnInfo{
				{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
				{Name: "code", Type: "VARCHAR", Description: "Contributor code (short human-readable identifier)"},
				{Name: "name", Type: "VARCHAR", Description: "Contributor name (full human-readable name)"},
			},
		},
		{
			Name:        "dz_devices",
			Description: "DZ network devices (DZDs). Joins: dz_devices.metro_pk = dz_metros.pk; dz_devices.contributor_pk = dz_contributors.pk. Link endpoints: dz_links.side_a_pk = dz_devices.pk; dz_links.side_z_pk = dz_devices.pk.",
			Columns: []sqltools.ColumnInfo{
				{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
				{Name: "status", Type: "VARCHAR", Description: "pending, activated, suspended, deleted, rejected, soft-drained, hard-drained"},
				{Name: "device_type", Type: "VARCHAR", Description: "hybrid, transit, edge"},
				{Name: "code", Type: "VARCHAR", Description: "Device code (e.g. la2-dz01, ny5-dz01)"},
				{Name: "public_ip", Type: "VARCHAR", Description: "Public IP address"},
				{Name: "contributor_pk", Type: "VARCHAR", Description: "Foreign key → dz_contributors.pk"},
				{Name: "metro_pk", Type: "VARCHAR", Description: "Foreign key → dz_metros.pk"},
			},
		},
		{
			Name:        "dz_metros",
			Description: "Metro areas (exchanges). Join: dz_devices.metro_pk = dz_metros.pk. Columns: pk, code, name, longitude, latitude. No country column.",
			Columns: []sqltools.ColumnInfo{
				{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
				{Name: "code", Type: "VARCHAR", Description: "Metro code (e.g. nyc, lon, fra)"},
				{Name: "name", Type: "VARCHAR", Description: "Metro name (e.g. New York, London, Frankfurt)"},
				{Name: "longitude", Type: "DOUBLE", Description: "Longitude"},
				{Name: "latitude", Type: "DOUBLE", Description: "Latitude"},
			},
		},
		{
			Name:        "dz_links",
			Description: "Links connecting two devices. Joins: dz_links.side_a_pk = dz_devices.pk; dz_links.side_z_pk = dz_devices.pk; dz_links.contributor_pk = dz_contributors.pk. link_type: WAN (inter-metro) or DZX (intra-metro). For inter-metro analysis, join devices as endpoints and filter da.metro_pk != dz.metro_pk.",
			Columns: []sqltools.ColumnInfo{
				{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
				{Name: "status", Type: "VARCHAR", Description: "pending, activated, suspended, deleted, rejected, requested, hard-drained, soft-drained"},
				{Name: "code", Type: "VARCHAR", Description: "Link code (e.g. la2-dz01:ny5-dz01)"},
				{Name: "tunnel_net", Type: "VARCHAR", Description: "Tunnel network CIDR (e.g. 172.16.0.0/31)"},
				{Name: "contributor_pk", Type: "VARCHAR", Description: "Foreign key → dz_contributors.pk"},
				{Name: "side_a_pk", Type: "VARCHAR", Description: "Foreign key → dz_devices.pk"},
				{Name: "side_z_pk", Type: "VARCHAR", Description: "Foreign key → dz_devices.pk"},
				{Name: "side_a_iface_name", Type: "VARCHAR", Description: "Interface name on side A"},
				{Name: "side_z_iface_name", Type: "VARCHAR", Description: "Interface name on side Z"},
				{Name: "link_type", Type: "VARCHAR", Description: "WAN or DZX"},
				{Name: "delay_ns", Type: "BIGINT", Description: "Committed delay (nanoseconds)"},
				{Name: "jitter_ns", Type: "BIGINT", Description: "Committed jitter (nanoseconds)"},
				{Name: "bandwidth_bps", Type: "BIGINT", Description: "Link capacity in bits per second"},
			},
		},
		{
			Name:        "dz_users",
			Description: "DZ users connected via devices. Join: dz_users.device_pk = dz_devices.pk. Some users map to Solana gossip nodes via dz_users.dz_ip = solana_gossip_nodes.gossip_ip.",
			Columns: []sqltools.ColumnInfo{
				{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
				{Name: "owner_pk", Type: "VARCHAR", Description: "Owner public key"},
				{Name: "status", Type: "VARCHAR", Description: "pending, activated, suspended, deleted, rejected, pending_ban, banned, updating"},
				{Name: "kind", Type: "VARCHAR", Description: "Connection type (ibrl, ibrl_with_allocated_ip, edge_filtering, multicast)"},
				{Name: "client_ip", Type: "VARCHAR", Description: "Client IP address"},
				{Name: "dz_ip", Type: "VARCHAR", Description: "DoubleZero IP address"},
				{Name: "device_pk", Type: "VARCHAR", Description: "Foreign key → dz_devices.pk"},
			},
		},
	},
}
