package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	"github.com/malbeclabs/doublezero/lake/pkg/querier"
	"github.com/stretchr/testify/require"
)

func testLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testDB(t *testing.T) duck.DB {
	db, err := duck.NewDB(context.Background(), "", testLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
	})
	return db
}

func getFreeListener(t *testing.T) net.Listener {
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	t.Cleanup(func() {
		listener.Close()
	})
	return listener
}

// waitForServerReady waits for the server to be ready by attempting to connect
func waitForServerReady(t *testing.T, ctx context.Context, addr string, maxAttempts int) {
	t.Helper()
	for i := 0; i < maxAttempts; i++ {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		if i < maxAttempts-1 {
			time.Sleep(50 * time.Millisecond * time.Duration(i+1)) // exponential backoff
		}
	}
	t.Fatalf("server at %s not ready after %d attempts", addr, maxAttempts)
}

func TestLake_Querier_Server_PostgreSQL_WireProtocol(t *testing.T) {
	t.Parallel()

	t.Run("connects and executes simple query", func(t *testing.T) {

		ctx := context.Background()
		db := testDB(t)

		// Set up test data
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(ctx, `CREATE TABLE test_table (id INTEGER, name VARCHAR, value DOUBLE)`)
		require.NoError(t, err)

		_, err = conn.ExecContext(ctx, `INSERT INTO test_table VALUES (1, 'test1', 10.5), (2, 'test2', 20.3)`)
		require.NoError(t, err)

		// Create server with PostgreSQL wire protocol
		httpListener := getFreeListener(t)
		postgresListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)
		require.NotNil(t, srv)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready by checking if we can connect
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Connect as PostgreSQL client
		pgConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://user:password@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)

		// Execute query
		rows, err := pgConn.Query(ctx, "SELECT id, name, value FROM test_table ORDER BY id")
		require.NoError(t, err)

		// Read first row
		// Note: Types are now properly inferred, so we can scan as native types
		require.True(t, rows.Next())
		var id int32
		var name string
		var value float64
		err = rows.Scan(&id, &name, &value)
		require.NoError(t, err)
		require.Equal(t, int32(1), id)
		require.Equal(t, "test1", name)
		require.InDelta(t, 10.5, value, 0.01)

		// Read second row
		require.True(t, rows.Next())
		err = rows.Scan(&id, &name, &value)
		require.NoError(t, err)
		require.Equal(t, int32(2), id)
		require.Equal(t, "test2", name)
		require.InDelta(t, 20.3, value, 0.01)

		// No more rows
		require.False(t, rows.Next())
		require.NoError(t, rows.Err())

		// Close connection before shutting down server
		pgConn.Close(ctx)

		// Shutdown server
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("handles empty result set", func(t *testing.T) {

		ctx := context.Background()
		db := testDB(t)

		// Set up test data
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(ctx, `CREATE TABLE empty_table (id INTEGER)`)
		require.NoError(t, err)

		// Create server with PostgreSQL wire protocol
		httpListener := getFreeListener(t)
		postgresListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready by checking if we can connect
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Connect as PostgreSQL client
		pgConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://user:password@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)

		// Execute query
		rows, err := pgConn.Query(ctx, "SELECT id FROM empty_table")
		require.NoError(t, err)

		// Should have no rows
		require.False(t, rows.Next())
		require.NoError(t, rows.Err())

		// Close rows and connection before shutting down server
		rows.Close()
		pgConn.Close(ctx)

		// Give server a moment to process connection closure
		time.Sleep(100 * time.Millisecond)

		// Shutdown server
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("handles -- ping query", func(t *testing.T) {
		ctx := context.Background()
		db := testDB(t)

		// Create server with PostgreSQL wire protocol
		httpListener := getFreeListener(t)
		postgresListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready by checking if we can connect
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Connect as PostgreSQL client
		pgConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://user:password@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)

		// Execute -- ping query (test various case and whitespace variations)
		testCases := []string{
			"-- ping",
			"-- PING",
			"--  ping  ",
			"-- Ping",
		}

		for _, query := range testCases {
			rows, err := pgConn.Query(ctx, query)
			require.NoError(t, err, "query: %q", query)

			// Should have one row with column "pong" and value "pong"
			require.True(t, rows.Next(), "query: %q", query)
			var pong string
			err = rows.Scan(&pong)
			require.NoError(t, err, "query: %q", query)
			require.Equal(t, "pong", pong, "query: %q", query)

			// Verify column name
			columns := rows.FieldDescriptions()
			require.Len(t, columns, 1, "query: %q", query)
			require.Equal(t, "pong", columns[0].Name, "query: %q", query)

			// No more rows
			require.False(t, rows.Next(), "query: %q", query)
			require.NoError(t, rows.Err(), "query: %q", query)

			rows.Close()
		}

		// Close connection before shutting down server
		pgConn.Close(ctx)

		// Give server a moment to process connection closure
		time.Sleep(100 * time.Millisecond)

		// Shutdown server
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("handles NULL values", func(t *testing.T) {

		ctx := context.Background()
		db := testDB(t)

		// Set up test data
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(ctx, `CREATE TABLE null_table (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		_, err = conn.ExecContext(ctx, `INSERT INTO null_table VALUES (1, NULL), (NULL, 'test')`)
		require.NoError(t, err)

		// Create server with PostgreSQL wire protocol
		httpListener := getFreeListener(t)
		postgresListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready by checking if we can connect
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Connect as PostgreSQL client
		pgConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://user:password@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)

		// Execute query
		rows, err := pgConn.Query(ctx, "SELECT id, name FROM null_table ORDER BY COALESCE(id, 999)")
		require.NoError(t, err)

		// Read first row (id=1, name=NULL)
		// Note: Types are now properly inferred
		require.True(t, rows.Next())
		var id pgtype.Int4
		var name pgtype.Text
		err = rows.Scan(&id, &name)
		require.NoError(t, err)
		require.True(t, id.Valid)
		require.Equal(t, int32(1), id.Int32)
		require.False(t, name.Valid)

		// Read second row (id=NULL, name='test')
		require.True(t, rows.Next())
		err = rows.Scan(&id, &name)
		require.NoError(t, err)
		require.False(t, id.Valid)
		require.True(t, name.Valid)
		require.Equal(t, "test", name.String)

		// No more rows
		require.False(t, rows.Next())
		require.NoError(t, rows.Err())

		// Close connection before shutting down server
		pgConn.Close(ctx)

		// Shutdown server
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("handles query errors", func(t *testing.T) {

		ctx := context.Background()
		db := testDB(t)

		// Create server with PostgreSQL wire protocol
		httpListener := getFreeListener(t)
		postgresListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready by checking if we can connect
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Connect as PostgreSQL client
		pgConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://user:password@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)

		// Execute invalid query
		_, err = pgConn.Query(ctx, "SELECT * FROM nonexistent_table")
		require.Error(t, err)
		require.Contains(t, err.Error(), "nonexistent_table")

		// Close connection before shutting down server
		pgConn.Close(ctx)

		// Shutdown server
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("handles different data types", func(t *testing.T) {
		ctx := context.Background()
		db := testDB(t)

		// Set up test data with various types
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(ctx, `CREATE TABLE types_table (
			id INTEGER,
			big_id BIGINT,
			is_active BOOLEAN,
			price DOUBLE,
			created_at TIMESTAMP,
			birth_date DATE,
			data BLOB,
			metadata JSON
		)`)
		require.NoError(t, err)

		// Use string format for timestamps that DuckDB can parse
		testTimeStr := "2024-01-15 10:30:00"
		testDateStr := "2024-01-15"
		_, err = conn.ExecContext(ctx, fmt.Sprintf(`INSERT INTO types_table VALUES
			(1, 9223372036854775807, true, 99.99, TIMESTAMP '%s', DATE '%s', 'binary data', '{"key": "value"}'),
			(2, -9223372036854775808, false, 0.0, TIMESTAMP '%s', DATE '%s', NULL, NULL)`,
			testTimeStr, testDateStr, testTimeStr, testDateStr))
		require.NoError(t, err)

		// Create server with PostgreSQL wire protocol
		httpListener := getFreeListener(t)
		postgresListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Connect as PostgreSQL client
		pgConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://user:password@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)

		// Execute query
		rows, err := pgConn.Query(ctx, "SELECT id, big_id, is_active, price, created_at, birth_date, data, metadata FROM types_table ORDER BY id")
		require.NoError(t, err)

		// Read first row
		require.True(t, rows.Next())
		var id int32
		var bigID int64
		var isActive bool
		var price float64
		var createdAt time.Time
		var birthDate pgtype.Date
		var data []byte
		var metadata pgtype.Text
		err = rows.Scan(&id, &bigID, &isActive, &price, &createdAt, &birthDate, &data, &metadata)
		require.NoError(t, err)
		require.Equal(t, int32(1), id)
		require.Equal(t, int64(9223372036854775807), bigID)
		require.True(t, isActive)
		require.InDelta(t, 99.99, price, 0.01)
		// Verify timestamp includes both date and time (not just time)
		require.False(t, createdAt.IsZero())
		require.Equal(t, 2024, createdAt.Year(), "timestamp should include date (year)")
		require.Equal(t, time.January, createdAt.Month(), "timestamp should include date (month)")
		require.Equal(t, 15, createdAt.Day(), "timestamp should include date (day)")
		require.True(t, birthDate.Valid)
		require.Equal(t, "binary data", string(data))
		require.True(t, metadata.Valid)
		require.Contains(t, metadata.String, "key")

		// Read second row (with NULLs)
		require.True(t, rows.Next())
		err = rows.Scan(&id, &bigID, &isActive, &price, &createdAt, &birthDate, &data, &metadata)
		require.NoError(t, err)
		require.Equal(t, int32(2), id)
		require.Equal(t, int64(-9223372036854775808), bigID)
		require.False(t, isActive)
		require.InDelta(t, 0.0, price, 0.01)
		require.Nil(t, data)
		require.False(t, metadata.Valid)

		// No more rows
		require.False(t, rows.Next())
		require.NoError(t, rows.Err())

		// Close rows and connection before shutting down server
		rows.Close()
		pgConn.Close(ctx)

		// Give server a moment to process connection closure
		time.Sleep(100 * time.Millisecond)

		// Shutdown server
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("timestamp includes date and time", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := testDB(t)

		// Set up test data with TIMESTAMP column
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(ctx, `CREATE TABLE timestamp_test (
			id INTEGER,
			ts TIMESTAMP,
			ts_tz TIMESTAMPTZ
		)`)
		require.NoError(t, err)

		// Insert a timestamp with both date and time
		testTimeStr := "2024-01-15 10:30:45"
		_, err = conn.ExecContext(ctx, fmt.Sprintf(`INSERT INTO timestamp_test VALUES
			(1, TIMESTAMP '%s', TIMESTAMP '%s')`,
			testTimeStr, testTimeStr))
		require.NoError(t, err)

		// Create server with PostgreSQL wire protocol
		httpListener := getFreeListener(t)
		postgresListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Connect as PostgreSQL client
		pgConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://user:password@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)
		defer pgConn.Close(ctx)

		// Query timestamp as time.Time
		rows, err := pgConn.Query(ctx, "SELECT id, ts, ts_tz FROM timestamp_test ORDER BY id")
		require.NoError(t, err)
		defer rows.Close()

		require.True(t, rows.Next())
		var id int32
		var ts time.Time
		var tsTz time.Time
		err = rows.Scan(&id, &ts, &tsTz)
		require.NoError(t, err)
		require.Equal(t, int32(1), id)

		// Verify timestamp includes date (not just time)
		// The timestamp should have a date component (year, month, day)
		require.Equal(t, 2024, ts.Year())
		require.Equal(t, time.January, ts.Month())
		require.Equal(t, 15, ts.Day())
		require.Equal(t, 10, ts.Hour())
		require.Equal(t, 30, ts.Minute())
		require.Equal(t, 45, ts.Second())

		// Verify TIMESTAMPTZ also includes date
		require.Equal(t, 2024, tsTz.Year())
		require.Equal(t, time.January, tsTz.Month())
		require.Equal(t, 15, tsTz.Day())

		// No more rows
		require.False(t, rows.Next())
		require.NoError(t, rows.Err())
		rows.Close() // Close rows before making another query

		// Query timestamp as string to verify string representation includes date
		var tsStr string
		var tsTzStr string
		err = pgConn.QueryRow(ctx, "SELECT ts::text, ts_tz::text FROM timestamp_test WHERE id = 1").Scan(&tsStr, &tsTzStr)
		require.NoError(t, err)

		// String representation should include date (YYYY-MM-DD or similar)
		// It should NOT be just time (HH:MM:SS)
		require.Contains(t, tsStr, "2024", "timestamp string should include year")
		require.Contains(t, tsStr, "01", "timestamp string should include month")
		require.Contains(t, tsStr, "15", "timestamp string should include day")
		require.NotRegexp(t, `^\d{2}:\d{2}:\d{2}`, tsStr, "timestamp should not be just time without date")

		require.Contains(t, tsTzStr, "2024", "timestamptz string should include year")
		require.NotRegexp(t, `^\d{2}:\d{2}:\d{2}`, tsTzStr, "timestamptz should not be just time without date")

		// Shutdown server
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("authentication disabled when no credentials configured", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		db := testDB(t)

		// Set up test data
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(ctx, `CREATE TABLE test_table (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		_, err = conn.ExecContext(ctx, `INSERT INTO test_table VALUES (1, 'test1')`)
		require.NoError(t, err)

		// Create server without authentication credentials
		postgresListener := getFreeListener(t)
		httpListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
			// No PostgresAccounts set - auth should be disabled
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Connect without password - should work (authentication disabled)
		pgConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://anyuser@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)

		// Also test with password - should also work when auth is disabled
		pgConn2, err := pgx.Connect(ctx, fmt.Sprintf("postgres://anyuser:anypass@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)
		pgConn2.Close(ctx)

		// Execute query - should succeed
		rows, err := pgConn.Query(ctx, "SELECT id, name FROM test_table")
		require.NoError(t, err)
		require.True(t, rows.Next())
		rows.Close()
		pgConn.Close(ctx)

		time.Sleep(100 * time.Millisecond)
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("authentication enabled with correct credentials", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		db := testDB(t)

		// Set up test data
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(ctx, `CREATE TABLE test_table (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		_, err = conn.ExecContext(ctx, `INSERT INTO test_table VALUES (1, 'test1')`)
		require.NoError(t, err)

		// Create server with authentication credentials
		postgresListener := getFreeListener(t)
		httpListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
			PostgresAccounts: map[string]string{
				"testuser": "testpass",
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Connect with correct credentials - should work
		pgConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://testuser:testpass@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)

		// Execute query - should succeed
		rows, err := pgConn.Query(ctx, "SELECT id, name FROM test_table")
		require.NoError(t, err)
		require.True(t, rows.Next())
		rows.Close()
		pgConn.Close(ctx)

		time.Sleep(100 * time.Millisecond)
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("authentication fails with wrong credentials", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		db := testDB(t)

		// Create server with authentication credentials
		postgresListener := getFreeListener(t)
		httpListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
			PostgresAccounts: map[string]string{
				"testuser": "testpass",
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Connect with wrong password - should fail
		_, err = pgx.Connect(ctx, fmt.Sprintf("postgres://testuser:wrongpass@%s/postgres?sslmode=disable", postgresAddr))
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid username/password")

		// Connect with wrong username - should fail
		_, err = pgx.Connect(ctx, fmt.Sprintf("postgres://wronguser:testpass@%s/postgres?sslmode=disable", postgresAddr))
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid username/password")

		time.Sleep(100 * time.Millisecond)
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("authentication supports multiple accounts", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		db := testDB(t)

		// Set up test data
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(ctx, `CREATE TABLE test_table (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		_, err = conn.ExecContext(ctx, `INSERT INTO test_table VALUES (1, 'test1')`)
		require.NoError(t, err)

		// Create server with multiple authentication accounts
		postgresListener := getFreeListener(t)
		httpListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
			PostgresAccounts: map[string]string{
				"user1": "pass1",
				"user2": "pass2",
				"admin": "adminpass",
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Test user1 can connect
		pgConn1, err := pgx.Connect(ctx, fmt.Sprintf("postgres://user1:pass1@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)
		rows, err := pgConn1.Query(ctx, "SELECT id, name FROM test_table")
		require.NoError(t, err)
		require.True(t, rows.Next())
		rows.Close()
		pgConn1.Close(ctx)

		// Test user2 can connect
		pgConn2, err := pgx.Connect(ctx, fmt.Sprintf("postgres://user2:pass2@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)
		rows, err = pgConn2.Query(ctx, "SELECT id, name FROM test_table")
		require.NoError(t, err)
		require.True(t, rows.Next())
		rows.Close()
		pgConn2.Close(ctx)

		// Test admin can connect
		pgConn3, err := pgx.Connect(ctx, fmt.Sprintf("postgres://admin:adminpass@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)
		rows, err = pgConn3.Query(ctx, "SELECT id, name FROM test_table")
		require.NoError(t, err)
		require.True(t, rows.Next())
		rows.Close()
		pgConn3.Close(ctx)

		// Test wrong password for user1 fails
		_, err = pgx.Connect(ctx, fmt.Sprintf("postgres://user1:wrongpass@%s/postgres?sslmode=disable", postgresAddr))
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid username/password")

		time.Sleep(100 * time.Millisecond)
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("works without PostgreSQL wire protocol", func(t *testing.T) {

		ctx := context.Background()
		db := testDB(t)

		httpListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  nil, // No PostgreSQL wire protocol
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)
		require.Nil(t, srv.psqlSrv) // Should not have PostgreSQL server

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready by checking if we can connect
		httpAddr := httpListener.Addr().String()
		waitForServerReady(t, ctx, httpAddr, 10)

		// HTTP server should still work
		resp, err := http.Get(fmt.Sprintf("http://%s/healthz", httpAddr))
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		resp.Body.Close()

		// Shutdown server
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})
}

func TestLake_Querier_Config_LoadFromEnv(t *testing.T) {
	t.Run("loads accounts from POSTGRES_ACCOUNTS environment variable", func(t *testing.T) {
		ctx := context.Background()
		db := testDB(t)

		// Set environment variable
		t.Setenv("POSTGRES_ACCOUNTS", "envuser1:envpass1,envuser2:envpass2")

		// Create server - should load accounts from env
		postgresListener := getFreeListener(t)
		httpListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
			// PostgresAccounts not set - should be loaded from env
		}

		err := cfg.LoadFromEnv()
		require.NoError(t, err)
		require.Equal(t, 2, len(cfg.PostgresAccounts))
		require.Equal(t, "envpass1", cfg.PostgresAccounts["envuser1"])
		require.Equal(t, "envpass2", cfg.PostgresAccounts["envuser2"])

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Test envuser1 can connect
		pgConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://envuser1:envpass1@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)
		pgConn.Close(ctx)

		// Test envuser2 can connect
		pgConn2, err := pgx.Connect(ctx, fmt.Sprintf("postgres://envuser2:envpass2@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)
		pgConn2.Close(ctx)

		time.Sleep(100 * time.Millisecond)
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("returns error for invalid format", func(t *testing.T) {
		cfg := Config{}
		t.Setenv("POSTGRES_ACCOUNTS", "invalidformat")
		err := cfg.LoadFromEnv()
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid account format")

		cfg = Config{}
		t.Setenv("POSTGRES_ACCOUNTS", ":password")
		err = cfg.LoadFromEnv()
		require.Error(t, err)
		require.Contains(t, err.Error(), "username cannot be empty")
	})

	t.Run("handles empty environment variable", func(t *testing.T) {
		cfg := Config{}
		t.Setenv("POSTGRES_ACCOUNTS", "")
		err := cfg.LoadFromEnv()
		require.NoError(t, err)
		require.Equal(t, 0, len(cfg.PostgresAccounts))
	})

	t.Run("handles whitespace in accounts", func(t *testing.T) {
		cfg := Config{}
		t.Setenv("POSTGRES_ACCOUNTS", " user1 : pass1 , user2 : pass2 ")
		err := cfg.LoadFromEnv()
		require.NoError(t, err)
		require.Equal(t, 2, len(cfg.PostgresAccounts))
		require.Equal(t, "pass1", cfg.PostgresAccounts["user1"])
		require.Equal(t, "pass2", cfg.PostgresAccounts["user2"])
	})
}

func TestLake_Querier_Server_QueryRewriting(t *testing.T) {
	t.Run("detects and rewrites PostgreSQL table listing query", func(t *testing.T) {
		postgresQuery := `SELECT
  CASE
    WHEN quote_ident(table_schema) IN (
      SELECT
        CASE
          WHEN trim(s[i]) = '"$user"' THEN user
          ELSE trim(s[i])
        END
      FROM
        generate_series(
          array_lower(string_to_array(current_setting('search_path'), ','), 1),
          array_upper(string_to_array(current_setting('search_path'), ','), 1)
        ) AS i,
        string_to_array(current_setting('search_path'), ',') s
    )
    THEN quote_ident(table_name)
    ELSE quote_ident(table_schema) || '.' || quote_ident(table_name)
  END AS "table"
FROM information_schema.tables
WHERE quote_ident(table_schema) NOT IN (
  'information_schema',
  'pg_catalog',
  '_timescaledb_cache',
  '_timescaledb_catalog',
  '_timescaledb_internal',
  '_timescaledb_config',
  'timescaledb_information',
  'timescaledb_experimental'
)
ORDER BY
  CASE
    WHEN quote_ident(table_schema) IN (
      SELECT
        CASE
          WHEN trim(s[i]) = '"$user"' THEN user
          ELSE trim(s[i])
        END
      FROM
        generate_series(
          array_lower(string_to_array(current_setting('search_path'), ','), 1),
          array_upper(string_to_array(current_setting('search_path'), ','), 1)
        ) AS i,
        string_to_array(current_setting('search_path'), ',') s
    )
    THEN 0
    ELSE 1
  END,
  1;`

		rewritten := rewriteQueryForDuckDB(postgresQuery)
		require.NotEqual(t, postgresQuery, rewritten, "query should be rewritten")
		require.Contains(t, rewritten, "information_schema.tables", "rewritten query should still query information_schema.tables")
		require.Contains(t, rewritten, `"table"`, "rewritten query should have table column")
		require.Contains(t, rewritten, "current_schema()", "rewritten query should use current_schema()")
		require.NotContains(t, strings.ToLower(rewritten), "search_path", "rewritten query should not contain search_path")
	})

	t.Run("does not rewrite regular queries", func(t *testing.T) {
		regularQueries := []string{
			"SELECT * FROM test_table",
			"SELECT id, name FROM users WHERE id = 1",
			"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public'",
		}

		for _, query := range regularQueries {
			rewritten := rewriteQueryForDuckDB(query)
			require.Equal(t, query, rewritten, "regular query should not be rewritten: %q", query)
		}
	})

	t.Run("handles queries with different whitespace", func(t *testing.T) {
		// Query with extra whitespace and newlines
		postgresQuery := `SELECT   CASE
    WHEN quote_ident(table_schema) IN (
      SELECT CASE WHEN trim(s[i]) = '"$user"' THEN user ELSE trim(s[i]) END
      FROM generate_series(
          array_lower(string_to_array(current_setting('search_path'), ','), 1),
          array_upper(string_to_array(current_setting('search_path'), ','), 1)
        ) AS i,
        string_to_array(current_setting('search_path'), ',') s
    )
    THEN quote_ident(table_name)
    ELSE quote_ident(table_schema) || '.' || quote_ident(table_name)
  END AS "table"
FROM information_schema.tables
WHERE quote_ident(table_schema) NOT IN ('information_schema', 'pg_catalog', '_timescaledb_cache')`

		rewritten := rewriteQueryForDuckDB(postgresQuery)
		require.NotEqual(t, postgresQuery, rewritten, "query with different whitespace should still be rewritten")
		require.Contains(t, rewritten, "information_schema.tables", "rewritten query should still query information_schema.tables")
	})

	t.Run("rewrites PostgreSQL table listing query to DuckDB", func(t *testing.T) {
		ctx := context.Background()
		db := testDB(t)

		// Set up test data - create some tables
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(ctx, `CREATE TABLE test_table1 (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		_, err = conn.ExecContext(ctx, `CREATE TABLE test_table2 (id INTEGER, value DOUBLE)`)
		require.NoError(t, err)

		_, err = conn.ExecContext(ctx, `CREATE TABLE another_table (id INTEGER)`)
		require.NoError(t, err)

		// Create server with PostgreSQL wire protocol
		httpListener := getFreeListener(t)
		postgresListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Connect as PostgreSQL client
		pgConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://user:password@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)

		// Execute the PostgreSQL table listing query (the one that should be rewritten)
		postgresTableQuery := `SELECT
  CASE
    WHEN quote_ident(table_schema) IN (
      SELECT
        CASE
          WHEN trim(s[i]) = '"$user"' THEN user
          ELSE trim(s[i])
        END
      FROM
        generate_series(
          array_lower(string_to_array(current_setting('search_path'), ','), 1),
          array_upper(string_to_array(current_setting('search_path'), ','), 1)
        ) AS i,
        string_to_array(current_setting('search_path'), ',') s
    )
    THEN quote_ident(table_name)
    ELSE quote_ident(table_schema) || '.' || quote_ident(table_name)
  END AS "table"
FROM information_schema.tables
WHERE quote_ident(table_schema) NOT IN (
  'information_schema',
  'pg_catalog',
  '_timescaledb_cache',
  '_timescaledb_catalog',
  '_timescaledb_internal',
  '_timescaledb_config',
  'timescaledb_information',
  'timescaledb_experimental'
)
ORDER BY
  CASE
    WHEN quote_ident(table_schema) IN (
      SELECT
        CASE
          WHEN trim(s[i]) = '"$user"' THEN user
          ELSE trim(s[i])
        END
      FROM
        generate_series(
          array_lower(string_to_array(current_setting('search_path'), ','), 1),
          array_upper(string_to_array(current_setting('search_path'), ','), 1)
        ) AS i,
        string_to_array(current_setting('search_path'), ',') s
    )
    THEN 0
    ELSE 1
  END,
  1;`

		rows, err := pgConn.Query(ctx, postgresTableQuery)
		require.NoError(t, err)

		// Collect all table names
		var tableNames []string
		for rows.Next() {
			var tableName string
			err = rows.Scan(&tableName)
			require.NoError(t, err)
			tableNames = append(tableNames, tableName)
		}
		require.NoError(t, rows.Err())
		rows.Close()

		// Verify we got the expected tables (should include our test tables)
		// The exact list may vary, but should include our test tables
		require.GreaterOrEqual(t, len(tableNames), 3, "should have at least 3 tables")

		// Verify our test tables are in the results
		tableMap := make(map[string]bool)
		for _, name := range tableNames {
			tableMap[name] = true
		}

		// Check that our test tables are present
		// They might be returned as just the table name or schema.table
		foundTable1 := tableMap["test_table1"] || tableMap[fmt.Sprintf("%s.test_table1", db.Schema())]
		foundTable2 := tableMap["test_table2"] || tableMap[fmt.Sprintf("%s.test_table2", db.Schema())]
		foundAnother := tableMap["another_table"] || tableMap[fmt.Sprintf("%s.another_table", db.Schema())]

		require.True(t, foundTable1, "test_table1 should be in results: %v", tableNames)
		require.True(t, foundTable2, "test_table2 should be in results: %v", tableNames)
		require.True(t, foundAnother, "another_table should be in results: %v", tableNames)

		// Verify column name is "table"
		rows, err = pgConn.Query(ctx, postgresTableQuery)
		require.NoError(t, err)
		columns := rows.FieldDescriptions()
		require.Len(t, columns, 1)
		require.Equal(t, "table", columns[0].Name)
		rows.Close()

		// Close connection before shutting down server
		pgConn.Close(ctx)

		// Give server a moment to process connection closure
		time.Sleep(100 * time.Millisecond)

		// Shutdown server
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})

	t.Run("detects and rewrites PostgreSQL column listing query", func(t *testing.T) {
		postgresQuery := `SELECT
  quote_ident(column_name) AS "column",
  data_type AS "type"
FROM information_schema.columns
WHERE
  CASE
    WHEN array_length(parse_ident('dz_contributors_current'), 1) = 2
    THEN
      quote_ident(table_schema) = (parse_ident('dz_contributors_current'))[1]
      AND quote_ident(table_name) = (parse_ident('dz_contributors_current'))[2]
    ELSE
      quote_ident(table_name) = 'dz_contributors_current'
      AND quote_ident(table_schema) IN (
        SELECT
          CASE
            WHEN trim(s[i]) = '"$user"' THEN user
            ELSE trim(s[i])
          END
        FROM
          generate_series(
            array_lower(string_to_array(current_setting('search_path'), ','), 1),
            array_upper(string_to_array(current_setting('search_path'), ','), 1)
          ) AS i,
          string_to_array(current_setting('search_path'), ',') s
      )
  END;`

		rewritten := rewriteQueryForDuckDB(postgresQuery)
		require.NotEqual(t, postgresQuery, rewritten, "query should be rewritten")
		require.Contains(t, rewritten, "information_schema.columns", "rewritten query should still query information_schema.columns")
		require.Contains(t, rewritten, `"column"`, "rewritten query should have column column")
		require.Contains(t, rewritten, `"type"`, "rewritten query should have type column")
		require.Contains(t, rewritten, "dz_contributors_current", "rewritten query should filter by table name")
		require.NotContains(t, strings.ToLower(rewritten), "search_path", "rewritten query should not contain search_path")
		require.NotContains(t, strings.ToLower(rewritten), "parse_ident", "rewritten query should not contain parse_ident")
	})

	t.Run("rewrites PostgreSQL column listing query to DuckDB", func(t *testing.T) {
		ctx := context.Background()
		db := testDB(t)

		// Set up test data - create a table with columns
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(ctx, `CREATE TABLE test_columns_table (id INTEGER, name VARCHAR, value DOUBLE)`)
		require.NoError(t, err)

		// Create server with PostgreSQL wire protocol
		httpListener := getFreeListener(t)
		postgresListener := getFreeListener(t)

		cfg := Config{
			HTTPListener:      httpListener,
			PostgresListener:  postgresListener,
			ReadHeaderTimeout: 30 * time.Second,
			ShutdownTimeout:   10 * time.Second,
			QuerierConfig: querier.Config{
				Logger: testLogger(t),
				DB:     db,
			},
		}

		srv, err := New(ctx, cfg)
		require.NoError(t, err)

		// Start server
		serverCtx, serverCancel := context.WithCancel(ctx)
		defer serverCancel()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- srv.Run(serverCtx)
		}()

		// Wait for server to be ready
		postgresAddr := postgresListener.Addr().String()
		waitForServerReady(t, ctx, postgresAddr, 10)

		// Connect as PostgreSQL client
		pgConn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://user:password@%s/postgres?sslmode=disable", postgresAddr))
		require.NoError(t, err)

		// Execute the PostgreSQL column listing query
		postgresColumnQuery := `SELECT
  quote_ident(column_name) AS "column",
  data_type AS "type"
FROM information_schema.columns
WHERE
  CASE
    WHEN array_length(parse_ident('test_columns_table'), 1) = 2
    THEN
      quote_ident(table_schema) = (parse_ident('test_columns_table'))[1]
      AND quote_ident(table_name) = (parse_ident('test_columns_table'))[2]
    ELSE
      quote_ident(table_name) = 'test_columns_table'
      AND quote_ident(table_schema) IN (
        SELECT
          CASE
            WHEN trim(s[i]) = '"$user"' THEN user
            ELSE trim(s[i])
          END
        FROM
          generate_series(
            array_lower(string_to_array(current_setting('search_path'), ','), 1),
            array_upper(string_to_array(current_setting('search_path'), ','), 1)
          ) AS i,
          string_to_array(current_setting('search_path'), ',') s
      )
  END;`

		rows, err := pgConn.Query(ctx, postgresColumnQuery)
		require.NoError(t, err)

		// Collect all column names
		var columnNames []string
		var columnTypes []string
		for rows.Next() {
			var colName, colType string
			err = rows.Scan(&colName, &colType)
			require.NoError(t, err)
			columnNames = append(columnNames, colName)
			columnTypes = append(columnTypes, colType)
		}
		require.NoError(t, rows.Err())
		rows.Close()

		// Verify we got the expected columns
		require.GreaterOrEqual(t, len(columnNames), 3, "should have at least 3 columns")
		require.Contains(t, columnNames, "id", "should have id column")
		require.Contains(t, columnNames, "name", "should have name column")
		require.Contains(t, columnNames, "value", "should have value column")

		// Verify column names
		rows, err = pgConn.Query(ctx, postgresColumnQuery)
		require.NoError(t, err)
		columns := rows.FieldDescriptions()
		require.Len(t, columns, 2)
		require.Equal(t, "column", columns[0].Name)
		require.Equal(t, "type", columns[1].Name)
		rows.Close()

		// Close connection before shutting down server
		pgConn.Close(ctx)

		// Give server a moment to process connection closure
		time.Sleep(100 * time.Millisecond)

		// Shutdown server
		serverCancel()
		select {
		case err := <-serverErrCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shutdown in time")
		}
	})
}
