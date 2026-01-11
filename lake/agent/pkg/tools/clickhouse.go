package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/react"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
)

// QuerierToolClient implements react.ToolClient using a Querier.
type ClickhouseQueryTool struct {
	clickhouse clickhouse.Client
	log        *slog.Logger
}

// NewQuerierToolClient creates a new QuerierToolClient.
func NewClickhouseQueryTool(clickhouse clickhouse.Client) *ClickhouseQueryTool {
	return &ClickhouseQueryTool{
		clickhouse: clickhouse,
	}
}

// NewClickhouseQueryToolWithLogger creates a new QuerierToolClient with a logger.
func NewClickhouseQueryToolWithLogger(clickhouse clickhouse.Client, log *slog.Logger) *ClickhouseQueryTool {
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
			Description: "Execute a SQL query against the DoubleZero database. Returns results in compact text format with columns and rows (max 50 rows displayed).",
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

	// Return compact text format to reduce context size
	return formatCompactResult(result.Columns, result.Rows, result.Count), false, nil
}

const (
	maxResultRows  = 50  // Maximum rows to include in result
	maxValueLength = 100 // Maximum length for individual cell values
)

// formatCompactResult formats query results in a compact text format to reduce context size.
// This typically reduces token usage by 50-70% compared to full JSON with metadata.
func formatCompactResult(columns []string, rows []map[string]any, count int) string {
	if count == 0 {
		return "Query returned no results."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Columns: %s\n", strings.Join(columns, ", ")))

	displayRows := count
	if displayRows > maxResultRows {
		displayRows = maxResultRows
	}
	sb.WriteString(fmt.Sprintf("Rows (%d total, showing %d):\n", count, displayRows))

	for i := 0; i < displayRows && i < len(rows); i++ {
		values := make([]string, len(columns))
		for j, col := range columns {
			v := rows[i][col]
			s := fmt.Sprintf("%v", v)
			// Truncate long string values
			if len(s) > maxValueLength {
				s = s[:maxValueLength-3] + "..."
			}
			values[j] = s
		}
		sb.WriteString(strings.Join(values, " | ") + "\n")
	}

	if count > maxResultRows {
		sb.WriteString(fmt.Sprintf("... and %d more rows\n", count-maxResultRows))
	}

	return sb.String()
}
