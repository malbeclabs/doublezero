package clickhouse

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/malbeclabs/doublezero/lake"
)

// MigrationOptions configures how migrations are executed
type MigrationOptions struct {
	// SingleNode transforms cluster-specific SQL for single-node ClickHouse.
	// Removes ON CLUSTER clauses and converts Replicated*MergeTree to non-replicated variants.
	SingleNode bool
}

// RunMigrations executes all SQL migration files from the embedded filesystem
// Migrations are executed in filename order (0001_*.sql, 0002_*.sql, etc.)
func RunMigrations(ctx context.Context, log *slog.Logger, conn Connection) error {
	return RunMigrationsWithOptions(ctx, log, conn, MigrationOptions{})
}

// RunMigrationsWithOptions executes migrations with configurable options
func RunMigrationsWithOptions(ctx context.Context, log *slog.Logger, conn Connection, opts MigrationOptions) error {
	log.Info("running ClickHouse migrations")

	// Read all migration files
	entries, err := lake.MigrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Filter and sort SQL files
	var migrationFiles []fs.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			migrationFiles = append(migrationFiles, entry)
		}
	}

	// Sort by filename to ensure correct execution order
	sort.Slice(migrationFiles, func(i, j int) bool {
		return migrationFiles[i].Name() < migrationFiles[j].Name()
	})

	if len(migrationFiles) == 0 {
		log.Warn("no migration files found")
		return nil
	}

	log.Info("found migration files", "count", len(migrationFiles))

	// Execute each migration file
	for _, entry := range migrationFiles {
		migrationPath := fmt.Sprintf("migrations/%s", entry.Name())
		log.Info("executing migration", "file", entry.Name())

		// Read migration file content
		content, err := lake.MigrationsFS.ReadFile(migrationPath)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", entry.Name(), err)
		}

		// Split by semicolon to handle multiple statements
		statements := splitSQLStatements(string(content))
		for i, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}

			// Transform for single-node mode if needed
			if opts.SingleNode {
				stmt = transformForSingleNode(stmt)
			}

			// Execute statement
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("failed to execute migration %s (statement %d): %w", entry.Name(), i+1, err)
			}
		}

		log.Info("completed migration", "file", entry.Name())
	}

	log.Info("all migrations completed successfully", "count", len(migrationFiles))
	return nil
}

// splitSQLStatements splits SQL content by semicolon, handling comments and multi-line statements
func splitSQLStatements(content string) []string {
	var statements []string
	var current strings.Builder
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}

		current.WriteString(line)
		current.WriteString("\n")

		// If line ends with semicolon, it's the end of a statement
		if strings.HasSuffix(trimmed, ";") {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
		}
	}

	// Handle any remaining statement without trailing semicolon
	if current.Len() > 0 {
		stmt := strings.TrimSpace(current.String())
		if stmt != "" {
			statements = append(statements, stmt)
		}
	}

	return statements
}

// Regex patterns for single-node transformations
var (
	// Matches "ON CLUSTER lake" or "ON CLUSTER <name>" with surrounding whitespace
	onClusterRegex = regexp.MustCompile(`(?i)\s*ON\s+CLUSTER\s+\w+\s*`)

	// Matches ReplicatedReplacingMergeTree with ZK path, replica, and version column
	// e.g., ReplicatedReplacingMergeTree('/clickhouse/tables/{shard}/lake/...', '{replica}', ingested_at)
	replicatedReplacingMergeTreeRegex = regexp.MustCompile(
		`(?i)ENGINE\s*=\s*ReplicatedReplacingMergeTree\s*\(\s*'[^']+'\s*,\s*'[^']+'\s*,\s*(\w+)\s*\)`,
	)

	// Matches plain ReplicatedMergeTree (no arguments)
	replicatedMergeTreeRegex = regexp.MustCompile(`(?i)ENGINE\s*=\s*ReplicatedMergeTree\b`)
)

// transformForSingleNode converts cluster-specific SQL to work with single-node ClickHouse.
// - Removes ON CLUSTER clauses
// - Converts ReplicatedMergeTree to MergeTree
// - Converts ReplicatedReplacingMergeTree(...) to ReplacingMergeTree(version)
func transformForSingleNode(stmt string) string {
	// Remove ON CLUSTER clause
	stmt = onClusterRegex.ReplaceAllString(stmt, "\n")

	// Convert ReplicatedReplacingMergeTree to ReplacingMergeTree, preserving the version column
	stmt = replicatedReplacingMergeTreeRegex.ReplaceAllString(stmt, "ENGINE = ReplacingMergeTree($1)")

	// Convert ReplicatedMergeTree to MergeTree
	stmt = replicatedMergeTreeRegex.ReplaceAllString(stmt, "ENGINE = MergeTree")

	return stmt
}
