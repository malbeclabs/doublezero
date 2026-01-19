package v3

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Tool definitions for v3 workflow.
var (
	// ExecuteSQLTool allows the model to execute SQL queries.
	ExecuteSQLTool = Tool{
		Name:        "execute_sql",
		Description: "Execute one or more SQL queries against the ClickHouse database. Queries run in parallel. Each query should answer a specific data question.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"queries": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"question": {
								"type": "string",
								"description": "The data question this query answers, e.g. 'How many validators are on DZ?'"
							},
							"sql": {
								"type": "string",
								"description": "The SQL query to execute"
							}
						},
						"required": ["question", "sql"]
					},
					"description": "List of queries to execute"
				}
			},
			"required": ["queries"]
		}`),
	}

	// ExecuteCypherTool allows the model to execute Cypher queries against Neo4j.
	ExecuteCypherTool = Tool{
		Name:        "execute_cypher",
		Description: "Execute one or more Cypher queries against the Neo4j graph database. Use this for topology questions, path finding, reachability, and relationship traversal. Queries run in parallel.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"queries": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"question": {
								"type": "string",
								"description": "The graph question this query answers, e.g. 'What is the path between device A and device B?'"
							},
							"cypher": {
								"type": "string",
								"description": "The Cypher query to execute"
							}
						},
						"required": ["question", "cypher"]
					},
					"description": "List of Cypher queries to execute"
				}
			},
			"required": ["queries"]
		}`),
	}
)

// DefaultTools returns the default set of tools for the v3 workflow.
func DefaultTools() []Tool {
	return []Tool{ExecuteSQLTool}
}

// DefaultToolsWithGraph returns tools including graph database support.
func DefaultToolsWithGraph() []Tool {
	return []Tool{ExecuteSQLTool, ExecuteCypherTool}
}

// ParseQueries extracts QueryInput from execute_sql parameters.
func ParseQueries(params map[string]any) ([]QueryInput, error) {
	queriesRaw, ok := params["queries"].([]any)
	if !ok {
		// Debug: log what type we actually got
		if params["queries"] != nil {
			fmt.Printf("DEBUG ParseQueries: params['queries'] type=%T value=%v\n", params["queries"], truncateStr(fmt.Sprintf("%v", params["queries"]), 200))
		}

		// Model might send queries as a string containing JSON (common with some model behaviors)
		if queriesStr, strOk := params["queries"].(string); strOk {
			// Clean up any XML-style tags that models sometimes include
			// (e.g., </invoke><invoke name="...">)
			cleanStr := cleanXMLTags(queriesStr)

			var arr []any
			if json.Unmarshal([]byte(cleanStr), &arr) == nil {
				queriesRaw = arr
			} else {
				return nil, fmt.Errorf("params['queries'] is a string but not valid JSON: %s", truncateStr(queriesStr, 100))
			}
		} else {
			if params == nil {
				return nil, fmt.Errorf("params is nil")
			}
			if _, exists := params["queries"]; !exists {
				keys := make([]string, 0, len(params))
				for k := range params {
					keys = append(keys, k)
				}
				return nil, fmt.Errorf("params missing 'queries' key, got keys: %v", keys)
			}
			return nil, fmt.Errorf("params['queries'] is not []any or string, got %T", params["queries"])
		}
	}

	var queries []QueryInput
	for _, q := range queriesRaw {
		qMap, ok := q.(map[string]any)
		if !ok {
			continue
		}

		question, _ := qMap["question"].(string)
		sql, _ := qMap["sql"].(string)

		if question != "" && sql != "" {
			queries = append(queries, QueryInput{
				Question: question,
				SQL:      sql,
			})
		}
	}

	return queries, nil
}

// cleanXMLTags removes XML-style invocation tags that models sometimes include.
// This handles cases like: [...]}]</invoke><invoke name="execute_cypher">...
func cleanXMLTags(s string) string {
	// Find the first occurrence of </invoke> or similar XML tags and truncate there
	if idx := strings.Index(s, "</invoke>"); idx > 0 {
		s = s[:idx]
	}
	if idx := strings.Index(s, "<invoke"); idx > 0 {
		s = s[:idx]
	}
	// Also handle </parameter> tags
	if idx := strings.Index(s, "</parameter>"); idx > 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)

	// The model sometimes outputs ]} after the array (extra closing brace)
	// Try to fix by finding a valid JSON array ending
	if strings.HasSuffix(s, "]}") {
		// Check if removing the extra } makes it valid JSON
		trimmed := s[:len(s)-1] // Remove trailing }
		var test []any
		if json.Unmarshal([]byte(trimmed), &test) == nil {
			return trimmed
		}
	}

	return s
}

// truncateStr truncates a string for error messages.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// CypherQueryInput represents a single query in an execute_cypher tool call.
type CypherQueryInput struct {
	Question string `json:"question"`
	Cypher   string `json:"cypher"`
}

// ParseCypherQueries extracts CypherQueryInput from execute_cypher parameters.
func ParseCypherQueries(params map[string]any) ([]CypherQueryInput, error) {
	queriesRaw, ok := params["queries"].([]any)
	if !ok {
		// Model might send queries as a string containing JSON (common with some model behaviors)
		if queriesStr, strOk := params["queries"].(string); strOk {
			// Clean up any XML-style tags that models sometimes include
			cleanStr := cleanXMLTags(queriesStr)

			var arr []any
			if json.Unmarshal([]byte(cleanStr), &arr) == nil {
				queriesRaw = arr
			} else {
				return nil, fmt.Errorf("params['queries'] is a string but not valid JSON: %s", truncateStr(queriesStr, 100))
			}
		} else {
			if params == nil {
				return nil, fmt.Errorf("params is nil")
			}
			if _, exists := params["queries"]; !exists {
				keys := make([]string, 0, len(params))
				for k := range params {
					keys = append(keys, k)
				}
				return nil, fmt.Errorf("params missing 'queries' key, got keys: %v", keys)
			}
			return nil, fmt.Errorf("params['queries'] is not []any or string, got %T", params["queries"])
		}
	}

	var queries []CypherQueryInput
	for _, q := range queriesRaw {
		qMap, ok := q.(map[string]any)
		if !ok {
			continue
		}

		question, _ := qMap["question"].(string)
		cypher, _ := qMap["cypher"].(string)

		if question != "" && cypher != "" {
			queries = append(queries, CypherQueryInput{
				Question: question,
				Cypher:   cypher,
			})
		}
	}

	return queries, nil
}
