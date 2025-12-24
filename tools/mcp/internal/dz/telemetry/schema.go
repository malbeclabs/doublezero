package dztelem

import sqltools "github.com/malbeclabs/doublezero/tools/mcp/internal/tools/sql"

func (v *View) SchemaTool() (*sqltools.SchemaTool, error) {
	return sqltools.NewSchemaTool(sqltools.SchemaToolConfig{
		Logger: v.log,
		DB:     v.cfg.DB,
		Schema: SCHEMA,
	})
}

var SCHEMA = &sqltools.Schema{
	Name:        "doublezero-telemetry",
	Description: "DoubleZero telemetry dataset. Use this dataset for questions about the network performance, latency, and bandwidth.",
	Tables: []sqltools.TableInfo{
		{
			Name:        "dz_device_link_circuits",
			Description: "Device-to-device circuits. Join: dz_device_link_latency_samples.circuit_code → dz_device_link_circuits.code → dz_devices → dz_metros. Metro pair: origin_metro.code || ' → ' || target_metro.code",
			Columns: []sqltools.ColumnInfo{
				{Name: "code", Type: "VARCHAR", Description: "Circuit code (format: origin → target (link_suffix)). Join key from dz_device_link_latency_samples.circuit_code"},
				{Name: "origin_device_pk", Type: "VARCHAR", Description: "Join to devices.pk → devices.metro_pk → metros (origin metro)"},
				{Name: "target_device_pk", Type: "VARCHAR", Description: "Join to devices.pk → devices.metro_pk → metros (target metro)"},
				{Name: "link_pk", Type: "VARCHAR", Description: "Join to links.pk"},
				{Name: "link_code", Type: "VARCHAR", Description: "Link code"},
				{Name: "link_type", Type: "VARCHAR", Description: "DZX (direct metro) or WAN"},
				{Name: "contributor_code", Type: "VARCHAR", Description: "Contributor code"},
				{Name: "committed_rtt", Type: "DOUBLE", Description: "Committed RTT (microseconds, from link SLA)"},
				{Name: "committed_jitter", Type: "DOUBLE", Description: "Committed jitter (microseconds, from link SLA)"},
			},
		},
		{
			Name:        "dz_device_link_latency_samples",
			Description: "RTT samples for device-to-device circuits (probes, not user traffic). When rtt_us=0, packet loss occurred. Join circuit_code → dz_device_link_circuits → dz_devices → dz_metros. Metro pair format: origin || ' → ' || target",
			Columns: []sqltools.ColumnInfo{
				{Name: "circuit_code", Type: "VARCHAR", Description: "Join to dz_device_link_circuits.code → dz_device_link_circuits.origin_device_pk/target_device_pk → dz_devices → dz_metros. Metro pair: origin_metro.code || ' → ' || target_metro.code"},
				{Name: "epoch", Type: "BIGINT", Description: "Solana epoch"},
				{Name: "sample_index", Type: "INTEGER", Description: "Sample index within epoch (0-based)"},
				{Name: "timestamp_us", Type: "BIGINT", Description: "Timestamp (microseconds since UNIX epoch)"},
				{Name: "rtt_us", Type: "BIGINT", Description: "RTT in microseconds (BIGINT). rtt_us=0 = packet loss. Filter with WHERE rtt_us > 0. For arithmetic, use CAST(rtt_us AS BIGINT) * CAST(rtt_us AS BIGINT)"},
			},
		},
		{
			Name:        "dz_internet_metro_latency_samples",
			Description: "RTT samples for metro-to-metro over public internet (probes, not user traffic). circuit_code format: 'origin → target' (e.g., 'nyc → lon'). Match bidirectionally with dz_device_link_latency_samples metro pairs",
			Columns: []sqltools.ColumnInfo{
				{Name: "circuit_code", Type: "VARCHAR", Description: "Metro pair code: 'origin → target' (e.g., 'nyc → lon'). Match bidirectionally with dz_device_link_latency_samples metro pairs"},
				{Name: "data_provider", Type: "VARCHAR", Description: "Data provider (e.g., cloudping, pingdom)"},
				{Name: "epoch", Type: "BIGINT", Description: "Solana epoch"},
				{Name: "sample_index", Type: "INTEGER", Description: "Sample index within epoch (0-based)"},
				{Name: "timestamp_us", Type: "BIGINT", Description: "Timestamp (microseconds since UNIX epoch)"},
				{Name: "rtt_us", Type: "BIGINT", Description: "RTT in microseconds (BIGINT). All values valid (no packet loss). For arithmetic, use CAST(rtt_us AS BIGINT) * CAST(rtt_us AS BIGINT)"},
			},
		},
	},
}
