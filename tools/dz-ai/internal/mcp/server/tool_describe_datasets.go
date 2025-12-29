package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	schematypes "github.com/malbeclabs/doublezero/lake/pkg/indexer/schema"
	"github.com/malbeclabs/doublezero/lake/pkg/querier"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/server/metrics"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type DescribeDatasetsInput struct {
	DatasetNames []string `json:"dataset_names"`
}

type DescribeDatasetsOutput struct {
	Datasets []schematypes.Dataset `json:"datasets"`
}

func RegisterDescribeDatasetsTool(log *slog.Logger, server *mcp.Server, querier *querier.Querier) error {
	req, err := jsonschema.For[DescribeDatasetsInput](nil)
	if err != nil {
		return fmt.Errorf("failed to create describe-datasets input schema: %w", err)
	}

	res, err := jsonschema.For[DescribeDatasetsOutput](nil)
	if err != nil {
		return fmt.Errorf("failed to create describe-datasets output schema: %w", err)
	}

	tool := &mcp.Tool{
		Name: "describe-datasets",
		Description: `
			Get full schema details for one or more datasets including usage context and column descriptions.
			Use list-datasets first to discover available datasets with brief descriptions.
			Use query tool to execute "DESCRIBE TABLE <table_name>" to get column details before writing SQL.
		`,
		InputSchema:  req,
		OutputSchema: res,
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, req DescribeDatasetsInput) (*mcp.CallToolResult, DescribeDatasetsOutput, error) {
		startTime := time.Now()
		toolName := "describe-datasets"
		duration := time.Since(startTime).Seconds()

		log.Debug("mcp/tool: handling describe-datasets", "datasets", req.DatasetNames)

		if len(req.DatasetNames) == 0 {
			metrics.ToolCallsTotal.WithLabelValues(toolName, "error").Inc()
			metrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)
			return nil, DescribeDatasetsOutput{}, fmt.Errorf("at least one dataset name is required")
		}

		datasetsByName := make(map[string]schematypes.Dataset)
		for _, dataset := range querier.Datasets() {
			datasetsByName[dataset.Name] = dataset
		}

		foundDatasets := make([]schematypes.Dataset, 0, len(req.DatasetNames))
		for _, datasetName := range req.DatasetNames {
			if dataset, ok := datasetsByName[datasetName]; ok {
				foundDatasets = append(foundDatasets, dataset)
			}
		}

		if len(foundDatasets) == 0 {
			metrics.ToolCallsTotal.WithLabelValues(toolName, "error").Inc()
			metrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)
			return nil, DescribeDatasetsOutput{}, fmt.Errorf("no datasets found")
		}

		metrics.ToolCallsTotal.WithLabelValues(toolName, "success").Inc()
		metrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)
		return nil, DescribeDatasetsOutput{
			Datasets: foundDatasets,
		}, nil
	}

	mcp.AddTool(server, tool, handler)
	return nil
}
