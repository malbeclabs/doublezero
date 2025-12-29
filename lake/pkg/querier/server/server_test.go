package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
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
		// Timestamp might be returned in different format, just check it's a valid time
		require.False(t, createdAt.IsZero())
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
