package tools

import (
	"context"
	"fmt"

	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
)

// QuerierToolClient implements react.ToolClient using a Querier.
type QuerierToolClient struct {
	querier Querier
}

// NewQuerierToolClient creates a new QuerierToolClient.
func NewQuerierToolClient(querier Querier) *QuerierToolClient {
	return &QuerierToolClient{
		querier: querier,
	}
}

// ListTools returns the available tools.
func (q *QuerierToolClient) ListTools(ctx context.Context) ([]react.Tool, error) {
	return []react.Tool{
		{
			Name:        "query",
			Description: "Execute a SQL query against the DoubleZero database. Returns results as JSON with columns, column types, and rows.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sql": map[string]any{
						"type":        "string",
						"description": "The SQL query to execute",
					},
				},
				"required": []string{"sql"},
			},
		},
	}, nil
}

// CallToolText calls a tool and returns the result as text.
func (q *QuerierToolClient) CallToolText(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	if name != "query" {
		return "", true, fmt.Errorf("unknown tool: %s", name)
	}

	sql, ok := args["sql"].(string)
	if !ok {
		return "", true, fmt.Errorf("sql parameter is required and must be a string")
	}
	result, err := q.querier.Query(ctx, sql)
	if err != nil {
		return fmt.Sprintf("Error executing query: %v", err), true, nil
	}
	jsonStr, err := (&QueryResponse{result}).ToJSON() // Use the wrapper to call ToJSON
	if err != nil {
		return fmt.Sprintf("Error formatting result: %v", err), true, nil
	}
	return jsonStr, false, nil
}

