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
			Description: "Device-to-device circuits. Join: dz_device_link_latency_samples.circuit_code = dz_device_link_circuits.code (NOT dz_device_link_circuits.circuit_code - the column is named 'code'). Then join to dz_devices → dz_metros. Metro pair: origin_metro.code || ' → ' || target_metro.code",
			Columns: []sqltools.ColumnInfo{
				{Name: "code", Type: "VARCHAR", Description: "Circuit code (format: origin → target (link_suffix)). Join: dz_device_link_latency_samples.circuit_code = dz_device_link_circuits.code (NOT circuit_code - the column is named 'code')"},
				{Name: "origin_device_pk", Type: "VARCHAR", Description: "Foreign key. Join: circuits.origin_device_pk = devices.pk (alias 'dev_o'), then dev_o.metro_pk = metros.pk (alias 'mo') for origin metro. There is NO origin_metro_pk or side_a_metro_pk column - you must join through devices. Never use 'do' as an alias - it's a SQL reserved keyword."},
				{Name: "target_device_pk", Type: "VARCHAR", Description: "Foreign key. Join: circuits.target_device_pk = devices.pk (alias 'dev_t'), then dev_t.metro_pk = metros.pk (alias 'mt') for target metro. There is NO target_metro_pk or side_z_metro_pk column - you must join through devices. Never use 'dt' as an alias - it's a SQL reserved keyword."},
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
			Description: "RTT samples for device-to-device circuits (probes, not user traffic). When rtt_us=0, packet loss occurred. To get metro information: samples.circuit_code = circuits.code, then circuits.origin_device_pk = devices.pk (alias 'dev_o'), then dev_o.metro_pk = metros.pk (alias 'mo'). For target: circuits.target_device_pk = devices.pk (alias 'dev_t'), then dev_t.metro_pk = metros.pk (alias 'mt'). There is NO side_a_metro_pk, side_z_metro_pk, origin_metro_pk, target_metro_pk, origin_metro_code, or target_metro_code column - you must join through circuits → devices → metros, then reference mo.code and mt.code. Metro pair format: mo.code || ' → ' || mt.code. Never use 'do' or 'dt' as aliases - they are SQL reserved keywords.",
			Columns: []sqltools.ColumnInfo{
				{Name: "circuit_code", Type: "VARCHAR", Description: "Foreign key. Join path to metros: samples.circuit_code = circuits.code, then circuits.origin_device_pk = devices.pk (alias 'dev_o'), then dev_o.metro_pk = metros.pk (alias 'mo') for origin metro. For target: circuits.target_device_pk = devices.pk (alias 'dev_t'), then dev_t.metro_pk = metros.pk (alias 'mt'). There is NO side_a_metro_pk, side_z_metro_pk, origin_metro_pk, target_metro_pk, origin_metro_code, or target_metro_code column. To GROUP BY metro codes, join and use mo.code and mt.code (NOT origin_metro_code or target_metro_code). Never use 'do' or 'dt' as aliases - they are SQL reserved keywords."},
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
