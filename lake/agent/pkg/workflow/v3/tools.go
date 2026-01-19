package v3

import "encoding/json"

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
		return nil, nil
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

// CypherQueryInput represents a single query in an execute_cypher tool call.
type CypherQueryInput struct {
	Question string `json:"question"`
	Cypher   string `json:"cypher"`
}

// ParseCypherQueries extracts CypherQueryInput from execute_cypher parameters.
func ParseCypherQueries(params map[string]any) ([]CypherQueryInput, error) {
	queriesRaw, ok := params["queries"].([]any)
	if !ok {
		return nil, nil
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
