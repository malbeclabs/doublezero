package v3

import "testing"

func TestEscapeNewlinesInStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no newlines",
			input:    `{"sql": "SELECT * FROM table"}`,
			expected: `{"sql": "SELECT * FROM table"}`,
		},
		{
			name:     "newline outside string is preserved",
			input:    "{\n\"sql\": \"SELECT * FROM table\"\n}",
			expected: "{\n\"sql\": \"SELECT * FROM table\"\n}",
		},
		{
			name:     "newline inside string is escaped",
			input:    "{\"sql\": \"SELECT\n  * FROM table\"}",
			expected: "{\"sql\": \"SELECT\\n  * FROM table\"}",
		},
		{
			name:     "multiple newlines inside string",
			input:    "{\"sql\": \"SELECT\n  dz_status,\n  COUNT(*)\nFROM table\"}",
			expected: "{\"sql\": \"SELECT\\n  dz_status,\\n  COUNT(*)\\nFROM table\"}",
		},
		{
			name:     "escaped quote inside string",
			input:    "{\"sql\": \"SELECT \\\"col\\\" FROM table\"}",
			expected: "{\"sql\": \"SELECT \\\"col\\\" FROM table\"}",
		},
		{
			name:     "escaped backslash inside string",
			input:    "{\"sql\": \"SELECT \\\\ FROM table\"}",
			expected: "{\"sql\": \"SELECT \\\\ FROM table\"}",
		},
		{
			name:     "real world example from logs",
			input:    "[\n    {\n        \"question\": \"Validator performance\",\n        \"sql\": \"SELECT \n            dz_status, \n            COUNT(*) AS count\n        FROM table\"\n    }\n]",
			expected: "[\n    {\n        \"question\": \"Validator performance\",\n        \"sql\": \"SELECT \\n            dz_status, \\n            COUNT(*) AS count\\n        FROM table\"\n    }\n]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeNewlinesInStrings(tt.input)
			if result != tt.expected {
				t.Errorf("escapeNewlinesInStrings(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

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
