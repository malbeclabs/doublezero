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
	Name: "doublezero-telemetry",
	Description: `
		Use this dataset for **network performance measurements**:
		- RTT, jitter, packet loss
		- Circuit-level and link-level performance
		- Time-series latency data
		- Internet metro-to-metro measurements

		CRITICAL COMPARISON RULE:
		- When comparing DZ latency to Internet latency, **only compare DZ WAN links**:
			WHERElink_type='WAN'
		- Do **not** compare DZX (intra-metro) links to Internet paths; they are not comparable.

		METRIC SCOPE:
		- Packet loss exists only for device-link telemetry.
		- Internet metro telemetry does not include packet loss.
	`,
	Tables: []sqltools.TableInfo{
		{
			Name:        "dz_device_link_circuits",
			Description: "Device-to-device circuits. Join: dz_device_link_latency_samples.circuit_code = dz_device_link_circuits.code (the column is named 'code', not 'circuit_code'). To get metro information: circuits.origin_device_pk = devices.pk (alias 'dev_o'), then dev_o.metro_pk = metros.pk (alias 'mo') for origin metro. For target: circuits.target_device_pk = devices.pk (alias 'dev_t'), then dev_t.metro_pk = metros.pk (alias 'mt'). There is NO origin_metro_pk, target_metro_pk, side_a_metro_pk, or side_z_metro_pk column - you must join through devices. Metro pair format: mo.code || ' → ' || mt.code",
			Columns: []sqltools.ColumnInfo{
				{Name: "code", Type: "VARCHAR", Description: "Circuit code (format: origin → target (link_suffix)). Join key from dz_device_link_latency_samples.circuit_code"},
				{Name: "origin_device_pk", Type: "VARCHAR", Description: "Foreign key. Join to dz_devices.pk for origin device"},
				{Name: "target_device_pk", Type: "VARCHAR", Description: "Foreign key. Join to dz_devices.pk for target device"},
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
			Description: "RTT samples for device-to-device circuits (probes, not user traffic). When rtt_us=0, packet loss occurred. Join: samples.circuit_code = dz_device_link_circuits.code. To get metro information, join through circuits → devices → metros (see dz_device_link_circuits description). There is NO origin_metro_code or target_metro_code column - use mo.code and mt.code from joined metros tables. Metro pair format: mo.code || ' → ' || mt.code",
			Columns: []sqltools.ColumnInfo{
				{Name: "circuit_code", Type: "VARCHAR", Description: "Foreign key. Join to dz_device_link_circuits.code"},
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
