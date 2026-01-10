package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
	"github.com/malbeclabs/doublezero/lake/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/pkg/clickhouse/dataset"
)

// QuerierToolClient implements react.ToolClient using a Querier.
type ClickhouseQueryTool struct {
	clickhouse clickhouse.DB
	log        *slog.Logger
}

// NewQuerierToolClient creates a new QuerierToolClient.
func NewClickhouseQueryTool(clickhouse clickhouse.DB) *ClickhouseQueryTool {
	return &ClickhouseQueryTool{
		clickhouse: clickhouse,
	}
}

// NewClickhouseQueryToolWithLogger creates a new QuerierToolClient with a logger.
func NewClickhouseQueryToolWithLogger(clickhouse clickhouse.DB, log *slog.Logger) *ClickhouseQueryTool {
	return &ClickhouseQueryTool{
		clickhouse: clickhouse,
		log:        log,
	}
}

// ListTools returns the available tools.
func (q *ClickhouseQueryTool) ListTools(ctx context.Context) ([]react.Tool, error) {
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
func (q *ClickhouseQueryTool) CallToolText(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	if name != "query" {
		return "", true, fmt.Errorf("unknown tool: %s", name)
	}

	sql, ok := args["sql"].(string)
	if !ok {
		return "", true, fmt.Errorf("sql parameter is required and must be a string")
	}

	// Log query when verbose mode is enabled
	if q.log != nil {
		q.log.Info("executing query", "sql", sql)
	}

	conn, err := q.clickhouse.Conn(ctx)
	if err != nil {
		return fmt.Sprintf("Error connecting to database: %v", err), true, nil
	}
	defer conn.Close()

	// Set readonly mode to prevent write operations
	// May conflict with client settings like max_execution_time
	setReadonlyErr := conn.Exec(ctx, "SET readonly = 1")

	var result *dataset.QueryResult
	var queryErr error
	result, queryErr = dataset.Query(ctx, conn, sql, nil)
	if queryErr != nil {
		// If readonly was set and query fails with readonly conflict, retry without readonly
		if setReadonlyErr == nil {
			errStr := queryErr.Error()
			if strings.Contains(errStr, "readonly") && strings.Contains(errStr, "max_execution_time") {
				result, queryErr = dataset.Query(ctx, conn, sql, nil)
				if queryErr != nil {
					return fmt.Sprintf("Error executing query: %v", queryErr), true, nil
				}
			} else {
				return fmt.Sprintf("Error executing query: %v", queryErr), true, nil
			}
		} else {
			return fmt.Sprintf("Error executing query: %v", queryErr), true, nil
		}
	}

	colTypes := make([]ColumnType, len(result.ColumnTypes))
	for i, ct := range result.ColumnTypes {
		colTypes[i] = ColumnType{
			Name:             ct.Name,
			DatabaseTypeName: ct.DatabaseTypeName,
			ScanType:         ct.ScanType,
		}
	}

	queryRows := make([]QueryRow, len(result.Rows))
	for i, row := range result.Rows {
		queryRows[i] = QueryRow(row)
	}

	queryResp := QueryResponse{
		Columns:     result.Columns,
		ColumnTypes: colTypes,
		Rows:        queryRows,
		Count:       result.Count,
	}

	jsonStr, err := queryResp.ToJSON()
	if err != nil {
		return fmt.Sprintf("Error formatting result: %v", err), true, nil
	}
	return jsonStr, false, nil
}

// QueryResponse represents the result of a query execution.
type QueryResponse struct {
	Columns     []string     `json:"columns"`
	ColumnTypes []ColumnType `json:"column_types"`
	Rows        []QueryRow   `json:"rows"`
	Count       int          `json:"count"`
}

// ColumnType represents metadata about a column.
type ColumnType struct {
	Name             string `json:"name"`
	DatabaseTypeName string `json:"database_type_name"`
	ScanType         string `json:"scan_type"`
}

// QueryRow represents a single row of query results as a map.
type QueryRow map[string]any

// ToJSON converts the QueryResponse to a JSON string.
func (qr *QueryResponse) ToJSON() (string, error) {
	data, err := json.Marshal(qr)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query response: %w", err)
	}
	return string(data), nil
}
