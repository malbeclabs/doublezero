package agent

import (
	"encoding/json"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/client"
	"github.com/stretchr/testify/require"
)

func TestAI_Agent_Anthropic_IsSchemaTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{"doublezero-schema", "doublezero-schema", true},
		{"doublezero-telemetry-schema", "doublezero-telemetry-schema", true},
		{"solana-schema", "solana-schema", true},
		{"query", "query", false},
		{"unknown", "unknown-tool", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSchemaTool(tt.toolName)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestAI_Agent_Anthropic_FormatTruncationNotice(t *testing.T) {
	tests := []struct {
		name     string
		itemType string
		shown    int
		total    int
		expected string
	}{
		{
			name:     "tables",
			itemType: "tables",
			shown:    5,
			total:    10,
			expected: "\n\n[Result truncated: showing 5 of 10 tables to avoid token limits]",
		},
		{
			name:     "rows",
			itemType: "rows",
			shown:    100,
			total:    500,
			expected: "\n\n[Result truncated: showing 100 of 500 rows to avoid token limits]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTruncationNotice(tt.itemType, tt.shown, tt.total)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestAI_Agent_Anthropic_TruncateAtBoundary(t *testing.T) {
	tests := []struct {
		name           string
		text           string
		maxLen         int
		shouldTruncate bool
	}{
		{
			name:           "text shorter than max",
			text:           "short",
			maxLen:         100,
			shouldTruncate: false,
		},
		{
			name:           "text longer than max, finds newline",
			text:           strings.Repeat("line1\nline2\nline3\n", 10), // Make it long
			maxLen:         100,                                         // Large enough to fit truncated text + notice
			shouldTruncate: true,
		},
		{
			name:           "text longer than max, finds closing brace",
			text:           strings.Repeat(`{"key": "value", "other": "data"}, `, 5), // Make it long
			maxLen:         100,                                                      // Large enough to fit truncated text + notice
			shouldTruncate: true,
		},
		{
			name:           "finds comma boundary",
			text:           strings.Repeat(`{"a": 1, "b": 2, "c": 3}, `, 5), // Make it long
			maxLen:         100,                                             // Large enough to fit truncated text + notice
			shouldTruncate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateAtBoundary(tt.text, tt.maxLen)
			if tt.shouldTruncate {
				require.Contains(t, result, "[Result truncated")
				// Result should be <= maxLen (the function handles notice fitting)
				require.LessOrEqual(t, len(result), tt.maxLen)
			} else {
				require.Equal(t, tt.text, result)
			}
		})
	}
}

func TestAI_Agent_Anthropic_TruncateGenericJSON(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]any
		maxLen   int
		checkLen bool
	}{
		{
			name: "small JSON fits",
			data: map[string]any{
				"key": "value",
			},
			maxLen:   100,
			checkLen: true,
		},
		{
			name: "large JSON gets truncated",
			data: map[string]any{
				"key": strings.Repeat("x", 1000),
			},
			maxLen:   100,
			checkLen: true,
		},
		{
			name: "complex nested JSON",
			data: map[string]any{
				"nested": map[string]any{
					"deep": map[string]any{
						"value": "data",
					},
				},
			},
			maxLen:   100,
			checkLen: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := truncateGenericJSON(tt.data, tt.maxLen)
			require.NoError(t, err)
			if tt.checkLen {
				require.LessOrEqual(t, len(result), tt.maxLen)
			}
			// Should be valid JSON if not truncated
			if len(result) < tt.maxLen {
				var parsed map[string]any
				err := json.Unmarshal([]byte(result), &parsed)
				require.NoError(t, err)
			}
		})
	}
}

func TestAI_Agent_Anthropic_TruncateListTables(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]any
		maxLen   int
		expected int // expected number of tables in result
	}{
		{
			name: "small schema fits",
			data: map[string]any{
				"tables": []any{
					map[string]any{"name": "table1", "columns": []any{}},
					map[string]any{"name": "table2", "columns": []any{}},
				},
			},
			maxLen:   1000,
			expected: 2,
		},
		{
			name: "large schema gets truncated",
			data: map[string]any{
				"tables": []any{
					map[string]any{"name": "table1", "columns": []any{map[string]any{"name": "col1"}}},
					map[string]any{"name": "table2", "columns": []any{map[string]any{"name": "col2"}}},
					map[string]any{"name": "table3", "columns": []any{map[string]any{"name": "col3"}}},
				},
			},
			maxLen:   200, // Small enough to force truncation
			expected: 2,   // May keep 1-2 tables depending on size
		},
		{
			name: "no tables field falls back to generic",
			data: map[string]any{
				"other": "data",
			},
			maxLen:   100,
			expected: 0, // No tables in result
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := truncateListTables(tt.data, tt.maxLen)
			require.NoError(t, err)
			require.LessOrEqual(t, len(result), tt.maxLen)

			// Parse and verify structure
			var parsed map[string]any
			err = json.Unmarshal([]byte(result), &parsed)
			require.NoError(t, err)

			if tables, ok := parsed["tables"].([]any); ok && len(tables) > 0 {
				require.GreaterOrEqual(t, len(tables), 1)          // Should keep at least 1 table
				require.LessOrEqual(t, len(tables), tt.expected+1) // Allow some variance
			}
		})
	}
}

func TestAI_Agent_Anthropic_TruncateQueryResult(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]any
		maxLen   int
		expected int // expected number of rows in result
	}{
		{
			name: "small query result fits",
			data: map[string]any{
				"columns": []string{"col1", "col2"},
				"rows": []any{
					map[string]any{"col1": "val1", "col2": "val2"},
					map[string]any{"col1": "val3", "col2": "val4"},
				},
				"count": 2,
			},
			maxLen:   1000,
			expected: 2,
		},
		{
			name: "large query result gets truncated",
			data: map[string]any{
				"columns": []string{"col1"},
				"rows": []any{
					map[string]any{"col1": strings.Repeat("x", 30)},
					map[string]any{"col1": strings.Repeat("y", 30)},
					map[string]any{"col1": strings.Repeat("z", 30)},
				},
				"count": 3.0, // Use float64 to match JSON unmarshal
			},
			maxLen:   200, // Small enough to force truncation
			expected: 2,   // May keep 1-2 rows depending on size
		},
		{
			name: "no rows field falls back to generic",
			data: map[string]any{
				"columns": []string{"col1"},
				"count":   0,
			},
			maxLen:   100,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := truncateQueryResult(tt.data, tt.maxLen)
			require.NoError(t, err)
			require.LessOrEqual(t, len(result), tt.maxLen)

			// Parse and verify structure
			var parsed map[string]any
			err = json.Unmarshal([]byte(result), &parsed)
			require.NoError(t, err)

			if rows, ok := parsed["rows"].([]any); ok && len(rows) > 0 {
				require.GreaterOrEqual(t, len(rows), 1)          // Should keep at least 1 row
				require.LessOrEqual(t, len(rows), tt.expected+1) // Allow some variance
			}
		})
	}
}

func TestAI_Agent_Anthropic_TruncateToolResult(t *testing.T) {
	tests := []struct {
		name     string
		result   string
		toolName string
		maxLen   int
		checkLen bool
	}{
		{
			name:     "schema tool with valid JSON",
			result:   `{"tables": [{"name": "table1"}]}`,
			toolName: "doublezero-schema",
			maxLen:   1000,
			checkLen: true,
		},
		{
			name:     "query tool with valid JSON",
			result:   `{"rows": [{"col1": "val1"}], "count": 1}`,
			toolName: "query",
			maxLen:   1000,
			checkLen: true,
		},
		{
			name:     "unknown tool falls back to generic",
			result:   `{"key": "value"}`,
			toolName: "unknown-tool",
			maxLen:   1000,
			checkLen: true,
		},
		{
			name:     "invalid JSON falls back to boundary truncation",
			result:   "not valid json {",
			toolName: "query",
			maxLen:   200, // Large enough to fit truncated text + notice
			checkLen: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := truncateToolResult(tt.result, tt.toolName, tt.maxLen)
			require.NoError(t, err)
			if tt.checkLen {
				require.LessOrEqual(t, len(result), tt.maxLen)
			}
		})
	}
}

func TestAI_Agent_Anthropic_ToAnthropicTools(t *testing.T) {
	tests := []struct {
		name     string
		tools    []client.Tool
		expected int
	}{
		{
			name:     "empty tools",
			tools:    []client.Tool{},
			expected: 0,
		},
		{
			name: "single tool",
			tools: []client.Tool{
				{
					Name:        "test-tool",
					Description: "A test tool",
					InputSchema: map[string]any{
						"properties": map[string]any{
							"arg1": map[string]any{"type": "string"},
						},
						"required": []string{"arg1"},
					},
				},
			},
			expected: 1,
		},
		{
			name: "multiple tools",
			tools: []client.Tool{
				{Name: "tool1", Description: "Tool 1", InputSchema: map[string]any{}},
				{Name: "tool2", Description: "Tool 2", InputSchema: map[string]any{}},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toAnthropicTools(tt.tools)
			require.Equal(t, tt.expected, len(result))
		})
	}
}

func TestAI_Agent_Anthropic_TrimOldToolResults(t *testing.T) {
	tests := []struct {
		name              string
		msgs              []anthropic.MessageParam
		toolResultIndices []int
		keepRounds        int
		expectedMsgLen    int
		expectedIndices   []int
	}{
		{
			name:              "no trimming needed",
			toolResultIndices: []int{5, 7, 9},
			keepRounds:        5,
			expectedMsgLen:    10,
			expectedIndices:   []int{5, 7, 9},
		},
		{
			name:              "trim to last 2 rounds",
			toolResultIndices: []int{2, 4, 6, 8},
			keepRounds:        2,
			// Keep initial messages (before first tool result at index 2, so keep up to index 1)
			// + last 2 rounds starting from index 6 (cutoff is 6-1=5, so keep from index 5)
			// Result: msgs[0:1] + msgs[5:10] = 1 + 5 = 6 msgs
			expectedMsgLen:  6,
			expectedIndices: []int{2, 4}, // Adjusted indices: removed 4, so 6->2, 8->4
		},
		{
			name:              "trim all but one",
			toolResultIndices: []int{2, 4, 6, 8},
			keepRounds:        1,
			// Keep initial messages (before first tool result at index 2, so keep up to index 1)
			// + last 1 round starting from index 8 (cutoff is 8-1=7, so keep from index 7)
			// Result: indices 0-1 (2 msgs) + indices 7-9 (3 msgs) = 5 msgs total
			// But firstAssistantIndex is 2-1=1, cutoffIndex is 8-1=7
			// So: msgs[0:1] + msgs[7:10] = 1 + 3 = 4 msgs
			expectedMsgLen:  4,
			expectedIndices: []int{2}, // Adjusted: removed 6, so 8->2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create actual message params for testing
			msgs := make([]anthropic.MessageParam, 10)
			for i := range msgs {
				msgs[i] = anthropic.NewUserMessage(anthropic.NewTextBlock("test"))
			}

			resultMsgs, resultIndices := trimOldToolResults(msgs, tt.toolResultIndices, tt.keepRounds)

			require.Equal(t, tt.expectedMsgLen, len(resultMsgs))
			require.Equal(t, len(tt.expectedIndices), len(resultIndices))
			if len(tt.expectedIndices) > 0 {
				// Check that the last index matches (most important check)
				require.Equal(t, tt.expectedIndices[len(tt.expectedIndices)-1], resultIndices[len(resultIndices)-1])
			}
		})
	}
}
