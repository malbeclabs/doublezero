package workflow

import (
	"testing"
)

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		// Nil handling
		{"nil value", nil, ""},

		// Pointer dereferencing - the main bug this test prevents
		{"pointer to float64", floatPtr(3.14159), "3.14159"},
		{"pointer to int", intPtr(42), "42"},
		{"pointer to string", stringPtr("hello"), "hello"},
		{"nil pointer", (*float64)(nil), ""},

		// Double pointer (edge case from ClickHouse Decimal types)
		{"double pointer to float64", func() any {
			f := 2.718
			p := &f
			return &p
		}(), "2.718"},

		// Basic types
		{"float64", float64(123.456), "123.456"},
		{"float64 whole number", float64(100), "100"},
		{"float32", float32(1.5), "1.5"},
		{"string", "test", "test"},
		{"int", 42, "42"},
		{"int64", int64(9999), "9999"},
		{"uint64", uint64(12345), "12345"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatValue(tt.input)
			if result != tt.expected {
				t.Errorf("FormatValue(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestFormatValueNoHexAddresses verifies that pointer types don't produce
// hex memory addresses (like "0x14000e9a170") which was the original bug.
func TestFormatValueNoHexAddresses(t *testing.T) {
	// Create various pointer types that could potentially produce hex output
	f := 3.14
	i := 42
	s := "test"

	pointerValues := []any{
		&f,
		&i,
		&s,
		func() any { p := &f; return &p }(), // double pointer
	}

	for _, v := range pointerValues {
		result := FormatValue(v)
		if len(result) > 2 && result[:2] == "0x" {
			t.Errorf("FormatValue produced hex address %q for pointer type - this is the bug we fixed!", result)
		}
	}
}

// Helper functions for creating pointers
func floatPtr(f float64) *float64 { return &f }
func intPtr(i int) *int           { return &i }
func stringPtr(s string) *string  { return &s }
