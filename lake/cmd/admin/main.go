package main

import (
	"fmt"
	"os"

	flag "github.com/spf13/pflag"

	"github.com/malbeclabs/doublezero/lake/internal/admin"
	"github.com/malbeclabs/doublezero/lake/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	verboseFlag := flag.Bool("verbose", false, "enable verbose (debug) logging")

	// ClickHouse configuration
	clickhouseAddrFlag := flag.String("clickhouse-addr", "", "ClickHouse address (host:port) (or set CLICKHOUSE_ADDR env var)")
	clickhouseDatabaseFlag := flag.String("clickhouse-database", "default", "ClickHouse database name (or set CLICKHOUSE_DATABASE env var)")
	clickhouseUsernameFlag := flag.String("clickhouse-username", "default", "ClickHouse username (or set CLICKHOUSE_USERNAME env var)")
	clickhousePasswordFlag := flag.String("clickhouse-password", "", "ClickHouse password (or set CLICKHOUSE_PASSWORD env var)")

	// Commands
	resetDBFlag := flag.Bool("reset-db", false, "Drop all database tables (dim_*, stg_*, fact_*) and views")
	renameOwnerPKFlag := flag.Bool("rename-owner-pk", false, "Rename owner_pk column to owner_pubkey on all tables that have it")
	removeIsDeletedFromViewsFlag := flag.Bool("remove-is-deleted-from-views", false, "Remove is_deleted column from all *_current views")
	dryRunFlag := flag.Bool("dry-run", false, "Dry run mode - show what would be done without actually executing")
	yesFlag := flag.Bool("yes", false, "Skip confirmation prompt (use with caution)")

	flag.Parse()

	log := logger.New(*verboseFlag)

	// Override ClickHouse flags with environment variables if set
	if envClickhouseAddr := os.Getenv("CLICKHOUSE_ADDR"); envClickhouseAddr != "" {
		*clickhouseAddrFlag = envClickhouseAddr
	}
	if envClickhouseDatabase := os.Getenv("CLICKHOUSE_DATABASE"); envClickhouseDatabase != "" {
		*clickhouseDatabaseFlag = envClickhouseDatabase
	}
	if envClickhouseUsername := os.Getenv("CLICKHOUSE_USERNAME"); envClickhouseUsername != "" {
		*clickhouseUsernameFlag = envClickhouseUsername
	}
	if envClickhousePassword := os.Getenv("CLICKHOUSE_PASSWORD"); envClickhousePassword != "" {
		*clickhousePasswordFlag = envClickhousePassword
	}

	// Execute commands
	if *resetDBFlag {
		if *clickhouseAddrFlag == "" {
			return fmt.Errorf("--clickhouse-addr is required for --reset-db")
		}
		return admin.ResetDB(log, *clickhouseAddrFlag, *clickhouseDatabaseFlag, *clickhouseUsernameFlag, *clickhousePasswordFlag, *dryRunFlag, *yesFlag)
	}

	if *renameOwnerPKFlag {
		if *clickhouseAddrFlag == "" {
			return fmt.Errorf("--clickhouse-addr is required for --rename-owner-pk")
		}
		return admin.RenameOwnerPK(log, *clickhouseAddrFlag, *clickhouseDatabaseFlag, *clickhouseUsernameFlag, *clickhousePasswordFlag, *dryRunFlag, *yesFlag)
	}

	if *removeIsDeletedFromViewsFlag {
		if *clickhouseAddrFlag == "" {
			return fmt.Errorf("--clickhouse-addr is required for --remove-is-deleted-from-views")
		}
		return admin.RemoveIsDeletedFromViews(log, *clickhouseAddrFlag, *clickhouseDatabaseFlag, *clickhouseUsernameFlag, *clickhousePasswordFlag, *dryRunFlag, *yesFlag)
	}

	return nil
}
