package analytics

import "testing"

func TestColumnRegistry_IsValidColumn(t *testing.T) {
	reg := NewColumnRegistry(DefaultColumns())

	tests := []struct {
		name     string
		column   string
		expected bool
	}{
		{"valid column src_addr", "src_addr", true},
		{"valid column dst_port", "dst_port", true},
		{"valid column bytes", "bytes", true},
		{"invalid column", "nonexistent", false},
		{"empty column", "", false},
		{"sql injection attempt", "src_addr; DROP TABLE", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reg.IsValidColumn(tt.column)
			if got != tt.expected {
				t.Errorf("IsValidColumn(%q) = %v, want %v", tt.column, got, tt.expected)
			}
		})
	}
}

func TestColumnRegistry_IsNumericColumn(t *testing.T) {
	reg := NewColumnRegistry(DefaultColumns())

	tests := []struct {
		name     string
		column   string
		expected bool
	}{
		{"UInt16 column", "dst_port", true},
		{"UInt32 column", "src_as", true},
		{"UInt64 column", "bytes", true},
		{"Int64 column", "in_if", true},
		{"UInt8 column", "ip_tos", true},
		{"String column", "src_addr", false},
		{"Array column", "as_path", false},
		{"nonexistent column", "nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reg.IsNumericColumn(tt.column)
			if got != tt.expected {
				t.Errorf("IsNumericColumn(%q) = %v, want %v", tt.column, got, tt.expected)
			}
		})
	}
}

func TestColumnRegistry_GetDimensionColumns(t *testing.T) {
	reg := NewColumnRegistry(DefaultColumns())
	dims := reg.GetDimensionColumns()

	if len(dims) == 0 {
		t.Error("expected dimension columns, got none")
	}

	// Verify all returned columns are dimensions
	for _, col := range dims {
		if col.Category != "dimension" {
			t.Errorf("expected dimension category, got %q for column %q", col.Category, col.Name)
		}
	}

	// Verify sorted order
	for i := 1; i < len(dims); i++ {
		if dims[i-1].Name > dims[i].Name {
			t.Errorf("columns not sorted: %q > %q", dims[i-1].Name, dims[i].Name)
		}
	}
}

func TestColumnRegistry_GetColumnGroups(t *testing.T) {
	reg := NewColumnRegistry(DefaultColumns())
	groups := reg.GetColumnGroups()

	if len(groups) == 0 {
		t.Error("expected column groups, got none")
	}

	// Verify expected groups exist
	expectedGroups := map[string]bool{
		"network":   false,
		"location":  false,
		"as":        false,
		"interface": false,
	}

	for _, g := range groups {
		if _, ok := expectedGroups[g.Name]; ok {
			expectedGroups[g.Name] = true
		}
		// Verify columns within group are sorted
		for i := 1; i < len(g.Columns); i++ {
			if g.Columns[i-1].Name > g.Columns[i].Name {
				t.Errorf("columns in group %q not sorted: %q > %q",
					g.Name, g.Columns[i-1].Name, g.Columns[i].Name)
			}
		}
	}

	for name, found := range expectedGroups {
		if !found {
			t.Errorf("expected group %q not found", name)
		}
	}
}

func TestValidIntervals(t *testing.T) {
	validCases := []string{
		"", "1 second", "5 second", "1 minute", "5 minute",
		"15 minute", "30 minute", "1 hour", "6 hour", "1 day",
	}

	for _, interval := range validCases {
		if !ValidIntervals[interval] {
			t.Errorf("expected %q to be valid interval", interval)
		}
	}

	invalidCases := []string{
		"2 minute", "3 hour", "invalid", "1minute", "DROP TABLE",
	}

	for _, interval := range invalidCases {
		if ValidIntervals[interval] {
			t.Errorf("expected %q to be invalid interval", interval)
		}
	}
}
