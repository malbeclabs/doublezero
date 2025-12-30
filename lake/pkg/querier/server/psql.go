package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	wire "github.com/jeroenrinzema/psql-wire"
	"github.com/jeroenrinzema/psql-wire/codes"
	pgerror "github.com/jeroenrinzema/psql-wire/errors"
	"github.com/jeroenrinzema/psql-wire/pkg/buffer"
	"github.com/jeroenrinzema/psql-wire/pkg/types"
	"github.com/lib/pq/oid"
)

// createAuthStrategy creates an authentication strategy that checks accounts dynamically at runtime
func createAuthStrategy(log *slog.Logger, accounts map[string]string) wire.AuthStrategy {
	return func(ctx context.Context, writer *buffer.Writer, reader *buffer.Reader) (context.Context, error) {
		params := wire.ClientParameters(ctx)
		database := params[wire.ParamDatabase]
		username := params[wire.ParamUsername]

		// Check accounts at runtime (not just at server creation time)
		if len(accounts) == 0 {
			// No accounts configured - accept immediately without password prompt
			writer.Start(types.ServerAuth)
			writer.AddInt32(0) // authOK = 0
			if err := writer.End(); err != nil {
				return ctx, err
			}
			log.Debug("postgres: authentication disabled, allowing connection", "database", database, "username", username)
			return ctx, nil
		}

		// Authentication is enabled - use ClearTextPassword flow
		// Write authClearTextPassword to request password
		writer.Start(types.ServerAuth)
		writer.AddInt32(3) // authClearTextPassword = 3
		if err := writer.End(); err != nil {
			return ctx, err
		}

		// Read password message from client
		t, _, err := reader.ReadTypedMsg()
		if err != nil {
			return ctx, err
		}

		if t != types.ClientPassword {
			return ctx, fmt.Errorf("unexpected password message type: %v", t)
		}

		password, err := reader.GetString()
		if err != nil {
			return ctx, err
		}

		// Validate credentials
		log.Debug("postgres: authentication requested", "database", database, "username", username, "has_password", password != "")
		expectedPassword, exists := accounts[username]
		if !exists || password != expectedPassword {
			log.Debug("postgres: authentication failed", "username", username)
			authErr := pgerror.WithCode(errors.New("invalid username/password"), codes.InvalidPassword)
			if err := wire.ErrorCode(writer, authErr); err != nil {
				return ctx, err
			}
			return ctx, authErr
		}

		log.Debug("postgres: authentication successful", "username", username)
		// Write authOK
		writer.Start(types.ServerAuth)
		writer.AddInt32(0) // authOK = 0
		return ctx, writer.End()
	}
}

// queryHandler handles PostgreSQL wire protocol queries
func (s *Server) queryHandler(ctx context.Context, query string) (wire.PreparedStatements, error) {
	s.log.Debug("incoming query", "query", query)

	// Handle empty queries (whitespace-only queries or just semicolons)
	// PostgreSQL clients often send empty queries or just ";" to test the connection
	normalizedQuery := strings.TrimSpace(query)
	if normalizedQuery == "" || normalizedQuery == ";" {
		// Return an empty result set with no columns - this is what PostgreSQL does for empty queries
		// The wire protocol will send CommandComplete with no rows
		return wire.Prepared(wire.NewStatement(
			func(ctx context.Context, writer wire.DataWriter, parameters []wire.Parameter) error {
				// Complete immediately with no rows - this is the correct response for an empty query
				return writer.Complete("")
			},
			wire.WithColumns(wire.Columns{}),
		)), nil
	}

	// Handle special ping query
	// Normalize whitespace to handle variations like "-- ping", "--  ping  ", etc.
	normalizedPing := strings.ToLower(strings.Join(strings.Fields(query), " "))
	if normalizedPing == "-- ping" {
		columns := wire.Columns{
			wire.Column{
				Name: "pong",
				Oid:  pgtype.TextOID,
			},
		}
		return wire.Prepared(wire.NewStatement(
			func(ctx context.Context, writer wire.DataWriter, parameters []wire.Parameter) error {
				if err := writer.Row([]any{"pong"}); err != nil {
					return err
				}
				return writer.Complete("SELECT")
			},
			wire.WithColumns(columns),
		)), nil
	}

	// Rewrite PostgreSQL-specific queries to DuckDB-compatible ones
	rewrittenQuery := rewriteQueryForDuckDB(query)
	if rewrittenQuery != query {
		s.log.Debug("rewrote query for DuckDB", "original", query, "rewritten", rewrittenQuery)
		query = rewrittenQuery
	}

	// Execute the query to get results and column information
	// We need to do this here because we need column info to create the prepared statement
	resp, err := s.querier.Query(ctx, query)
	if err != nil {
		// Return error directly from ParseFn - psql-wire will send error response to client
		return nil, fmt.Errorf("query execution failed: %w", err)
	}

	// Create columns with proper PostgreSQL types based on DuckDB types
	columns := make(wire.Columns, len(resp.Columns))
	for i, colName := range resp.Columns {
		var oidType oid.Oid
		if i < len(resp.ColumnTypes) {
			oidType = mapDuckDBTypeToPostgreSQLOID(resp.ColumnTypes[i].DatabaseTypeName)
		} else {
			// Fallback to TEXT if type info is not available
			oidType = pgtype.TextOID
		}
		columns[i] = wire.Column{
			Name: colName,
			Oid:  oidType,
		}
	}

	// Create prepared statement with columns and cached data
	return wire.Prepared(wire.NewStatement(
		func(ctx context.Context, writer wire.DataWriter, parameters []wire.Parameter) error {
			// Write rows (if any)
			for _, row := range resp.Rows {
				values := make([]any, len(resp.Columns))
				for i, colName := range resp.Columns {
					val := row[colName]
					var oidType oid.Oid
					if i < len(columns) {
						oidType = columns[i].Oid
					} else {
						oidType = pgtype.TextOID
					}

					encodedVal, err := encodeValueForPostgreSQL(val, oidType)
					if err != nil {
						return fmt.Errorf("failed to encode value for column %s: %w", colName, err)
					}
					values[i] = encodedVal
				}
				if err := writer.Row(values); err != nil {
					return err
				}
			}

			// Complete the response (works for both empty and non-empty results)
			return writer.Complete("SELECT")
		},
		wire.WithColumns(columns),
	)), nil
}

// mapDuckDBTypeToPostgreSQLOID maps DuckDB database type names to PostgreSQL OIDs
func mapDuckDBTypeToPostgreSQLOID(dbTypeName string) oid.Oid {
	// Normalize the type name (case-insensitive, remove whitespace)
	dbTypeName = strings.ToUpper(strings.TrimSpace(dbTypeName))

	switch {
	case strings.HasPrefix(dbTypeName, "BOOLEAN") || strings.HasPrefix(dbTypeName, "BOOL"):
		return pgtype.BoolOID
	case strings.HasPrefix(dbTypeName, "TINYINT"):
		return pgtype.Int2OID // Use INT2 for TINYINT
	case strings.HasPrefix(dbTypeName, "SMALLINT") || strings.HasPrefix(dbTypeName, "INT2"):
		return pgtype.Int2OID
	case strings.HasPrefix(dbTypeName, "INTEGER") || strings.HasPrefix(dbTypeName, "INT") || strings.HasPrefix(dbTypeName, "INT4"):
		return pgtype.Int4OID
	case strings.HasPrefix(dbTypeName, "BIGINT") || strings.HasPrefix(dbTypeName, "INT8"):
		return pgtype.Int8OID
	case strings.HasPrefix(dbTypeName, "REAL") || strings.HasPrefix(dbTypeName, "FLOAT") || strings.HasPrefix(dbTypeName, "FLOAT4"):
		return pgtype.Float4OID
	case strings.HasPrefix(dbTypeName, "DOUBLE") || strings.HasPrefix(dbTypeName, "FLOAT8"):
		return pgtype.Float8OID
	case strings.HasPrefix(dbTypeName, "DECIMAL") || strings.HasPrefix(dbTypeName, "NUMERIC"):
		return pgtype.NumericOID
	case strings.HasPrefix(dbTypeName, "VARCHAR") || strings.HasPrefix(dbTypeName, "CHAR") || strings.HasPrefix(dbTypeName, "STRING") || strings.HasPrefix(dbTypeName, "TEXT"):
		return pgtype.TextOID
	case strings.HasPrefix(dbTypeName, "DATE"):
		return pgtype.DateOID
	case strings.HasPrefix(dbTypeName, "TIMESTAMPTZ") || strings.HasPrefix(dbTypeName, "TIMESTAMP WITH TIME ZONE"):
		return pgtype.TimestamptzOID
	case strings.HasPrefix(dbTypeName, "TIMESTAMP") || strings.HasPrefix(dbTypeName, "DATETIME"):
		return pgtype.TimestampOID
	case strings.HasPrefix(dbTypeName, "TIME"):
		return pgtype.TimeOID
	case strings.HasPrefix(dbTypeName, "BLOB") || strings.HasPrefix(dbTypeName, "BYTEA") || strings.HasPrefix(dbTypeName, "BINARY"):
		return pgtype.ByteaOID
	case strings.HasPrefix(dbTypeName, "UUID"):
		return pgtype.UUIDOID
	case strings.HasPrefix(dbTypeName, "JSON") || strings.HasPrefix(dbTypeName, "JSONB"):
		return pgtype.JSONOID
	default:
		// Default to TEXT for unknown types
		return pgtype.TextOID
	}
}

// encodeValueForPostgreSQL encodes a value for PostgreSQL based on its type
func encodeValueForPostgreSQL(val any, oidType oid.Oid) (any, error) {
	if val == nil {
		return nil, nil
	}

	switch oidType {
	case pgtype.BoolOID:
		switch v := val.(type) {
		case bool:
			return v, nil
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("failed to parse bool: %w", err)
			}
			return b, nil
		default:
			return val, nil // Let pgx handle conversion
		}
	case pgtype.Int2OID, pgtype.Int4OID, pgtype.Int8OID:
		// Return as-is, pgx will handle conversion
		return val, nil
	case pgtype.Float4OID, pgtype.Float8OID:
		// Return as-is, pgx will handle conversion
		return val, nil
	case pgtype.NumericOID:
		// Return as string for numeric, pgx will handle conversion
		return fmt.Sprintf("%v", val), nil
	case pgtype.TextOID, pgtype.VarcharOID:
		// Convert to string
		return fmt.Sprintf("%v", val), nil
	case pgtype.DateOID, pgtype.TimeOID, pgtype.TimestampOID, pgtype.TimestamptzOID:
		// Return time.Time as-is, or convert string to time.Time
		if t, ok := val.(time.Time); ok {
			return t, nil
		}
		// If it's a string, try to parse it
		if s, ok := val.(string); ok {
			// Try common formats
			for _, layout := range []string{
				time.RFC3339,
				time.RFC3339Nano,
				"2006-01-02 15:04:05",
				"2006-01-02T15:04:05",
				"2006-01-02",
			} {
				if t, err := time.Parse(layout, s); err == nil {
					return t, nil
				}
			}
		}
		return fmt.Sprintf("%v", val), nil
	case pgtype.ByteaOID:
		// Handle byte arrays
		switch v := val.(type) {
		case []byte:
			return v, nil
		case string:
			return []byte(v), nil
		default:
			return []byte(fmt.Sprintf("%v", val)), nil
		}
	case pgtype.UUIDOID:
		// Return as string, pgx will handle conversion
		return fmt.Sprintf("%v", val), nil
	case pgtype.JSONOID, pgtype.JSONBOID:
		// Return as string, pgx will handle conversion
		return fmt.Sprintf("%v", val), nil
	default:
		// Default: convert to string
		return fmt.Sprintf("%v", val), nil
	}
}

// rewriteQueryForDuckDB rewrites PostgreSQL-specific queries to DuckDB-compatible ones
func rewriteQueryForDuckDB(query string) string {
	// Normalize whitespace for pattern matching
	normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))

	// Detect PostgreSQL table listing query pattern
	// This pattern matches queries that:
	// 1. Select from information_schema.tables
	// 2. Have CASE statements with search_path logic
	// 3. Exclude system schemas
	if isPostgreSQLTableListingQuery(normalized) {
		return rewriteTableListingQuery()
	}

	// Detect PostgreSQL column listing query pattern
	// This pattern matches queries that:
	// 1. Select from information_schema.columns
	// 2. Have parse_ident and search_path logic
	// 3. Filter by table name
	if isPostgreSQLColumnListingQuery(normalized) {
		if tableName := extractTableNameFromColumnQuery(query); tableName != "" {
			return rewriteColumnListingQuery(tableName)
		}
	}

	return query
}

// isPostgreSQLTableListingQuery detects if a query is the PostgreSQL table listing query
func isPostgreSQLTableListingQuery(normalizedQuery string) bool {
	// Check for key indicators:
	// 1. Contains "from information_schema.tables"
	// 2. Contains "case" and "search_path" (indicating the complex PostgreSQL query)
	// 3. Contains exclusions for system schemas
	hasInfoSchema := strings.Contains(normalizedQuery, "from information_schema.tables")
	hasSearchPath := strings.Contains(normalizedQuery, "search_path")
	hasCase := strings.Contains(normalizedQuery, "case")
	hasSystemSchemaExclusions := strings.Contains(normalizedQuery, "pg_catalog") ||
		strings.Contains(normalizedQuery, "information_schema") ||
		strings.Contains(normalizedQuery, "timescaledb")

	return hasInfoSchema && hasSearchPath && hasCase && hasSystemSchemaExclusions
}

// rewriteTableListingQuery rewrites the PostgreSQL table listing query to DuckDB-compatible SQL
func rewriteTableListingQuery() string {
	// DuckDB-compatible query that returns table names in a similar format
	// Returns just table_name as "table" since DuckDB doesn't have the same search_path concept
	// We include only the main schema
	return `SELECT
  CASE
    WHEN table_schema = current_schema() THEN table_name
    ELSE table_schema || '.' || table_name
  END AS "table"
FROM information_schema.tables
WHERE table_schema = 'main'
ORDER BY
  CASE
    WHEN table_schema = current_schema() THEN 0
    ELSE 1
  END,
  "table"`
}

// isPostgreSQLColumnListingQuery detects if a query is the PostgreSQL column listing query
func isPostgreSQLColumnListingQuery(normalizedQuery string) bool {
	// Check for key indicators:
	// 1. Contains "from information_schema.columns"
	// 2. Contains "parse_ident" (indicating table name parsing)
	// 3. Contains "search_path" (indicating the complex PostgreSQL query)
	hasInfoSchema := strings.Contains(normalizedQuery, "from information_schema.columns")
	hasParseIdent := strings.Contains(normalizedQuery, "parse_ident")
	hasSearchPath := strings.Contains(normalizedQuery, "search_path")
	hasColumnAndType := strings.Contains(normalizedQuery, `"column"`) && strings.Contains(normalizedQuery, `"type"`)

	return hasInfoSchema && hasParseIdent && hasSearchPath && hasColumnAndType
}

// extractTableNameFromColumnQuery extracts the table name from the PostgreSQL column listing query
func extractTableNameFromColumnQuery(query string) string {
	// Pattern 1: parse_ident('table_name') or parse_ident('schema.table_name')
	// This is the primary pattern used in the query
	parseIdentPattern := regexp.MustCompile(`parse_ident\s*\(\s*'([^']+)'`)
	matches := parseIdentPattern.FindStringSubmatch(query)
	if len(matches) > 1 {
		// Return the full spec (could be 'table' or 'schema.table')
		// The rewrite function will handle splitting it
		return matches[1]
	}

	// Pattern 2: quote_ident(table_name) = 'table_name'
	// Look for patterns like: quote_ident(table_name) = 'table_name'
	tableNamePattern := regexp.MustCompile(`quote_ident\(table_name\)\s*=\s*'([^']+)'`)
	matches = tableNamePattern.FindStringSubmatch(strings.ToLower(query))
	if len(matches) > 1 {
		return matches[1]
	}

	return ""
}

// rewriteColumnListingQuery rewrites the PostgreSQL column listing query to DuckDB-compatible SQL
func rewriteColumnListingQuery(tableName string) string {
	// DuckDB-compatible query that returns column information
	// Handles both schema.table and just table name formats
	tableParts := strings.Split(tableName, ".")
	var whereClause string
	if len(tableParts) == 2 {
		// Schema-qualified table name
		schemaName := tableParts[0]
		actualTableName := tableParts[1]
		whereClause = fmt.Sprintf("table_schema = '%s' AND table_name = '%s'", schemaName, actualTableName)
	} else {
		// Just table name, use current schema
		whereClause = fmt.Sprintf("table_name = '%s' AND table_schema = current_schema()", tableName)
	}

	return fmt.Sprintf(`SELECT
  column_name AS "column",
  data_type AS "type"
FROM information_schema.columns
WHERE %s
ORDER BY ordinal_position`, whereClause)
}
