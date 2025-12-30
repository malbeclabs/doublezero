package duck

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/minio"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestLake_Duck_ValidateCatalogURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty URI",
			uri:     "",
			wantErr: true,
			errMsg:  "catalog URI is required",
		},
		{
			name:    "valid file URI",
			uri:     "file:///tmp/catalog.db",
			wantErr: false,
		},
		{
			name:    "empty file path",
			uri:     "file://",
			wantErr: true,
			errMsg:  "catalog URI file:// path cannot be empty",
		},
		{
			name:    "valid postgres URI",
			uri:     "postgres://user:pass@localhost:5432/mydb",
			wantErr: false,
		},
		{
			name:    "valid postgresql URI",
			uri:     "postgresql://user:pass@localhost:5432/mydb",
			wantErr: false,
		},
		{
			name:    "postgres URI without host",
			uri:     "postgres:///mydb",
			wantErr: true,
			errMsg:  "postgres URI must include a host",
		},
		{
			name:    "postgres URI without database",
			uri:     "postgres://user:pass@localhost:5432/",
			wantErr: true,
			errMsg:  "postgres URI must include a database name in the path",
		},
		{
			name:    "postgres URI with root path only",
			uri:     "postgres://user:pass@localhost:5432",
			wantErr: true,
			errMsg:  "postgres URI must include a database name in the path",
		},
		{
			name:    "invalid scheme",
			uri:     "http://example.com",
			wantErr: true,
			errMsg:  "catalog URI must start with file://, postgres://, postgresql://, or be in libpq format (got: \"http://example.com\")",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCatalogURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					require.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLake_Duck_ValidateStorageURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty URI",
			uri:     "",
			wantErr: true,
			errMsg:  "storage URI is required",
		},
		{
			name:    "valid file URI",
			uri:     "file:///tmp/storage",
			wantErr: false,
		},
		{
			name:    "empty file path",
			uri:     "file://",
			wantErr: true,
			errMsg:  "storage URI file:// path cannot be empty",
		},
		{
			name:    "valid s3 URI",
			uri:     "s3://my-bucket/path",
			wantErr: false,
		},
		{
			name:    "s3 URI without bucket",
			uri:     "s3:///path",
			wantErr: true,
			errMsg:  "s3:// URI must include a bucket name",
		},
		{
			name:    "s3 URI with short bucket name",
			uri:     "s3://ab/path",
			wantErr: true,
			errMsg:  "s3 bucket name must be between 3 and 63 characters",
		},
		{
			name:    "s3 URI with long bucket name",
			uri:     "s3://" + strings.Repeat("a", 64) + "/path",
			wantErr: true,
			errMsg:  "s3 bucket name must be between 3 and 63 characters",
		},
		{
			name:    "invalid scheme",
			uri:     "http://example.com",
			wantErr: true,
			errMsg:  "storage URI must start with file:// or s3://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStorageURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					require.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLake_Duck_NewLake_FileCatalogFileStorage(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	tmpDir := t.TempDir()
	catalogPath := filepath.Join(tmpDir, "catalog.db")
	storagePath := filepath.Join(tmpDir, "storage")

	catalogURI := "file://" + catalogPath
	storageURI := "file://" + storagePath

	lake, err := NewLake(ctx, log, "test_catalog", catalogURI, storageURI, false)
	require.NoError(t, err)
	require.NotNil(t, lake)
	defer lake.Close()

	require.Equal(t, "test_catalog", lake.Catalog())

	// Test that we can get a connection
	conn, err := lake.Conn(ctx)
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// Test creating a table, writing data, and reading it back
	_, err = conn.ExecContext(ctx, `
		CREATE TABLE test_table (
			id INTEGER,
			name VARCHAR,
			value INTEGER
		)
	`)
	require.NoError(t, err)

	// Insert data
	_, err = conn.ExecContext(ctx, "INSERT INTO test_table (id, name, value) VALUES (1, 'test', 42)")
	require.NoError(t, err)

	// Read data back
	var id int
	var name string
	var value int
	err = conn.QueryRowContext(ctx, "SELECT id, name, value FROM test_table WHERE id = 1").Scan(&id, &name, &value)
	require.NoError(t, err)
	require.Equal(t, 1, id)
	require.Equal(t, "test", name)
	require.Equal(t, 42, value)
}

func TestLake_Duck_NewLake_PostgresCatalogFileStorage(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Start postgres container
	pgContainer, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
	)
	require.NoError(t, err)
	defer func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to cleanup postgres container: %v", err)
		}
	}()

	tmpDir := t.TempDir()
	storagePath := filepath.Join(tmpDir, "storage")
	storageURI := "file://" + storagePath

	// Get connection details and build postgres:// URI directly
	host, err := pgContainer.Host(ctx)
	require.NoError(t, err)
	port, err := pgContainer.MappedPort(ctx, "5432")
	require.NoError(t, err)
	// Build postgres:// URI directly instead of using libpq format
	catalogURI := fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())

	lake, err := NewLake(ctx, log, "test_catalog", catalogURI, storageURI, false)
	require.NoError(t, err)
	require.NotNil(t, lake)
	defer lake.Close()

	require.Equal(t, "test_catalog", lake.Catalog())

	// Test that we can get a connection
	conn, err := lake.Conn(ctx)
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// Verify we can query
	var result int
	err = conn.QueryRowContext(ctx, "SELECT 1").Scan(&result)
	require.NoError(t, err)
	require.Equal(t, 1, result)

	// Test creating a table, writing data, and reading it back
	_, err = conn.ExecContext(ctx, `
		CREATE TABLE test_table (
			id INTEGER,
			name VARCHAR,
			value INTEGER
		)
	`)
	require.NoError(t, err)

	// Insert data
	_, err = conn.ExecContext(ctx, "INSERT INTO test_table (id, name, value) VALUES (1, 'test', 42)")
	require.NoError(t, err)

	// Read data back
	var id int
	var name string
	var value int
	err = conn.QueryRowContext(ctx, "SELECT id, name, value FROM test_table WHERE id = 1").Scan(&id, &name, &value)
	require.NoError(t, err)
	require.Equal(t, 1, id)
	require.Equal(t, "test", name)
	require.Equal(t, 42, value)
}

func TestLake_Duck_NewLake_FileCatalogS3Storage(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Start minio container
	minioContainer, err := minio.Run(ctx, "minio/minio:latest",
		minio.WithUsername("minioadmin"),
		minio.WithPassword("minioadmin"),
	)
	require.NoError(t, err)
	defer func() {
		if err := minioContainer.Terminate(ctx); err != nil {
			t.Logf("failed to cleanup minio container: %v", err)
		}
	}()

	// Get host and port separately to ensure DuckDB can access MinIO
	// ConnectionString() may return localhost which doesn't work in all network contexts
	// Use 127.0.0.1 instead of localhost to avoid DNS resolution issues
	host, err := minioContainer.Host(ctx)
	require.NoError(t, err)
	// Replace localhost with 127.0.0.1 for better compatibility
	if host == "localhost" {
		host = "127.0.0.1"
	}
	port, err := minioContainer.MappedPort(ctx, "9000")
	require.NoError(t, err)
	endpoint := fmt.Sprintf("%s:%s", host, port.Port())

	// Create S3 client to create the bucket
	creds := credentials.NewStaticCredentialsProvider(
		minioContainer.Username,
		minioContainer.Password,
		"",
	)
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(creds),
	)
	require.NoError(t, err)

	// Ensure endpoint has protocol for AWS SDK
	endpointURL := "http://" + endpoint
	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = &endpointURL
		o.UsePathStyle = true // Required for MinIO
	})

	// Create the bucket
	bucketName := "test-bucket"
	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: &bucketName,
	})
	require.NoError(t, err)

	tmpDir := t.TempDir()
	catalogPath := filepath.Join(tmpDir, "catalog.db")
	catalogURI := "file://" + catalogPath
	storageURI := fmt.Sprintf("s3://%s/data", bucketName)

	s3Config := &S3Config{
		AccessKeyID:     minioContainer.Username,
		SecretAccessKey: minioContainer.Password,
		Endpoint:        endpoint,
		Region:          "us-east-1",
		UseSSL:          false,
		URLStyle:        "path",
	}

	lake, err := NewLake(ctx, log, "test_catalog", catalogURI, storageURI, false, s3Config)
	require.NoError(t, err)
	require.NotNil(t, lake)
	defer lake.Close()

	require.Equal(t, "test_catalog", lake.Catalog())

	// Test that we can get a connection
	conn, err := lake.Conn(ctx)
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// Verify we can query
	var result int
	err = conn.QueryRowContext(ctx, "SELECT 1").Scan(&result)
	require.NoError(t, err)
	require.Equal(t, 1, result)

	// Test creating a table, writing data, and reading it back
	_, err = conn.ExecContext(ctx, `
		CREATE TABLE test_table (
			id INTEGER,
			name VARCHAR,
			value INTEGER
		)
	`)
	require.NoError(t, err)

	// Insert data
	_, err = conn.ExecContext(ctx, "INSERT INTO test_table (id, name, value) VALUES (1, 'test', 42)")
	require.NoError(t, err)

	// Read data back
	var id int
	var name string
	var value int
	err = conn.QueryRowContext(ctx, "SELECT id, name, value FROM test_table WHERE id = 1").Scan(&id, &name, &value)
	require.NoError(t, err)
	require.Equal(t, 1, id)
	require.Equal(t, "test", name)
	require.Equal(t, 42, value)
}

func TestLake_Duck_NewLake_PostgresCatalogS3Storage(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Start postgres container
	pgContainer, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
	)
	require.NoError(t, err)
	defer func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to cleanup postgres container: %v", err)
		}
	}()

	// Start minio container
	minioContainer, err := minio.Run(ctx, "minio/minio:latest",
		minio.WithUsername("minioadmin"),
		minio.WithPassword("minioadmin"),
	)
	require.NoError(t, err)
	defer func() {
		if err := minioContainer.Terminate(ctx); err != nil {
			t.Logf("failed to cleanup minio container: %v", err)
		}
	}()

	// Get host and port separately to ensure DuckDB can access MinIO
	// ConnectionString() may return localhost which doesn't work in all network contexts
	// Use 127.0.0.1 instead of localhost to avoid DNS resolution issues
	minioHost, err := minioContainer.Host(ctx)
	require.NoError(t, err)
	// Replace localhost with 127.0.0.1 for better compatibility
	if minioHost == "localhost" {
		minioHost = "127.0.0.1"
	}
	minioPort, err := minioContainer.MappedPort(ctx, "9000")
	require.NoError(t, err)
	endpoint := fmt.Sprintf("%s:%s", minioHost, minioPort.Port())

	// Create S3 client to create the bucket
	creds := credentials.NewStaticCredentialsProvider(
		minioContainer.Username,
		minioContainer.Password,
		"",
	)
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(creds),
	)
	require.NoError(t, err)

	// Ensure endpoint has protocol for AWS SDK
	endpointURL := "http://" + endpoint
	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = &endpointURL
		o.UsePathStyle = true // Required for MinIO
	})

	// Create the bucket
	bucketName := "test-bucket"
	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: &bucketName,
	})
	require.NoError(t, err)

	// Get host and port from postgres container
	host, err := pgContainer.Host(ctx)
	require.NoError(t, err)
	port, err := pgContainer.MappedPort(ctx, "5432")
	require.NoError(t, err)

	catalogURI := "postgres://testuser:testpass@" + host + ":" + port.Port() + "/testdb?sslmode=disable"
	storageURI := fmt.Sprintf("s3://%s/data", bucketName)

	s3Config := &S3Config{
		AccessKeyID:     minioContainer.Username,
		SecretAccessKey: minioContainer.Password,
		Endpoint:        endpoint,
		Region:          "us-east-1",
		UseSSL:          false,
		URLStyle:        "path",
	}

	lake, err := NewLake(ctx, log, "test_catalog", catalogURI, storageURI, false, s3Config)
	require.NoError(t, err)
	require.NotNil(t, lake)
	defer lake.Close()

	require.Equal(t, "test_catalog", lake.Catalog())

	// Test that we can get a connection
	conn, err := lake.Conn(ctx)
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// Verify we can query
	var result int
	err = conn.QueryRowContext(ctx, "SELECT 1").Scan(&result)
	require.NoError(t, err)
	require.Equal(t, 1, result)

	// Test creating a table, writing data, and reading it back
	_, err = conn.ExecContext(ctx, `
		CREATE TABLE test_table (
			id INTEGER,
			name VARCHAR,
			value INTEGER
		)
	`)
	require.NoError(t, err)

	// Insert data
	_, err = conn.ExecContext(ctx, "INSERT INTO test_table (id, name, value) VALUES (1, 'test', 42)")
	require.NoError(t, err)

	// Read data back
	var id int
	var name string
	var value int
	err = conn.QueryRowContext(ctx, "SELECT id, name, value FROM test_table WHERE id = 1").Scan(&id, &name, &value)
	require.NoError(t, err)
	require.Equal(t, 1, id)
	require.Equal(t, "test", name)
	require.Equal(t, 42, value)
}

func TestLake_Duck_NewLake_S3ConfigRequired(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	tmpDir := t.TempDir()
	catalogPath := filepath.Join(tmpDir, "catalog.db")
	catalogURI := "file://" + catalogPath
	storageURI := "s3://test-bucket/data"

	// Test without S3 config
	_, err := NewLake(ctx, log, "test_catalog", catalogURI, storageURI, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "S3 configuration is required when using s3:// storage URI")
}
