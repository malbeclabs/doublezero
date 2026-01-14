package gnmi

import "time"

// IsisAdjacencyRecord represents an ISIS adjacency for storage in ClickHouse.
type IsisAdjacencyRecord struct {
	Timestamp           time.Time `json:"timestamp" ch:"timestamp"`
	DeviceCode          string    `json:"device_code" ch:"device_code"`
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
	Timestamp  time.Time `json:"timestamp" ch:"timestamp"`
	DeviceCode string    `json:"device_code" ch:"device_code"`
	Hostname   string    `json:"hostname,omitempty" ch:"hostname"`
	MemTotal   uint64    `json:"mem_total,omitempty" ch:"mem_total"`
	MemUsed    uint64    `json:"mem_used,omitempty" ch:"mem_used"`
	MemFree    uint64    `json:"mem_free,omitempty" ch:"mem_free"`
	CpuUser    float64   `json:"cpu_user,omitempty" ch:"cpu_user"`
	CpuSystem  float64   `json:"cpu_system,omitempty" ch:"cpu_system"`
	CpuIdle    float64   `json:"cpu_idle,omitempty" ch:"cpu_idle"`
}

// TableName returns the ClickHouse table name for system state.
func (r SystemStateRecord) TableName() string {
	return "system_state"
}

// BgpNeighborRecord represents a BGP neighbor for storage in ClickHouse.
type BgpNeighborRecord struct {
	Timestamp              time.Time `json:"timestamp" ch:"timestamp"`
	DeviceCode             string    `json:"device_code" ch:"device_code"`
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
	DeviceCode    string    `json:"device_code" ch:"device_code"`
	InterfaceName string    `json:"interface_name" ch:"interface_name"`
	SubifIndex    uint32    `json:"subif_index" ch:"subif_index"`
	Ifindex       uint32    `json:"ifindex" ch:"ifindex"`
}

// TableName returns the ClickHouse table name for interface ifindex mappings.
func (r InterfaceIfindexRecord) TableName() string {
	return "interface_ifindex"
}
