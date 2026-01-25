package neo4jtesting

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	tcneo4j "github.com/testcontainers/testcontainers-go/modules/neo4j"
)

type DBConfig struct {
	Username       string
	Password       string
	ContainerImage string
}

type DB struct {
	log       *slog.Logger
	cfg       *DBConfig
	boltURL   string
	container *tcneo4j.Neo4jContainer
}

// BoltURL returns the Bolt protocol URL for the Neo4j container.
func (db *DB) BoltURL() string {
	return db.boltURL
}

// Username returns the Neo4j username.
func (db *DB) Username() string {
	return db.cfg.Username
}

// Password returns the Neo4j password.
func (db *DB) Password() string {
	return db.cfg.Password
}

func (db *DB) Close() {
	terminateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.container.Terminate(terminateCtx); err != nil {
		db.log.Error("failed to terminate Neo4j container", "error", err)
	}
}

func (cfg *DBConfig) Validate() error {
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

// NewTestClient creates a test Neo4j client connected to the shared container.
func NewTestClient(t *testing.T, db *DB) (neo4j.Client, error) {
	return newTestClient(t, db, false)
}

// NewReadOnlyTestClient creates a read-only test Neo4j client connected to the shared container.
func NewReadOnlyTestClient(t *testing.T, db *DB) (neo4j.Client, error) {
	return newTestClient(t, db, true)
}

func newTestClient(t *testing.T, db *DB, readOnly bool) (neo4j.Client, error) {
	var client neo4j.Client
	var err error
	if readOnly {
		client, err = neo4j.NewReadOnlyClient(t.Context(), slog.Default(), db.boltURL, neo4j.DefaultDatabase, db.cfg.Username, db.cfg.Password)
	} else {
		client, err = neo4j.NewClient(t.Context(), slog.Default(), db.boltURL, neo4j.DefaultDatabase, db.cfg.Username, db.cfg.Password)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j client: %w", err)
	}

	// For read-only clients, skip database clearing and migrations
	if readOnly {
		t.Cleanup(func() {
			client.Close(t.Context())
		})
		return client, nil
	}

	// Clear the database for this test
	session, err := client.Session(t.Context())
	if err != nil {
		client.Close(t.Context())
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Delete all nodes and relationships
	res, err := session.Run(t.Context(), "MATCH (n) DETACH DELETE n", nil)
	if err != nil {
		session.Close(t.Context())
		client.Close(t.Context())
		return nil, fmt.Errorf("failed to clear database: %w", err)
	}
	if _, err := res.Consume(t.Context()); err != nil {
		session.Close(t.Context())
		client.Close(t.Context())
		return nil, fmt.Errorf("failed to consume clear result: %w", err)
	}
	session.Close(t.Context())

	// Run migrations to initialize schema
	if err := neo4j.RunMigrations(t.Context(), slog.Default(), neo4j.MigrationConfig{
		URI:      db.boltURL,
		Database: neo4j.DefaultDatabase,
		Username: db.cfg.Username,
		Password: db.cfg.Password,
	}); err != nil {
		client.Close(t.Context())
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	t.Cleanup(func() {
		client.Close(t.Context())
	})

	return client, nil
}

// NewDB creates a new Neo4j testcontainer.
func NewDB(ctx context.Context, log *slog.Logger, cfg *DBConfig) (*DB, error) {
	if cfg == nil {
		cfg = &DBConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate DB config: %w", err)
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
			if isRetryableContainerStartErr(err) && attempt < 3 {
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
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get Neo4j bolt URL: %w", err)
	}

	db := &DB{
		log:       log,
		cfg:       cfg,
		boltURL:   boltURL,
		container: container,
	}

	return db, nil
}

func isRetryableContainerStartErr(err error) bool {
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
