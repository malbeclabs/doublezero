package isis

// LSP represents an IS-IS Link State PDU from a router.
type LSP struct {
	SystemID  string     // IS-IS system ID, e.g., "ac10.0001.0000.00-00"
	Hostname  string     // Router hostname, e.g., "DZ-NY7-SW01"
	RouterID  string     // Router ID from capabilities, e.g., "172.16.0.1"
	Neighbors []Neighbor // Adjacent neighbors
}

// Neighbor represents an IS-IS adjacency to a neighboring router.
type Neighbor struct {
	SystemID     string   // Neighbor's IS-IS system ID
	Metric       uint32   // IS-IS metric (latency in microseconds)
	NeighborAddr string   // IP address of neighbor interface
	AdjSIDs      []uint32 // Segment routing adjacency SIDs
}
