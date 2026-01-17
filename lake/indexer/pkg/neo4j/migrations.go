package neo4j

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/malbeclabs/doublezero/lake/indexer"
)

// MigrationConfig holds the configuration for running Neo4j migrations
type MigrationConfig struct {
	URI      string
	Database string
	Username string
	Password string
}

// migration represents a single migration file
type migration struct {
	version   uint
	direction string // "up" or "down"
	content   string
}

// RunMigrations executes all Cypher migration files
func RunMigrations(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	log.Info("running Neo4j migrations")

	// Create a client for running migrations
	client, err := NewClient(ctx, log, cfg.URI, cfg.Database, cfg.Username, cfg.Password)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j client: %w", err)
	}
	defer client.Close(ctx)

	// Ensure schema_migrations constraint exists
	if err := ensureMigrationSchema(ctx, client); err != nil {
		return fmt.Errorf("failed to ensure migration schema: %w", err)
	}

	// Get current version
	currentVersion, dirty, err := getCurrentVersion(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	if dirty {
		return fmt.Errorf("database is in dirty state at version %d, manual intervention required", currentVersion)
	}

	// Load migrations from embedded FS
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Filter to only up migrations and sort by version
	var upMigrations []migration
	for _, m := range migrations {
		if m.direction == "up" && m.version > currentVersion {
			upMigrations = append(upMigrations, m)
		}
	}
	sort.Slice(upMigrations, func(i, j int) bool {
		return upMigrations[i].version < upMigrations[j].version
	})

	if len(upMigrations) == 0 {
		log.Info("no new migrations to apply", "current_version", currentVersion)
		return nil
	}

	// Apply migrations in order
	for _, m := range upMigrations {
		log.Info("applying migration", "version", m.version)

		// Set dirty flag before running migration
		if err := setVersion(ctx, client, m.version, true); err != nil {
			return fmt.Errorf("failed to set dirty flag for version %d: %w", m.version, err)
		}

		// Run migration statements
		if err := runMigration(ctx, client, m.content); err != nil {
			return fmt.Errorf("failed to run migration %d: %w", m.version, err)
		}

		// Clear dirty flag after successful migration
		if err := setVersion(ctx, client, m.version, false); err != nil {
			return fmt.Errorf("failed to clear dirty flag for version %d: %w", m.version, err)
		}

		log.Info("migration applied successfully", "version", m.version)
	}

	log.Info("Neo4j migrations completed successfully", "applied_count", len(upMigrations))
	return nil
}

// MigrationStatus returns the status of all migrations
func MigrationStatus(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	log.Info("checking Neo4j migration status")

	// Create a client
	client, err := NewClient(ctx, log, cfg.URI, cfg.Database, cfg.Username, cfg.Password)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j client: %w", err)
	}
	defer client.Close(ctx)

	// Ensure schema exists
	if err := ensureMigrationSchema(ctx, client); err != nil {
		return fmt.Errorf("failed to ensure migration schema: %w", err)
	}

	// Get current version
	version, dirty, err := getCurrentVersion(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	if version == 0 {
		log.Info("no migrations have been applied")
	} else {
		log.Info("current migration status", "version", version, "dirty", dirty)
	}

	// Load migrations to show pending
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	var pending int
	for _, m := range migrations {
		if m.direction == "up" && m.version > version {
			pending++
		}
	}

	if pending > 0 {
		log.Info("pending migrations", "count", pending)
	}

	return nil
}

// ensureMigrationSchema creates the SchemaMigration node label constraint if it doesn't exist
func ensureMigrationSchema(ctx context.Context, client Client) error {
	session, err := client.Session(ctx)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	// Create constraint for migration version tracking using Neo4j 5.x syntax
	query := "CREATE CONSTRAINT schema_migration_version IF NOT EXISTS FOR (n:SchemaMigration) REQUIRE n.version IS UNIQUE"
	res, err := session.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to create migration constraint: %w", err)
	}
	if _, err := res.Consume(ctx); err != nil {
		return fmt.Errorf("failed to consume constraint result: %w", err)
	}

	return nil
}

// getCurrentVersion gets the current migration version from the database
func getCurrentVersion(ctx context.Context, client Client) (uint, bool, error) {
	session, err := client.Session(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	query := "MATCH (n:SchemaMigration) RETURN n.version AS version, n.dirty AS dirty ORDER BY n.version DESC LIMIT 1"
	res, err := session.Run(ctx, query, nil)
	if err != nil {
		return 0, false, fmt.Errorf("failed to query version: %w", err)
	}

	if res.Next(ctx) {
		record := res.Record()
		versionVal, _ := record.Get("version")
		dirtyVal, _ := record.Get("dirty")

		version, ok := versionVal.(int64)
		if !ok {
			return 0, false, fmt.Errorf("unexpected version type: %T", versionVal)
		}

		dirty, _ := dirtyVal.(bool)
		return uint(version), dirty, nil
	}

	if err := res.Err(); err != nil {
		return 0, false, fmt.Errorf("failed to read version result: %w", err)
	}

	// No migrations have been applied
	return 0, false, nil
}

// setVersion creates or updates the migration version record
func setVersion(ctx context.Context, client Client, version uint, dirty bool) error {
	session, err := client.Session(ctx)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	// Delete any existing version records and create the new one
	query := `
		MATCH (n:SchemaMigration) DELETE n
		WITH 1 AS dummy
		CREATE (n:SchemaMigration {version: $version, dirty: $dirty})
	`
	res, err := session.Run(ctx, query, map[string]any{
		"version": int64(version),
		"dirty":   dirty,
	})
	if err != nil {
		return fmt.Errorf("failed to set version: %w", err)
	}
	if _, err := res.Consume(ctx); err != nil {
		return fmt.Errorf("failed to consume set version result: %w", err)
	}

	return nil
}

// runMigration executes the migration content
func runMigration(ctx context.Context, client Client, content string) error {
	session, err := client.Session(ctx)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	// Split content by semicolons and run each statement
	statements := splitStatements(content)
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		res, err := session.Run(ctx, stmt, nil)
		if err != nil {
			return fmt.Errorf("failed to run statement: %w", err)
		}
		if _, err := res.Consume(ctx); err != nil {
			return fmt.Errorf("failed to consume statement result: %w", err)
		}
	}

	return nil
}

// splitStatements splits a migration file content by semicolons
func splitStatements(content string) []string {
	var statements []string
	for _, s := range strings.Split(content, ";") {
		s = strings.TrimSpace(s)
		if s != "" {
			statements = append(statements, s)
		}
	}
	return statements
}

// loadMigrations loads all migration files from the embedded filesystem
func loadMigrations() ([]migration, error) {
	var migrations []migration

	// Migration filename pattern: 000001_name.up.cypher or 000001_name.down.cypher
	pattern := regexp.MustCompile(`^(\d+)_.*\.(up|down)\.cypher$`)

	err := fs.WalkDir(indexer.Neo4jMigrationsFS, "db/neo4j/migrations", func(filepath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		filename := path.Base(filepath)
		matches := pattern.FindStringSubmatch(filename)
		if matches == nil {
			return nil // Skip non-migration files
		}

		version, err := strconv.ParseUint(matches[1], 10, 32)
		if err != nil {
			return fmt.Errorf("invalid version in filename %s: %w", filename, err)
		}

		content, err := fs.ReadFile(indexer.Neo4jMigrationsFS, filepath)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", filename, err)
		}

		migrations = append(migrations, migration{
			version:   uint(version),
			direction: matches[2],
			content:   string(content),
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk migrations directory: %w", err)
	}

	return migrations, nil
}
