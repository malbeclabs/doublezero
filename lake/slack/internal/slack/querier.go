package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
)

// ClickhouseQuerier implements pipeline.Querier using the clickhouse client
type ClickhouseQuerier struct {
	db clickhouse.Client
}

// NewClickhouseQuerier creates a new ClickhouseQuerier
func NewClickhouseQuerier(db clickhouse.Client) *ClickhouseQuerier {
	return &ClickhouseQuerier{db: db}
}

// Query executes a SQL query and returns the result
func (q *ClickhouseQuerier) Query(ctx context.Context, sql string) (pipeline.QueryResult, error) {
	sql = strings.TrimSuffix(strings.TrimSpace(sql), ";")

	conn, err := q.db.Conn(ctx)
	if err != nil {
		return pipeline.QueryResult{SQL: sql, Error: fmt.Sprintf("connection error: %v", err)}, nil
	}
	defer conn.Close()

	result, err := dataset.Query(ctx, conn, sql, nil)
	if err != nil {
		return pipeline.QueryResult{SQL: sql, Error: err.Error()}, nil
	}

	qr := pipeline.QueryResult{
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
func formatQueryResult(result pipeline.QueryResult) string {
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
			values = append(values, fmt.Sprintf("%v", row[col]))
		}
		sb.WriteString(strings.Join(values, " | ") + "\n")
	}

	if len(result.Rows) > 50 {
		sb.WriteString(fmt.Sprintf("... and %d more rows\n", len(result.Rows)-50))
	}

	return sb.String()
}
