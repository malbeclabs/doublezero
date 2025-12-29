package analytics

import (
	"slices"
	"strings"
)

// ValidIntervals is an allowlist of permitted interval values to prevent SQL injection.
var ValidIntervals = map[string]bool{
	"":          true, // empty means auto-select
	"1 second":  true,
	"5 second":  true,
	"10 second": true,
	"30 second": true,
	"1 minute":  true,
	"5 minute":  true,
	"15 minute": true,
	"30 minute": true,
	"1 hour":    true,
	"6 hour":    true,
	"12 hour":   true,
	"1 day":     true,
	"7 day":     true,
}

// ColumnRegistry provides efficient column lookup and validation.
type ColumnRegistry struct {
	columns       []ColumnInfo
	columnsByName map[string]ColumnInfo
}

// NewColumnRegistry creates a new ColumnRegistry with the given columns.
func NewColumnRegistry(columns []ColumnInfo) *ColumnRegistry {
	byName := make(map[string]ColumnInfo, len(columns))
	for _, col := range columns {
		byName[col.Name] = col
	}
	return &ColumnRegistry{
		columns:       columns,
		columnsByName: byName,
	}
}

// All returns all columns.
func (r *ColumnRegistry) All() []ColumnInfo {
	return r.columns
}

// IsValidColumn checks if a column name exists.
func (r *ColumnRegistry) IsValidColumn(name string) bool {
	_, ok := r.columnsByName[name]
	return ok
}

// GetColumn returns a column by name, or nil if not found.
func (r *ColumnRegistry) GetColumn(name string) *ColumnInfo {
	if col, ok := r.columnsByName[name]; ok {
		return &col
	}
	return nil
}

// IsNumericColumn checks if a column type is numeric.
func (r *ColumnRegistry) IsNumericColumn(name string) bool {
	col, ok := r.columnsByName[name]
	if !ok {
		return false
	}
	return strings.HasPrefix(col.Type, "UInt") ||
		strings.HasPrefix(col.Type, "Int") ||
		col.Type == "Float64" ||
		col.Type == "Float32"
}

// GetDimensionColumns returns dimension columns sorted alphabetically.
func (r *ColumnRegistry) GetDimensionColumns() []ColumnInfo {
	var dims []ColumnInfo
	for _, col := range r.columns {
		if col.Category == "dimension" {
			dims = append(dims, col)
		}
	}
	slices.SortFunc(dims, func(a, b ColumnInfo) int {
		return strings.Compare(a.Name, b.Name)
	})
	return dims
}

// GetColumnGroups returns columns organized by UI category for display.
func (r *ColumnRegistry) GetColumnGroups() []ColumnGroup {
	// Define category order and display names
	categoryOrder := []struct {
		name        string
		displayName string
	}{
		{"network", "Network"},
		{"location", "Location/Device"},
		{"as", "AS Numbers"},
		{"bgp", "BGP"},
		{"interface", "Interface"},
		{"vlan", "VLAN"},
		{"tcp_ip", "TCP/IP"},
		{"flow", "Flow Metadata"},
	}

	// Group columns by UI category
	grouped := make(map[string][]ColumnInfo)
	for _, col := range r.columns {
		if col.Category == "dimension" {
			grouped[col.UICategory] = append(grouped[col.UICategory], col)
		}
	}

	// Sort columns within each group alphabetically
	for category := range grouped {
		slices.SortFunc(grouped[category], func(a, b ColumnInfo) int {
			return strings.Compare(a.Name, b.Name)
		})
	}

	// Build result in defined order
	var result []ColumnGroup
	for _, cat := range categoryOrder {
		if cols, ok := grouped[cat.name]; ok && len(cols) > 0 {
			result = append(result, ColumnGroup{
				Name:        cat.name,
				DisplayName: cat.displayName,
				Columns:     cols,
			})
		}
	}

	return result
}

// DefaultColumns returns the default column definitions for the flows table.
func DefaultColumns() []ColumnInfo {
	return []ColumnInfo{
		// Network Identifiers
		{Name: "dst_addr", Type: "String", Category: "dimension", UICategory: "network", Description: "Destination IP Address"},
		{Name: "dst_net", Type: "String", Category: "dimension", UICategory: "network", Description: "Destination Network Prefix"},
		{Name: "dst_port", Type: "UInt16", Category: "dimension", UICategory: "network", Description: "Destination Port"},
		{Name: "etype", Type: "String", Category: "dimension", UICategory: "network", Description: "Ethernet Type"},
		{Name: "proto", Type: "String", Category: "dimension", UICategory: "network", Description: "Protocol (TCP, UDP, etc)"},
		{Name: "src_addr", Type: "String", Category: "dimension", UICategory: "network", Description: "Source IP Address"},
		{Name: "src_net", Type: "String", Category: "dimension", UICategory: "network", Description: "Source Network Prefix"},
		{Name: "src_port", Type: "UInt16", Category: "dimension", UICategory: "network", Description: "Source Port"},

		// Location/Device
		{Name: "dst_device_code", Type: "String", Category: "dimension", UICategory: "location", Description: "Destination Device Code"},
		{Name: "dst_exchange", Type: "String", Category: "dimension", UICategory: "location", Description: "Destination Exchange"},
		{Name: "dst_location", Type: "String", Category: "dimension", UICategory: "location", Description: "Destination Location"},
		{Name: "sampler_address", Type: "String", Category: "dimension", UICategory: "location", Description: "Flow Sampler Address"},
		{Name: "src_device_code", Type: "String", Category: "dimension", UICategory: "location", Description: "Source Device Code"},
		{Name: "src_exchange", Type: "String", Category: "dimension", UICategory: "location", Description: "Source Exchange"},
		{Name: "src_location", Type: "String", Category: "dimension", UICategory: "location", Description: "Source Location"},

		// AS Information
		{Name: "as_path", Type: "Array(String)", Category: "dimension", UICategory: "as", Description: "BGP AS Path"},
		{Name: "dst_as", Type: "UInt32", Category: "dimension", UICategory: "as", Description: "Destination AS Number"},
		{Name: "next_hop_as", Type: "UInt32", Category: "dimension", UICategory: "as", Description: "Next Hop AS Number"},
		{Name: "src_as", Type: "UInt32", Category: "dimension", UICategory: "as", Description: "Source AS Number"},

		// BGP
		{Name: "bgp_communities", Type: "Array(String)", Category: "dimension", UICategory: "bgp", Description: "BGP Communities"},
		{Name: "bgp_next_hop", Type: "String", Category: "dimension", UICategory: "bgp", Description: "BGP Next Hop"},

		// Interface Information
		{Name: "in_if", Type: "Int64", Category: "dimension", UICategory: "interface", Description: "Input Interface Index"},
		{Name: "in_ifname", Type: "String", Category: "dimension", UICategory: "interface", Description: "Input Interface Name"},
		{Name: "out_if", Type: "Int64", Category: "dimension", UICategory: "interface", Description: "Output Interface Index"},
		{Name: "out_ifname", Type: "String", Category: "dimension", UICategory: "interface", Description: "Output Interface Name"},

		// VLAN
		{Name: "dst_vlan", Type: "UInt16", Category: "dimension", UICategory: "vlan", Description: "Destination VLAN"},
		{Name: "src_vlan", Type: "UInt16", Category: "dimension", UICategory: "vlan", Description: "Source VLAN"},
		{Name: "vlan_id", Type: "UInt16", Category: "dimension", UICategory: "vlan", Description: "VLAN ID"},

		// TCP/IP Flags
		{Name: "ip_tos", Type: "UInt8", Category: "dimension", UICategory: "tcp_ip", Description: "IP Type of Service"},
		{Name: "ip_ttl", Type: "UInt8", Category: "dimension", UICategory: "tcp_ip", Description: "IP TTL"},
		{Name: "tcp_flags", Type: "UInt8", Category: "dimension", UICategory: "tcp_ip", Description: "TCP Flags"},

		// Flow Metadata
		{Name: "forwarding_status", Type: "UInt8", Category: "dimension", UICategory: "flow", Description: "Forwarding Status"},
		{Name: "sampling_rate", Type: "UInt32", Category: "dimension", UICategory: "flow", Description: "Sampling Rate"},
		{Name: "type", Type: "String", Category: "dimension", UICategory: "flow", Description: "Flow Type"},

		// Metrics
		{Name: "bytes", Type: "UInt64", Category: "metric", UICategory: "metric", Description: "Byte Count"},
		{Name: "packets", Type: "UInt32", Category: "metric", UICategory: "metric", Description: "Packet Count"},

		// Time
		{Name: "time_flow_end_ns", Type: "DateTime64(9)", Category: "time", UICategory: "time", Description: "Flow End Time"},
		{Name: "time_flow_start_ns", Type: "DateTime64(9)", Category: "time", UICategory: "time", Description: "Flow Start Time"},
		{Name: "time_received_ns", Type: "DateTime64(9)", Category: "time", UICategory: "time", Description: "Time Received"},
	}
}
