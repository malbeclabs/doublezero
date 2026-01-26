package dataset

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
)

// InitializeScanTargets creates scan targets (valuePtrs and values) based on column types.
// Returns valuePtrs (pointers for scanning) and values (same pointers, for compatibility).
func InitializeScanTargets(columnTypes []driver.ColumnType) ([]any, []any) {
	valuePtrs := make([]any, len(columnTypes))
	values := make([]any, len(columnTypes))

	for i, colType := range columnTypes {
		dbType := colType.DatabaseTypeName()
		// Check if it's a Nullable type
		isNullable := strings.HasPrefix(dbType, "Nullable(")
		baseType := dbType
		if isNullable {
			// Extract the inner type from Nullable(Type)
			baseType = strings.TrimPrefix(dbType, "Nullable(")
			baseType = strings.TrimSuffix(baseType, ")")
		}

		switch {
		case baseType == "String" || baseType == "FixedString":
			var v string
			if isNullable {
				// For nullable, we need a pointer to pointer for scanning
				var p *string
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "Date" || baseType == "Date32" || baseType == "DateTime" || baseType == "DateTime64" || strings.HasPrefix(baseType, "DateTime"):
			var v time.Time
			if isNullable {
				var p *time.Time
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "UInt8":
			var v uint8
			if isNullable {
				var p *uint8
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "UInt16":
			var v uint16
			if isNullable {
				var p *uint16
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "UInt32":
			var v uint32
			if isNullable {
				var p *uint32
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "UInt64":
			var v uint64
			if isNullable {
				var p *uint64
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "Int8":
			var v int8
			if isNullable {
				var p *int8
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "Int16":
			var v int16
			if isNullable {
				var p *int16
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "Int32":
			var v int32
			if isNullable {
				var p *int32
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "Int64":
			var v int64
			if isNullable {
				var p *int64
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "Float32":
			var v float32
			if isNullable {
				var p *float32
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "Float64":
			var v float64
			if isNullable {
				var p *float64
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "Bool":
			var v bool
			if isNullable {
				var p *bool
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		case baseType == "UUID":
			var v uuid.UUID
			if isNullable {
				var p *uuid.UUID
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		default:
			var v string
			if isNullable {
				var p *string
				valuePtrs[i] = &p
			} else {
				valuePtrs[i] = &v
			}
		}
		values[i] = valuePtrs[i]
	}

	return valuePtrs, values
}

// dereferencePointersToMap converts scanned pointers to a map.
// For nullable columns: returns nil for nulls, concrete value otherwise (not **T).
func dereferencePointersToMap(valuePtrs []any, columns []string) map[string]any {
	result := make(map[string]any, len(columns))
	for i, col := range columns {
		switch v := valuePtrs[i].(type) {
		case *string:
			result[col] = *v
		case **string:
			// Nullable string - return nil if null, else dereference
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *time.Time:
			result[col] = *v
		case **time.Time:
			// Nullable time - return nil if null, else dereference
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *uint8:
			result[col] = *v
		case **uint8:
			// Nullable uint8 - return nil if null, else dereference
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *uint16:
			result[col] = *v
		case **uint16:
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *uint32:
			result[col] = *v
		case **uint32:
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *uint64:
			result[col] = *v
		case **uint64:
			// Nullable uint64 - return nil if null, else dereference
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *int8:
			result[col] = *v
		case **int8:
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *int16:
			result[col] = *v
		case **int16:
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *int32:
			result[col] = *v
		case **int32:
			// Nullable int32 - return nil if null, else dereference
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *int64:
			result[col] = *v
		case **int64:
			// Nullable int64 - return nil if null, else dereference
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *float32:
			result[col] = *v
		case **float32:
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *float64:
			result[col] = *v
		case **float64:
			// Nullable float64 - return nil if null, else dereference
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *bool:
			result[col] = *v
		case **bool:
			// Nullable bool - return nil if null, else dereference
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		case *uuid.UUID:
			result[col] = *v
		case **uuid.UUID:
			// Nullable UUID - return nil if null, else dereference
			if v == nil || *v == nil {
				result[col] = nil
			} else {
				result[col] = **v
			}
		default:
			// Use DereferencePointer as fallback to avoid leaking pointers for unsupported types
			result[col] = DereferencePointer(valuePtrs[i])
		}
	}
	return result
}

// DereferencePointer dereferences a pointer to its underlying value.
// Handles both *T (non-nullable) and **T (nullable) types.
// For nullable types, returns nil if the inner pointer is nil, else dereferences twice.
func DereferencePointer(ptr any) any {
	if ptr == nil {
		return nil
	}
	switch v := ptr.(type) {
	case *string:
		return *v
	case **string:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *time.Time:
		return *v
	case **time.Time:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *uint8:
		return *v
	case **uint8:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *uint16:
		return *v
	case **uint16:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *uint32:
		return *v
	case **uint32:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *uint64:
		return *v
	case **uint64:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *int8:
		return *v
	case **int8:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *int16:
		return *v
	case **int16:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *int32:
		return *v
	case **int32:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *int64:
		return *v
	case **int64:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *float32:
		return *v
	case **float32:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *float64:
		return *v
	case **float64:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *bool:
		return *v
	case **bool:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *uuid.UUID:
		return *v
	case **uuid.UUID:
		if v == nil || *v == nil {
			return nil
		}
		return **v
	case *any: // For default case in initializeScanTargets
		return *v
	default:
		return ptr // Should not happen if initializeScanTargets is comprehensive
	}
}

// scanIntoStruct scans query results into a struct using reflection.
// It maps column names to struct fields (case-insensitive, with snake_case to CamelCase conversion).
func scanIntoStruct[T any](valuePtrs []any, columns []string) (T, error) {
	var result T
	resultValue := reflect.ValueOf(&result).Elem()
	resultType := resultValue.Type()

	// Build a map of column names to field indices (case-insensitive)
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

	// Map values from columns to struct fields
	for i, colName := range columns {
		colNameLower := strings.ToLower(colName)
		fieldIdx, ok := fieldMap[colNameLower]
		if !ok {
			continue // Skip columns that don't have a matching struct field
		}

		field := resultType.Field(fieldIdx)
		fieldValue := resultValue.Field(fieldIdx)

		if !fieldValue.CanSet() {
			continue // Skip unexported fields
		}

		// Get the value from the pointer
		val := DereferencePointer(valuePtrs[i])

		// Convert and set the value
		if err := setFieldValue(fieldValue, field.Type, val); err != nil {
			return result, err
		}
	}

	return result, nil
}

// setFieldValue sets a struct field value, handling type conversions.
func setFieldValue(fieldValue reflect.Value, fieldType reflect.Type, val any) error {
	if val == nil {
		// Set zero value for nil
		fieldValue.Set(reflect.Zero(fieldType))
		return nil
	}

	valValue := reflect.ValueOf(val)
	valType := valValue.Type()

	// Direct assignment if types match
	if valType.AssignableTo(fieldType) {
		fieldValue.Set(valValue)
		return nil
	}

	// Handle pointer types
	if fieldType.Kind() == reflect.Ptr {
		if valType.AssignableTo(fieldType.Elem()) {
			ptr := reflect.New(fieldType.Elem())
			ptr.Elem().Set(valValue)
			fieldValue.Set(ptr)
			return nil
		}
	}

	// Handle conversions
	if valType.ConvertibleTo(fieldType) {
		fieldValue.Set(valValue.Convert(fieldType))
		return nil
	}

	// Handle pointer to pointer
	if fieldType.Kind() == reflect.Ptr && valType.ConvertibleTo(fieldType.Elem()) {
		ptr := reflect.New(fieldType.Elem())
		ptr.Elem().Set(valValue.Convert(fieldType.Elem()))
		fieldValue.Set(ptr)
		return nil
	}

	// Return error for impossible conversions to catch schema drift bugs
	return fmt.Errorf("cannot convert %s to %s for field", valType, fieldType)
}

// camelToSnake converts CamelCase to snake_case.
func camelToSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}
