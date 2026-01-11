package dataset

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
)

// ColumnMetadata represents metadata about a column.
type ColumnMetadata struct {
	Name             string
	DatabaseTypeName string
	ScanType         string
}

// QueryResult represents the result of a query execution with column metadata.
type QueryResult struct {
	Columns     []string
	ColumnTypes []ColumnMetadata
	Rows        []map[string]any
	Count       int
}

// ScanQueryResults scans query results into a slice of maps with column metadata.
func ScanQueryResults(rows driver.Rows) ([]string, []ColumnMetadata, []map[string]any, error) {
	columns := rows.Columns()
	columnTypes := rows.ColumnTypes()

	// Build column metadata
	colMetadata := make([]ColumnMetadata, len(columns))
	for i, colType := range columnTypes {
		colMetadata[i] = ColumnMetadata{
			Name:             colType.Name(),
			DatabaseTypeName: colType.DatabaseTypeName(),
			ScanType:         "",
		}
		if colType.ScanType() != nil {
			colMetadata[i].ScanType = colType.ScanType().String()
		}
	}

	// Initialize scan targets
	valuePtrs, _ := InitializeScanTargets(columnTypes)

	var resultRows []map[string]any
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Convert scanned values to map
		row := dereferencePointersToMap(valuePtrs, columns)
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return columns, colMetadata, resultRows, nil
}

// Query executes a raw SQL query and returns the results with column metadata.
//
// Example:
//
//	result, err := dataset.Query(ctx, conn, "SELECT * FROM table WHERE id = ?", []any{123})
func Query(ctx context.Context, conn clickhouse.Connection, query string, args []any) (*QueryResult, error) {
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	columns, colMetadata, resultRows, err := ScanQueryResults(rows)
	if err != nil {
		return nil, err
	}

	return &QueryResult{
		Columns:     columns,
		ColumnTypes: colMetadata,
		Rows:        resultRows,
		Count:       len(resultRows),
	}, nil
}
