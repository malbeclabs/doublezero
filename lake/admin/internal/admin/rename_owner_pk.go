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

func RenameOwnerPK(log *slog.Logger, addr, database, username, password string, secure, dryRun, skipConfirm bool) error {
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

	// Query for all tables that have an owner_pk column
	// We need to check system.columns to find tables with this column
	columnQuery := `
		SELECT DISTINCT table
		FROM system.columns
		WHERE database = ?
		  AND name = 'owner_pk'
		  AND table NOT LIKE '.%'
		ORDER BY table
	`

	columnRows, err := conn.Query(ctx, columnQuery, database)
	if err != nil {
		return fmt.Errorf("failed to query columns: %w", err)
	}
	defer columnRows.Close()

	var tables []string
	for columnRows.Next() {
		var tableName string
		if err := columnRows.Scan(&tableName); err != nil {
			return fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, tableName)
	}

	if len(tables) == 0 {
		fmt.Println("No tables found with owner_pk column")
		return nil
	}

	fmt.Printf("Found %d table(s) with owner_pk column:\n\n", len(tables))
	for _, table := range tables {
		fmt.Printf("  - %s\n", table)
	}

	if dryRun {
		fmt.Println("\n[DRY RUN] Would rename owner_pk to owner_pubkey on the above tables")
		return nil
	}

	// Prompt for confirmation unless --yes flag is set
	if !skipConfirm {
		fmt.Printf("\n⚠️  This will rename owner_pk to owner_pubkey on %d table(s) in database '%s'\n", len(tables), database)
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

	// Rename the column on each table
	fmt.Println("Renaming owner_pk to owner_pubkey...")
	for _, table := range tables {
		// Check if owner_pubkey already exists to avoid errors
		checkQuery := `
			SELECT COUNT(*)
			FROM system.columns
			WHERE database = ?
			  AND table = ?
			  AND name = 'owner_pubkey'
		`
		var count uint64
		checkRows, err := conn.Query(ctx, checkQuery, database, table)
		if err != nil {
			return fmt.Errorf("failed to check for owner_pubkey in %s: %w", table, err)
		}
		if checkRows.Next() {
			if err := checkRows.Scan(&count); err != nil {
				checkRows.Close()
				return fmt.Errorf("failed to scan count: %w", err)
			}
		}
		checkRows.Close()

		if count > 0 {
			fmt.Printf("  ⚠ Skipping %s: owner_pubkey column already exists\n", table)
			continue
		}

		// Use ALTER TABLE to rename the column
		renameQuery := fmt.Sprintf("ALTER TABLE %s RENAME COLUMN owner_pk TO owner_pubkey", table)
		if err := conn.Exec(ctx, renameQuery); err != nil {
			return fmt.Errorf("failed to rename column in %s: %w", table, err)
		}
		fmt.Printf("  ✓ Renamed owner_pk to owner_pubkey in %s\n", table)
	}

	fmt.Printf("\nSuccessfully renamed owner_pk to owner_pubkey on %d table(s)\n", len(tables))
	return nil
}
