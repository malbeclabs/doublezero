package dataset

import (
	"context"
	"fmt"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
)

// queryType represents the type of query to build
type queryType int

const (
	queryTypeCurrent queryType = iota
	queryTypeAsOf
)

// queryParams holds parameters for building queries
type queryParams struct {
	queryType        queryType
	entityID         SurrogateKey
	entityIDs        []SurrogateKey
	asOfTime         time.Time
	historyTableName string
}

// buildQuery builds the SQL query based on the query parameters
func buildQuery(p queryParams) (string, []any) {
	var query string
	var args []any

	switch p.queryType {
	case queryTypeCurrent:
		// Query history table instead of current table
		// Use deterministic "latest row per entity" definition
		if p.entityID != "" {
			// Single entity - current
			// Get the latest row first (regardless of is_deleted), then filter
			// This ensures we return nil if the latest row is a tombstone
			query = fmt.Sprintf(`
				SELECT *
				FROM (
					SELECT *
					FROM %s
					WHERE entity_id = ?
					ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC
					LIMIT 1
				)
				WHERE is_deleted = 0
			`, p.historyTableName)
			args = []any{string(p.entityID)}
		} else {
			// Multiple entities or all - current
			if len(p.entityIDs) == 0 {
				query = fmt.Sprintf(`
					WITH ranked AS (
						SELECT
							*,
							ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
						FROM %s
					)
					SELECT *
					FROM ranked
					WHERE rn = 1 AND is_deleted = 0
				`, p.historyTableName)
			} else {
				placeholders := buildPlaceholders(len(p.entityIDs))
				query = fmt.Sprintf(`
					WITH ranked AS (
						SELECT
							*,
							ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
						FROM %s
						WHERE entity_id IN (%s)
					)
					SELECT *
					FROM ranked
					WHERE rn = 1 AND is_deleted = 0
				`, p.historyTableName, placeholders)
				args = convertToAnySlice(p.entityIDs)
			}
		}

	case queryTypeAsOf:
		if p.entityID != "" {
			// Single entity - as of
			// Get latest row (deleted or not) at/before asOf, return only if not deleted
			// This correctly handles "deleted then re-created" scenarios
			query = fmt.Sprintf(`
				SELECT *
				FROM (
					SELECT *
					FROM %s
					WHERE entity_id = ? AND snapshot_ts <= ?
					ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC
					LIMIT 1
				)
				WHERE is_deleted = 0
			`, p.historyTableName)
			args = []any{string(p.entityID), p.asOfTime}
		} else {
			// Multiple entities or all - as of
			// Get latest row (deleted or not) at/before asOf per entity, return only if not deleted
			if len(p.entityIDs) == 0 {
				query = fmt.Sprintf(`
					WITH ranked AS (
						SELECT
							*,
							ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
						FROM %s
						WHERE snapshot_ts <= ?
					)
					SELECT *
					FROM ranked
					WHERE rn = 1 AND is_deleted = 0
				`, p.historyTableName)
				args = []any{p.asOfTime}
			} else {
				placeholders := buildPlaceholders(len(p.entityIDs))
				query = fmt.Sprintf(`
					WITH ranked AS (
						SELECT
							*,
							ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
						FROM %s
						WHERE snapshot_ts <= ? AND entity_id IN (%s)
					)
					SELECT *
					FROM ranked
					WHERE rn = 1 AND is_deleted = 0
				`, p.historyTableName, placeholders)
				args = []any{p.asOfTime}
				args = append(args, convertToAnySlice(p.entityIDs)...)
			}
		}
	}

	return query, args
}

// buildPlaceholders builds a comma-separated list of ? placeholders
func buildPlaceholders(count int) string {
	if count == 0 {
		return ""
	}
	placeholders := ""
	for i := 0; i < count; i++ {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += "?"
	}
	return placeholders
}

// convertToAnySlice converts []SurrogateKey to []any
func convertToAnySlice(ids []SurrogateKey) []any {
	result := make([]any, len(ids))
	for i, id := range ids {
		result[i] = string(id) // Explicitly convert to string
	}
	return result
}

// scanRows scans query results into a slice of results using the provided scanner function
func scanRows[T any](
	ctx context.Context,
	conn clickhouse.Connection,
	query string,
	args []any,
	scanner func(valuePtrs []any, columns []string) (T, error),
	errorMsg string,
) ([]T, error) {
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errorMsg, err)
	}
	defer rows.Close()

	columns := rows.Columns()
	columnTypes := rows.ColumnTypes()
	valuePtrs, _ := InitializeScanTargets(columnTypes)

	var results []T
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		result, err := scanner(valuePtrs, columns)
		if err != nil {
			return nil, fmt.Errorf("failed to scan result: %w", err)
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}

// scanSingleRow scans a single row from query results
func scanSingleRow[T any](
	ctx context.Context,
	conn clickhouse.Connection,
	query string,
	args []any,
	scanner func(valuePtrs []any, columns []string) (T, error),
	errorMsg string,
) (*T, error) {
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errorMsg, err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil // Not found
	}

	columns := rows.Columns()
	columnTypes := rows.ColumnTypes()
	valuePtrs, _ := InitializeScanTargets(columnTypes)

	if err := rows.Scan(valuePtrs...); err != nil {
		return nil, fmt.Errorf("failed to scan row: %w", err)
	}

	result, err := scanner(valuePtrs, columns)
	if err != nil {
		return nil, fmt.Errorf("failed to scan result: %w", err)
	}

	return &result, nil
}

// mapScanner creates a scanner function that converts to map[string]any
func mapScanner(valuePtrs []any, columns []string) (map[string]any, error) {
	result := dereferencePointersToMap(valuePtrs, columns)
	return result, nil
}

// typedScanner creates a scanner function that converts to a typed struct
// Note: SQL queries already filter is_deleted=0 for current/as-of queries,
// so deleted rows should never reach this scanner for those query types.
func typedScanner[T any](valuePtrs []any, columns []string) (T, error) {
	return scanIntoStruct[T](valuePtrs, columns)
}
