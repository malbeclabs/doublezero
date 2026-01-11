package dataset

import (
	"context"
	"fmt"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
)

// WriteBatch writes a batch of fact table data to ClickHouse using PrepareBatch.
// The writeRowFn should return data in the order specified by the Columns configuration.
// Note: ingested_at should be included in writeRowFn output if required by the table schema.
func (f *FactDataset) WriteBatch(
	ctx context.Context,
	conn clickhouse.Connection,
	count int,
	writeRowFn func(int) ([]any, error),
) error {
	if count == 0 {
		return nil
	}

	f.log.Debug("writing fact batch", "table", f.schema.Name(), "count", count)

	// Build batch insert
	batch, err := conn.PrepareBatch(ctx, fmt.Sprintf("INSERT INTO %s", f.TableName()))
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %w", err)
	}

	for i := range count {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during batch insert: %w", ctx.Err())
		default:
		}

		// Get row data from callback
		row, err := writeRowFn(i)
		if err != nil {
			return fmt.Errorf("failed to get row data %d: %w", i, err)
		}

		expectedColCount := len(f.cols)
		if len(row) != expectedColCount {
			return fmt.Errorf("row %d has %d columns, expected exactly %d", i, len(row), expectedColCount)
		}

		if err := batch.Append(row...); err != nil {
			return fmt.Errorf("failed to append row %d: %w", i, err)
		}
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("failed to send batch: %w", err)
	}

	f.log.Debug("wrote fact batch", "table", f.schema.Name(), "count", count)
	return nil
}
