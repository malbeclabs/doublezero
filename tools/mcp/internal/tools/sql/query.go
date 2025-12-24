package sqltools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/jsonschema-go/jsonschema"
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
		res, err := t.handleQuery(req)
		if err != nil {
			return nil, QueryOutput{}, err
		}
		return nil, res, nil
	})
	return nil
}

func (t *QueryTool) handleQuery(req QueryInput) (QueryOutput, error) {
	t.log.Debug("query: running query tool")

	rows, err := t.db.Query(req.SQL)
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
