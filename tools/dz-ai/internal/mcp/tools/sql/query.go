package sqltools

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	mcpmetrics "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/metrics"
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

type QueryToolConfig struct {
	Logger *slog.Logger
	DB     DB

	Name        string
	Description string
}

func (cfg *QueryToolConfig) Validate() error {
	if cfg.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	if cfg.DB == nil {
		return fmt.Errorf("database is required")
	}
	if cfg.Name == "" {
		return fmt.Errorf("name is required")
	}
	if cfg.Description == "" {
		return fmt.Errorf("description is required")
	}
	return nil
}

type QueryTool struct {
	log *slog.Logger
	cfg QueryToolConfig
	db  DB
}

func NewQueryTool(cfg QueryToolConfig) (*QueryTool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate query tool config: %w", err)
	}
	return &QueryTool{
		log: cfg.Logger,
		cfg: cfg,
		db:  cfg.DB,
	}, nil
}

func (t *QueryTool) Register(server *mcp.Server) error {
	req, err := jsonschema.For[QueryInput](nil)
	if err != nil {
		return fmt.Errorf("failed to create query input schema: %w", err)
	}

	res, err := jsonschema.For[QueryOutput](nil)
	if err != nil {
		return fmt.Errorf("failed to create query output schema: %w", err)
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:         t.cfg.Name,
		Description:  t.cfg.Description,
		InputSchema:  req,
		OutputSchema: res,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, req QueryInput) (*mcp.CallToolResult, QueryOutput, error) {
		startTime := time.Now()
		toolName := t.cfg.Name
		res, err := t.handleQuery(ctx, req)
		duration := time.Since(startTime).Seconds()

		if err != nil {
			mcpmetrics.ToolCallsTotal.WithLabelValues(toolName, "error").Inc()
			mcpmetrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)
			return nil, QueryOutput{}, err
		}
		mcpmetrics.ToolCallsTotal.WithLabelValues(toolName, "success").Inc()
		mcpmetrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)
		return nil, res, nil
	})
	return nil
}

func (t *QueryTool) handleQuery(ctx context.Context, req QueryInput) (QueryOutput, error) {
	t.log.Debug("query: running query tool")

	conn, err := t.db.Conn(ctx)
	if err != nil {
		return QueryOutput{}, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, req.SQL)
	if err != nil {
		return QueryOutput{}, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return QueryOutput{}, fmt.Errorf("failed to get columns: %w", err)
	}

	var resultRows []QueryRow
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return QueryOutput{}, fmt.Errorf("failed to scan row: %w", err)
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
		return QueryOutput{}, fmt.Errorf("error iterating rows: %w", err)
	}

	return QueryOutput{
		Columns: columns,
		Rows:    resultRows,
		Count:   len(resultRows),
	}, nil
}
