package admin

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
)

func ResetDB(log *slog.Logger, addr, database, username, password string, secure, dryRun, skipConfirm bool) error {
	ctx := context.Background()

	// Connect to ClickHouse
	chDB, err := clickhouse.NewClient(ctx, log, addr, database, username, password, secure)
	if err != nil {
		return fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}
	defer chDB.Close()

	conn, err := chDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	// Query for all tables matching our patterns
	tableQuery := `
		SELECT name
		FROM system.tables
		WHERE database = ?
		  AND engine != 'View'
		  AND (name LIKE 'dim_%' OR name LIKE 'stg_%' OR name LIKE 'fact_%')
		ORDER BY name
	`

	tableRows, err := conn.Query(ctx, tableQuery, database)
	if err != nil {
		return fmt.Errorf("failed to query tables: %w", err)
	}
	defer tableRows.Close()

	var tables []string
	for tableRows.Next() {
		var tableName string
		if err := tableRows.Scan(&tableName); err != nil {
			return fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, tableName)
	}

	// Query for all views
	// Check for both View and MaterializedView engine types
	viewQuery := `
		SELECT name
		FROM system.tables
		WHERE database = ?
		  AND engine IN ('View', 'MaterializedView')
		ORDER BY name
	`

	viewRows, err := conn.Query(ctx, viewQuery, database)
	if err != nil {
		return fmt.Errorf("failed to query views: %w", err)
	}
	defer viewRows.Close()

	var views []string
	for viewRows.Next() {
		var viewName string
		if err := viewRows.Scan(&viewName); err != nil {
			return fmt.Errorf("failed to scan view name: %w", err)
		}
		views = append(views, viewName)
	}

	if len(tables) == 0 && len(views) == 0 {
		fmt.Println("No tables or views found matching patterns")
		return nil
	}

	fmt.Printf("⚠️  WARNING: This will DROP %d table(s) and %d view(s) from database '%s':\n\n", len(tables), len(views), database)
	if len(tables) > 0 {
		fmt.Println("Tables:")
		for _, table := range tables {
			fmt.Printf("  - %s\n", table)
		}
	}
	if len(views) > 0 {
		fmt.Println("\nViews:")
		for _, view := range views {
			fmt.Printf("  - %s\n", view)
		}
	}

	if dryRun {
		fmt.Println("\n[DRY RUN] Would drop the above tables and views")
		return nil
	}

	// Prompt for confirmation unless --yes flag is set
	if !skipConfirm {
		fmt.Printf("\n⚠️  This is a DESTRUCTIVE operation that cannot be undone!\n")
		fmt.Printf("Type 'yes' to confirm: ")

		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read confirmation: %w", err)
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response != "yes" {
			fmt.Printf("\nConfirmation failed. Operation cancelled.\n")
			return nil
		}
		fmt.Println()
	}

	// Drop views first (they may depend on tables)
	if len(views) > 0 {
		fmt.Println("Dropping views...")
		for _, view := range views {
			dropQuery := fmt.Sprintf("DROP VIEW IF EXISTS %s", view)
			if err := conn.Exec(ctx, dropQuery); err != nil {
				return fmt.Errorf("failed to drop view %s: %w", view, err)
			}
			fmt.Printf("  ✓ Dropped view %s\n", view)
		}
	}

	// Drop tables
	if len(tables) > 0 {
		fmt.Println("Dropping tables...")
		for _, table := range tables {
			dropQuery := fmt.Sprintf("DROP TABLE IF EXISTS %s", table)
			if err := conn.Exec(ctx, dropQuery); err != nil {
				return fmt.Errorf("failed to drop table %s: %w", table, err)
			}
			fmt.Printf("  ✓ Dropped %s\n", table)
		}
	}

	fmt.Printf("\nSuccessfully dropped %d table(s) and %d view(s)\n", len(tables), len(views))
	return nil
}
