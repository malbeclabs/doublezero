package dzsvc

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type QueryRequest struct {
	SQL string `json:"sql" jsonschema:"SQL query to execute. Use doublezero-schema to see available tables and schemas. Examples: SELECT * FROM dz_devices WHERE status = 'activated', SELECT status, COUNT(*) FROM dz_devices GROUP BY status, SELECT d.*, c.name FROM dz_devices d JOIN dz_contributors c ON d.contributor_pk = c.pk. IMPORTANT: (1) When performing arithmetic operations (multiplication, squaring, etc.) on BIGINT columns like rtt_us, explicitly cast to BIGINT to avoid INT32 overflow: CAST(rtt_us AS BIGINT) * CAST(rtt_us AS BIGINT) instead of rtt_us * rtt_us. (2) Always aggregate data and use LIMIT clauses to keep results manageable - avoid returning large numbers of raw rows. Use GROUP BY, aggregations, and LIMIT to summarize data rather than returning individual samples."`
}

type QueryResponse struct {
	Columns []string   `json:"columns"`
	Rows    []QueryRow `json:"rows"`
	Count   int        `json:"count"`
}

type QueryRow map[string]any

func (t *Tools) registerQuery(server *mcp.Server) error {
	req, err := jsonschema.For[QueryRequest](nil)
	if err != nil {
		return fmt.Errorf("failed to create query input schema: %w", err)
	}

	res, err := jsonschema.For[QueryResponse](nil)
	if err != nil {
		return fmt.Errorf("failed to create query output schema: %w", err)
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:         "doublezero-query",
		Description:  "Execute SQL queries against the DoubleZero serviceability data (PRIMARY dataset). Use this for questions about network structure, devices, links, contributors, users, or metro locations. Use doublezero-telemetry-query for performance/latency metrics, or solana-query for Solana validator data. Use doublezero-schema to see available tables and their schemas. Supports SELECT, JOINs, WHERE clauses, GROUP BY, aggregations (COUNT, SUM, AVG, etc.), ORDER BY, and more. IMPORTANT: (1) When performing arithmetic operations (multiplication, squaring, etc.) on BIGINT columns like rtt_us, explicitly cast to BIGINT to avoid INT32 overflow: use CAST(rtt_us AS BIGINT) * CAST(rtt_us AS BIGINT) instead of rtt_us * rtt_us. (2) Always aggregate data and use LIMIT clauses to keep results manageable - avoid returning large numbers of raw rows. Use GROUP BY, aggregations, and LIMIT to summarize data rather than returning individual samples. For more information about DoubleZero, see https://doublezero.xyz",
		InputSchema:  req,
		OutputSchema: res,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, req QueryRequest) (*mcp.CallToolResult, QueryResponse, error) {
		res, err := t.handleQuery(ctx, req)
		if err != nil {
			return nil, QueryResponse{}, err
		}
		return nil, res, nil
	})

	return nil
}

func (t *Tools) handleQuery(ctx context.Context, req QueryRequest) (QueryResponse, error) {
	t.log.Debug("doublezero/query: running query tool")

	rows, err := t.db.Query(req.SQL)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return QueryResponse{}, fmt.Errorf("failed to get columns: %w", err)
	}

	var resultRows []QueryRow
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return QueryResponse{}, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(QueryRow)
		for i, col := range columns {
			val := values[i]
			if val == nil {
				row[col] = nil
			} else {
				switch v := val.(type) {
				case []byte:
					row[col] = string(v)
				default:
					row[col] = val
				}
			}
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return QueryResponse{}, fmt.Errorf("error iterating rows: %w", err)
	}

	return QueryResponse{
		Columns: columns,
		Rows:    resultRows,
		Count:   len(resultRows),
	}, nil
}
