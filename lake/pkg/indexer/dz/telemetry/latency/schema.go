package dztelemlatency

import (
	schematypes "github.com/malbeclabs/doublezero/lake/pkg/indexer/schema"
)

var Schema = &schematypes.Schema{
	Name: "doublezero-telemetry-latency",
	Description: `
Use for DZ performance measurements:
- device↔device latency RTT samples, jitter, loss proxy
- metro↔metro Internet RTT samples
- time-series and epoch-based analysis

COMPARISON RULE (DZ vs Internet):
- Only compare DZ WAN links (link_type = 'WAN') to Internet metro pairs.
- Do not compare DZX (intra-metro) links to Internet paths.

LOSS SCOPE:
- Device-link telemetry uses rtt_us = 0 to indicate loss.
- Internet metro telemetry has no loss signal.
`,
	Tables: []schematypes.TableInfo{
		{
			Name:        "dz_device_link_circuits",
			Description: "Device↔device circuit definitions. Join samples: dz_device_link_latency_samples.circuit_code = dz_device_link_circuits.code. Metro info requires joins via devices→metros: origin_device_pk→dz_devices.pk (dev_o)→dz_metros.pk (mo); target_device_pk→dz_devices.pk (dev_t)→dz_metros.pk (mt).",
			Columns: []schematypes.ColumnInfo{
				{Name: "code", Type: "VARCHAR", Description: "Circuit code"},
				{Name: "origin_device_pk", Type: "VARCHAR", Description: "Foreign key → dz_devices.pk (origin)"},
				{Name: "target_device_pk", Type: "VARCHAR", Description: "Foreign key → dz_devices.pk (target)"},
				{Name: "link_pk", Type: "VARCHAR", Description: "Foreign key → dz_links.pk"},
				{Name: "link_code", Type: "VARCHAR", Description: "Link code"},
				{Name: "link_type", Type: "VARCHAR", Description: "WAN (inter-metro) or DZX (intra-metro)"},
				{Name: "contributor_code", Type: "VARCHAR", Description: "Contributor code"},
				{Name: "committed_rtt", Type: "DOUBLE", Description: "Committed RTT (microseconds)."},
				{Name: "committed_jitter", Type: "DOUBLE", Description: "Committed jitter (microseconds)"},
			},
		},
		{
			Name:        "dz_device_link_latency_samples",
			Description: "RTT samples for device↔device circuits (probes). Join: circuit_code = dz_device_link_circuits.code. Loss convention: rtt_us = 0 indicates loss; use WHERE rtt_us > 0 for latency stats.",
			Columns: []schematypes.ColumnInfo{
				{Name: "circuit_code", Type: "VARCHAR", Description: "Foreign key → dz_device_link_circuits.code"},
				{Name: "epoch", Type: "BIGINT", Description: "DZ ledger blockchain epoch, not unix timestamp "},
				{Name: "sample_index", Type: "INTEGER", Description: "Sample index within epoch (0-based)"},
				{Name: "timestamp_us", Type: "BIGINT", Description: "Timestamp (microseconds since UNIX epoch)"},
				{Name: "rtt_us", Type: "BIGINT", Description: "RTT in microseconds; rtt_us = 0 indicates loss"},
			},
		},
		{
			Name:        "dz_internet_metro_latency_samples",
			Description: "RTT samples for metro↔metro over public Internet (probes). circuit_code format: 'origin → target' (e.g. 'nyc → lon'). No packet-loss signal in this table.",
			Columns: []schematypes.ColumnInfo{
				{Name: "circuit_code", Type: "VARCHAR", Description: "Metro pair code"},
				{Name: "data_provider", Type: "VARCHAR", Description: "Data provider"},
				{Name: "epoch", Type: "BIGINT", Description: "DZ ledger blockchain epoch, not unix timestamp "},
				{Name: "sample_index", Type: "INTEGER", Description: "Sample index within epoch (0-based)"},
				{Name: "timestamp_us", Type: "BIGINT", Description: "Timestamp (microseconds since UNIX epoch)"},
				{Name: "rtt_us", Type: "BIGINT", Description: "RTT in microseconds"},
			},
		},
	},
}
