package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/querier"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/server/metrics"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type QueryInput struct {
	SQL string `json:"sql"`
}

type QueryOutput struct {
	Columns []string   `json:"columns"`
	Rows    []QueryRow `json:"rows"`
	Count   int        `json:"count"`
}

type QueryRow map[string]any

func RegisterQueryTool(log *slog.Logger, server *mcp.Server, querier *querier.Querier, name string, description string) error {
	req, err := jsonschema.For[QueryInput](nil)
	if err != nil {
		return fmt.Errorf("failed to create query input schema: %w", err)
	}

	res, err := jsonschema.For[QueryOutput](nil)
	if err != nil {
		return fmt.Errorf("failed to create query output schema: %w", err)
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:         name,
		Description:  description,
		InputSchema:  req,
		OutputSchema: res,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, req QueryInput) (*mcp.CallToolResult, QueryOutput, error) {
		startTime := time.Now()
		toolName := name
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
