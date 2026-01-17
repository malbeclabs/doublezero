package dataset

import (
	"context"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
)

// GetCurrentRow returns the current (non-deleted) row for a single entity
// Queries history table using deterministic "latest row per entity" definition
func (d *DimensionType2Dataset) GetCurrentRow(ctx context.Context, conn clickhouse.Connection, entityID SurrogateKey) (map[string]any, error) {
	query, args := buildQuery(queryParams{
		queryType:        queryTypeCurrent,
		historyTableName: d.HistoryTableName(),
		entityID:         entityID,
	})

	result, err := scanSingleRow(ctx, conn, query, args, mapScanner, "failed to query current entity")
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil // Entity not found or deleted (SQL already filters is_deleted=0)
	}
	return *result, nil
}

// GetCurrentRows returns the current (non-deleted) rows for multiple entities.
// If entityIDs is nil or empty, returns all current entities.
// Queries history table using deterministic "latest row per entity" definition
func (d *DimensionType2Dataset) GetCurrentRows(ctx context.Context, conn clickhouse.Connection, entityIDs []SurrogateKey) ([]map[string]any, error) {
	query, args := buildQuery(queryParams{
		queryType:        queryTypeCurrent,
		historyTableName: d.HistoryTableName(),
		entityIDs:        entityIDs,
	})

	results, err := scanRows(ctx, conn, query, args, mapScanner, "failed to query current entities")
	if err != nil {
		return nil, err
	}

	// SQL already filters is_deleted=0, so no need to filter in Go
	return results, nil
}

// GetAsOfRow returns the row for a single entity as of a specific timestamp.
// Returns the most recent version of the entity that was valid at or before the given timestamp.
// If the entity was deleted at or before the given timestamp, returns nil.
func (d *DimensionType2Dataset) GetAsOfRow(ctx context.Context, conn clickhouse.Connection, entityID SurrogateKey, asOfTime time.Time) (map[string]any, error) {
	query, args := buildQuery(queryParams{
		queryType:        queryTypeAsOf,
		historyTableName: d.HistoryTableName(),
		entityID:         entityID,
		asOfTime:         asOfTime,
	})

	result, err := scanSingleRow(ctx, conn, query, args, mapScanner, "failed to query entity as of time")
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return *result, nil
}

// GetAsOfRows returns rows for multiple entities as of a specific timestamp.
// If entityIDs is nil or empty, returns all entities valid at that time.
// Entities that were deleted at or before the given timestamp are excluded.
func (d *DimensionType2Dataset) GetAsOfRows(ctx context.Context, conn clickhouse.Connection, entityIDs []SurrogateKey, asOfTime time.Time) ([]map[string]any, error) {
	query, args := buildQuery(queryParams{
		queryType:        queryTypeAsOf,
		historyTableName: d.HistoryTableName(),
		entityIDs:        entityIDs,
		asOfTime:         asOfTime,
	})

	return scanRows(ctx, conn, query, args, mapScanner, "failed to query entities as of time")
}

// Query executes a raw SQL query and returns the results as []map[string]any.
// The query can be any valid SQL query. Use ? placeholders for parameters and provide them in args.
//
// Example:
//
//	rows, err := dataset.Query(ctx, conn, "SELECT * FROM dim_test_contributors_current WHERE code = ?", []any{"CODE1"})
func (d *DimensionType2Dataset) Query(ctx context.Context, conn clickhouse.Connection, query string, args []any) ([]map[string]any, error) {
	return scanRows(ctx, conn, query, args, mapScanner, "failed to execute query")
}
