package indexer

import (
	"context"
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/duck"
	schematypes "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/schema"
)

func (i *Indexer) validateSchemas(ctx context.Context) error {
	for _, schema := range i.Schemas() {
		if err := ValidateSchema(ctx, i.cfg.DB, schema); err != nil {
			return fmt.Errorf("failed to validate schema: %w", err)
		}
	}
	return nil
}

func ValidateSchema(ctx context.Context, db duck.DB, schema *schematypes.Schema) error {
	// Build list of expected table names from in-code schema
	expectedTables := make([]string, 0, len(schema.Tables))
	for _, table := range schema.Tables {
		expectedTables = append(expectedTables, table.Name)
	}

	// Build query with explicit table names
	tableNames := make([]string, len(expectedTables))
	for i, name := range expectedTables {
		tableNames[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(name, "'", "''"))
	}
	query := fmt.Sprintf(`
		SELECT
			table_name,
			column_name,
			data_type
		FROM information_schema.columns
		WHERE table_catalog = '%s' AND table_schema = '%s'
			AND table_name IN (%s)
		ORDER BY table_name, ordinal_position
	`, db.Catalog(), db.Schema(), strings.Join(tableNames, ", "))

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query schema: %w", err)
	}
	defer rows.Close()

	tableColumnMap := make(map[string]map[string]string)
	for rows.Next() {
		var tableName, columnName, dataType string
		if err := rows.Scan(&tableName, &columnName, &dataType); err != nil {
			return fmt.Errorf("failed to scan schema row: %w", err)
		}
		if tableColumnMap[tableName] == nil {
			tableColumnMap[tableName] = make(map[string]string)
		}
		tableColumnMap[tableName][columnName] = dataType
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating schema rows: %w", err)
	}

	// Build map of in-code schemas
	inCodeSchemas := make(map[string]map[string]schematypes.ColumnInfo)
	for _, schema := range schema.Tables {
		inCodeSchemas[schema.Name] = make(map[string]schematypes.ColumnInfo)
		for _, col := range schema.Columns {
			inCodeSchemas[schema.Name][col.Name] = col
		}
	}

	var missing []string
	for tableName, dbColumns := range tableColumnMap {
		inCodeTable, exists := inCodeSchemas[tableName]
		if !exists {
			missing = append(missing, fmt.Sprintf("table %s: missing from in-code schema", tableName))
			continue
		}

		for colName := range dbColumns {
			inCodeCol, exists := inCodeTable[colName]
			if !exists {
				missing = append(missing, fmt.Sprintf("table %s, column %s: missing from in-code schema", tableName, colName))
				continue
			}
			if inCodeCol.Description == "" {
				missing = append(missing, fmt.Sprintf("table %s, column %s: missing description", tableName, colName))
			}
		}
	}

	// Check for tables in in-code schema that don't exist in database
	for tableName := range inCodeSchemas {
		if _, exists := tableColumnMap[tableName]; !exists {
			missing = append(missing, fmt.Sprintf("table %s: in-code schema but not in database", tableName))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("schema validation failed:\n  %s", strings.Join(missing, "\n  "))
	}

	return nil
}
