package v3

import (
	"encoding/json"
	"fmt"
	"regexp"
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
								"description": "The SQL query to execute. Must be a single JSON string - do NOT use string concatenation with + operators."
							}
						},
						"required": ["question", "sql"]
					},
					"description": "List of queries to execute. Must be valid JSON array - do NOT use string concatenation."
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
								"description": "The Cypher query to execute. Must be a single JSON string - do NOT use string concatenation with + operators."
							}
						},
						"required": ["question", "cypher"]
					},
					"description": "List of Cypher queries to execute. Must be valid JSON array - do NOT use string concatenation."
				}
			},
			"required": ["queries"]
		}`),
	}

	// ReadDocsTool allows the model to read DoubleZero documentation.
	ReadDocsTool = Tool{
		Name:        "read_docs",
		Description: "Read DoubleZero documentation to answer questions about concepts, architecture, setup, troubleshooting, or how the network works. Use this when users ask 'what is DZ', 'how do I set up', 'why isn't X working', or similar conceptual/procedural questions.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"page": {
					"type": "string",
					"enum": ["index", "architecture", "setup", "troubleshooting", "connect", "connect-multicast", "contribute", "contribute-overview", "contribute-operations", "contribute-provisioning", "users-overview", "paying-fees", "paying-fees2z", "Swapping-sol-to-2z", "DZ RPC-Connection", "DZ Testnet Connection", "DZ Mainnet-beta Connection", "mainnet-beta-migration", "multicast-admin"],
					"description": "The documentation page to read"
				}
			},
			"required": ["page"]
		}`),
	}
)

// DefaultTools returns the default set of tools for the v3 workflow.
func DefaultTools() []Tool {
	return []Tool{ExecuteSQLTool, ReadDocsTool}
}

// DefaultToolsWithGraph returns tools including graph database support.
func DefaultToolsWithGraph() []Tool {
	return []Tool{ExecuteSQLTool, ExecuteCypherTool, ReadDocsTool}
}

// ReadDocsInput represents the input for the read_docs tool.
type ReadDocsInput struct {
	Page string `json:"page"`
}

// ParseReadDocsInput extracts ReadDocsInput from read_docs parameters.
func ParseReadDocsInput(params map[string]any) (*ReadDocsInput, error) {
	page, ok := params["page"].(string)
	if !ok || page == "" {
		return nil, fmt.Errorf("missing or invalid 'page' parameter")
	}
	return &ReadDocsInput{Page: page}, nil
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
			// Clean up JavaScript-style string concatenation that models sometimes use
			// (e.g., "SELECT " + "* FROM" instead of "SELECT * FROM")
			cleanStr = cleanJSStringConcat(cleanStr)
			// Escape literal newlines inside JSON string values
			// (models sometimes format SQL with literal newlines which breaks JSON)
			cleanStr = escapeNewlinesInStrings(cleanStr)
			// Fix trailing brace that models sometimes add (must be after newline escaping
			// so json.Unmarshal can validate the result)
			cleanStr = fixTrailingBrace(cleanStr)
			suffixStart := len(cleanStr) - 50
			if suffixStart < 0 {
				suffixStart = 0
			}
			fmt.Printf("DEBUG cleanJSON: input_len=%d output_len=%d output_suffix=%q\n",
				len(queriesStr), len(cleanStr), cleanStr[suffixStart:])

			var arr []any
			if err := json.Unmarshal([]byte(cleanStr), &arr); err == nil {
				queriesRaw = arr
			} else {
				fmt.Printf("DEBUG JSON unmarshal failed: %v\n", err)
				return nil, fmt.Errorf("params['queries'] is a string but not valid JSON after cleaning: %s", truncateStr(cleanStr, 200))
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

// cleanJSStringConcat joins JavaScript-style concatenated strings that models sometimes produce.
// e.g., "SELECT " + "* FROM" → "SELECT * FROM"
// This handles cases where the model tries to format long queries readably but breaks JSON.
var jsStringConcatRegex = regexp.MustCompile(`"\s*\+\s*"`)

func cleanJSStringConcat(s string) string {
	return jsStringConcatRegex.ReplaceAllString(s, "")
}

// escapeNewlinesInStrings escapes literal newlines inside JSON string values.
// JSON strings cannot contain literal newlines; they must be escaped as \n.
// This handles cases where models format SQL queries with literal newlines.
func escapeNewlinesInStrings(s string) string {
	var result strings.Builder
	result.Grow(len(s))
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if escaped {
			result.WriteByte(ch)
			escaped = false
			continue
		}

		if ch == '\\' && inString {
			result.WriteByte(ch)
			escaped = true
			continue
		}

		if ch == '"' {
			inString = !inString
			result.WriteByte(ch)
			continue
		}

		if ch == '\n' && inString {
			result.WriteString("\\n")
			continue
		}

		result.WriteByte(ch)
	}

	return result.String()
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
	return strings.TrimSpace(s)
}

// fixTrailingBrace repairs JSON when the model outputs an extra closing brace.
// e.g., "[{...}]}" -> "[{...}]"
// This must be called AFTER escapeNewlinesInStrings so json.Unmarshal can validate.
func fixTrailingBrace(s string) string {
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
			// Clean up JavaScript-style string concatenation
			cleanStr = cleanJSStringConcat(cleanStr)
			// Escape literal newlines inside JSON string values
			cleanStr = escapeNewlinesInStrings(cleanStr)
			// Fix trailing brace that models sometimes add
			cleanStr = fixTrailingBrace(cleanStr)

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
			// Fallback: model sent {question, cypher} directly without queries wrapper
			if question, qOk := params["question"].(string); qOk {
				if cypher, cOk := params["cypher"].(string); cOk && question != "" && cypher != "" {
					return []CypherQueryInput{{Question: question, Cypher: cypher}}, nil
				}
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
