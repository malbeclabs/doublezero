package dztelemusage

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
)

// MockInfluxDBClient implements InfluxDBClient using real topology from ClickHouse
// to generate synthetic device interface counter data.
type MockInfluxDBClient struct {
	clickhouse clickhouse.Client
	log        *slog.Logger

	// Cached topology data
	mu         sync.RWMutex
	topology   *mockTopology
	lastUpdate time.Time
}

// MockInfluxDBClientConfig contains configuration for the mock client
type MockInfluxDBClientConfig struct {
	ClickHouse clickhouse.Client
	Logger     *slog.Logger
}

// mockTopology holds the cached topology data from ClickHouse
type mockTopology struct {
	// devices maps device_pk -> device info
	devices map[string]*mockDevice
	// linkLookup maps "device_pk:iface_name" -> link info
	linkLookup map[string]*mockLinkInfo
	// tunnelLookup maps "device_pk:tunnel_id" -> user_tunnel_id
	tunnelLookup map[string]int64
}

type mockDevice struct {
	pk         string
	code       string
	interfaces []mockInterface
}

type mockInterface struct {
	name   string
	ip     string
	status string
}

type mockLinkInfo struct {
	linkPK   string
	linkSide string // "A" or "Z"
}

// NewMockInfluxDBClient creates a new mock InfluxDB client that generates
// synthetic device interface counter data based on real topology from ClickHouse.
func NewMockInfluxDBClient(cfg MockInfluxDBClientConfig) *MockInfluxDBClient {
	return &MockInfluxDBClient{
		clickhouse: cfg.ClickHouse,
		log:        cfg.Logger,
	}
}

// QuerySQL implements InfluxDBClient.QuerySQL by generating mock counter data
func (c *MockInfluxDBClient) QuerySQL(ctx context.Context, sqlQuery string) ([]map[string]any, error) {
	// Detect baseline queries (contain ROW_NUMBER) and return empty results
	// Mock data has no historical data before the query window
	if strings.Contains(sqlQuery, "ROW_NUMBER") {
		c.log.Debug("mock influxdb: baseline query detected, returning empty results")
		return []map[string]any{}, nil
	}

	// Parse time range from SQL query
	startTime, endTime, err := parseTimeRange(sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to parse time range: %w", err)
	}

	// Refresh topology if needed (cache for 1 minute)
	if err := c.refreshTopologyIfNeeded(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh topology: %w", err)
	}

	// Generate mock data
	return c.generateMockData(startTime, endTime)
}

// Close implements InfluxDBClient.Close
func (c *MockInfluxDBClient) Close() error {
	return nil
}

// refreshTopologyIfNeeded refreshes the cached topology from ClickHouse if stale
func (c *MockInfluxDBClient) refreshTopologyIfNeeded(ctx context.Context) error {
	c.mu.RLock()
	needsRefresh := c.topology == nil || time.Since(c.lastUpdate) > time.Minute
	c.mu.RUnlock()

	if !needsRefresh {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.topology != nil && time.Since(c.lastUpdate) <= time.Minute {
		return nil
	}

	topology, err := c.loadTopology(ctx)
	if err != nil {
		return err
	}

	c.topology = topology
	c.lastUpdate = time.Now()
	c.log.Info("mock influxdb: topology refreshed",
		"devices", len(topology.devices),
		"links", len(topology.linkLookup),
		"tunnels", len(topology.tunnelLookup))

	return nil
}

// loadTopology loads topology data from ClickHouse dimension tables
func (c *MockInfluxDBClient) loadTopology(ctx context.Context) (*mockTopology, error) {
	topology := &mockTopology{
		devices:      make(map[string]*mockDevice),
		linkLookup:   make(map[string]*mockLinkInfo),
		tunnelLookup: make(map[string]int64),
	}

	conn, err := c.clickhouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	// Load devices with interfaces
	if err := c.loadDevices(ctx, conn, topology); err != nil {
		return nil, fmt.Errorf("failed to load devices: %w", err)
	}

	// Load links for interface -> link mapping
	if err := c.loadLinks(ctx, conn, topology); err != nil {
		return nil, fmt.Errorf("failed to load links: %w", err)
	}

	// Load users for tunnel interface mapping
	if err := c.loadUsers(ctx, conn, topology); err != nil {
		return nil, fmt.Errorf("failed to load users: %w", err)
	}

	return topology, nil
}

// deviceInterfaceJSON matches the JSON structure in the interfaces column
type deviceInterfaceJSON struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Status string `json:"status"`
}

func (c *MockInfluxDBClient) loadDevices(ctx context.Context, conn clickhouse.Connection, topology *mockTopology) error {
	rows, err := conn.Query(ctx, "SELECT pk, code, interfaces FROM dz_devices_current")
	if err != nil {
		return fmt.Errorf("failed to query devices: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var pk, code, interfacesJSON string
		if err := rows.Scan(&pk, &code, &interfacesJSON); err != nil {
			return fmt.Errorf("failed to scan device row: %w", err)
		}

		device := &mockDevice{
			pk:   pk,
			code: code,
		}

		// Parse interfaces JSON
		if interfacesJSON != "" && interfacesJSON != "[]" {
			var ifaces []deviceInterfaceJSON
			if err := json.Unmarshal([]byte(interfacesJSON), &ifaces); err != nil {
				c.log.Warn("mock influxdb: failed to parse interfaces JSON",
					"device_pk", pk, "error", err)
			} else {
				for _, iface := range ifaces {
					device.interfaces = append(device.interfaces, mockInterface{
						name:   iface.Name,
						ip:     iface.IP,
						status: iface.Status,
					})
				}
			}
		}

		// If no interfaces from JSON, add some default interfaces
		if len(device.interfaces) == 0 {
			device.interfaces = []mockInterface{
				{name: "eth0", status: "up"},
				{name: "eth1", status: "up"},
				{name: "Loopback0", status: "up"},
			}
		}

		topology.devices[pk] = device
	}

	return nil
}

func (c *MockInfluxDBClient) loadLinks(ctx context.Context, conn clickhouse.Connection, topology *mockTopology) error {
	query := `
		SELECT
			pk as link_pk,
			side_a_pk,
			side_z_pk,
			side_a_iface_name,
			side_z_iface_name
		FROM dz_links_current
		WHERE side_a_pk != '' AND side_z_pk != ''
	`
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query links: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var linkPK, sideAPK, sideZPK, sideAIface, sideZIface string
		if err := rows.Scan(&linkPK, &sideAPK, &sideZPK, &sideAIface, &sideZIface); err != nil {
			return fmt.Errorf("failed to scan link row: %w", err)
		}

		// Add side A mapping
		if sideAPK != "" && sideAIface != "" {
			key := fmt.Sprintf("%s:%s", sideAPK, sideAIface)
			topology.linkLookup[key] = &mockLinkInfo{
				linkPK:   linkPK,
				linkSide: "A",
			}
		}

		// Add side Z mapping
		if sideZPK != "" && sideZIface != "" {
			key := fmt.Sprintf("%s:%s", sideZPK, sideZIface)
			topology.linkLookup[key] = &mockLinkInfo{
				linkPK:   linkPK,
				linkSide: "Z",
			}
		}
	}

	return nil
}

func (c *MockInfluxDBClient) loadUsers(ctx context.Context, conn clickhouse.Connection, topology *mockTopology) error {
	rows, err := conn.Query(ctx, "SELECT device_pk, tunnel_id FROM dz_users_current WHERE device_pk != ''")
	if err != nil {
		return fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var devicePK string
		var tunnelID int32 // ClickHouse stores as Int32
		if err := rows.Scan(&devicePK, &tunnelID); err != nil {
			return fmt.Errorf("failed to scan user row: %w", err)
		}

		// Map device_pk + Tunnel<id> interface name to tunnel_id
		key := fmt.Sprintf("%s:Tunnel%d", devicePK, tunnelID)
		topology.tunnelLookup[key] = int64(tunnelID)
	}

	return nil
}

// generateMockData generates mock counter data for all devices and interfaces
func (c *MockInfluxDBClient) generateMockData(startTime, endTime time.Time) ([]map[string]any, error) {
	c.mu.RLock()
	topology := c.topology
	c.mu.RUnlock()

	if topology == nil {
		return []map[string]any{}, nil
	}

	// Generate data points every 5 minutes
	interval := 5 * time.Minute
	var results []map[string]any

	for _, device := range topology.devices {
		for _, iface := range device.interfaces {
			// Generate deterministic seed based on device and interface
			seed := hashSeed(device.pk, iface.name)

			// Generate data points for each interval
			for t := startTime; t.Before(endTime); t = t.Add(interval) {
				row := c.generateCounterRow(device, iface, t, seed)
				results = append(results, row)
			}
		}
	}

	c.log.Debug("mock influxdb: generated mock data",
		"rows", len(results),
		"start", startTime,
		"end", endTime)

	return results, nil
}

// counterReferenceTime is a fixed reference point for counter calculations.
// Counters represent cumulative bytes/packets since this reference time,
// simulating an interface that was reset/initialized at this point.
var counterReferenceTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// generateCounterRow generates a single counter row for a device/interface at a given time
func (c *MockInfluxDBClient) generateCounterRow(device *mockDevice, iface mockInterface, t time.Time, seed uint64) map[string]any {
	// Calculate elapsed seconds since our reference time (simulating interface uptime)
	elapsed := t.Sub(counterReferenceTime).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}

	// Get interface capacity and base utilization for this interface
	// These are stable per-interface to ensure counters always increase
	capacityBps := c.getInterfaceCapacity(iface.name, seed)
	baseUtilization := c.getBaseUtilization(iface.name, seed)

	// Calculate cumulative bytes based on capacity * average_utilization * time
	// This ensures counters always increase monotonically
	// Divide by 8 to convert bits to bytes
	avgBytesPerSecond := capacityBps * baseUtilization / 8.0

	// Add per-interface variation based on seed (±5%)
	seedVariation := 1.0 + (float64(seed%100)-50.0)/1000.0

	// Calculate cumulative counters
	inOctets := int64(elapsed * avgBytesPerSecond * seedVariation)
	// Outbound is typically slightly less than inbound (responses are smaller than requests)
	outOctets := int64(elapsed * avgBytesPerSecond * seedVariation * 0.85)

	// Packets: realistic average packet sizes vary by interface type
	avgPacketSize := c.getAvgPacketSize(iface.name)
	inPkts := inOctets / avgPacketSize
	outPkts := outOctets / avgPacketSize

	// Traffic breakdown - varies by interface type
	var unicastPct, multicastPct, broadcastPct int64
	if strings.HasPrefix(iface.name, "Loopback") {
		// Loopback is almost all unicast
		unicastPct, multicastPct, broadcastPct = 100, 0, 0
	} else if strings.HasPrefix(iface.name, "Tunnel") {
		// Tunnels are encapsulated, nearly all unicast
		unicastPct, multicastPct, broadcastPct = 99, 1, 0
	} else {
		// Physical interfaces have some multicast/broadcast (routing protocols, ARP, etc.)
		unicastPct, multicastPct, broadcastPct = 97, 2, 1
	}

	inUnicastPkts := inPkts * unicastPct / 100
	outUnicastPkts := outPkts * unicastPct / 100
	inMulticastPkts := inPkts * multicastPct / 100
	outMulticastPkts := outPkts * multicastPct / 100
	inBroadcastPkts := inPkts * broadcastPct / 100
	outBroadcastPkts := outPkts * broadcastPct / 100

	row := map[string]any{
		"time":                 t.UTC().Format("2006-01-02 15:04:05.999999999 +0000 UTC"),
		"dzd_pubkey":           device.pk,
		"host":                 device.code,
		"intf":                 iface.name,
		"model_name":           "MockModel",
		"serial_number":        fmt.Sprintf("MOCK%s", truncateString(device.pk, 8)),
		"carrier-transitions":  int64(0),
		"in-broadcast-pkts":    inBroadcastPkts,
		"in-multicast-pkts":    inMulticastPkts,
		"in-octets":            inOctets,
		"in-pkts":              inPkts,
		"in-unicast-pkts":      inUnicastPkts,
		"out-broadcast-pkts":   outBroadcastPkts,
		"out-multicast-pkts":   outMulticastPkts,
		"out-octets":           outOctets,
		"out-pkts":             outPkts,
		"out-unicast-pkts":     outUnicastPkts,
	}

	// Sparse counters (errors/discards): mostly null, occasionally small values
	// Use deterministic "randomness" based on seed and time interval
	timeInterval := uint64(t.Unix() / 300) // Changes every 5 minutes
	combined := seed ^ timeInterval

	// Errors are rare on healthy interfaces (~2-5% of intervals)
	// Values are cumulative, so they should increase over time
	baseErrors := int64(timeInterval - uint64(counterReferenceTime.Unix()/300))

	if combined%40 == 0 {
		// Occasional in-errors (CRC errors, runts, etc.)
		row["in-errors"] = baseErrors/40 + int64(combined%3)
	}
	if combined%50 == 0 {
		// Out-errors even more rare
		row["out-errors"] = baseErrors/50 + int64(combined%2)
	}
	if combined%30 == 0 {
		// Discards happen during congestion
		row["in-discards"] = baseErrors/30 + int64(combined%5)
	}
	if combined%35 == 0 {
		row["out-discards"] = baseErrors/35 + int64(combined%5)
	}
	if combined%100 == 0 {
		// FCS errors are rare (usually indicates physical layer issues)
		row["in-fcs-errors"] = baseErrors/100 + int64(combined%2)
	}

	return row
}

// getInterfaceCapacity returns the interface capacity in bps based on interface type
func (c *MockInfluxDBClient) getInterfaceCapacity(ifaceName string, seed uint64) float64 {
	switch {
	case strings.HasPrefix(ifaceName, "Tunnel"):
		// Tunnel interfaces: capacity varies (100 Mbps - 1 Gbps)
		return float64(100_000_000 + (seed%900_000_000))
	case strings.HasPrefix(ifaceName, "Loopback"):
		// Loopback: typically just control plane traffic
		return float64(100_000_000) // 100 Mbps nominal
	case strings.HasPrefix(ifaceName, "eth") || strings.HasPrefix(ifaceName, "Ethernet"):
		// Physical interfaces: typically 10 Gbps or 100 Gbps
		if seed%3 == 0 {
			return float64(100_000_000_000) // 100 Gbps
		}
		return float64(10_000_000_000) // 10 Gbps
	case strings.HasPrefix(ifaceName, "Port-Channel"):
		// Port channels: aggregated bandwidth
		return float64(40_000_000_000) // 40 Gbps typical
	default:
		return float64(10_000_000_000) // 10 Gbps default
	}
}

// getBaseUtilization returns a stable average utilization percentage (0.0-1.0) for an interface.
// This is deterministic per interface to ensure cumulative counters always increase monotonically.
func (c *MockInfluxDBClient) getBaseUtilization(ifaceName string, seed uint64) float64 {
	switch {
	case strings.HasPrefix(ifaceName, "Loopback"):
		// Loopback has minimal traffic (control plane only)
		return 0.001 // 0.1%
	case strings.HasPrefix(ifaceName, "Tunnel"):
		// Tunnel utilization varies by user (15-45%)
		return 0.15 + float64(seed%30)/100.0
	default:
		// Physical interfaces: backbone traffic (25-60%)
		return 0.25 + float64(seed%35)/100.0
	}
}

// getAvgPacketSize returns realistic average packet size based on interface type
func (c *MockInfluxDBClient) getAvgPacketSize(ifaceName string) int64 {
	switch {
	case strings.HasPrefix(ifaceName, "Loopback"):
		// Control plane: smaller packets (BGP, OSPF, etc.)
		return 200
	case strings.HasPrefix(ifaceName, "Tunnel"):
		// Encapsulated traffic: larger due to overhead
		return 1200
	default:
		// Mixed traffic: average around 800 bytes
		// (mix of small ACKs and larger data packets)
		return 800
	}
}


// parseTimeRange extracts start and end times from an InfluxDB SQL query
func parseTimeRange(sqlQuery string) (time.Time, time.Time, error) {
	// Match patterns like: time >= '2024-01-01T00:00:00Z' AND time < '2024-01-01T01:00:00Z'
	timePattern := regexp.MustCompile(`time\s*>=\s*'([^']+)'\s+AND\s+time\s*<\s*'([^']+)'`)
	matches := timePattern.FindStringSubmatch(sqlQuery)

	if len(matches) != 3 {
		// Default to last hour if we can't parse
		now := time.Now().UTC()
		return now.Add(-time.Hour), now, nil
	}

	startTime, err := parseInfluxTime(matches[1])
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to parse start time: %w", err)
	}

	endTime, err := parseInfluxTime(matches[2])
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to parse end time: %w", err)
	}

	return startTime, endTime, nil
}

// parseInfluxTime parses a time string in various formats
func parseInfluxTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

// hashSeed creates a deterministic hash from device PK and interface name
func hashSeed(devicePK, ifaceName string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(devicePK))
	h.Write([]byte(":"))
	h.Write([]byte(ifaceName))
	return h.Sum64()
}

// truncateString returns up to maxLen characters from s
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
