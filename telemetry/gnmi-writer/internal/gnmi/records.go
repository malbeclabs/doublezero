package gnmi

import "time"

// IsisAdjacencyRecord represents an ISIS adjacency for storage in ClickHouse.
type IsisAdjacencyRecord struct {
	Timestamp           time.Time `json:"timestamp" ch:"timestamp"`
	DevicePubkey        string    `json:"device_pubkey" ch:"device_pubkey"`
	InterfaceID         string    `json:"interface_id" ch:"interface_id"`
	Level               uint8     `json:"level" ch:"level"`
	SystemID            string    `json:"system_id" ch:"system_id"`
	AdjacencyState      string    `json:"adjacency_state" ch:"adjacency_state"`
	NeighborIPv4        string    `json:"neighbor_ipv4,omitempty" ch:"neighbor_ipv4"`
	NeighborIPv6        string    `json:"neighbor_ipv6,omitempty" ch:"neighbor_ipv6"`
	NeighborCircuitType string    `json:"neighbor_circuit_type,omitempty" ch:"neighbor_circuit_type"`
	AreaAddress         string    `json:"area_address,omitempty" ch:"area_address"`
	UpTimestamp         int64     `json:"up_timestamp,omitempty" ch:"up_timestamp"`
	LocalCircuitID      uint32    `json:"local_circuit_id,omitempty" ch:"local_circuit_id"`
	NeighborCircuitID   uint32    `json:"neighbor_circuit_id,omitempty" ch:"neighbor_circuit_id"`
}

// TableName returns the ClickHouse table name for ISIS adjacencies.
func (r IsisAdjacencyRecord) TableName() string {
	return "isis_adjacencies"
}

// SystemStateRecord represents system state for storage in ClickHouse.
type SystemStateRecord struct {
	Timestamp    time.Time `json:"timestamp" ch:"timestamp"`
	DevicePubkey string    `json:"device_pubkey" ch:"device_pubkey"`
	Hostname     string    `json:"hostname,omitempty" ch:"hostname"`
	MemTotal     uint64    `json:"mem_total,omitempty" ch:"mem_total"`
	MemUsed      uint64    `json:"mem_used,omitempty" ch:"mem_used"`
	MemFree      uint64    `json:"mem_free,omitempty" ch:"mem_free"`
	CpuUser      float64   `json:"cpu_user,omitempty" ch:"cpu_user"`
	CpuSystem    float64   `json:"cpu_system,omitempty" ch:"cpu_system"`
	CpuIdle      float64   `json:"cpu_idle,omitempty" ch:"cpu_idle"`
}

// TableName returns the ClickHouse table name for system state.
func (r SystemStateRecord) TableName() string {
	return "system_state"
}

// BgpNeighborRecord represents a BGP neighbor for storage in ClickHouse.
type BgpNeighborRecord struct {
	Timestamp              time.Time `json:"timestamp" ch:"timestamp"`
	DevicePubkey           string    `json:"device_pubkey" ch:"device_pubkey"`
	NetworkInstance        string    `json:"network_instance" ch:"network_instance"`
	NeighborAddress        string    `json:"neighbor_address" ch:"neighbor_address"`
	Description            string    `json:"description" ch:"description"`
	PeerAs                 uint32    `json:"peer_as" ch:"peer_as"`
	LocalAs                uint32    `json:"local_as" ch:"local_as"`
	PeerType               string    `json:"peer_type" ch:"peer_type"`
	SessionState           string    `json:"session_state" ch:"session_state"`
	EstablishedTransitions uint64    `json:"established_transitions" ch:"established_transitions"`
	LastEstablished        int64     `json:"last_established" ch:"last_established"`
	MessagesReceivedUpdate uint64    `json:"messages_received_update" ch:"messages_received_update"`
	MessagesSentUpdate     uint64    `json:"messages_sent_update" ch:"messages_sent_update"`
}

// TableName returns the ClickHouse table name for BGP neighbors.
func (r BgpNeighborRecord) TableName() string {
	return "bgp_neighbors"
}

// InterfaceIfindexRecord represents an interface ifindex mapping for storage in ClickHouse.
type InterfaceIfindexRecord struct {
	Timestamp     time.Time `json:"timestamp" ch:"timestamp"`
	DevicePubkey  string    `json:"device_pubkey" ch:"device_pubkey"`
	InterfaceName string    `json:"interface_name" ch:"interface_name"`
	Ifindex       uint32    `json:"ifindex" ch:"ifindex"`
}

// TableName returns the ClickHouse table name for interface ifindex mappings.
func (r InterfaceIfindexRecord) TableName() string {
	return "interface_ifindex"
}

// TransceiverStateRecord represents optical transceiver channel state for storage in ClickHouse.
type TransceiverStateRecord struct {
	Timestamp        time.Time `json:"timestamp" ch:"timestamp"`
	DevicePubkey     string    `json:"device_pubkey" ch:"device_pubkey"`
	InterfaceName    string    `json:"interface_name" ch:"interface_name"`
	ChannelIndex     uint16    `json:"channel_index" ch:"channel_index"`
	InputPower       float64   `json:"input_power,omitempty" ch:"input_power"`
	OutputPower      float64   `json:"output_power,omitempty" ch:"output_power"`
	LaserBiasCurrent float64   `json:"laser_bias_current,omitempty" ch:"laser_bias_current"`
}

// TableName returns the ClickHouse table name for transceiver state records.
func (r TransceiverStateRecord) TableName() string {
	return "transceiver_state"
}

// transceiverStateKey is used to group TransceiverStateRecords for aggregation.
type transceiverStateKey struct {
	Timestamp     int64
	DevicePubkey  string
	InterfaceName string
	ChannelIndex  uint16
}

// AggregateTransceiverState merges TransceiverStateRecords with the same
// (timestamp, device_pubkey, interface_name, channel_index) key into a single record.
// gNMI may send individual updates for each power metric, so this function combines
// them into complete rows.
func AggregateTransceiverState(records []Record) []Record {
	var result []Record
	stateMap := make(map[transceiverStateKey]*TransceiverStateRecord)

	for _, r := range records {
		state, ok := r.(TransceiverStateRecord)
		if !ok {
			// Keep non-state records as-is
			result = append(result, r)
			continue
		}

		key := transceiverStateKey{
			Timestamp:     state.Timestamp.UnixNano(),
			DevicePubkey:  state.DevicePubkey,
			InterfaceName: state.InterfaceName,
			ChannelIndex:  state.ChannelIndex,
		}

		existing, found := stateMap[key]
		if !found {
			// First record for this key - store a copy
			copy := state
			stateMap[key] = &copy
			continue
		}

		// Merge non-zero values into existing record
		if state.InputPower != 0 {
			existing.InputPower = state.InputPower
		}
		if state.OutputPower != 0 {
			existing.OutputPower = state.OutputPower
		}
		if state.LaserBiasCurrent != 0 {
			existing.LaserBiasCurrent = state.LaserBiasCurrent
		}
	}

	// Append aggregated state records
	for _, state := range stateMap {
		result = append(result, *state)
	}

	return result
}

// InterfaceStateRecord represents interface state for storage in ClickHouse.
type InterfaceStateRecord struct {
	Timestamp          time.Time `json:"timestamp" ch:"timestamp"`
	DevicePubkey       string    `json:"device_pubkey" ch:"device_pubkey"`
	InterfaceName      string    `json:"interface_name" ch:"interface_name"`
	AdminStatus        string    `json:"admin_status" ch:"admin_status"`
	OperStatus         string    `json:"oper_status" ch:"oper_status"`
	Ifindex            uint32    `json:"ifindex,omitempty" ch:"ifindex"`
	Mtu                uint16    `json:"mtu,omitempty" ch:"mtu"`
	LastChange         int64     `json:"last_change,omitempty" ch:"last_change"`
	CarrierTransitions uint64    `json:"carrier_transitions,omitempty" ch:"carrier_transitions"`
	InOctets           uint64    `json:"in_octets,omitempty" ch:"in_octets"`
	OutOctets          uint64    `json:"out_octets,omitempty" ch:"out_octets"`
	InPkts             uint64    `json:"in_pkts,omitempty" ch:"in_pkts"`
	OutPkts            uint64    `json:"out_pkts,omitempty" ch:"out_pkts"`
	InErrors           uint64    `json:"in_errors,omitempty" ch:"in_errors"`
	OutErrors          uint64    `json:"out_errors,omitempty" ch:"out_errors"`
	InDiscards         uint64    `json:"in_discards,omitempty" ch:"in_discards"`
	OutDiscards        uint64    `json:"out_discards,omitempty" ch:"out_discards"`
}

// TableName returns the ClickHouse table name for interface state records.
func (r InterfaceStateRecord) TableName() string {
	return "interface_state"
}

// TransceiverThresholdRecord represents transceiver alarm thresholds for storage in ClickHouse.
type TransceiverThresholdRecord struct {
	Timestamp              time.Time `json:"timestamp" ch:"timestamp"`
	DevicePubkey           string    `json:"device_pubkey" ch:"device_pubkey"`
	InterfaceName          string    `json:"interface_name" ch:"interface_name"`
	Severity               string    `json:"severity" ch:"severity"`
	InputPowerLower        float64   `json:"input_power_lower,omitempty" ch:"input_power_lower"`
	InputPowerUpper        float64   `json:"input_power_upper,omitempty" ch:"input_power_upper"`
	OutputPowerLower       float64   `json:"output_power_lower,omitempty" ch:"output_power_lower"`
	OutputPowerUpper       float64   `json:"output_power_upper,omitempty" ch:"output_power_upper"`
	LaserBiasCurrentLower  float64   `json:"laser_bias_current_lower,omitempty" ch:"laser_bias_current_lower"`
	LaserBiasCurrentUpper  float64   `json:"laser_bias_current_upper,omitempty" ch:"laser_bias_current_upper"`
	ModuleTemperatureLower float64   `json:"module_temperature_lower,omitempty" ch:"module_temperature_lower"`
	ModuleTemperatureUpper float64   `json:"module_temperature_upper,omitempty" ch:"module_temperature_upper"`
	SupplyVoltageLower     float64   `json:"supply_voltage_lower,omitempty" ch:"supply_voltage_lower"`
	SupplyVoltageUpper     float64   `json:"supply_voltage_upper,omitempty" ch:"supply_voltage_upper"`
}

// TableName returns the ClickHouse table name for transceiver thresholds.
func (r TransceiverThresholdRecord) TableName() string {
	return "transceiver_thresholds"
}

// thresholdKey is used to group TransceiverThresholdRecords for aggregation.
type thresholdKey struct {
	Timestamp     int64
	DevicePubkey  string
	InterfaceName string
	Severity      string
}

// AggregateTransceiverThresholds merges TransceiverThresholdRecords with the same
// (timestamp, device_pubkey, interface_name, severity) key into a single record.
// gNMI sends individual updates for each threshold field, so this function combines
// them into complete rows.
func AggregateTransceiverThresholds(records []Record) []Record {
	var result []Record
	thresholdMap := make(map[thresholdKey]*TransceiverThresholdRecord)

	for _, r := range records {
		threshold, ok := r.(TransceiverThresholdRecord)
		if !ok {
			// Keep non-threshold records as-is
			result = append(result, r)
			continue
		}

		key := thresholdKey{
			Timestamp:     threshold.Timestamp.UnixNano(),
			DevicePubkey:  threshold.DevicePubkey,
			InterfaceName: threshold.InterfaceName,
			Severity:      threshold.Severity,
		}

		existing, found := thresholdMap[key]
		if !found {
			// First record for this key - store a copy
			copy := threshold
			thresholdMap[key] = &copy
			continue
		}

		// Merge non-zero values into existing record
		if threshold.InputPowerLower != 0 {
			existing.InputPowerLower = threshold.InputPowerLower
		}
		if threshold.InputPowerUpper != 0 {
			existing.InputPowerUpper = threshold.InputPowerUpper
		}
		if threshold.OutputPowerLower != 0 {
			existing.OutputPowerLower = threshold.OutputPowerLower
		}
		if threshold.OutputPowerUpper != 0 {
			existing.OutputPowerUpper = threshold.OutputPowerUpper
		}
		if threshold.LaserBiasCurrentLower != 0 {
			existing.LaserBiasCurrentLower = threshold.LaserBiasCurrentLower
		}
		if threshold.LaserBiasCurrentUpper != 0 {
			existing.LaserBiasCurrentUpper = threshold.LaserBiasCurrentUpper
		}
		if threshold.ModuleTemperatureLower != 0 {
			existing.ModuleTemperatureLower = threshold.ModuleTemperatureLower
		}
		if threshold.ModuleTemperatureUpper != 0 {
			existing.ModuleTemperatureUpper = threshold.ModuleTemperatureUpper
		}
		if threshold.SupplyVoltageLower != 0 {
			existing.SupplyVoltageLower = threshold.SupplyVoltageLower
		}
		if threshold.SupplyVoltageUpper != 0 {
			existing.SupplyVoltageUpper = threshold.SupplyVoltageUpper
		}
	}

	// Append aggregated threshold records
	for _, threshold := range thresholdMap {
		result = append(result, *threshold)
	}

	return result
}
