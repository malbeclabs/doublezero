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

CIRCUIT DEFINITIONS:
- Device-link circuits: uniquely identified by (origin_device_pk, target_device_pk, link_pk)
  - Circuits are bi-directional: (A→B, link) and (B→A, link) are both valid for the same physical link
  - origin_device_pk and target_device_pk can be joined to dz_links.side_a_pk and dz_links.side_z_pk (in either direction)
- Internet-metro circuits: uniquely identified by (origin_metro_pk, target_metro_pk)
  - Circuits are bi-directional: (metro_A→metro_B) and (metro_B→metro_A) are both valid

COMPARISON RULE (DZ vs Internet):
- Only compare DZ WAN links (link_type = 'WAN') to Internet metro pairs.
- Do not compare DZX (intra-metro) links to Internet paths.

LOSS SCOPE:
- Device-link telemetry uses rtt_us = 0 to indicate loss.
- Internet metro telemetry has no loss signal.
`,
	Tables: []schematypes.TableInfo{
		{
			Name:        "dz_device_link_latency_samples",
			Description: "RTT samples for device↔device links (probes). Circuit definition: uniquely identified by (origin_device_pk, target_device_pk, link_pk). Circuits are bi-directional: (A→B, link) and (B→A, link) are both valid for the same physical link. Loss convention: rtt_us = 0 indicates loss; use WHERE rtt_us > 0 for latency stats. JOIN PATHS: origin_device_pk → dz_devices.pk (dev_o) to get dev_o.metro_pk, dev_o.contributor_pk; target_device_pk → dz_devices.pk (dev_t) to get dev_t.metro_pk, dev_t.contributor_pk; then join dev_o.metro_pk → dz_metros.pk (mo) and dev_t.metro_pk → dz_metros.pk (mt) for metro info; join dev_o.contributor_pk → dz_contributors.pk and dev_t.contributor_pk → dz_contributors.pk for contributor info; link_pk → dz_links.pk to get committed_rtt_ns for comparison with measured rtt_us. Note: origin_device_pk and target_device_pk can be joined to dz_links.side_a_pk and dz_links.side_z_pk (in either direction, since links are bi-directional).",
			Columns: []schematypes.ColumnInfo{
				{Name: "time", Type: "TIMESTAMP", Description: "Sample timestamp"},
				{Name: "epoch", Type: "BIGINT", Description: "DZ ledger blockchain epoch, not unix timestamp "},
				{Name: "sample_index", Type: "INTEGER", Description: "Sample index within epoch (0-based)"},
				{Name: "origin_device_pk", Type: "VARCHAR", Description: "Foreign key → dz_devices.pk (origin). Part of circuit key (origin_device_pk, target_device_pk, link_pk)."},
				{Name: "target_device_pk", Type: "VARCHAR", Description: "Foreign key → dz_devices.pk (target). Part of circuit key (origin_device_pk, target_device_pk, link_pk)."},
				{Name: "link_pk", Type: "VARCHAR", Description: "Foreign key → dz_links.pk. Part of circuit key (origin_device_pk, target_device_pk, link_pk). Join to get committed_rtt_ns for comparison."},
				{Name: "rtt_us", Type: "BIGINT", Description: "RTT in microseconds; rtt_us = 0 indicates loss"},
				{Name: "loss", Type: "BOOLEAN", Description: "True if packet loss detected (rtt_us = 0)"},
			},
		},
		{
			Name:        "dz_internet_metro_latency_samples",
			Description: "RTT samples for metro↔metro over public Internet (probes). Circuit definition: uniquely identified by (origin_metro_pk, target_metro_pk). Circuits are bi-directional: (metro_A→metro_B) and (metro_B→metro_A) are both valid. No packet-loss signal in this table. JOIN PATHS: origin_metro_pk → dz_metros.pk (mo) and target_metro_pk → dz_metros.pk (mt) for metro info (code, name, coordinates). Use for comparison with device-link latency samples by joining device-link samples' origin_device_pk → dz_devices.pk → dz_devices.metro_pk → dz_metros.pk to match metro pairs.",
			Columns: []schematypes.ColumnInfo{
				{Name: "time", Type: "TIMESTAMP", Description: "Sample timestamp"},
				{Name: "epoch", Type: "BIGINT", Description: "DZ ledger blockchain epoch, not unix timestamp "},
				{Name: "sample_index", Type: "INTEGER", Description: "Sample index within epoch (0-based)"},
				{Name: "origin_metro_pk", Type: "VARCHAR", Description: "Foreign key → dz_metros.pk (origin). Part of circuit key (origin_metro_pk, target_metro_pk)."},
				{Name: "target_metro_pk", Type: "VARCHAR", Description: "Foreign key → dz_metros.pk (target). Part of circuit key (origin_metro_pk, target_metro_pk)."},
				{Name: "data_provider", Type: "VARCHAR", Description: "Data provider"},
				{Name: "rtt_us", Type: "BIGINT", Description: "RTT in microseconds"},
			},
		},
	},
}
