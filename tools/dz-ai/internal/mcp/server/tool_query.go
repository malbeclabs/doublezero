package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/malbeclabs/doublezero/lake/pkg/querier"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/server/metrics"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const queryToolDescription = `
PURPOSE:
Execute DuckDB SQL queries across all DoubleZero datasets (serviceability, telemetry latency, telemetry usage, and Solana).

USAGE RULES:
- Always use list-datasets to discover available datasets and their underlying tables and columns before writing SQL.
- Do not guess column names. Execute DESCRIBE TABLE <table_name> to get column details before writing SQL.
- Cast all columns to their specified types when querying the underlying tables with SQL.
- Prefer aggregated, well-constructed queries that return summarized results.
- Aggregate data using 'GROUP BY' and apply 'LIMIT' to keep result sets small.

PARALLEL EXECUTION:
- Execute multiple queries simultaneously when you need data from multiple tables, datasets, or sources.
- Do not run queries sequentially when they can be executed concurrently. Batch independent queries and run them in parallel.

SUPPORTED SQL:
- 'SELECT', 'JOIN', 'WHERE', 'GROUP BY', aggregations ('COUNT', 'SUM', 'AVG', percentiles), 'ORDER BY', 'LIMIT'

IMPORTANT CONSTRAINTS:
1. When performing arithmetic on 'BIGINT' columns (e.g. 'rtt_us'), explicitly cast operands to 'BIGINT' to avoid overflow:
	CAST(rtt_us AS BIGINT) * CAST(rtt_us AS BIGINT)
2. Do not return large volumes of raw rows. Summarize whenever possible.

SQL INVARIANTS (NON-NEGOTIABLE):
- Never use SQL keywords or grammar terms as identifiers (tables, CTEs, aliases, columns), even if quoted.
- Treat DuckDB grammar terms, relation producers (e.g. 'unnest', 'read_*', '*_scan'), window/planning terms, and cross-dialect keywords ('do', 'set', 'execute') as reserved.
- Primary keys are always named 'pk' (VARCHAR).
- Foreign keys typically follow '{referenced_table}_pk' pattern (e.g., device_pk, link_pk, contributor_pk, metro_pk) and always join to 'pk'. Some use descriptive names (e.g., side_a_pk, side_z_pk, origin_device_pk, target_device_pk) but still join to 'pk'.
- Joins must match foreign key → primary key ('table.fk = other.pk').
- Never use 'do' or 'dt' as aliases.
- SCD2 TABLES: Many tables use SCD2 (Slowly Changing Dimension Type 2). Always query {table}_current for current state; {table}_history contains historical versions. See schema descriptions for details.
- FACT TABLES: Time-series fact tables use the {table}_raw suffix (e.g., dz_device_link_latency_samples_raw, dz_device_iface_usage_raw). These are append-only tables. Query the _raw tables for raw data.

For general information about DoubleZero, see https://doublezero.xyz/
`

const queryToolName = "query"

type QueryInput struct {
	SQL string `json:"sql"`
}

type QueryOutput struct {
	Columns []string   `json:"columns"`
	Rows    []QueryRow `json:"rows"`
	Count   int        `json:"count"`
}

type QueryRow map[string]any

func RegisterQueryTool(log *slog.Logger, server *mcp.Server, querier *querier.Querier) error {
	req, err := jsonschema.For[QueryInput](nil)
	if err != nil {
		return fmt.Errorf("failed to create query input schema: %w", err)
	}

	res, err := jsonschema.For[QueryOutput](nil)
	if err != nil {
		return fmt.Errorf("failed to create query output schema: %w", err)
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:         queryToolName,
		Description:  queryToolDescription,
		InputSchema:  req,
		OutputSchema: res,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, req QueryInput) (*mcp.CallToolResult, QueryOutput, error) {
		startTime := time.Now()
		toolName := queryToolName
		res, err := handleQuery(ctx, log, querier, req)
		duration := time.Since(startTime).Seconds()

		log.Debug("mcp/tool: handling query", "sql", req.SQL)

		if err != nil {
			metrics.ToolCallsTotal.WithLabelValues(toolName, "error").Inc()
			metrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)
			return nil, QueryOutput{}, err
		}
		metrics.ToolCallsTotal.WithLabelValues(toolName, "success").Inc()
		metrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)
		return nil, res, nil
	})
	return nil
}

func handleQuery(ctx context.Context, log *slog.Logger, querier *querier.Querier, req QueryInput) (QueryOutput, error) {
	resp, err := querier.Query(ctx, req.SQL)
	if err != nil {
		return QueryOutput{}, fmt.Errorf("failed to execute query: %w", err)
	}

	rows := make([]QueryRow, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		queryRow := make(QueryRow)
		for _, col := range resp.Columns {
			queryRow[col] = row[col]
		}
		rows = append(rows, queryRow)
	}

	return QueryOutput{
		Columns: resp.Columns,
		Rows:    rows,
		Count:   resp.Count,
	}, nil
}
