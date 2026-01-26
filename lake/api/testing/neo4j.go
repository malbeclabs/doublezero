package apitesting

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	"github.com/stretchr/testify/require"
	tcneo4j "github.com/testcontainers/testcontainers-go/modules/neo4j"
)

// Neo4jDBConfig holds the Neo4j test container configuration.
type Neo4jDBConfig struct {
	Username       string
	Password       string
	ContainerImage string
}

// Neo4jDB represents a Neo4j test container.
type Neo4jDB struct {
	log       *slog.Logger
	cfg       *Neo4jDBConfig
	boltURL   string
	container *tcneo4j.Neo4jContainer
}

// BoltURL returns the Bolt protocol URL for the Neo4j container.
func (db *Neo4jDB) BoltURL() string {
	return db.boltURL
}

// Username returns the Neo4j username.
func (db *Neo4jDB) Username() string {
	return db.cfg.Username
}

// Password returns the Neo4j password.
func (db *Neo4jDB) Password() string {
	return db.cfg.Password
}

// Close terminates the Neo4j container.
func (db *Neo4jDB) Close() {
	terminateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.container.Terminate(terminateCtx); err != nil {
		db.log.Error("failed to terminate Neo4j container", "error", err)
	}
}

func (cfg *Neo4jDBConfig) Validate() error {
	if cfg.Username == "" {
		cfg.Username = "neo4j"
	}
	if cfg.Password == "" {
		cfg.Password = "password"
	}
	if cfg.ContainerImage == "" {
		cfg.ContainerImage = "neo4j:5-community"
	}
	return nil
}

// NewNeo4jDB creates a new Neo4j testcontainer.
func NewNeo4jDB(ctx context.Context, log *slog.Logger, cfg *Neo4jDBConfig) (*Neo4jDB, error) {
	if cfg == nil {
		cfg = &Neo4jDBConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate Neo4j DB config: %w", err)
	}

	// Retry container start up to 3 times for retryable errors
	var container *tcneo4j.Neo4jContainer
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		var err error
		container, err = tcneo4j.Run(ctx,
			cfg.ContainerImage,
			tcneo4j.WithAdminPassword(cfg.Password),
			tcneo4j.WithoutAuthentication(),
		)
		if err != nil {
			lastErr = err
			if isRetryableNeo4jContainerStartErr(err) && attempt < 3 {
				time.Sleep(time.Duration(attempt) * 750 * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("failed to start Neo4j container after retries: %w", lastErr)
		}
		break
	}

	if container == nil {
		return nil, fmt.Errorf("failed to start Neo4j container after retries: %w", lastErr)
	}

	boltURL, err := container.BoltUrl(ctx)
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get Neo4j bolt URL: %w", err)
	}

	db := &Neo4jDB{
		log:       log,
		cfg:       cfg,
		boltURL:   boltURL,
		container: container,
	}

	return db, nil
}

// SetupTestNeo4j sets up a test Neo4j client and configures config.Neo4jClient.
// Creates a read-only client matching the API's usage pattern.
func SetupTestNeo4j(t *testing.T, db *Neo4jDB) {
	ctx := t.Context()

	// Create a read-only client (matches API usage)
	client, err := neo4j.NewReadOnlyClient(ctx, slog.Default(), db.boltURL, neo4j.DefaultDatabase, db.cfg.Username, db.cfg.Password)
	require.NoError(t, err, "failed to create Neo4j client")

	// Save old client and swap
	oldClient := config.Neo4jClient
	oldDatabase := config.Neo4jDatabase
	config.Neo4jClient = client
	config.Neo4jDatabase = neo4j.DefaultDatabase

	t.Cleanup(func() {
		client.Close(context.Background())
		config.Neo4jClient = oldClient
		config.Neo4jDatabase = oldDatabase
	})
}

// SetupTestNeo4jWithData sets up a test Neo4j client with optional data seeding.
// Uses a read-write client for setup, then swaps to read-only for the test.
func SetupTestNeo4jWithData(t *testing.T, db *Neo4jDB, seedFunc func(ctx context.Context, session neo4j.Session) error) {
	ctx := t.Context()

	// Create a read-write client for seeding
	rwClient, err := neo4j.NewClient(ctx, slog.Default(), db.boltURL, neo4j.DefaultDatabase, db.cfg.Username, db.cfg.Password)
	require.NoError(t, err, "failed to create Neo4j read-write client")

	// Clear database
	session, err := rwClient.Session(ctx)
	require.NoError(t, err, "failed to create Neo4j session")

	result, err := session.Run(ctx, "MATCH (n) DETACH DELETE n", nil)
	require.NoError(t, err, "failed to clear Neo4j database")
	_, err = result.Consume(ctx)
	require.NoError(t, err, "failed to consume clear result")

	// Run migrations
	err = neo4j.RunMigrations(ctx, slog.Default(), neo4j.MigrationConfig{
		URI:      db.boltURL,
		Database: neo4j.DefaultDatabase,
		Username: db.cfg.Username,
		Password: db.cfg.Password,
	})
	require.NoError(t, err, "failed to run Neo4j migrations")

	// Seed data if function provided
	if seedFunc != nil {
		err = seedFunc(ctx, session)
		require.NoError(t, err, "failed to seed Neo4j data")
	}

	session.Close(ctx)
	rwClient.Close(ctx)

	// Create a read-only client (matches API usage)
	roClient, err := neo4j.NewReadOnlyClient(ctx, slog.Default(), db.boltURL, neo4j.DefaultDatabase, db.cfg.Username, db.cfg.Password)
	require.NoError(t, err, "failed to create Neo4j read-only client")

	// Save old client and swap
	oldClient := config.Neo4jClient
	oldDatabase := config.Neo4jDatabase
	config.Neo4jClient = roClient
	config.Neo4jDatabase = neo4j.DefaultDatabase

	t.Cleanup(func() {
		roClient.Close(context.Background())
		config.Neo4jClient = oldClient
		config.Neo4jDatabase = oldDatabase
	})
}

func isRetryableNeo4jContainerStartErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "wait until ready") ||
		strings.Contains(s, "mapped port") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "/containers/") && strings.Contains(s, "json") ||
		strings.Contains(s, "Get \"http://%2Fvar%2Frun%2Fdocker.sock")
}
