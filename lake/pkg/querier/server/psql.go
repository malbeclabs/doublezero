package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	case strings.HasPrefix(dbTypeName, "TIME"):
		return pgtype.TimeOID
	case strings.HasPrefix(dbTypeName, "TIMESTAMP") || strings.HasPrefix(dbTypeName, "DATETIME"):
		return pgtype.TimestampOID
	case strings.HasPrefix(dbTypeName, "TIMESTAMPTZ") || strings.HasPrefix(dbTypeName, "TIMESTAMP WITH TIME ZONE"):
		return pgtype.TimestamptzOID
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
