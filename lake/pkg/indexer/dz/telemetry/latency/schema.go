package dztelemlatency

import (
	schematypes "github.com/malbeclabs/doublezero/lake/pkg/indexer/schema"
)

var Datasets = []schematypes.Dataset{
	{
		Name:        "dz_device_link_latency_samples",
		DatasetType: schematypes.DatasetTypeFact,
		Purpose: `
			Time-series RTT probe measurements for device-to-device links over the DZ network.
			Contains round-trip time samples (microseconds), packet loss indicators, and circuit identifiers (origin/target devices, link).
			Use for DZ network latency analysis, performance monitoring, and comparison with Internet paths.
		`,
		Tables: []string{"dz_device_link_latency_samples_raw"},
		Description: `
		USAGE:
		- Always query using time filter ("time" column).
		- Cast all columns to their specified types when querying the underlying tables with SQL.
		- Joins:
			- dz_device_link_latency_samples_raw.origin_device_pk = dz_devices_current.pk
			- dz_device_link_latency_samples_raw.target_device_pk = dz_devices_current.pk
			- dz_device_link_latency_samples_raw.link_pk = dz_links_current.pk
		- Join paths: origin_device_pk → dz_devices_current.pk (dev_o) to get dev_o.metro_pk, dev_o.contributor_pk; target_device_pk → dz_devices_current.pk (dev_t) to get dev_t.metro_pk, dev_t.contributor_pk; then join dev_o.metro_pk → dz_metros_current.pk (mo) and dev_t.metro_pk → dz_metros_current.pk (mt) for metro info; join dev_o.contributor_pk → dz_contributors_current.pk and dev_t.contributor_pk → dz_contributors_current.pk for contributor info; link_pk → dz_links_current.pk to get committed_rtt_ns for comparison with measured rtt_us.
		- Note: origin_device_pk and target_device_pk can be joined to dz_links_current.side_a_pk and dz_links_current.side_z_pk (in either direction, since links are bi-directional).

		CIRCUIT DEFINITIONS:
		- Device-link circuits: uniquely identified by (origin_device_pk, target_device_pk, link_pk)
		- Circuits are bi-directional: (A→B, link) and (B→A, link) are both valid for the same physical link
		- origin_device_pk and target_device_pk can be joined to dz_links_current.side_a_pk and dz_links_current.side_z_pk (in either direction)

		COMPARISON WITH INTERNET LATENCY:
		- You can compare DZ network latency with public Internet latency using dz_internet_metro_latency_samples.
		- Only compare DZ WAN links (link_type = 'WAN') to Internet metro pairs.
		- Do not compare DZX (intra-metro) links to Internet paths.
		- To compare: join device-link samples' origin_device_pk → dz_devices_current.pk → dz_devices_current.metro_pk → dz_metros_current.pk to get origin metro, and target_device_pk → dz_devices_current.pk → dz_devices_current.metro_pk → dz_metros_current.pk to get target metro, then match with dz_internet_metro_latency_samples for the same metro pair (origin_metro_pk, target_metro_pk).

		LOSS SCOPE:
		- Device-link telemetry uses rtt_us = 0 to indicate loss.
		- Use WHERE rtt_us > 0 for latency stats.

		COLUMNS:
		- time (TIMESTAMP): Sample timestamp
		- epoch (BIGINT): DZ ledger blockchain epoch, not unix timestamp
		- sample_index (INTEGER): Sample index within epoch (0-based)
		- origin_device_pk (VARCHAR): Foreign key → dz_devices_current.pk (origin). Part of circuit key (origin_device_pk, target_device_pk, link_pk).
		- target_device_pk (VARCHAR): Foreign key → dz_devices_current.pk (target). Part of circuit key (origin_device_pk, target_device_pk, link_pk).
		- link_pk (VARCHAR): Foreign key → dz_links_current.pk. Part of circuit key (origin_device_pk, target_device_pk, link_pk). Join to get committed_rtt_ns for comparison.
		- rtt_us (BIGINT): RTT in microseconds; rtt_us = 0 indicates loss
		- loss (BOOLEAN): True if packet loss detected (rtt_us = 0)
		`,
	},
	{
		Name:        "dz_internet_metro_latency_samples",
		DatasetType: schematypes.DatasetTypeFact,
		Purpose: `
			Time-series RTT probe measurements for metro-to-metro over public Internet.
			Contains round-trip time samples (microseconds) and circuit identifiers (origin/target metros).
			Use for Internet latency analysis and comparison with DZ network performance.
		`,
		Tables: []string{"dz_internet_metro_latency_samples_raw"},
		Description: `
		USAGE:
		- Always query using time filter ("time" column).
		- Cast all columns to their specified types when querying the underlying tables with SQL.
		- Joins:
			- dz_internet_metro_latency_samples_raw.origin_metro_pk = dz_metros_current.pk
			- dz_internet_metro_latency_samples_raw.target_metro_pk = dz_metros_current.pk
		- Join paths: origin_metro_pk → dz_metros_current.pk (mo) and target_metro_pk → dz_metros_current.pk (mt) for metro info (code, name, coordinates).

		CIRCUIT DEFINITIONS:
		- Internet-metro circuits: uniquely identified by (origin_metro_pk, target_metro_pk)
		- Circuits are bi-directional: (metro_A→metro_B) and (metro_B→metro_A) are both valid

		COMPARISON WITH DZ NETWORK LATENCY:
		- You can compare Internet metro latency with DZ network latency using dz_device_link_latency_samples.
		- Only compare DZ WAN links (link_type = 'WAN') to Internet metro pairs.
		- Do not compare DZX (intra-metro) links to Internet paths.
		- To compare: match metro pairs by joining dz_device_link_latency_samples' origin_device_pk → dz_devices_current.pk → dz_devices_current.metro_pk → dz_metros_current.pk to get origin metro, and target_device_pk → dz_devices_current.pk → dz_devices_current.metro_pk → dz_metros_current.pk to get target metro, then compare with this dataset (dz_internet_metro_latency_samples) for the same metro pair (origin_metro_pk, target_metro_pk).

		LOSS SCOPE:
		- Internet metro telemetry has no loss signal.

		COLUMNS:
		- time (TIMESTAMP): Sample timestamp
		- epoch (BIGINT): DZ ledger blockchain epoch, not unix timestamp
		- sample_index (INTEGER): Sample index within epoch (0-based)
		- origin_metro_pk (VARCHAR): Foreign key → dz_metros_current.pk (origin). Part of circuit key (origin_metro_pk, target_metro_pk).
		- target_metro_pk (VARCHAR): Foreign key → dz_metros_current.pk (target). Part of circuit key (origin_metro_pk, target_metro_pk).
		- data_provider (VARCHAR): Data provider
		- rtt_us (BIGINT): RTT in microseconds
		`,
	},
}
