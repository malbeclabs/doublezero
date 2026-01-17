package v3

import "encoding/json"

// Tool definitions for v3 workflow.
var (
	// ThinkTool allows the model to externalize reasoning for streaming to users.
	ThinkTool = Tool{
		Name:        "think",
		Description: "Record your reasoning, interpretation, or analysis plan. This is shown to users so they can follow your thought process. Use this before executing queries to explain what you're investigating and why.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"content": {
					"type": "string",
					"description": "Your reasoning or analysis"
				}
			},
			"required": ["content"]
		}`),
	}

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
)

// DefaultTools returns the default set of tools for the v3 workflow.
func DefaultTools() []Tool {
	return []Tool{ThinkTool, ExecuteSQLTool}
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
