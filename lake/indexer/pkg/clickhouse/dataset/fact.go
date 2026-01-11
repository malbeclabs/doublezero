package dataset

import (
	"fmt"
	"log/slog"
	"slices"
)

type DedupMode string

const (
	DedupNone      DedupMode = "none"
	DedupReplacing DedupMode = "replacing"
)

type FactDataset struct {
	log           *slog.Logger
	schema        FactSchema
	payloadCols   []string
	uniqueKeyCols []string
	cols          []string
}

func NewFactDataset(log *slog.Logger, schema FactSchema) (*FactDataset, error) {
	if schema.Name() == "" {
		return nil, fmt.Errorf("table_name is required")
	}
	if len(schema.Columns()) == 0 {
		return nil, fmt.Errorf("columns is required")
	}

	payloadCols, err := extractColumnNames(schema.Columns())
	if err != nil {
		return nil, fmt.Errorf("failed to extract column names: %w", err)
	}

	cols := payloadCols
	if schema.TimeColumn() != "" && !slices.Contains(payloadCols, schema.TimeColumn()) {
		cols = append(cols, schema.TimeColumn())
	}

	uniqueKeyCols := schema.UniqueKeyColumns()
	if len(cols) > 0 {
		for _, uniqueKeyCol := range uniqueKeyCols {
			if !slices.Contains(cols, uniqueKeyCol) {
				return nil, fmt.Errorf("unique key column %q must be a subset of columns", uniqueKeyCol)
			}
		}
	}

	if schema.DedupMode() == DedupReplacing && schema.DedupVersionColumn() == "" {
		return nil, fmt.Errorf("dedup version column is required when dedup mode is replacing")
	}

	if schema.TimeColumn() == "" && schema.PartitionByTime() {
		return nil, fmt.Errorf("time column is required when partition by time is true")
	}

	return &FactDataset{
		log:           log,
		schema:        schema,
		payloadCols:   payloadCols,
		uniqueKeyCols: uniqueKeyCols,
		cols:          cols,
	}, nil
}

func (f *FactDataset) TableName() string {
	return "fact_" + f.schema.Name()
}
