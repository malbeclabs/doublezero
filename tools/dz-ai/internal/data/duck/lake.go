package duck

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

type LocalLake struct {
	log     *slog.Logger
	db      *sql.DB
	catalog string
	schema  string
}

type LocalLakeConnection struct {
	conn *sql.Conn
	db   *LocalLake
	mu   sync.Mutex
}

func (c *LocalLakeConnection) DB() DB {
	return c.db
}

func (c *LocalLakeConnection) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.conn.ExecContext(ctx, query, args...)
}

func (c *LocalLakeConnection) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}

func (c *LocalLakeConnection) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.conn.QueryRowContext(ctx, query, args...)
}

func (c *LocalLakeConnection) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return c.conn.BeginTx(ctx, opts)
}

func (c *LocalLakeConnection) Close() error {
	return c.conn.Close()
}

// S3Config holds configuration for S3-compatible storage (AWS S3, MinIO, etc.)
type S3Config struct {
	AccessKeyID     string // S3 access key ID
	SecretAccessKey string // S3 secret access key
	Endpoint        string // S3 endpoint URL (e.g., "http://localhost:9000" for MinIO, empty for AWS)
	Region          string // S3 region (e.g., "us-east-1")
	UseSSL          bool   // Whether to use SSL/TLS (typically false for MinIO, true for AWS)
	URLStyle        string // URL style: "path" (for MinIO) or "virtual" (for AWS S3)
}

// NewLake creates a new DuckLake instance with the specified catalog and storage.
//
// Catalog URI formats:
//   - file://: Local SQLite catalog
//     Example: "file:///path/to/catalog.db"
//   - postgres:// or postgresql://: PostgreSQL catalog (converted to libpq format internally)
//     Example: "postgres://user:password@localhost:5432/dbname?sslmode=disable"
//   - libpq format: PostgreSQL connection string (key=value pairs)
//     Example: "host=localhost port=5432 user=testuser password=testpass dbname=testdb sslmode=disable"
//
// Storage URI formats:
//   - file://: Local file system storage
//     Example: "file:///path/to/storage"
//   - s3://: S3-compatible storage (AWS S3, MinIO, etc.)
//     Example: "s3://bucket-name/path/to/data"
//     Note: S3Config must be provided when using s3:// storage
//
// S3Config is required when storageURI uses s3://. For MinIO:
//   - Endpoint: "http://localhost:9000" (or your MinIO endpoint)
//   - UseSSL: false
//   - URLStyle: "path"
//
// For AWS S3:
//   - Endpoint: "" (empty, uses default AWS endpoints)
//   - UseSSL: true
//   - URLStyle: "virtual" (or empty, defaults to virtual)
func NewLake(ctx context.Context, log *slog.Logger, catalogName, catalogURI, storageURI string, s3Config ...*S3Config) (*LocalLake, error) {
	if err := validateCatalogURI(catalogURI); err != nil {
		return nil, err
	}
	if err := validateStorageURI(storageURI); err != nil {
		return nil, err
	}

	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Determine catalog type and prepare catalog connection string
	var catalogConnStr string
	if catalogPath, found := strings.CutPrefix(catalogURI, "file://"); found {
		catalogPath, err = filepath.Abs(catalogPath)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for catalog directory: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(catalogPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create catalog directory: %w", err)
		}
		catalogConnStr = catalogPath
	} else if strings.HasPrefix(catalogURI, "postgres://") || strings.HasPrefix(catalogURI, "postgresql://") {
		// Parse postgres URI and convert to libpq format for DuckDB's ducklake postgres connector
		// According to docs: ATTACH 'ducklake:postgres:dbname=ducklake_catalog host=localhost'
		parsedURI, err := url.Parse(catalogURI)
		if err != nil {
			return nil, fmt.Errorf("failed to parse postgres URI: %w", err)
		}
		// Build libpq format connection string (key=value pairs)
		var parts []string
		if parsedURI.Hostname() != "" {
			parts = append(parts, fmt.Sprintf("host=%s", parsedURI.Hostname()))
		}
		if parsedURI.Port() != "" {
			parts = append(parts, fmt.Sprintf("port=%s", parsedURI.Port()))
		}
		if parsedURI.User != nil {
			if username := parsedURI.User.Username(); username != "" {
				parts = append(parts, fmt.Sprintf("user=%s", username))
			}
			if password, ok := parsedURI.User.Password(); ok {
				parts = append(parts, fmt.Sprintf("password=%s", password))
			}
		}
		if parsedURI.Path != "" {
			dbname := strings.TrimPrefix(parsedURI.Path, "/")
			parts = append(parts, fmt.Sprintf("dbname=%s", dbname))
		}
		// Parse query parameters and add them
		if parsedURI.RawQuery != "" {
			queryParams, err := url.ParseQuery(parsedURI.RawQuery)
			if err == nil {
				for key, values := range queryParams {
					if len(values) > 0 {
						parts = append(parts, fmt.Sprintf("%s=%s", key, values[0]))
					}
				}
			}
		}
		catalogConnStr = strings.Join(parts, " ")
	} else if strings.Contains(catalogURI, "host=") && strings.Contains(catalogURI, "dbname=") {
		// Already in libpq format (from testcontainers ConnectionString)
		// DuckDB's ducklake postgres connector expects libpq format directly
		catalogConnStr = catalogURI
	} else {
		return nil, fmt.Errorf("catalog URI must be file:// or postgres:// or postgresql://")
	}

	// Determine storage type and prepare storage path
	var storagePath string
	var useS3 bool
	if path, found := strings.CutPrefix(storageURI, "file://"); found {
		storagePath, err = filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for storage directory: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(storagePath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create storage directory: %w", err)
		}
	} else if strings.HasPrefix(storageURI, "s3://") {
		// For S3 paths, we'll append the secret reference after creating the secret
		storagePath = storageURI
		useS3 = true
	} else {
		return nil, fmt.Errorf("storage URI must be file:// or s3://")
	}

	// Install required extensions
	extensions := []string{"ducklake"}
	if strings.HasPrefix(catalogURI, "postgres://") || strings.HasPrefix(catalogURI, "postgresql://") || strings.Contains(catalogURI, "host=") {
		extensions = append(extensions, "postgres")
	} else {
		extensions = append(extensions, "sqlite")
	}
	if useS3 {
		extensions = append(extensions, "httpfs")
	}

	for _, ext := range extensions {
		if _, err := db.Exec(fmt.Sprintf("INSTALL '%s'", ext)); err != nil {
			return nil, fmt.Errorf("failed to install extension %s: %w", ext, err)
		}
		// LOAD the extension after installing
		if _, err := db.Exec(fmt.Sprintf("LOAD '%s'", ext)); err != nil {
			return nil, fmt.Errorf("failed to load extension %s: %w", ext, err)
		}
	}

	// Configure S3 if using S3 storage
	if useS3 {
		var cfg *S3Config
		if len(s3Config) > 0 && s3Config[0] != nil {
			cfg = s3Config[0]
		}
		if cfg == nil {
			return nil, fmt.Errorf("S3 configuration is required when using s3:// storage URI")
		}

		// Create S3 secret
		secretSQL := "CREATE SECRET IF NOT EXISTS s3_secret (TYPE s3"
		if cfg.AccessKeyID != "" {
			secretSQL += fmt.Sprintf(", KEY_ID '%s'", strings.ReplaceAll(cfg.AccessKeyID, "'", "''"))
		}
		if cfg.SecretAccessKey != "" {
			secretSQL += fmt.Sprintf(", SECRET '%s'", strings.ReplaceAll(cfg.SecretAccessKey, "'", "''"))
		}
		if cfg.Endpoint != "" {
			// DuckDB's S3 secret ENDPOINT expects just host:port, not a full URL
			// Strip http:// or https:// prefix if present
			endpoint := cfg.Endpoint
			endpoint = strings.TrimPrefix(endpoint, "http://")
			endpoint = strings.TrimPrefix(endpoint, "https://")
			secretSQL += fmt.Sprintf(", ENDPOINT '%s'", endpoint)
		}
		if cfg.Region != "" {
			secretSQL += fmt.Sprintf(", REGION '%s'", cfg.Region)
		}
		urlStyle := cfg.URLStyle
		if urlStyle == "" {
			urlStyle = "path" // Default to path style for MinIO compatibility
		}
		secretSQL += fmt.Sprintf(", URL_STYLE '%s'", urlStyle)
		useSSL := cfg.UseSSL
		if cfg.Endpoint != "" && !strings.Contains(cfg.Endpoint, "amazonaws.com") {
			// Default to false for non-AWS endpoints (like MinIO)
			useSSL = false
		}
		secretSQL += fmt.Sprintf(", USE_SSL %t", useSSL)
		secretSQL += ")"

		if _, err := db.Exec(secretSQL); err != nil {
			return nil, fmt.Errorf("failed to create S3 secret: %w", err)
		}
		// Note: The S3 secret is created and should be used automatically by ducklake
		// for S3 operations. The storagePath is passed as-is to DATA_PATH.
		log.Info("configured S3 storage", "endpoint", cfg.Endpoint, "region", cfg.Region)
	}

	// Build ATTACH statement
	var attachSQL string
	isPostgres := strings.HasPrefix(catalogURI, "postgres://") || strings.HasPrefix(catalogURI, "postgresql://") || strings.Contains(catalogURI, "host=")
	if isPostgres {
		// DuckDB's ducklake postgres connector expects libpq format (key=value pairs)
		// Format: 'ducklake:postgres:dbname=ducklake_catalog host=localhost'
		attachSQL = fmt.Sprintf("ATTACH 'ducklake:postgres:%s' AS %s (DATA_PATH '%s')", catalogConnStr, catalogName, storagePath)
	} else {
		attachSQL = fmt.Sprintf("ATTACH 'ducklake:sqlite:%s' AS %s (DATA_PATH '%s')", catalogConnStr, catalogName, storagePath)
	}

	// For postgres, retry the attach operation to wait for the database to be ready
	if isPostgres {
		var attachErr error
		maxRetries := 8
		retryDelay := 500 * time.Millisecond
		for i := range maxRetries {
			_, attachErr = db.Exec(attachSQL)
			if attachErr == nil {
				break
			}
			if i < maxRetries-1 {
				log.Debug("postgres not ready, retrying attach", "attempt", i+1, "error", attachErr)
				// Use context-aware sleep to respect cancellation
				timer := time.NewTimer(retryDelay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, fmt.Errorf("context cancelled while waiting for postgres: %w", ctx.Err())
				case <-timer.C:
					// Timer expired, continue with retry
				}
				retryDelay *= 2 // Exponential backoff
			}
		}
		if attachErr != nil {
			return nil, fmt.Errorf("failed to attach ducklake after %d attempts: %w", maxRetries, attachErr)
		}
	} else {
		if _, err := db.Exec(attachSQL); err != nil {
			return nil, fmt.Errorf("failed to attach ducklake: %w", err)
		}
	}

	if _, err := db.Exec(fmt.Sprintf("USE %s", catalogName)); err != nil {
		return nil, fmt.Errorf("failed to use catalog: %w", err)
	}

	row := db.QueryRowContext(ctx, "SELECT current_database() AS catalog, current_schema() AS schema")
	var catalog, schema string
	err = row.Scan(&catalog, &schema)
	if err != nil {
		return nil, fmt.Errorf("failed to get current database and schema: %w", err)
	}

	return &LocalLake{
		log:     log,
		db:      db,
		catalog: catalogName,
		schema:  schema,
	}, nil
}

func (l *LocalLake) Catalog() string {
	return l.catalog
}

func (l *LocalLake) Schema() string {
	return l.schema
}

func (l *LocalLake) Close() error {
	return l.db.Close()
}

func (l *LocalLake) Conn(ctx context.Context) (Connection, error) {
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "USE "+l.catalog); err != nil {
		return nil, fmt.Errorf("USE failed: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "SET schema = "+l.schema); err != nil {
		return nil, fmt.Errorf("SET schema failed: %w", err)
	}
	return &LocalLakeConnection{
		conn: conn,
		db:   l,
	}, nil
}

func validateCatalogURI(uri string) error {
	if uri == "" {
		return fmt.Errorf("catalog URI is required")
	}

	if path, found := strings.CutPrefix(uri, "file://"); found {
		if path == "" {
			return fmt.Errorf("catalog URI file:// path cannot be empty")
		}
		return nil
	}

	if strings.HasPrefix(uri, "postgres://") || strings.HasPrefix(uri, "postgresql://") {
		parsed, err := url.Parse(uri)
		if err != nil {
			return fmt.Errorf("invalid postgres URI format: %w", err)
		}
		if parsed.Host == "" {
			return fmt.Errorf("postgres URI must include a host")
		}
		if parsed.Path == "" || parsed.Path == "/" {
			return fmt.Errorf("postgres URI must include a database name in the path")
		}
		return nil
	}

	return fmt.Errorf("catalog URI must start with file://, postgres://, postgresql://, or be in libpq format (got: %q)", uri)
}

func validateStorageURI(uri string) error {
	if uri == "" {
		return fmt.Errorf("storage URI is required")
	}

	if path, found := strings.CutPrefix(uri, "file://"); found {
		if path == "" {
			return fmt.Errorf("storage URI file:// path cannot be empty")
		}
		return nil
	}

	if strings.HasPrefix(uri, "s3://") {
		parsed, err := url.Parse(uri)
		if err != nil {
			return fmt.Errorf("invalid s3:// URI format: %w", err)
		}
		if parsed.Host == "" {
			return fmt.Errorf("s3:// URI must include a bucket name (e.g., s3://bucket-name/path)")
		}
		// Validate bucket name format (basic check)
		bucket := parsed.Host
		if len(bucket) < 3 || len(bucket) > 63 {
			return fmt.Errorf("s3 bucket name must be between 3 and 63 characters")
		}
		return nil
	}

	return fmt.Errorf("storage URI must start with file:// or s3:// (got: %q)", uri)
}
