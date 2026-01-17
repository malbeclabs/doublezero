package dataset

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
)

// TypedFactDataset provides typed read and write methods for a specific struct type T.
// This wrapper is necessary because Go doesn't support generic methods on non-generic receiver types.
//
// Usage:
//
//	typed := NewTypedFactDataset[MyStruct](dataset)
//	rows, err := typed.GetRows(ctx, conn, GetRowsOptions{})
//	err = typed.WriteBatch(ctx, conn, rows)
type TypedFactDataset[T any] struct {
	dataset *FactDataset
}

// NewTypedFactDataset creates a new TypedFactDataset for the given dataset and type.
func NewTypedFactDataset[T any](dataset *FactDataset) *TypedFactDataset[T] {
	return &TypedFactDataset[T]{dataset: dataset}
}

// GetRows queries fact table rows with optional filters and returns typed structs.
// The struct type T should have fields matching the column names (case-insensitive, snake_case to CamelCase).
// Use struct tags `ch:"column_name"` to explicitly map fields to columns.
func (t *TypedFactDataset[T]) GetRows(ctx context.Context, conn clickhouse.Connection, opts GetRowsOptions) ([]T, error) {
	query, args := t.dataset.buildGetRowsQuery(opts)

	return scanRows(ctx, conn, query, args, typedScanner[T], "failed to query fact table rows")
}

// Query executes a raw SQL query and returns the results as typed structs.
// The struct type T should have fields matching the column names (case-insensitive, snake_case to CamelCase).
// Use struct tags `ch:"column_name"` to explicitly map fields to columns.
//
// Example:
//
//	rows, err := typed.Query(ctx, conn, "SELECT * FROM fact_test_events WHERE value > ?", []any{100})
func (t *TypedFactDataset[T]) Query(ctx context.Context, conn clickhouse.Connection, query string, args []any) ([]T, error) {
	return scanRows(ctx, conn, query, args, typedScanner[T], "failed to execute query")
}

// WriteBatch writes a batch of typed structs to the fact dataset.
// The struct type T should have fields matching the column names (case-insensitive, snake_case to CamelCase).
// Use struct tags `ch:"column_name"` to explicitly map fields to columns.
// Values are extracted in the order specified by the dataset's Columns configuration.
func (t *TypedFactDataset[T]) WriteBatch(
	ctx context.Context,
	conn clickhouse.Connection,
	rows []T,
) error {
	if len(rows) == 0 {
		return nil
	}

	// Build field map for extracting values from struct
	fieldMap, err := buildFactStructFieldMap[T](t.dataset.cols)
	if err != nil {
		return fmt.Errorf("failed to build field map: %w", err)
	}

	// Convert typed rows to []any in the correct order
	writeRowFn := func(i int) ([]any, error) {
		if i >= len(rows) {
			return nil, fmt.Errorf("index %d out of range (len=%d)", i, len(rows))
		}

		row := rows[i]
		values := extractFactStructValues(row, fieldMap, t.dataset.cols)
		return values, nil
	}

	return t.dataset.WriteBatch(ctx, conn, len(rows), writeRowFn)
}

// buildFactStructFieldMap builds a map from column names to struct field indices for fact tables.
// It handles both struct tags (`ch:"column_name"`) and automatic snake_case conversion.
func buildFactStructFieldMap[T any](colNames []string) (map[string]int, error) {
	var zero T
	resultType := reflect.TypeOf(zero)
	if resultType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("type T must be a struct, got %v", resultType.Kind())
	}

	fieldMap := make(map[string]int)
	for i := 0; i < resultType.NumField(); i++ {
		field := resultType.Field(i)
		fieldName := field.Name

		// Check for struct tag (e.g., `ch:"column_name"`)
		if tag := field.Tag.Get("ch"); tag != "" {
			fieldMap[strings.ToLower(tag)] = i
		}

		// Map field name (CamelCase) to snake_case
		snakeCase := camelToSnake(fieldName)
		fieldMap[strings.ToLower(snakeCase)] = i
		fieldMap[strings.ToLower(fieldName)] = i
	}

	return fieldMap, nil
}

// extractFactStructValues extracts values from a struct in the order specified by colNames.
func extractFactStructValues[T any](row T, fieldMap map[string]int, cols []string) []any {
	rowValue := reflect.ValueOf(row)

	// Extract values in column order
	values := make([]any, 0, len(cols))
	for _, col := range cols {
		colLower := strings.ToLower(col)
		fieldIdx, ok := fieldMap[colLower]
		if !ok {
			// Column not found in struct - use zero value
			values = append(values, nil)
			continue
		}

		fieldValue := rowValue.Field(fieldIdx)
		if !fieldValue.IsValid() {
			values = append(values, nil)
			continue
		}

		// Extract the value, handling pointers
		val := extractFieldValue(fieldValue)
		values = append(values, val)
	}

	return values
}
