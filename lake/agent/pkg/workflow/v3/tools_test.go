package v3

import "testing"

func TestCleanJSStringConcat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple concatenation",
			input:    `"SELECT " + "* FROM table"`,
			expected: `"SELECT * FROM table"`,
		},
		{
			name:     "multiline concatenation",
			input:    "\"SELECT code, \" +\n           \"status FROM table\"",
			expected: `"SELECT code, status FROM table"`,
		},
		{
			name:     "multiple concatenations",
			input:    `"SELECT " + "* " + "FROM " + "table"`,
			expected: `"SELECT * FROM table"`,
		},
		{
			name:     "no concatenation",
			input:    `"SELECT * FROM table"`,
			expected: `"SELECT * FROM table"`,
		},
		{
			name:     "concatenation with tabs",
			input:    "\"SELECT \" +\t\"* FROM table\"",
			expected: `"SELECT * FROM table"`,
		},
		{
			name:     "real world example",
			input:    "\"MATCH (ma:Metro {code: 'nyc'}) \" +\n                  \"MATCH (mz:Metro {code: 'lon'}) \" +\n                  \"RETURN ma, mz\"",
			expected: `"MATCH (ma:Metro {code: 'nyc'}) MATCH (mz:Metro {code: 'lon'}) RETURN ma, mz"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanJSStringConcat(tt.input)
			if result != tt.expected {
				t.Errorf("cleanJSStringConcat(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
