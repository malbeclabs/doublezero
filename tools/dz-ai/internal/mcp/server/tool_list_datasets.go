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

type ListDatasetsInput struct{}

type DatasetSummary struct {
	Name        string `json:"name"`
	Purpose     string `json:"purpose"`
	Description string `json:"description"`
	DatasetType string `json:"dataset_type"`
}

type ListDatasetsOutput struct {
	Datasets []DatasetSummary `json:"datasets"`
}

func RegisterListDatasetsTool(log *slog.Logger, server *mcp.Server, querier *querier.Querier) error {
	req, err := jsonschema.For[ListDatasetsInput](nil)
	if err != nil {
		return fmt.Errorf("failed to create list-datasets input schema: %w", err)
	}

	res, err := jsonschema.For[ListDatasetsOutput](nil)
	if err != nil {
		return fmt.Errorf("failed to create list-datasets output schema: %w", err)
	}

	tool := &mcp.Tool{
		Name:         "list-datasets",
		Description:  `List all available datasets. Returns dataset names with brief descriptions. Use this to discover available datasets and their underlying tables before requesting full descriptions with "describe-datasets" or querying with "query".`,
		InputSchema:  req,
		OutputSchema: res,
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, req ListDatasetsInput) (*mcp.CallToolResult, ListDatasetsOutput, error) {
		startTime := time.Now()
		toolName := "list-datasets"
		duration := time.Since(startTime).Seconds()

		log.Debug("mcp/tool: handling list-datasets")

		datasets := querier.Datasets()
		summaries := make([]DatasetSummary, len(datasets))
		for i, dataset := range datasets {
			description := dataset.Description
			if len(description) > 100 {
				description = description[:100] + "..."
			}
			summaries[i] = DatasetSummary{
				Name:        dataset.Name,
				DatasetType: string(dataset.DatasetType),
				Purpose:     dataset.Purpose,
				Description: description,
			}
		}

		metrics.ToolCallsTotal.WithLabelValues(toolName, "success").Inc()
		metrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)
		return nil, ListDatasetsOutput{
			Datasets: summaries,
		}, nil
	}

	mcp.AddTool(server, tool, handler)
	return nil
}
