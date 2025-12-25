package sqltools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	mcpmetrics "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/metrics"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SchemaInput struct{}

type SchemaOutput struct {
	Schema Schema `json:"schema"`
}

type Schema struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Tables      []TableInfo `json:"tables"`
}

type TableInfo struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Columns     []ColumnInfo `json:"columns"`
}

type ColumnInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type SchemaToolConfig struct {
	Logger *slog.Logger
	DB     DB

	Schema *Schema
}

func (cfg *SchemaToolConfig) Validate() error {
	if cfg.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	if cfg.DB == nil {
		return fmt.Errorf("database is required")
	}
	if cfg.Schema == nil {
		return fmt.Errorf("schema is required")
	}
	if err := cfg.validateSchema(); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}
	return nil
}

type SchemaTool struct {
	log *slog.Logger
	cfg SchemaToolConfig
	db  DB
}

func NewSchemaTool(cfg SchemaToolConfig) (*SchemaTool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate schema tool config: %w", err)
	}
	return &SchemaTool{
		log: cfg.Logger,
		cfg: cfg,
		db:  cfg.DB,
	}, nil
}

func (t *SchemaTool) Register(server *mcp.Server) error {
	req, err := jsonschema.For[SchemaInput](nil)
	if err != nil {
		return fmt.Errorf("failed to create schema input schema: %w", err)
	}

	res, err := jsonschema.For[SchemaOutput](nil)
	if err != nil {
		return fmt.Errorf("failed to create schema output schema: %w", err)
	}

	tool := &mcp.Tool{
		Name:         t.cfg.Schema.Name,
		Description:  t.cfg.Schema.Description,
		InputSchema:  req,
		OutputSchema: res,
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, req SchemaInput) (*mcp.CallToolResult, SchemaOutput, error) {
		startTime := time.Now()
		toolName := t.cfg.Schema.Name
		res, err := t.handleSchema(req)
		duration := time.Since(startTime).Seconds()

		if err != nil {
			mcpmetrics.ToolCallsTotal.WithLabelValues(toolName, "error").Inc()
			mcpmetrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)
			return nil, SchemaOutput{}, err
		}
		mcpmetrics.ToolCallsTotal.WithLabelValues(toolName, "success").Inc()
		mcpmetrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)
		return nil, res, nil
	}

	mcp.AddTool(server, tool, handler)

	return nil
}

func (t *SchemaTool) handleSchema(req SchemaInput) (SchemaOutput, error) {
	t.log.Debug("schema: running schema tool")

	return SchemaOutput{
		Schema: *t.cfg.Schema,
	}, nil
}

func (c *SchemaToolConfig) validateSchema() error {
	// Build list of expected table names from in-code schema
	expectedTables := make([]string, 0, len(c.Schema.Tables))
	for _, schema := range c.Schema.Tables {
		expectedTables = append(expectedTables, schema.Name)
	}

	// Build query with explicit table names
	tableNames := make([]string, len(expectedTables))
	for i, name := range expectedTables {
		tableNames[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(name, "'", "''"))
	}
	query := fmt.Sprintf(`
		SELECT
			table_name,
			column_name,
			data_type
		FROM information_schema.columns
		WHERE table_schema = 'main'
			AND table_name IN (%s)
		ORDER BY table_name, ordinal_position
	`, strings.Join(tableNames, ", "))

	rows, err := c.DB.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query schema: %w", err)
	}
	defer rows.Close()

	tableColumnMap := make(map[string]map[string]string)
	for rows.Next() {
		var tableName, columnName, dataType string
		if err := rows.Scan(&tableName, &columnName, &dataType); err != nil {
			return fmt.Errorf("failed to scan schema row: %w", err)
		}
		if tableColumnMap[tableName] == nil {
			tableColumnMap[tableName] = make(map[string]string)
		}
		tableColumnMap[tableName][columnName] = dataType
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating schema rows: %w", err)
	}

	// Build map of in-code schemas
	inCodeSchemas := make(map[string]map[string]ColumnInfo)
	for _, schema := range c.Schema.Tables {
		inCodeSchemas[schema.Name] = make(map[string]ColumnInfo)
		for _, col := range schema.Columns {
			inCodeSchemas[schema.Name][col.Name] = col
		}
	}

	var missing []string
	for tableName, dbColumns := range tableColumnMap {
		inCodeTable, exists := inCodeSchemas[tableName]
		if !exists {
			missing = append(missing, fmt.Sprintf("table %s: missing from in-code schema", tableName))
			continue
		}

		for colName := range dbColumns {
			inCodeCol, exists := inCodeTable[colName]
			if !exists {
				missing = append(missing, fmt.Sprintf("table %s, column %s: missing from in-code schema", tableName, colName))
				continue
			}
			if inCodeCol.Description == "" {
				missing = append(missing, fmt.Sprintf("table %s, column %s: missing description", tableName, colName))
			}
		}
	}

	// Check for tables in in-code schema that don't exist in database
	for tableName := range inCodeSchemas {
		if _, exists := tableColumnMap[tableName]; !exists {
			missing = append(missing, fmt.Sprintf("table %s: in-code schema but not in database", tableName))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("schema validation failed:\n  %s", strings.Join(missing, "\n  "))
	}

	return nil
}
