package dataset

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
)

// GetRowsOptions provides options for querying fact table rows.
type GetRowsOptions struct {
	// StartTime filters rows where time column >= StartTime.
	// If TimeColumn is not set in config, this is ignored.
	StartTime *time.Time

	// EndTime filters rows where time column <= EndTime.
	// If TimeColumn is not set in config, this is ignored.
	EndTime *time.Time

	// WhereClause is an optional additional WHERE clause (without the WHERE keyword).
	// Example: "device_pk = ? AND value > ?"
	// Parameters should be provided in WhereArgs.
	WhereClause string

	// WhereArgs are arguments for the WhereClause placeholders.
	WhereArgs []any

	// Limit limits the number of rows returned (0 = no limit).
	Limit int

	// OrderBy specifies the ORDER BY clause (without the ORDER BY keyword).
	// Example: "event_ts DESC, ingested_at DESC"
	// If empty, defaults to ordering by the time column (if set) or no ordering.
	OrderBy string
}

// GetRows queries fact table rows with optional filters.
// Returns rows as []map[string]any where keys are column names.
func (f *FactDataset) GetRows(ctx context.Context, conn clickhouse.Connection, opts GetRowsOptions) ([]map[string]any, error) {
	query, args := f.buildGetRowsQuery(opts)

	return scanRows(ctx, conn, query, args, mapScanner, "failed to query fact table rows")
}

// buildGetRowsQuery builds a SELECT query for fact table rows based on options.
func (f *FactDataset) buildGetRowsQuery(opts GetRowsOptions) (string, []any) {
	var conditions []string
	var args []any

	// Add time range filters if TimeColumn is configured
	if f.schema.TimeColumn() != "" {
		if opts.StartTime != nil {
			conditions = append(conditions, fmt.Sprintf("%s >= ?", f.schema.TimeColumn()))
			args = append(args, *opts.StartTime)
		}
		if opts.EndTime != nil {
			conditions = append(conditions, fmt.Sprintf("%s <= ?", f.schema.TimeColumn()))
			args = append(args, *opts.EndTime)
		}
	}

	// Add custom WHERE clause if provided
	if opts.WhereClause != "" {
		conditions = append(conditions, opts.WhereClause)
		args = append(args, opts.WhereArgs...)
	}

	// Build WHERE clause
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Build ORDER BY clause
	orderByClause := ""
	if opts.OrderBy != "" {
		orderByClause = "ORDER BY " + opts.OrderBy
	} else if f.schema.TimeColumn() != "" {
		// Default to ordering by time column descending
		orderByClause = fmt.Sprintf("ORDER BY %s DESC", f.schema.TimeColumn())
	}

	// Build LIMIT clause
	limitClause := ""
	if opts.Limit > 0 {
		limitClause = fmt.Sprintf("LIMIT %d", opts.Limit)
	}

	// Build final query
	query := fmt.Sprintf(`
		SELECT *
		FROM %s
		%s
		%s
		%s
	`, f.TableName(), whereClause, orderByClause, limitClause)

	return strings.TrimSpace(query), args
}

// Query executes a raw SQL query and returns the results as []map[string]any.
// The query can be any valid SQL query. Use ? placeholders for parameters and provide them in args.
//
// Example:
//
//	rows, err := factDataset.Query(ctx, conn, "SELECT * FROM fact_test_events WHERE value > ?", []any{100})
func (f *FactDataset) Query(ctx context.Context, conn clickhouse.Connection, query string, args []any) ([]map[string]any, error) {
	return scanRows(ctx, conn, query, args, mapScanner, "failed to execute query")
}
