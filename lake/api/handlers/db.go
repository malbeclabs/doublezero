package handlers

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

// DBQuerier implements workflow.Querier using the global connection pool.
type DBQuerier struct{}

// NewDBQuerier creates a new DBQuerier.
func NewDBQuerier() *DBQuerier {
	return &DBQuerier{}
}

// Query executes a SQL query and returns the result.
func (q *DBQuerier) Query(ctx context.Context, sql string) (workflow.QueryResult, error) {
	sql = strings.TrimSuffix(strings.TrimSpace(sql), ";")

	start := time.Now()
	rows, err := config.DB.Query(ctx, sql)
	duration := time.Since(start)
	if err != nil {
		metrics.RecordClickHouseQuery(duration, err)
		return workflow.QueryResult{SQL: sql, Error: err.Error()}, nil
	}
	defer rows.Close()

	// Get column info
	columnTypes := rows.ColumnTypes()
	columns := make([]string, len(columnTypes))
	for i, ct := range columnTypes {
		columns[i] = ct.Name()
	}

	// Collect all rows as maps
	var resultRows []map[string]any
	for rows.Next() {
		// Create properly typed values based on column types
		values := make([]any, len(columnTypes))
		for i, ct := range columnTypes {
			values[i] = reflect.New(ct.ScanType()).Interface()
		}

		if err := rows.Scan(values...); err != nil {
			metrics.RecordClickHouseQuery(duration, err)
			return workflow.QueryResult{SQL: sql, Error: fmt.Sprintf("scan error: %v", err)}, nil
		}

		// Dereference pointers and build map
		row := make(map[string]any)
		for i, col := range columns {
			row[col] = reflect.ValueOf(values[i]).Elem().Interface()
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		metrics.RecordClickHouseQuery(duration, err)
		return workflow.QueryResult{SQL: sql, Error: err.Error()}, nil
	}

	metrics.RecordClickHouseQuery(duration, nil)

	// Sanitize rows to replace NaN/Inf values with nil (JSON-safe)
	workflow.SanitizeRows(resultRows)

	result := workflow.QueryResult{
		SQL:     sql,
		Columns: columns,
		Rows:    resultRows,
		Count:   len(resultRows),
	}
	result.Formatted = formatQueryResult(result)

	return result, nil
}

// formatQueryResult creates a human-readable format of the query result.
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
	maxRows := min(50, len(result.Rows))

	for i := range maxRows {
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

// DBSchemaFetcher implements workflow.SchemaFetcher using the global connection pool.
type DBSchemaFetcher struct{}

// NewDBSchemaFetcher creates a new DBSchemaFetcher.
func NewDBSchemaFetcher() *DBSchemaFetcher {
	return &DBSchemaFetcher{}
}

// FetchSchema retrieves table columns and view definitions from ClickHouse.
func (f *DBSchemaFetcher) FetchSchema(ctx context.Context) (string, error) {
	// Fetch columns
	start := time.Now()
	rows, err := config.DB.Query(ctx, `
		SELECT
			table,
			name,
			type
		FROM system.columns
		WHERE database = $1
		  AND table NOT LIKE 'stg_%'
		ORDER BY table, position
	`, config.Database())
	duration := time.Since(start)
	if err != nil {
		metrics.RecordClickHouseQuery(duration, err)
		return "", fmt.Errorf("failed to fetch columns: %w", err)
	}
	defer rows.Close()
	metrics.RecordClickHouseQuery(duration, nil)

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
	start = time.Now()
	viewRows, err := config.DB.Query(ctx, `
		SELECT
			name,
			as_select
		FROM system.tables
		WHERE database = $1
		  AND engine = 'View'
		  AND name NOT LIKE 'stg_%'
	`, config.Database())
	duration = time.Since(start)
	if err != nil {
		metrics.RecordClickHouseQuery(duration, err)
		return "", fmt.Errorf("failed to fetch views: %w", err)
	}
	defer viewRows.Close()
	metrics.RecordClickHouseQuery(duration, nil)

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
