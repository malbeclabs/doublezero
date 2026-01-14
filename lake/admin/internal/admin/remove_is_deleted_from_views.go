package admin

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
)

func RemoveIsDeletedFromViews(log *slog.Logger, addr, database, username, password string, secure, dryRun, skipConfirm bool) error {
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
		newQuery := removeIsDeletedFromViewDefinition(v.createQuery)

		// Check if we actually made a change
		if newQuery == v.createQuery {
			fmt.Printf("  ⚠ Could not find is_deleted in view definition for %s\n", v.name)
			fmt.Printf("    Original query:\n%s\n", v.createQuery)
			continue
		}

		// Replace CREATE VIEW with CREATE OR REPLACE VIEW
		newQuery = strings.Replace(newQuery, "CREATE VIEW", "CREATE OR REPLACE VIEW", 1)

		fmt.Printf("  Recreating %s...\n", v.name)
		if err := conn.Exec(ctx, newQuery); err != nil {
			return fmt.Errorf("failed to recreate view %s: %w\n    Query: %s", v.name, err, newQuery)
		}
		fmt.Printf("  ✓ Recreated %s without is_deleted\n", v.name)
	}

	fmt.Printf("\nSuccessfully recreated %d view(s) without is_deleted column\n", len(views))
	return nil
}

// removeIsDeletedFromViewDefinition removes is_deleted from the view's column definition.
// ClickHouse stores views with explicit column definitions like:
// CREATE VIEW xxx (`col1` Type, `is_deleted` UInt8, `col2` Type) AS SELECT ...
// We need to remove `is_deleted` UInt8 from this definition.
func removeIsDeletedFromViewDefinition(query string) string {
	// Pattern: find the column definition list after CREATE VIEW name
	// Format: CREATE VIEW db.name (`col1` Type, `is_deleted` UInt8, `col2` Type) AS ...

	// Find the column definition section (between first ( and ) AS)
	viewDefPattern := regexp.MustCompile(`(?s)(CREATE VIEW [^\(]+\()(.+?)(\) AS )`)
	match := viewDefPattern.FindStringSubmatchIndex(query)
	if match == nil {
		return query
	}

	// Extract the column definition portion (group 2)
	colDefStart := match[4]
	colDefEnd := match[5]
	colDef := query[colDefStart:colDefEnd]

	// Check if is_deleted is in the column definition
	if !strings.Contains(colDef, "`is_deleted`") {
		return query
	}

	// Remove `is_deleted` UInt8 from the column definition
	// Pattern 1: `, `is_deleted` UInt8` (middle or end of list)
	// Pattern 2: `is_deleted` UInt8, ` (beginning of list - unlikely but handle it)
	newColDef := colDef

	// Try removing ", `is_deleted` UInt8" (comma before)
	pattern1 := regexp.MustCompile(", `is_deleted` UInt8")
	if pattern1.MatchString(newColDef) {
		newColDef = pattern1.ReplaceAllString(newColDef, "")
	} else {
		// Try removing "`is_deleted` UInt8, " (comma after)
		pattern2 := regexp.MustCompile("`is_deleted` UInt8, ")
		newColDef = pattern2.ReplaceAllString(newColDef, "")
	}

	// If nothing changed, return original
	if newColDef == colDef {
		return query
	}

	// Reconstruct the query
	return query[:colDefStart] + newColDef + query[colDefEnd:]
}
