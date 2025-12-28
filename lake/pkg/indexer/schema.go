package indexer

import (
	"context"
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	schematypes "github.com/malbeclabs/doublezero/lake/pkg/indexer/schema"
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
	// For SCD2 tables, we validate the _current table instead of the base table name
	expectedTables := make([]string, 0, len(schema.Tables))
	tableNameMap := make(map[string]string) // Maps DB table name to schema table name
	for _, table := range schema.Tables {
		// Check if this is an SCD2 table by looking for "(SCD2)" in the description
		isSCD2 := strings.Contains(table.Description, "(SCD2)")
		if isSCD2 {
			actualTableName := table.Name + "_current"
			expectedTables = append(expectedTables, actualTableName)
			tableNameMap[actualTableName] = table.Name
		} else {
			expectedTables = append(expectedTables, table.Name)
			tableNameMap[table.Name] = table.Name
		}
	}

	// If no tables to validate, return early
	if len(expectedTables) == 0 {
		return nil
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

	// Build map of in-code schemas (using schema table names, not DB table names)
	inCodeSchemas := make(map[string]map[string]schematypes.ColumnInfo)
	for _, table := range schema.Tables {
		inCodeSchemas[table.Name] = make(map[string]schematypes.ColumnInfo)
		for _, col := range table.Columns {
			inCodeSchemas[table.Name][col.Name] = col
		}
	}

	var missing []string
	for dbTableName, dbColumns := range tableColumnMap {
		// Map DB table name back to schema table name
		schemaTableName, exists := tableNameMap[dbTableName]
		if !exists {
			// This is a table in the DB that we didn't expect (e.g., _history tables)
			// We can ignore these for now
			continue
		}

		inCodeTable, exists := inCodeSchemas[schemaTableName]
		if !exists {
			missing = append(missing, fmt.Sprintf("table %s (DB: %s): missing from in-code schema", schemaTableName, dbTableName))
			continue
		}

		// For SCD2 tables, we need to account for SCD2 metadata columns (as_of_ts, row_hash)
		// that are in the _current table but may not be in the schema columns
		// We'll validate that all schema columns exist, but allow extra columns in the DB
		for colName := range dbColumns {
			// Skip SCD2 metadata columns that are expected in _current tables
			if colName == "as_of_ts" || colName == "row_hash" {
				continue
			}
			inCodeCol, exists := inCodeTable[colName]
			if !exists {
				missing = append(missing, fmt.Sprintf("table %s (DB: %s), column %s: missing from in-code schema", schemaTableName, dbTableName, colName))
				continue
			}
			if inCodeCol.Description == "" {
				missing = append(missing, fmt.Sprintf("table %s (DB: %s), column %s: missing description", schemaTableName, dbTableName, colName))
			}
		}

		// Check that all schema columns exist in the DB table
		for colName := range inCodeTable {
			// Skip SCD2 metadata columns that are in the schema but handled separately
			if colName == "as_of_ts" || colName == "row_hash" {
				continue
			}
			if _, exists := dbColumns[colName]; !exists {
				missing = append(missing, fmt.Sprintf("table %s (DB: %s), column %s: in-code schema but not in database", schemaTableName, dbTableName, colName))
			}
		}
	}

	// Check for tables in in-code schema that don't exist in database
	for schemaTableName := range inCodeSchemas {
		// Find the corresponding DB table name
		var dbTableName string
		var isSCD2 bool
		for dbName, schemaName := range tableNameMap {
			if schemaName == schemaTableName {
				dbTableName = dbName
				// Check if this is an SCD2 table by checking if dbName ends with "_current"
				isSCD2 = strings.HasSuffix(dbName, "_current") && dbName != schemaTableName
				break
			}
		}
		if dbTableName == "" {
			missing = append(missing, fmt.Sprintf("table %s: in-code schema but not in database", schemaTableName))
			continue
		}
		if _, exists := tableColumnMap[dbTableName]; !exists {
			// For SCD2 tables, it's OK if they don't exist yet - they'll be created on first use
			// Only report missing tables for non-SCD2 tables
			if !isSCD2 {
				missing = append(missing, fmt.Sprintf("table %s (expected DB table: %s): in-code schema but not in database", schemaTableName, dbTableName))
			}
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("schema validation failed:\n  %s", strings.Join(missing, "\n  "))
	}

	return nil
}
