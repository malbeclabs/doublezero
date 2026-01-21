package v3

import (
	"strings"
	"testing"
)

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

func TestCleanXMLTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no XML tags",
			input:    `[{"question": "test", "sql": "SELECT 1"}]`,
			expected: `[{"question": "test", "sql": "SELECT 1"}]`,
		},
		{
			name:     "trailing </invoke>",
			input:    `[{"question": "test", "sql": "SELECT 1"}]</invoke>`,
			expected: `[{"question": "test", "sql": "SELECT 1"}]`,
		},
		{
			name:     "trailing </invoke> with content after",
			input:    `[{"question": "test", "sql": "SELECT 1"}]</invoke><invoke name="other">`,
			expected: `[{"question": "test", "sql": "SELECT 1"}]`,
		},
		{
			name:     "trailing <invoke",
			input:    `[{"question": "test", "sql": "SELECT 1"}]<invoke name="other">`,
			expected: `[{"question": "test", "sql": "SELECT 1"}]`,
		},
		{
			name:     "trailing </parameter>",
			input:    `[{"question": "test", "sql": "SELECT 1"}]</parameter>`,
			expected: `[{"question": "test", "sql": "SELECT 1"}]`,
		},
		{
			name:     "with newlines before tag",
			input:    "[{\"question\": \"test\", \"sql\": \"SELECT 1\"}]\n</invoke>]",
			expected: `[{"question": "test", "sql": "SELECT 1"}]`,
		},
		{
			name:     "whitespace trimmed",
			input:    "  [{\"question\": \"test\"}]  ",
			expected: `[{"question": "test"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanXMLTags(tt.input)
			if result != tt.expected {
				t.Errorf("cleanXMLTags(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFixTrailingBrace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid JSON array - no change",
			input:    `[{"question": "test", "sql": "SELECT 1"}]`,
			expected: `[{"question": "test", "sql": "SELECT 1"}]`,
		},
		{
			name:     "extra trailing brace",
			input:    `[{"question": "test", "sql": "SELECT 1"}]}`,
			expected: `[{"question": "test", "sql": "SELECT 1"}]`,
		},
		{
			name:     "extra brace with whitespace",
			input:    "[{\"question\": \"test\", \"sql\": \"SELECT 1\"}]\n}",
			expected: "[{\"question\": \"test\", \"sql\": \"SELECT 1\"}]\n}",
		},
		{
			name:     "not ending with ]} - no change",
			input:    `{"question": "test"}`,
			expected: `{"question": "test"}`,
		},
		{
			name:     "invalid JSON even without brace - no change",
			input:    `[{"question": "test"]}`,
			expected: `[{"question": "test"]}`,
		},
		{
			name:     "multiple queries with extra brace",
			input:    `[{"question": "q1", "sql": "SELECT 1"}, {"question": "q2", "sql": "SELECT 2"}]}`,
			expected: `[{"question": "q1", "sql": "SELECT 1"}, {"question": "q2", "sql": "SELECT 2"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fixTrailingBrace(tt.input)
			if result != tt.expected {
				t.Errorf("fixTrailingBrace(%q) = %q, want %q", tt.input, result, tt.expected)
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

func TestParseQueries(t *testing.T) {
	tests := []struct {
		name          string
		params        map[string]any
		expectedLen   int
		expectedQ1    string // expected first query question
		expectedSQL1  string // expected first query SQL (substring match)
		expectError   bool
		errorContains string
	}{
		{
			name: "valid array params",
			params: map[string]any{
				"queries": []any{
					map[string]any{"question": "How many validators?", "sql": "SELECT COUNT(*) FROM validators"},
				},
			},
			expectedLen:  1,
			expectedQ1:   "How many validators?",
			expectedSQL1: "SELECT COUNT(*)",
		},
		{
			name: "valid JSON string params",
			params: map[string]any{
				"queries": `[{"question": "Test query", "sql": "SELECT 1"}]`,
			},
			expectedLen:  1,
			expectedQ1:   "Test query",
			expectedSQL1: "SELECT 1",
		},
		{
			name: "JSON string with literal newlines in SQL",
			params: map[string]any{
				"queries": "[\n    {\n        \"question\": \"Validator count\",\n        \"sql\": \"SELECT \n            COUNT(*)\n        FROM validators\"\n    }\n]",
			},
			expectedLen:  1,
			expectedQ1:   "Validator count",
			expectedSQL1: "SELECT",
		},
		{
			name: "JSON string with trailing </invoke>",
			params: map[string]any{
				"queries": `[{"question": "Test", "sql": "SELECT 1"}]</invoke>`,
			},
			expectedLen:  1,
			expectedQ1:   "Test",
			expectedSQL1: "SELECT 1",
		},
		{
			name: "JSON string with extra trailing brace (the main bug)",
			params: map[string]any{
				"queries": `[{"question": "Test", "sql": "SELECT 1"}]}`,
			},
			expectedLen:  1,
			expectedQ1:   "Test",
			expectedSQL1: "SELECT 1",
		},
		{
			name: "real world case: newlines + extra brace + </invoke>",
			params: map[string]any{
				"queries": "[\n    {\n        \"question\": \"Bandwidth utilization\",\n        \"sql\": \"SELECT \n            link_code, \n            bandwidth_bps\n        FROM links\n        LIMIT 2\"\n    }\n]}\n</invoke>]",
			},
			expectedLen:  1,
			expectedQ1:   "Bandwidth utilization",
			expectedSQL1: "SELECT",
		},
		{
			name: "multiple queries with malformed ending",
			params: map[string]any{
				"queries": "[\n    {\n        \"question\": \"Query 1\",\n        \"sql\": \"SELECT 1\"\n    },\n    {\n        \"question\": \"Query 2\",\n        \"sql\": \"SELECT 2\"\n    }\n]}\n</invoke>]",
			},
			expectedLen:  2,
			expectedQ1:   "Query 1",
			expectedSQL1: "SELECT 1",
		},
		{
			name: "JS string concatenation in SQL",
			params: map[string]any{
				"queries": `[{"question": "Test", "sql": "SELECT " + "* FROM " + "table"}]`,
			},
			expectedLen:  1,
			expectedQ1:   "Test",
			expectedSQL1: "SELECT * FROM table",
		},
		{
			name:        "missing queries key",
			params:      map[string]any{},
			expectError: true,
		},
		{
			name:        "nil params",
			params:      nil,
			expectError: true,
		},
		{
			name: "invalid JSON string",
			params: map[string]any{
				"queries": `not valid json`,
			},
			expectError:   true,
			errorContains: "not valid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries, err := ParseQueries(tt.params)

			if tt.expectError {
				if err == nil {
					t.Errorf("ParseQueries() expected error, got nil")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("ParseQueries() error = %v, want error containing %q", err, tt.errorContains)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseQueries() unexpected error: %v", err)
				return
			}

			if len(queries) != tt.expectedLen {
				t.Errorf("ParseQueries() returned %d queries, want %d", len(queries), tt.expectedLen)
				return
			}

			if tt.expectedLen > 0 {
				if queries[0].Question != tt.expectedQ1 {
					t.Errorf("ParseQueries() first question = %q, want %q", queries[0].Question, tt.expectedQ1)
				}
				if !strings.Contains(queries[0].SQL, tt.expectedSQL1) {
					t.Errorf("ParseQueries() first SQL = %q, want it to contain %q", queries[0].SQL, tt.expectedSQL1)
				}
			}
		})
	}
}

func TestParseCypherQueries(t *testing.T) {
	tests := []struct {
		name           string
		params         map[string]any
		expectedLen    int
		expectedQ1     string
		expectedCypher string
		expectError    bool
	}{
		{
			name: "valid array params",
			params: map[string]any{
				"queries": []any{
					map[string]any{"question": "Find path", "cypher": "MATCH (a)-[r]->(b) RETURN a, b"},
				},
			},
			expectedLen:    1,
			expectedQ1:     "Find path",
			expectedCypher: "MATCH (a)",
		},
		{
			name: "valid JSON string params",
			params: map[string]any{
				"queries": `[{"question": "Test", "cypher": "MATCH (n) RETURN n"}]`,
			},
			expectedLen:    1,
			expectedQ1:     "Test",
			expectedCypher: "MATCH (n)",
		},
		{
			name: "JSON string with extra brace and invoke tag",
			params: map[string]any{
				"queries": "[\n    {\n        \"question\": \"Find devices\",\n        \"cypher\": \"MATCH (d:Device)\n        RETURN d\"\n    }\n]}\n</invoke>]",
			},
			expectedLen:    1,
			expectedQ1:     "Find devices",
			expectedCypher: "MATCH (d:Device)",
		},
		{
			name: "fallback to direct question/cypher params",
			params: map[string]any{
				"question": "Direct query",
				"cypher":   "MATCH (n) RETURN n",
			},
			expectedLen:    1,
			expectedQ1:     "Direct query",
			expectedCypher: "MATCH (n)",
		},
		{
			name:        "missing queries key and no fallback",
			params:      map[string]any{"foo": "bar"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries, err := ParseCypherQueries(tt.params)

			if tt.expectError {
				if err == nil {
					t.Errorf("ParseCypherQueries() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseCypherQueries() unexpected error: %v", err)
				return
			}

			if len(queries) != tt.expectedLen {
				t.Errorf("ParseCypherQueries() returned %d queries, want %d", len(queries), tt.expectedLen)
				return
			}

			if tt.expectedLen > 0 {
				if queries[0].Question != tt.expectedQ1 {
					t.Errorf("ParseCypherQueries() first question = %q, want %q", queries[0].Question, tt.expectedQ1)
				}
				if !strings.Contains(queries[0].Cypher, tt.expectedCypher) {
					t.Errorf("ParseCypherQueries() first cypher = %q, want it to contain %q", queries[0].Cypher, tt.expectedCypher)
				}
			}
		})
	}
}
