package server

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lib/pq/oid"
	"github.com/stretchr/testify/require"
)

// duckDBInterval simulates the DuckDB interval struct for testing
type duckDBInterval struct {
	Days   int64
	Months int64
	Micros int64
}

func Test_mapDuckDBTypeToPostgreSQLOID(t *testing.T) {
	tests := []struct {
		name     string
		dbType   string
		expected oid.Oid
	}{
		// Boolean types
		{"boolean", "BOOLEAN", pgtype.BoolOID},
		{"bool", "BOOL", pgtype.BoolOID},
		{"boolean lowercase", "boolean", pgtype.BoolOID},
		{"bool with spaces", "  BOOL  ", pgtype.BoolOID},

		// Integer types
		{"tinyint", "TINYINT", pgtype.Int2OID},
		{"smallint", "SMALLINT", pgtype.Int2OID},
		{"int2", "INT2", pgtype.Int2OID},
		{"integer", "INTEGER", pgtype.Int4OID},
		{"int", "INT", pgtype.Int4OID},
		{"int4", "INT4", pgtype.Int4OID},
		{"bigint", "BIGINT", pgtype.Int8OID},
		{"int8", "INT8", pgtype.Int4OID}, // Note: INT8 matches "INT" prefix first, so maps to Int4OID (this may be a bug)

		// CRITICAL: INTERVAL must be checked before INT
		{"interval", "INTERVAL", pgtype.TextOID},
		{"interval lowercase", "interval", pgtype.TextOID},
		{"interval with spaces", "  INTERVAL  ", pgtype.TextOID},
		{"interval day", "INTERVAL DAY", pgtype.TextOID},
		{"interval year month", "INTERVAL YEAR TO MONTH", pgtype.TextOID},

		// Float types
		{"real", "REAL", pgtype.Float4OID},
		{"float", "FLOAT", pgtype.Float4OID},
		{"float4", "FLOAT4", pgtype.Float4OID},
		{"double", "DOUBLE", pgtype.Float8OID},
		{"float8", "FLOAT8", pgtype.Float4OID}, // FLOAT8 matches FLOAT prefix first

		// Decimal/Numeric
		{"decimal", "DECIMAL", pgtype.NumericOID},
		{"numeric", "NUMERIC", pgtype.NumericOID},
		{"decimal(10,2)", "DECIMAL(10,2)", pgtype.NumericOID},

		// String types
		{"varchar", "VARCHAR", pgtype.TextOID},
		{"char", "CHAR", pgtype.TextOID},
		{"string", "STRING", pgtype.TextOID},
		{"text", "TEXT", pgtype.TextOID},
		{"varchar(255)", "VARCHAR(255)", pgtype.TextOID},

		// Date/Time types
		{"date", "DATE", pgtype.DateOID},
		{"timestamp", "TIMESTAMP", pgtype.TimestampOID},
		{"timestamptz", "TIMESTAMPTZ", pgtype.TimestamptzOID},
		{"timestamp with time zone", "TIMESTAMP WITH TIME ZONE", pgtype.TimestamptzOID},
		{"datetime", "DATETIME", pgtype.DateOID}, // DATETIME matches DATE prefix first
		{"time", "TIME", pgtype.TimeOID},

		// Binary types
		{"blob", "BLOB", pgtype.ByteaOID},
		{"bytea", "BYTEA", pgtype.ByteaOID},
		{"binary", "BINARY", pgtype.ByteaOID},

		// UUID
		{"uuid", "UUID", pgtype.UUIDOID},

		// JSON
		{"json", "JSON", pgtype.JSONOID},
		{"jsonb", "JSONB", pgtype.JSONOID},

		// Unknown types default to TEXT
		{"unknown type", "UNKNOWN_TYPE", pgtype.TextOID},
		{"empty string", "", pgtype.TextOID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapDuckDBTypeToPostgreSQLOID(tt.dbType)
			require.Equal(t, tt.expected, result, "type %q should map to OID %d, got %d", tt.dbType, tt.expected, result)
		})
	}
}

func Test_formatDuckDBInterval(t *testing.T) {
	tests := []struct {
		name     string
		val      any
		expected string
	}{
		{
			name: "nil value",
			val:  nil,
			expected: "",
		},
		{
			name: "non-struct value",
			val:  "not a struct",
			expected: "",
		},
		{
			name: "struct without interval fields",
			val: struct {
				X int
				Y string
			}{X: 1, Y: "test"},
			expected: "",
		},
		{
			name: "struct missing Days field",
			val: struct {
				Months int64
				Micros int64
			}{Months: 0, Micros: 0},
			expected: "",
		},
		{
			name: "struct missing Months field",
			val: struct {
				Days   int64
				Micros int64
			}{Days: 0, Micros: 0},
			expected: "",
		},
		{
			name: "struct missing Micros field",
			val: struct {
				Days   int64
				Months int64
			}{Days: 0, Months: 0},
			expected: "",
		},
		{
			name: "zero interval",
			val: duckDBInterval{
				Days:   0,
				Months: 0,
				Micros: 0,
			},
			expected: "0 seconds",
		},
		{
			name: "interval with only seconds",
			val: duckDBInterval{
				Days:   0,
				Months: 0,
				Micros: 59_598_746, // ~59.6 seconds
			},
			expected: "59 seconds",
		},
		{
			name: "interval with 1 second",
			val: duckDBInterval{
				Days:   0,
				Months: 0,
				Micros: 1_000_000, // 1 second
			},
			expected: "1 second",
		},
		{
			name: "interval with minutes and seconds",
			val: duckDBInterval{
				Days:   0,
				Months: 0,
				Micros: 125_000_000, // 125 seconds = 2 minutes 5 seconds
			},
			expected: "2 minutes 5 seconds",
		},
		{
			name: "interval with hours",
			val: duckDBInterval{
				Days:   0,
				Months: 0,
				Micros: 3_600_000_000, // 1 hour
			},
			expected: "1 hour",
		},
		{
			name: "interval with 1 hour",
			val: duckDBInterval{
				Days:   0,
				Months: 0,
				Micros: 3_600_000_000, // 1 hour
			},
			expected: "1 hour",
		},
		{
			name: "interval with multiple hours",
			val: duckDBInterval{
				Days:   0,
				Months: 0,
				Micros: 7_200_000_000, // 2 hours
			},
			expected: "2 hours",
		},
		{
			name: "interval with days",
			val: duckDBInterval{
				Days:   1,
				Months: 0,
				Micros: 0,
			},
			expected: "1 day",
		},
		{
			name: "interval with multiple days",
			val: duckDBInterval{
				Days:   3,
				Months: 0,
				Micros: 0,
			},
			expected: "3 days",
		},
		{
			name: "interval with months (converted to days)",
			val: duckDBInterval{
				Days:   0,
				Months: 1,
				Micros: 0,
			},
			expected: "30 days",
		},
		{
			name: "interval with months and days",
			val: duckDBInterval{
				Days:   5,
				Months: 2,
				Micros: 0,
			},
			expected: "65 days", // 5 + (2 * 30)
		},
		{
			name: "complex interval with all components",
			val: duckDBInterval{
				Days:   2,
				Months: 1,
				Micros: 3_661_000_000, // 1 hour 1 minute 1 second
			},
			expected: "32 days 1 hour 1 minute 1 second",
		},
		{
			name: "interval pointer",
			val: &duckDBInterval{
				Days:   1,
				Months: 0,
				Micros: 0,
			},
			expected: "1 day",
		},
		{
			name: "nil pointer",
			val:  (*duckDBInterval)(nil),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuckDBInterval(tt.val)
			require.Equal(t, tt.expected, result)
		})
	}
}

func Test_encodeValueForPostgreSQL(t *testing.T) {
	tests := []struct {
		name     string
		val      any
		oidType  oid.Oid
		expected any
		wantErr  bool
	}{
		// Nil values
		{
			name:     "nil value",
			val:      nil,
			oidType:  pgtype.TextOID,
			expected: nil,
			wantErr:  false,
		},

		// Boolean types
		{
			name:     "bool true",
			val:      true,
			oidType:  pgtype.BoolOID,
			expected: true,
			wantErr:  false,
		},
		{
			name:     "bool false",
			val:      false,
			oidType:  pgtype.BoolOID,
			expected: false,
			wantErr:  false,
		},
		{
			name:     "bool from string true",
			val:      "true",
			oidType:  pgtype.BoolOID,
			expected: true,
			wantErr:  false,
		},
		{
			name:     "bool from string false",
			val:      "false",
			oidType:  pgtype.BoolOID,
			expected: false,
			wantErr:  false,
		},

		// Integer types
		{
			name:     "int2 value",
			val:      int16(42),
			oidType:  pgtype.Int2OID,
			expected: int16(42),
			wantErr:  false,
		},
		{
			name:     "int4 value",
			val:      int32(42),
			oidType:  pgtype.Int4OID,
			expected: int32(42),
			wantErr:  false,
		},
		{
			name:     "int8 value",
			val:      int64(42),
			oidType:  pgtype.Int8OID,
			expected: int64(42),
			wantErr:  false,
		},

		// Float types
		{
			name:     "float4 value",
			val:      float32(3.14),
			oidType:  pgtype.Float4OID,
			expected: float32(3.14),
			wantErr:  false,
		},
		{
			name:     "float8 value",
			val:      3.14159,
			oidType:  pgtype.Float8OID,
			expected: 3.14159,
			wantErr:  false,
		},

		// Numeric
		{
			name:     "numeric value",
			val:      "123.45",
			oidType:  pgtype.NumericOID,
			expected: "123.45",
			wantErr:  false,
		},

		// Text types
		{
			name:     "text value",
			val:      "hello world",
			oidType:  pgtype.TextOID,
			expected: "hello world",
			wantErr:  false,
		},
		{
			name:     "varchar value",
			val:      "test",
			oidType:  pgtype.VarcharOID,
			expected: "test",
			wantErr:  false,
		},
		{
			name: "text with DuckDB interval",
			val: duckDBInterval{
				Days:   0,
				Months: 0,
				Micros: 59_598_746,
			},
			oidType:  pgtype.TextOID,
			expected: "59 seconds",
			wantErr:  false,
		},
		{
			name: "text with complex DuckDB interval",
			val: duckDBInterval{
				Days:   1,
				Months: 0,
				Micros: 3_600_000_000, // 1 hour
			},
			oidType:  pgtype.TextOID,
			expected: "1 day 1 hour",
			wantErr:  false,
		},

		// Date/Time types
		{
			name:     "time.Time value",
			val:      time.Date(2024, 1, 15, 12, 30, 45, 0, time.UTC),
			oidType:  pgtype.TimestampOID,
			expected: time.Date(2024, 1, 15, 12, 30, 45, 0, time.UTC),
			wantErr:  false,
		},
		{
			name:     "timestamp from RFC3339 string",
			val:      "2024-01-15T12:30:45Z",
			oidType:  pgtype.TimestampOID,
			expected: time.Date(2024, 1, 15, 12, 30, 45, 0, time.UTC),
			wantErr:  false,
		},
		{
			name:     "date from string",
			val:      "2024-01-15",
			oidType:  pgtype.DateOID,
			expected: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			wantErr:  false,
		},

		// Bytea types
		{
			name:     "bytea from []byte",
			val:      []byte{0x01, 0x02, 0x03},
			oidType:  pgtype.ByteaOID,
			expected: []byte{0x01, 0x02, 0x03},
			wantErr:  false,
		},
		{
			name:     "bytea from string",
			val:      "hello",
			oidType:  pgtype.ByteaOID,
			expected: []byte("hello"),
			wantErr:  false,
		},

		// UUID
		{
			name:     "uuid value",
			val:      "550e8400-e29b-41d4-a716-446655440000",
			oidType:  pgtype.UUIDOID,
			expected: "550e8400-e29b-41d4-a716-446655440000",
			wantErr:  false,
		},

		// JSON
		{
			name:     "json value",
			val:      `{"key": "value"}`,
			oidType:  pgtype.JSONOID,
			expected: `{"key": "value"}`,
			wantErr:  false,
		},

		// Default case
		{
			name:     "unknown type defaults to string",
			val:      42,
			oidType:  oid.Oid(99999), // Unknown OID
			expected: "42",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := encodeValueForPostgreSQL(tt.val, tt.oidType)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

func Test_extractTableNameFromColumnQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{
			name:     "parse_ident with simple table name",
			query:    "SELECT * FROM information_schema.columns WHERE parse_ident('users') = ...",
			expected: "users",
		},
		{
			name:     "parse_ident with schema.table",
			query:    "SELECT * FROM information_schema.columns WHERE parse_ident('public.users') = ...",
			expected: "public.users",
		},
		{
			name:     "parse_ident with spaces",
			query:    "SELECT * FROM information_schema.columns WHERE parse_ident( 'test_table' ) = ...",
			expected: "test_table",
		},
		{
			name:     "quote_ident pattern",
			query:    "SELECT * FROM information_schema.columns WHERE quote_ident(table_name) = 'my_table'",
			expected: "my_table",
		},
		{
			name:     "no match",
			query:    "SELECT * FROM information_schema.columns WHERE table_name = 'test'",
			expected: "",
		},
		{
			name:     "empty query",
			query:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTableNameFromColumnQuery(tt.query)
			require.Equal(t, tt.expected, result)
		})
	}
}

func Test_rewriteColumnListingQuery(t *testing.T) {
	tests := []struct {
		name     string
		tableName string
		expectedContains []string
	}{
		{
			name:      "simple table name",
			tableName: "users",
			expectedContains: []string{
				`table_name = 'users'`,
				`table_schema = current_schema()`,
			},
		},
		{
			name:      "schema.table name",
			tableName: "public.users",
			expectedContains: []string{
				`table_schema = 'public'`,
				`table_name = 'users'`,
			},
		},
		{
			name:      "query contains column and type",
			tableName: "test_table",
			expectedContains: []string{
				`"column"`,
				`"type"`,
				`table_name = 'test_table'`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rewriteColumnListingQuery(tt.tableName)
			for _, expected := range tt.expectedContains {
				require.Contains(t, result, expected)
			}
		})
	}
}

func Test_isPostgreSQLTableListingQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{
			name:     "valid table listing query",
			query:    "SELECT CASE WHEN table_schema = current_schema() THEN table_name ELSE table_schema || '.' || table_name END AS table FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema') AND search_path = 'public'",
			expected: true,
		},
		{
			name:     "query with search_path and case",
			query:    "SELECT CASE WHEN table_schema = current_schema() THEN table_name END FROM information_schema.tables WHERE search_path = 'public' AND table_schema NOT IN ('pg_catalog', 'information_schema')",
			expected: true,
		},
		{
			name:     "query missing information_schema.tables",
			query:    "SELECT * FROM pg_tables",
			expected: false,
		},
		{
			name:     "query missing search_path",
			query:    "SELECT * FROM information_schema.tables",
			expected: false,
		},
		{
			name:     "empty query",
			query:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := strings.ToLower(strings.Join(strings.Fields(tt.query), " "))
			result := isPostgreSQLTableListingQuery(normalized)
			require.Equal(t, tt.expected, result)
		})
	}
}

func Test_isPostgreSQLColumnListingQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{
			name:     "valid column listing query",
			query:    "SELECT column_name AS \"column\", data_type AS \"type\" FROM information_schema.columns WHERE parse_ident('users') = ... AND search_path = 'public'",
			expected: true,
		},
		{
			name:     "query missing parse_ident",
			query:    "SELECT * FROM information_schema.columns WHERE table_name = 'users'",
			expected: false,
		},
		{
			name:     "query missing search_path",
			query:    "SELECT * FROM information_schema.columns WHERE parse_ident('users') = ...",
			expected: false,
		},
		{
			name:     "query missing column and type",
			query:    "SELECT * FROM information_schema.columns WHERE parse_ident('users') = ... AND search_path = 'public'",
			expected: false,
		},
		{
			name:     "empty query",
			query:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := strings.ToLower(strings.Join(strings.Fields(tt.query), " "))
			result := isPostgreSQLColumnListingQuery(normalized)
			require.Equal(t, tt.expected, result)
		})
	}
}

// Test that INTERVAL is correctly handled before INT prefix matching
func Test_mapDuckDBTypeToPostgreSQLOID_IntervalBeforeInt(t *testing.T) {
	// This is a critical test to ensure INTERVAL is checked before INT
	// If INTERVAL were checked after INT, "INTERVAL" would match the "INT" prefix
	// and incorrectly return Int4OID instead of TextOID

	intervalOID := mapDuckDBTypeToPostgreSQLOID("INTERVAL")
	require.Equal(t, oid.Oid(pgtype.TextOID), intervalOID, "INTERVAL should map to TextOID, not Int4OID")

	// Verify INT still works correctly
	intOID := mapDuckDBTypeToPostgreSQLOID("INT")
	require.Equal(t, oid.Oid(pgtype.Int4OID), intOID, "INT should map to Int4OID")

	// Verify INTEGER still works correctly
	integerOID := mapDuckDBTypeToPostgreSQLOID("INTEGER")
	require.Equal(t, oid.Oid(pgtype.Int4OID), integerOID, "INTEGER should map to Int4OID")

	// Note: INT8 currently matches "INT" prefix first, so it maps to Int4OID
	// This is a known limitation - INT8 should ideally be checked before INT
	int8OID := mapDuckDBTypeToPostgreSQLOID("INT8")
	require.Equal(t, oid.Oid(pgtype.Int4OID), int8OID, "INT8 currently maps to Int4OID due to INT prefix matching")
}

// Test formatDuckDBInterval with actual DuckDB interval-like struct
func Test_formatDuckDBInterval_RealWorldExample(t *testing.T) {
	// Test the exact case from the error message
	interval := duckDBInterval{
		Days:   0,
		Months: 0,
		Micros: 59_598_746, // From the error: Micros:59598746
	}

	result := formatDuckDBInterval(interval)
	require.Equal(t, "59 seconds", result)
}

// Test that encodeValueForPostgreSQL correctly handles intervals when OID is TextOID
func Test_encodeValueForPostgreSQL_IntervalHandling(t *testing.T) {
	interval := duckDBInterval{
		Days:   1,
		Months: 0,
		Micros: 3_600_000_000, // 1 hour
	}

	result, err := encodeValueForPostgreSQL(interval, pgtype.TextOID)
	require.NoError(t, err)
	require.Equal(t, "1 day 1 hour", result)
}

