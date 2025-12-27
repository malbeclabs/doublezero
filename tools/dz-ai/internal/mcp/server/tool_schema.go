package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	schematypes "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/schema"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/server/metrics"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultSchemaReadyTTL = 5 * time.Minute
)

type SchemaInput struct{}

type SchemaOutput struct {
	Schema schematypes.Schema `json:"schema"`
}

func RegisterSchemaTool(log *slog.Logger, server *mcp.Server, schema *schematypes.Schema) error {
	req, err := jsonschema.For[SchemaInput](nil)
	if err != nil {
		return fmt.Errorf("failed to create schema input schema: %w", err)
	}

	res, err := jsonschema.For[SchemaOutput](nil)
	if err != nil {
		return fmt.Errorf("failed to create schema output schema: %w", err)
	}

	if schema == nil {
		return fmt.Errorf("schema is required")
	}

	tool := &mcp.Tool{
		Name:         schema.Name,
		Description:  schema.Description,
		InputSchema:  req,
		OutputSchema: res,
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, req SchemaInput) (*mcp.CallToolResult, SchemaOutput, error) {
		startTime := time.Now()
		toolName := schema.Name
		duration := time.Since(startTime).Seconds()

		log.Debug("mcp/tool: handling schema", "schema", schema.Name)

		if err != nil {
			metrics.ToolCallsTotal.WithLabelValues(toolName, "error").Inc()
			metrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)
			return nil, SchemaOutput{}, err
		}

		metrics.ToolCallsTotal.WithLabelValues(toolName, "success").Inc()
		metrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)
		return nil, SchemaOutput{
			Schema: *schema,
		}, nil
	}

	mcp.AddTool(server, tool, handler)

	return nil
}
