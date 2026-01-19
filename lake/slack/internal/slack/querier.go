package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
)

// ClickhouseQuerier implements workflow.Querier using the clickhouse client
type ClickhouseQuerier struct {
	db clickhouse.Client
}

// NewClickhouseQuerier creates a new ClickhouseQuerier
func NewClickhouseQuerier(db clickhouse.Client) *ClickhouseQuerier {
	return &ClickhouseQuerier{db: db}
}

// Query executes a SQL query and returns the result
func (q *ClickhouseQuerier) Query(ctx context.Context, sql string) (workflow.QueryResult, error) {
	sql = strings.TrimSuffix(strings.TrimSpace(sql), ";")

	conn, err := q.db.Conn(ctx)
	if err != nil {
		return workflow.QueryResult{SQL: sql, Error: fmt.Sprintf("connection error: %v", err)}, nil
	}
	defer conn.Close()

	result, err := dataset.Query(ctx, conn, sql, nil)
	if err != nil {
		return workflow.QueryResult{SQL: sql, Error: err.Error()}, nil
	}

	qr := workflow.QueryResult{
		SQL:     sql,
		Columns: result.Columns,
		Rows:    result.Rows,
		Count:   result.Count,
	}

	// Generate formatted output
	qr.Formatted = formatQueryResult(qr)

	return qr, nil
}

// formatQueryResult creates a human-readable format of the query result
func formatQueryResult(result workflow.QueryResult) string {
	if result.Error != "" {
		return fmt.Sprintf("Error: %s", result.Error)
	}

	if len(result.Rows) == 0 {
		return "Query returned no results."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Results (%d rows):\n", len(result.Rows)))
	sb.WriteString("Columns: " + strings.Join(result.Columns, " | ") + "\n")
	sb.WriteString(strings.Repeat("-", 40) + "\n")

	// Limit output to first 50 rows
	maxRows := 50
	if len(result.Rows) < maxRows {
		maxRows = len(result.Rows)
	}

	for i := 0; i < maxRows; i++ {
		row := result.Rows[i]
		var values []string
		for _, col := range result.Columns {
			// Use workflow.FormatValue to properly handle pointer types (e.g., ClickHouse Decimals)
			values = append(values, workflow.FormatValue(row[col]))
		}
		sb.WriteString(strings.Join(values, " | ") + "\n")
	}

	if len(result.Rows) > 50 {
		sb.WriteString(fmt.Sprintf("... and %d more rows\n", len(result.Rows)-50))
	}

	return sb.String()
}

// ClickhouseSchemaFetcher implements workflow.SchemaFetcher using the clickhouse client (TCP)
type ClickhouseSchemaFetcher struct {
	db       clickhouse.Client
	database string
}

// NewClickhouseSchemaFetcher creates a new ClickhouseSchemaFetcher
func NewClickhouseSchemaFetcher(db clickhouse.Client, database string) *ClickhouseSchemaFetcher {
	return &ClickhouseSchemaFetcher{db: db, database: database}
}

// FetchSchema retrieves table columns and view definitions from ClickHouse
func (f *ClickhouseSchemaFetcher) FetchSchema(ctx context.Context) (string, error) {
	conn, err := f.db.Conn(ctx)
	if err != nil {
		return "", fmt.Errorf("connection error: %w", err)
	}
	defer conn.Close()

	// Fetch columns
	rows, err := conn.Query(ctx, `
		SELECT
			table,
			name,
			type
		FROM system.columns
		WHERE database = $1
		  AND table NOT LIKE 'stg_%'
		ORDER BY table, position
	`, f.database)
	if err != nil {
		return "", fmt.Errorf("failed to fetch columns: %w", err)
	}
	defer rows.Close()

	type columnInfo struct {
		Table string
		Name  string
		Type  string
	}
	var columns []columnInfo
	for rows.Next() {
		var c columnInfo
		if err := rows.Scan(&c.Table, &c.Name, &c.Type); err != nil {
			return "", err
		}
		columns = append(columns, c)
	}

	// Fetch view definitions
	viewRows, err := conn.Query(ctx, `
		SELECT
			name,
			as_select
		FROM system.tables
		WHERE database = $1
		  AND engine = 'View'
		  AND name NOT LIKE 'stg_%'
	`, f.database)
	if err != nil {
		return "", fmt.Errorf("failed to fetch views: %w", err)
	}
	defer viewRows.Close()

	// Build view definitions map
	viewDefs := make(map[string]string)
	for viewRows.Next() {
		var name, asSelect string
		if err := viewRows.Scan(&name, &asSelect); err != nil {
			return "", err
		}
		viewDefs[name] = asSelect
	}

	// Format schema as readable text
	var sb strings.Builder
	currentTable := ""
	for _, col := range columns {
		if col.Table != currentTable {
			if currentTable != "" {
				if def, ok := viewDefs[currentTable]; ok {
					sb.WriteString("  Definition: " + def + "\n")
				}
				sb.WriteString("\n")
			}
			currentTable = col.Table
			if _, isView := viewDefs[col.Table]; isView {
				sb.WriteString(col.Table + " (VIEW):\n")
			} else {
				sb.WriteString(col.Table + ":\n")
			}
		}
		sb.WriteString("  - " + col.Name + " (" + col.Type + ")\n")
	}
	if def, ok := viewDefs[currentTable]; ok {
		sb.WriteString("  Definition: " + def + "\n")
	}

	return sb.String(), nil
}
