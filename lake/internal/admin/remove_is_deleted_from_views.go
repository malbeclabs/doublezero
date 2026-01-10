package admin

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/malbeclabs/doublezero/lake/pkg/clickhouse"
)

func RemoveIsDeletedFromViews(log *slog.Logger, addr, database, username, password string, dryRun, skipConfirm bool) error {
	ctx := context.Background()

	// Connect to ClickHouse
	chDB, err := clickhouse.NewClient(ctx, log, addr, database, username, password)
	if err != nil {
		return fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}
	defer chDB.Close()

	conn, err := chDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	// Find all *_current views that have is_deleted column
	viewQuery := `
		SELECT t.name, t.create_table_query
		FROM system.tables t
		INNER JOIN system.columns c ON t.database = c.database AND t.name = c.table
		WHERE t.database = ?
		  AND t.engine = 'View'
		  AND t.name LIKE '%_current'
		  AND c.name = 'is_deleted'
		ORDER BY t.name
	`

	viewRows, err := conn.Query(ctx, viewQuery, database)
	if err != nil {
		return fmt.Errorf("failed to query views: %w", err)
	}
	defer viewRows.Close()

	type viewInfo struct {
		name        string
		createQuery string
	}
	var views []viewInfo

	for viewRows.Next() {
		var v viewInfo
		if err := viewRows.Scan(&v.name, &v.createQuery); err != nil {
			return fmt.Errorf("failed to scan view info: %w", err)
		}
		views = append(views, v)
	}

	if len(views) == 0 {
		fmt.Println("No *_current views found with is_deleted column")
		return nil
	}

	fmt.Printf("Found %d view(s) with is_deleted column:\n\n", len(views))
	for _, v := range views {
		fmt.Printf("  - %s\n", v.name)
	}

	if dryRun {
		fmt.Println("\n[DRY RUN] Would recreate the above views without is_deleted column")
		return nil
	}

	// Prompt for confirmation unless --yes flag is set
	if !skipConfirm {
		fmt.Printf("\n⚠️  This will drop and recreate %d view(s) in database '%s' without is_deleted\n", len(views), database)
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

	// Recreate each view without is_deleted
	fmt.Println("Recreating views without is_deleted...")
	for _, v := range views {
		// Modify the CREATE query to remove is_deleted from SELECT
		// The pattern is "is_deleted,\n    " or just "is_deleted," in the SELECT list
		newQuery := v.createQuery

		// Remove "is_deleted,\n    " pattern (with newline and indentation)
		newQuery = strings.Replace(newQuery, "is_deleted,\n    ", "", 1)
		// Also try without newline (in case of different formatting)
		newQuery = strings.Replace(newQuery, "is_deleted, ", "", 1)
		newQuery = strings.Replace(newQuery, "is_deleted,", "", 1)

		// Replace CREATE VIEW with CREATE OR REPLACE VIEW
		newQuery = strings.Replace(newQuery, "CREATE VIEW", "CREATE OR REPLACE VIEW", 1)

		if err := conn.Exec(ctx, newQuery); err != nil {
			return fmt.Errorf("failed to recreate view %s: %w", v.name, err)
		}
		fmt.Printf("  ✓ Recreated %s without is_deleted\n", v.name)
	}

	fmt.Printf("\nSuccessfully recreated %d view(s) without is_deleted column\n", len(views))
	return nil
}

