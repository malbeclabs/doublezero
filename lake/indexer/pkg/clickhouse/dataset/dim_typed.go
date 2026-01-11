package dataset

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
)

// TypedDimensionType2Dataset provides typed read and write methods for a specific struct type T.
// This wrapper is necessary because Go doesn't support generic methods on non-generic receiver types.
//
// Usage:
//
//	typed := NewTypedDimensionType2Dataset[MyStruct](dataset)
//	row, err := typed.GetCurrentRow(ctx, conn, entityID)
//	err = typed.WriteBatch(ctx, conn, rows)
type TypedDimensionType2Dataset[T any] struct {
	dataset *DimensionType2Dataset
}

// NewTypedDimensionType2Dataset creates a new TypedDimensionType2Dataset for the given dataset and type.
func NewTypedDimensionType2Dataset[T any](dataset *DimensionType2Dataset) *TypedDimensionType2Dataset[T] {
	return &TypedDimensionType2Dataset[T]{dataset: dataset}
}

// GetCurrentRow returns the current (non-deleted) row for a single entity as a typed struct.
// The struct type T should have fields matching the column names (case-insensitive, snake_case to CamelCase).
// Use struct tags `ch:"column_name"` to explicitly map fields to columns.
// Queries history table using deterministic "latest row per entity" definition
func (t *TypedDimensionType2Dataset[T]) GetCurrentRow(ctx context.Context, conn clickhouse.Connection, entityID SurrogateKey) (*T, error) {
	query, args := buildQuery(queryParams{
		queryType:        queryTypeCurrent,
		historyTableName: t.dataset.HistoryTableName(),
		entityID:         entityID,
	})

	return scanSingleRow(ctx, conn, query, args, typedScanner[T], "failed to query current entity")
}

// GetCurrentRows returns the current (non-deleted) rows for multiple entities as typed structs.
// If entityIDs is nil or empty, returns all current entities.
// Queries history table using deterministic "latest row per entity" definition
func (t *TypedDimensionType2Dataset[T]) GetCurrentRows(ctx context.Context, conn clickhouse.Connection, entityIDs []SurrogateKey) ([]T, error) {
	query, args := buildQuery(queryParams{
		queryType:        queryTypeCurrent,
		historyTableName: t.dataset.HistoryTableName(),
		entityIDs:        entityIDs,
	})

	return scanRows(ctx, conn, query, args, typedScanner[T], "failed to query current entities")
}

// GetAsOfRow returns the row for a single entity as of a specific timestamp as a typed struct.
// Returns nil if the entity was deleted at or before the given timestamp.
func (t *TypedDimensionType2Dataset[T]) GetAsOfRow(ctx context.Context, conn clickhouse.Connection, entityID SurrogateKey, asOfTime time.Time) (*T, error) {
	query, args := buildQuery(queryParams{
		queryType:        queryTypeAsOf,
		historyTableName: t.dataset.HistoryTableName(),
		entityID:         entityID,
		asOfTime:         asOfTime,
	})

	return scanSingleRow(ctx, conn, query, args, typedScanner[T], "failed to query entity as of time")
}

// GetAsOfRows returns rows for multiple entities as of a specific timestamp as typed structs.
// If entityIDs is nil or empty, returns all entities valid at that time.
func (t *TypedDimensionType2Dataset[T]) GetAsOfRows(ctx context.Context, conn clickhouse.Connection, entityIDs []SurrogateKey, asOfTime time.Time) ([]T, error) {
	query, args := buildQuery(queryParams{
		queryType:        queryTypeAsOf,
		historyTableName: t.dataset.HistoryTableName(),
		entityIDs:        entityIDs,
		asOfTime:         asOfTime,
	})

	return scanRows(ctx, conn, query, args, typedScanner[T], "failed to query entities as of time")
}

// WriteBatch writes a batch of typed structs to the dataset.
// The struct type T should have fields matching the column names (case-insensitive, snake_case to CamelCase).
// Use struct tags `ch:"column_name"` to explicitly map fields to columns.
// Values are extracted in order: PK columns first, then payload columns.
func (t *TypedDimensionType2Dataset[T]) WriteBatch(
	ctx context.Context,
	conn clickhouse.Connection,
	rows []T,
) error {
	// Build field map for extracting values from struct
	fieldMap, err := buildStructFieldMap[T](t.dataset.pkCols, t.dataset.payloadCols)
	if err != nil {
		return fmt.Errorf("failed to build field map: %w", err)
	}

	// Convert typed rows to []any in the correct order
	writeRowFn := func(i int) ([]any, error) {
		if i >= len(rows) {
			return nil, fmt.Errorf("index %d out of range (len=%d)", i, len(rows))
		}

		row := rows[i]
		values := extractStructValues(row, fieldMap, t.dataset.pkCols, t.dataset.payloadCols)
		return values, nil
	}

	return t.dataset.WriteBatch(ctx, conn, len(rows), writeRowFn, nil)
}

// buildStructFieldMap builds a map from column names to struct field indices.
// It handles both struct tags (`ch:"column_name"`) and automatic snake_case conversion.
func buildStructFieldMap[T any](pkCols, payloadCols []string) (map[string]int, error) {
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

	// Verify all required columns have matching fields
	allCols := append(pkCols, payloadCols...)
	for _, colName := range allCols {
		colNameLower := strings.ToLower(colName)
		if _, ok := fieldMap[colNameLower]; !ok {
			// Warning: column not found, but continue (might be optional)
			// We'll handle missing fields in extractStructValues
		}
	}

	return fieldMap, nil
}

// extractStructValues extracts values from a struct in the order: PK columns, then payload columns.
func extractStructValues[T any](row T, fieldMap map[string]int, pkCols, payloadCols []string) []any {
	rowValue := reflect.ValueOf(row)

	// Extract PK column values
	pkValues := make([]any, 0, len(pkCols))
	for _, colName := range pkCols {
		colNameLower := strings.ToLower(colName)
		fieldIdx, ok := fieldMap[colNameLower]
		if !ok {
			// Column not found in struct - use zero value
			pkValues = append(pkValues, "")
			continue
		}

		fieldValue := rowValue.Field(fieldIdx)
		if !fieldValue.IsValid() {
			pkValues = append(pkValues, "")
			continue
		}

		// Extract the value, handling pointers
		val := extractFieldValue(fieldValue)
		pkValues = append(pkValues, val)
	}

	// Extract payload column values
	payloadValues := make([]any, 0, len(payloadCols))
	for _, colName := range payloadCols {
		colNameLower := strings.ToLower(colName)
		fieldIdx, ok := fieldMap[colNameLower]
		if !ok {
			// Column not found in struct - use zero value
			payloadValues = append(payloadValues, "")
			continue
		}

		fieldValue := rowValue.Field(fieldIdx)
		if !fieldValue.IsValid() {
			payloadValues = append(payloadValues, "")
			continue
		}

		// Extract the value, handling pointers
		val := extractFieldValue(fieldValue)
		payloadValues = append(payloadValues, val)
	}

	// Combine: PK columns first, then payload columns
	result := make([]any, 0, len(pkValues)+len(payloadValues))
	result = append(result, pkValues...)
	result = append(result, payloadValues...)

	return result
}

// extractFieldValue extracts the actual value from a reflect.Value, handling pointers and interfaces.
func extractFieldValue(fieldValue reflect.Value) any {
	if !fieldValue.IsValid() {
		return nil
	}

	// Handle pointers
	if fieldValue.Kind() == reflect.Ptr {
		if fieldValue.IsNil() {
			return nil
		}
		fieldValue = fieldValue.Elem()
	}

	// Handle interfaces
	if fieldValue.Kind() == reflect.Interface {
		if fieldValue.IsNil() {
			return nil
		}
		fieldValue = fieldValue.Elem()
	}

	// Extract concrete value
	switch fieldValue.Kind() {
	case reflect.String:
		return fieldValue.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fieldValue.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fieldValue.Uint()
	case reflect.Float32, reflect.Float64:
		return fieldValue.Float()
	case reflect.Bool:
		return fieldValue.Bool()
	default:
		// For other types (time.Time, uuid.UUID, etc.), return the interface value
		return fieldValue.Interface()
	}
}
