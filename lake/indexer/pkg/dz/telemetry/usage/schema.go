package dztelemusage

import (
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
)

type DeviceInterfaceCountersSchema struct{}

func (s *DeviceInterfaceCountersSchema) Name() string {
	return "dz_device_interface_counters"
}

func (s *DeviceInterfaceCountersSchema) UniqueKeyColumns() []string {
	return []string{"event_ts", "device_pk", "intf"}
}

func (s *DeviceInterfaceCountersSchema) Columns() []string {
	return []string{
		"ingested_at:TIMESTAMP",
		"device_pk:VARCHAR",
		"host:VARCHAR",
		"intf:VARCHAR",
		"user_tunnel_id:BIGINT",
		"link_pk:VARCHAR",
		"link_side:VARCHAR",
		"model_name:VARCHAR",
		"serial_number:VARCHAR",
		"carrier_transitions:BIGINT",
		"in_broadcast_pkts:BIGINT",
		"in_discards:BIGINT",
		"in_errors:BIGINT",
		"in_fcs_errors:BIGINT",
		"in_multicast_pkts:BIGINT",
		"in_octets:BIGINT",
		"in_pkts:BIGINT",
		"in_unicast_pkts:BIGINT",
		"out_broadcast_pkts:BIGINT",
		"out_discards:BIGINT",
		"out_errors:BIGINT",
		"out_multicast_pkts:BIGINT",
		"out_octets:BIGINT",
		"out_pkts:BIGINT",
		"out_unicast_pkts:BIGINT",
		"carrier_transitions_delta:BIGINT",
		"in_broadcast_pkts_delta:BIGINT",
		"in_discards_delta:BIGINT",
		"in_errors_delta:BIGINT",
		"in_fcs_errors_delta:BIGINT",
		"in_multicast_pkts_delta:BIGINT",
		"in_octets_delta:BIGINT",
		"in_pkts_delta:BIGINT",
		"in_unicast_pkts_delta:BIGINT",
		"out_broadcast_pkts_delta:BIGINT",
		"out_discards_delta:BIGINT",
		"out_errors_delta:BIGINT",
		"out_multicast_pkts_delta:BIGINT",
		"out_octets_delta:BIGINT",
		"out_pkts_delta:BIGINT",
		"out_unicast_pkts_delta:BIGINT",
		"delta_duration:DOUBLE",
	}
}

func (s *DeviceInterfaceCountersSchema) TimeColumn() string {
	return "event_ts"
}

func (s *DeviceInterfaceCountersSchema) PartitionByTime() bool {
	return true
}

func (s *DeviceInterfaceCountersSchema) DedupMode() dataset.DedupMode {
	return dataset.DedupReplacing
}

func (s *DeviceInterfaceCountersSchema) DedupVersionColumn() string {
	return "ingested_at"
}

func (s *DeviceInterfaceCountersSchema) ToRow(usage InterfaceUsage, ingestedAt time.Time) []any {
	// Order matches table schema: event_ts first, then all columns from Columns()
	return []any{
		usage.Time.UTC(),              // event_ts
		ingestedAt,                    // ingested_at
		usage.DevicePK,                // device_pk
		usage.Host,                    // host
		usage.Intf,                    // intf
		usage.UserTunnelID,            // user_tunnel_id
		usage.LinkPK,                  // link_pk
		usage.LinkSide,                // link_side
		usage.ModelName,               // model_name
		usage.SerialNumber,            // serial_number
		usage.CarrierTransitions,      // carrier_transitions
		usage.InBroadcastPkts,         // in_broadcast_pkts
		usage.InDiscards,              // in_discards
		usage.InErrors,                // in_errors
		usage.InFCSErrors,             // in_fcs_errors
		usage.InMulticastPkts,         // in_multicast_pkts
		usage.InOctets,                // in_octets
		usage.InPkts,                  // in_pkts
		usage.InUnicastPkts,           // in_unicast_pkts
		usage.OutBroadcastPkts,        // out_broadcast_pkts
		usage.OutDiscards,             // out_discards
		usage.OutErrors,               // out_errors
		usage.OutMulticastPkts,        // out_multicast_pkts
		usage.OutOctets,               // out_octets
		usage.OutPkts,                 // out_pkts
		usage.OutUnicastPkts,          // out_unicast_pkts
		usage.CarrierTransitionsDelta, // carrier_transitions_delta
		usage.InBroadcastPktsDelta,    // in_broadcast_pkts_delta
		usage.InDiscardsDelta,         // in_discards_delta
		usage.InErrorsDelta,           // in_errors_delta
		usage.InFCSErrorsDelta,        // in_fcs_errors_delta
		usage.InMulticastPktsDelta,    // in_multicast_pkts_delta
		usage.InOctetsDelta,           // in_octets_delta
		usage.InPktsDelta,             // in_pkts_delta
		usage.InUnicastPktsDelta,      // in_unicast_pkts_delta
		usage.OutBroadcastPktsDelta,   // out_broadcast_pkts_delta
		usage.OutDiscardsDelta,        // out_discards_delta
		usage.OutErrorsDelta,          // out_errors_delta
		usage.OutMulticastPktsDelta,   // out_multicast_pkts_delta
		usage.OutOctetsDelta,          // out_octets_delta
		usage.OutPktsDelta,            // out_pkts_delta
		usage.OutUnicastPktsDelta,     // out_unicast_pkts_delta
		usage.DeltaDuration,           // delta_duration
	}
}

var deviceInterfaceCountersSchema = &DeviceInterfaceCountersSchema{}

func NewDeviceInterfaceCountersDataset(log *slog.Logger) (*dataset.FactDataset, error) {
	return dataset.NewFactDataset(log, deviceInterfaceCountersSchema)
}
