package dataset

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

var (
	dimensionType2DatasetInternalCols = []string{"entity_id", "snapshot_ts", "ingested_at", "op_id", "is_deleted", "attrs_hash"}
)

type DimensionType2Dataset struct {
	log    *slog.Logger
	schema DimensionSchema

	pkCols       []string
	payloadCols  []string
	internalCols []string
}

func NewDimensionType2Dataset(log *slog.Logger, schema DimensionSchema) (*DimensionType2Dataset, error) {
	pkCols, err := extractColumnNames(schema.PrimaryKeyColumns())
	if err != nil {
		return nil, fmt.Errorf("failed to extract primary key columns: %w", err)
	}
	payloadCols, err := extractColumnNames(schema.PayloadColumns())
	if err != nil {
		return nil, fmt.Errorf("failed to extract payload columns: %w", err)
	}
	return &DimensionType2Dataset{
		log:          log,
		schema:       schema,
		pkCols:       pkCols,
		payloadCols:  payloadCols,
		internalCols: dimensionType2DatasetInternalCols,
	}, nil
}

func (d *DimensionType2Dataset) BaseTableName() string {
	return "dim_" + d.schema.Name()
}

func (d *DimensionType2Dataset) StagingTableName() string {
	return "stg_" + d.BaseTableName() + "_snapshot"
}

func (d *DimensionType2Dataset) CurrentTableName() string {
	return d.BaseTableName() + "_current"
}

func (d *DimensionType2Dataset) HistoryTableName() string {
	return d.BaseTableName() + "_history"
}

func (d *DimensionType2Dataset) TombstoneTableName() string {
	return d.BaseTableName() + "_tombstone"
}

func (d *DimensionType2Dataset) PrimaryKeyColumns() []string {
	return d.pkCols
}

func (d *DimensionType2Dataset) PayloadColumns() []string {
	return d.payloadCols
}

func (d *DimensionType2Dataset) InternalColumns() []string {
	return d.internalCols
}

func (d *DimensionType2Dataset) AttrsHashExpression() string {
	return d.AttrsHashExpressionWithPrefix("", false)
}

// AttrsHashExpressionWithPrefix builds attrs_hash expression with optional table prefix and is_deleted override
// If prefix is empty, uses column names directly
// If overrideIsDeleted is true, uses literal 1 for is_deleted instead of the column value
//
// IMPORTANT: attrs_hash excludes PK columns because:
// - entity_id = hash(PK), so if PK changes, it's a new entity (new entity_id)
// - attrs_hash is used to detect "has this entity's meaningful row changed?"
// - Since PK changes create a new entity, only payload columns + is_deleted need to be hashed
// - This invariant must be maintained: PK columns are NOT included in attrs_hash
func (d *DimensionType2Dataset) AttrsHashExpressionWithPrefix(prefix string, overrideIsDeleted bool) string {
	parts := make([]string, len(d.payloadCols)+1)
	for i, col := range d.payloadCols {
		if prefix == "" {
			parts[i] = fmt.Sprintf("toString(%s)", col)
		} else {
			parts[i] = fmt.Sprintf("toString(%s.%s)", prefix, col)
		}
	}
	if overrideIsDeleted {
		parts[len(d.payloadCols)] = "toString(toUInt8(1))"
	} else if prefix == "" {
		parts[len(d.payloadCols)] = "toString(is_deleted)"
	} else {
		parts[len(d.payloadCols)] = fmt.Sprintf("toString(%s.is_deleted)", prefix)
	}
	expr := fmt.Sprintf("cityHash64(tuple(%s))", strings.Join(parts, ", "))
	return expr
}

// buildSelectAttrs builds SELECT clause for attribute columns
func (d *DimensionType2Dataset) SelectAttrsExpression(prefix string) string {
	parts := make([]string, len(d.payloadCols))
	for i, col := range d.payloadCols {
		if prefix == "" {
			parts[i] = col
		} else {
			parts[i] = fmt.Sprintf("%s.%s", prefix, col)
		}
	}
	return strings.Join(parts, ", ")
}

// buildArgMaxAttrs builds argMax expressions for attribute columns
func (d *DimensionType2Dataset) ArgMaxAttrsExpression(payloadColNames []string) string {
	if len(d.payloadCols) == 0 {
		return ""
	}
	parts := make([]string, len(d.payloadCols))
	for i, col := range d.payloadCols {
		parts[i] = fmt.Sprintf("argMax(%s, (snapshot_ts, ingested_at, op_id)) AS %s", col, col)
	}
	return strings.Join(parts, ", ")
}

func (d *DimensionType2Dataset) AllColumns() ([]string, error) {
	return append(d.InternalColumns(), append(d.pkCols, d.payloadCols...)...), nil
}

// extractColumnNames extracts column names from a slice of "name:type" format strings
func extractColumnNames(colDefs []string) ([]string, error) {
	names := make([]string, 0, len(colDefs))
	for _, colDef := range colDefs {
		name, err := extractColumnName(colDef)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

// extractColumnName extracts the column name from a "name:type" format string
func extractColumnName(colDef string) (string, error) {
	parts := strings.SplitN(colDef, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid column definition %q: expected format 'name:type'", colDef)
	}
	return strings.TrimSpace(parts[0]), nil
}

// generateOpID generates a unique operation ID for idempotency
// Returns a UUID v4 string (e.g., "550e8400-e29b-41d4-a716-446655440000")
func generateOpID() string {
	return uuid.New().String()
}
