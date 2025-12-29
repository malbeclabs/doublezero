package dztelemusage

import (
	schematypes "github.com/malbeclabs/doublezero/lake/pkg/indexer/schema"
)

var Datasets = []schematypes.Dataset{
	{
		Name:        "dz_device_iface_usage",
		DatasetType: schematypes.DatasetTypeFact,
		Purpose: `
			Time-series device interface utilization measurements (cumulative counters and deltas).
			Contains packet/octet counters (in/out), error/discard statistics, interface metadata, and per-user tunnel attribution.
			Use for network usage analysis, interface health monitoring, device load analysis, and per-user traffic attribution.
		`,
		Tables: []string{"dz_device_iface_usage_raw"},
		Description: `
		USAGE:
		- Always query using time filter.
		- Joins:
			- dz_device_iface_usage_raw.device_pk = dz_devices_current.pk
			- dz_device_iface_usage_raw.link_pk = dz_links_current.pk
			- For per-user attribution, join to users using (device_pk, user_tunnel_id) = (dz_users_current.device_pk, dz_users_current.tunnel_id) (do not join on tunnel_id alone).

		ANSWERING RULES:
		- Interface errors or discards are first-order health signals; always surface them in summaries, even when counts are small, and provide brief, data-grounded context.
		- When summarizing network health, interface errors or discards must appear in the initial health summary alongside loss and drain signals, not only in follow-up sections.
		- Interface error reporting must include the specific devices and interfaces involved; if many are affected, list the most impacted and summarize the rest.

		METRICS RULES:
		- Utilization is defined by throughput rate, not total transferred volume.
		- Total bytes/GB are contextual only and must not be used to characterize load or saturation unless explicitly requested.
		- Interface counters (in/out octets, packets) are cumulative and passively sampled; never sum raw counters.
		- Compute rates as (last_counter - first_counter) / delta_duration over a consistent time window.
		- Exclude zero or negative deltas.
		- Convert octets to Gbps as: (octets * 8) / delta_duration / 1e9.
		- Report rates in Gbps by default; use Mbps only when values are < 1 Gbps.
		- Counter deltas indicate traffic occurred during the interval; they do not imply continuous or instantaneous transmission.
		- Do not aggregate in/out directions together; account for duplication across devices.

		AGGREGATION RULES:
		- Interface counters are per-interface; summing deltas at the same observation time represents aggregate device load.
		- To compute device load from user traffic, sum interface deltas across all user tunnels per device per observation, then compute rates.
		- Do not average per-user rates to infer device throughput.
		- For device-level reporting, compute average, p95, and peak from per-observation summed rates.
		- For per-user analysis, compute rates per user first, then aggregate statistics separately.
		- Explicitly call out anomalies or outliers when present.

		COLUMNS:
		- time (TIMESTAMP): Timestamp of the measurement
		- device_pk (VARCHAR): Foreign key → dz_devices_current.pk (DoubleZero device public key)
		- host (VARCHAR): Host identifier (INTERNAL USE ONLY)
		- intf (VARCHAR): Interface name
		- user_tunnel_id (BIGINT): Tunnel ID extracted from interface name (e.g., Tunnel501 -> 501). Join to users using the composite key (device_pk, user_tunnel_id) = (users.device_pk, users.tunnel_id); tunnel_id alone is not globally unique.
		- link_pk (VARCHAR): Foreign key → dz_links_current.pk. Populated by matching (device_pk, intf) to link side A or Z.
		- link_side (VARCHAR): Link side: 'A' or 'Z'. Indicates which side of the link this interface belongs to.
		- model_name (VARCHAR): Device model name
		- serial_number (VARCHAR): Device serial number
		- carrier_transitions (BIGINT): Number of carrier transitions
		- in_broadcast_pkts (BIGINT): Incoming broadcast packets
		- in_discards (BIGINT): Incoming discarded packets
		- in_errors (BIGINT): Incoming error packets
		- in_fcs_errors (BIGINT): Incoming FCS error packets
		- in_multicast_pkts (BIGINT): Incoming multicast packets
		- in_octets (BIGINT): Incoming octets (bytes)
		- in_pkts (BIGINT): Incoming packets
		- in_unicast_pkts (BIGINT): Incoming unicast packets
		- out_broadcast_pkts (BIGINT): Outgoing broadcast packets
		- out_discards (BIGINT): Outgoing discarded packets
		- out_errors (BIGINT): Outgoing error packets
		- out_multicast_pkts (BIGINT): Outgoing multicast packets
		- out_octets (BIGINT): Outgoing octets (bytes)
		- out_pkts (BIGINT): Outgoing packets
		- out_unicast_pkts (BIGINT): Outgoing unicast packets
		- carrier_transitions_delta (BIGINT): Change in carrier transitions from previous measurement
		- in_broadcast_pkts_delta (BIGINT): Change in incoming broadcast packets from previous measurement
		- in_discards_delta (BIGINT): Change in incoming discarded packets from previous measurement
		- in_errors_delta (BIGINT): Change in incoming error packets from previous measurement
		- in_fcs_errors_delta (BIGINT): Change in incoming FCS error packets from previous measurement
		- in_multicast_pkts_delta (BIGINT): Change in incoming multicast packets from previous measurement
		- in_octets_delta (BIGINT): Change in incoming octets from previous measurement
		- in_pkts_delta (BIGINT): Change in incoming packets from previous measurement
		- in_unicast_pkts_delta (BIGINT): Change in incoming unicast packets from previous measurement
		- out_broadcast_pkts_delta (BIGINT): Change in outgoing broadcast packets from previous measurement
		- out_discards_delta (BIGINT): Change in outgoing discarded packets from previous measurement
		- out_errors_delta (BIGINT): Change in outgoing error packets from previous measurement
		- out_multicast_pkts_delta (BIGINT): Change in outgoing multicast packets from previous measurement
		- out_octets_delta (BIGINT): Change in outgoing octets from previous measurement
		- out_pkts_delta (BIGINT): Change in outgoing packets from previous measurement
		- out_unicast_pkts_delta (BIGINT): Change in outgoing unicast packets from previous measurement
		- delta_duration (DOUBLE): Time difference in seconds between this measurement and the previous one for the same device/interface
		`,
	},
}
