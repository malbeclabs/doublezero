package dztelemusage

import (
	schematypes "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/schema"
)

var Schema = &schematypes.Schema{
	Name: "doublezero-telemetry-usage",
	Description: `
Use for DZ interface usage and utilization statistics:
- Interface packet and octet counters (in/out)
- Error and discard statistics
- Device metadata (interface, model, serial)
- Time-series interface statistics

DEVICE UTILIZATION METRICS RULES:
- Utilization is defined by throughput rate, not total transferred volume.
- Total bytes/GB are contextual only and must not be used to characterize load or saturation.
- Report and reason in Gbps/Mbps by default; include totals only when explicitly requested or when explaining rate derivation.
- Interface counters (in/out octets, packets) are cumulative and passively sampled; never sum counters.
- Compute rates as (last - first) / delta_duration.
- Report rates by default; do not report totals unless explicitly requested.
- Convert octets to Gbps: (octets * 8) / delta_duration / 1e9.
- Prefer Gbps; use Mbps only when values are < 1 Gbps.
- Exclude zero or negative deltas.
- Counter deltas indicate traffic occurred during the measurement interval; they do not imply continuous, instantaneous, or “active” transmission.
- Do not aggregate in/out directions together; account for in/out duplication across devices.

DEVICE UTILIZATION AGGREGATION RULES:
- Interface counters are per-interface; summing deltas within the same timestamp represents aggregate device load.
- To show device load from user traffic, sum interface deltas across all user tunnels per device per timestamp, then compute rates.
- Do NOT average per-user samples to infer device throughput; this obscures true load.
- For device-level reporting, compute average, p95, and peak rates from the per-timestamp summed totals.
- For per-user analysis, compute rates per user first, then aggregate statistics across users separately.
- Call out anomalies or outliers when present.
`,
	Tables: []schematypes.TableInfo{
		{
			Name:        "dz_device_iface_usage",
			Description: "Interface usage/utilization (cumulative counters + deltas). Joins: dz_device_iface_usage.device_pk = dz_devices.pk. For per-user attribution, join to users using (device_pk, user_tunnel_id) = (users.device_pk, users.tunnel_id) (do not join on tunnel_id alone).",
			Columns: []schematypes.ColumnInfo{
				{Name: "time", Type: "TIMESTAMP", Description: "Timestamp of the measurement"},
				{Name: "device_pk", Type: "VARCHAR", Description: "Foreign key → dz_devices.pk (DoubleZero device public key)"},
				{Name: "host", Type: "VARCHAR", Description: "Host identifier (INTERNAL USE ONLY)"},
				{Name: "intf", Type: "VARCHAR", Description: "Interface name"},
				{Name: "user_tunnel_id", Type: "BIGINT", Description: "Tunnel ID extracted from interface name (e.g., Tunnel501 -> 501). Join to users using the composite key (device_pk, user_tunnel_id) = (users.device_pk, users.tunnel_id); tunnel_id alone is not globally unique."},
				{Name: "link_pk", Type: "VARCHAR", Description: "Foreign key → dz_links.pk. Populated by matching (device_pk, intf) to link side A or Z."},
				{Name: "link_side", Type: "VARCHAR", Description: "Link side: 'A' or 'Z'. Indicates which side of the link this interface belongs to."},
				{Name: "model_name", Type: "VARCHAR", Description: "Device model name"},
				{Name: "serial_number", Type: "VARCHAR", Description: "Device serial number"},
				{Name: "carrier_transitions", Type: "BIGINT", Description: "Number of carrier transitions"},
				{Name: "in_broadcast_pkts", Type: "BIGINT", Description: "Incoming broadcast packets"},
				{Name: "in_discards", Type: "BIGINT", Description: "Incoming discarded packets"},
				{Name: "in_errors", Type: "BIGINT", Description: "Incoming error packets"},
				{Name: "in_fcs_errors", Type: "BIGINT", Description: "Incoming FCS error packets"},
				{Name: "in_multicast_pkts", Type: "BIGINT", Description: "Incoming multicast packets"},
				{Name: "in_octets", Type: "BIGINT", Description: "Incoming octets (bytes)"},
				{Name: "in_pkts", Type: "BIGINT", Description: "Incoming packets"},
				{Name: "in_unicast_pkts", Type: "BIGINT", Description: "Incoming unicast packets"},
				{Name: "out_broadcast_pkts", Type: "BIGINT", Description: "Outgoing broadcast packets"},
				{Name: "out_discards", Type: "BIGINT", Description: "Outgoing discarded packets"},
				{Name: "out_errors", Type: "BIGINT", Description: "Outgoing error packets"},
				{Name: "out_multicast_pkts", Type: "BIGINT", Description: "Outgoing multicast packets"},
				{Name: "out_octets", Type: "BIGINT", Description: "Outgoing octets (bytes)"},
				{Name: "out_pkts", Type: "BIGINT", Description: "Outgoing packets"},
				{Name: "out_unicast_pkts", Type: "BIGINT", Description: "Outgoing unicast packets"},
				// Delta fields (change from previous value)
				{Name: "carrier_transitions_delta", Type: "BIGINT", Description: "Change in carrier transitions from previous measurement"},
				{Name: "in_broadcast_pkts_delta", Type: "BIGINT", Description: "Change in incoming broadcast packets from previous measurement"},
				{Name: "in_discards_delta", Type: "BIGINT", Description: "Change in incoming discarded packets from previous measurement"},
				{Name: "in_errors_delta", Type: "BIGINT", Description: "Change in incoming error packets from previous measurement"},
				{Name: "in_fcs_errors_delta", Type: "BIGINT", Description: "Change in incoming FCS error packets from previous measurement"},
				{Name: "in_multicast_pkts_delta", Type: "BIGINT", Description: "Change in incoming multicast packets from previous measurement"},
				{Name: "in_octets_delta", Type: "BIGINT", Description: "Change in incoming octets from previous measurement"},
				{Name: "in_pkts_delta", Type: "BIGINT", Description: "Change in incoming packets from previous measurement"},
				{Name: "in_unicast_pkts_delta", Type: "BIGINT", Description: "Change in incoming unicast packets from previous measurement"},
				{Name: "out_broadcast_pkts_delta", Type: "BIGINT", Description: "Change in outgoing broadcast packets from previous measurement"},
				{Name: "out_discards_delta", Type: "BIGINT", Description: "Change in outgoing discarded packets from previous measurement"},
				{Name: "out_errors_delta", Type: "BIGINT", Description: "Change in outgoing error packets from previous measurement"},
				{Name: "out_multicast_pkts_delta", Type: "BIGINT", Description: "Change in outgoing multicast packets from previous measurement"},
				{Name: "out_octets_delta", Type: "BIGINT", Description: "Change in outgoing octets from previous measurement"},
				{Name: "out_pkts_delta", Type: "BIGINT", Description: "Change in outgoing packets from previous measurement"},
				{Name: "out_unicast_pkts_delta", Type: "BIGINT", Description: "Change in outgoing unicast packets from previous measurement"},
				{Name: "delta_duration", Type: "DOUBLE", Description: "Time difference in seconds between this measurement and the previous one for the same device/interface"},
			},
		},
	},
}
