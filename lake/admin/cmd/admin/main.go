package main

import (
	"fmt"
	"os"
	"time"

	flag "github.com/spf13/pflag"

	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/lake/admin/internal/admin"
	"github.com/malbeclabs/doublezero/lake/utils/pkg/logger"
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

	// InfluxDB configuration (for usage backfill)
	influxURLFlag := flag.String("influx-url", "", "InfluxDB URL (or set INFLUX_URL env var)")
	influxTokenFlag := flag.String("influx-token", "", "InfluxDB token (or set INFLUX_TOKEN env var)")
	influxBucketFlag := flag.String("influx-bucket", "", "InfluxDB bucket (or set INFLUX_BUCKET env var)")

	// Commands
	resetDBFlag := flag.Bool("reset-db", false, "Drop all database tables (dim_*, stg_*, fact_*) and views")
	renameOwnerPKFlag := flag.Bool("rename-owner-pk", false, "Rename owner_pk column to owner_pubkey on all tables that have it")
	removeIsDeletedFromViewsFlag := flag.Bool("remove-is-deleted-from-views", false, "Remove is_deleted column from all *_current views")
	dryRunFlag := flag.Bool("dry-run", false, "Dry run mode - show what would be done without actually executing")
	yesFlag := flag.Bool("yes", false, "Skip confirmation prompt (use with caution)")

	// Backfill commands
	backfillDeviceLinkLatencyFlag := flag.Bool("backfill-device-link-latency", false, "Backfill device link latency fact table from on-chain data")
	backfillInternetMetroLatencyFlag := flag.Bool("backfill-internet-metro-latency", false, "Backfill internet metro latency fact table from on-chain data")
	backfillDeviceInterfaceCountersFlag := flag.Bool("backfill-device-interface-counters", false, "Backfill device interface counters fact table from InfluxDB")

	// Backfill options (latency - epoch-based)
	dzEnvFlag := flag.String("env", config.EnvMainnetBeta, "DoubleZero environment (devnet, testnet, mainnet-beta)")
	startEpochFlag := flag.Int64("start-epoch", -1, "Start epoch for latency backfill (-1 = auto-calculate: end-epoch - 9)")
	endEpochFlag := flag.Int64("end-epoch", -1, "End epoch for latency backfill (-1 = current epoch - 1)")
	maxConcurrencyFlag := flag.Int("max-concurrency", 32, "Maximum concurrent RPC requests during backfill")

	// Backfill options (usage - time-based)
	startTimeFlag := flag.String("start-time", "", "Start time for usage backfill (RFC3339 format, e.g. 2024-01-01T00:00:00Z)")
	endTimeFlag := flag.String("end-time", "", "End time for usage backfill (RFC3339 format, empty = now)")
	chunkIntervalFlag := flag.Duration("chunk-interval", 1*time.Hour, "Chunk interval for usage backfill")
	queryDelayFlag := flag.Duration("query-delay", 5*time.Second, "Delay between InfluxDB queries to avoid rate limits")

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

	// Override InfluxDB flags with environment variables if set
	if envInfluxURL := os.Getenv("INFLUX_URL"); envInfluxURL != "" {
		*influxURLFlag = envInfluxURL
	}
	if envInfluxToken := os.Getenv("INFLUX_TOKEN"); envInfluxToken != "" {
		*influxTokenFlag = envInfluxToken
	}
	if envInfluxBucket := os.Getenv("INFLUX_BUCKET"); envInfluxBucket != "" {
		*influxBucketFlag = envInfluxBucket
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

	if *backfillDeviceLinkLatencyFlag {
		if *clickhouseAddrFlag == "" {
			return fmt.Errorf("--clickhouse-addr is required for --backfill-device-link-latency")
		}
		return admin.BackfillDeviceLinkLatency(
			log,
			*clickhouseAddrFlag, *clickhouseDatabaseFlag, *clickhouseUsernameFlag, *clickhousePasswordFlag,
			*dzEnvFlag,
			admin.BackfillDeviceLinkLatencyConfig{
				StartEpoch:     *startEpochFlag,
				EndEpoch:       *endEpochFlag,
				MaxConcurrency: *maxConcurrencyFlag,
				DryRun:         *dryRunFlag,
			},
		)
	}

	if *backfillInternetMetroLatencyFlag {
		if *clickhouseAddrFlag == "" {
			return fmt.Errorf("--clickhouse-addr is required for --backfill-internet-metro-latency")
		}
		return admin.BackfillInternetMetroLatency(
			log,
			*clickhouseAddrFlag, *clickhouseDatabaseFlag, *clickhouseUsernameFlag, *clickhousePasswordFlag,
			*dzEnvFlag,
			admin.BackfillInternetMetroLatencyConfig{
				StartEpoch:     *startEpochFlag,
				EndEpoch:       *endEpochFlag,
				MaxConcurrency: *maxConcurrencyFlag,
				DryRun:         *dryRunFlag,
			},
		)
	}

	if *backfillDeviceInterfaceCountersFlag {
		if *clickhouseAddrFlag == "" {
			return fmt.Errorf("--clickhouse-addr is required for --backfill-device-interface-counters")
		}
		if *influxURLFlag == "" {
			return fmt.Errorf("--influx-url is required for --backfill-device-interface-counters")
		}
		if *influxTokenFlag == "" {
			return fmt.Errorf("--influx-token is required for --backfill-device-interface-counters")
		}
		if *influxBucketFlag == "" {
			return fmt.Errorf("--influx-bucket is required for --backfill-device-interface-counters")
		}

		var startTime, endTime time.Time
		if *startTimeFlag != "" {
			var err error
			startTime, err = time.Parse(time.RFC3339, *startTimeFlag)
			if err != nil {
				return fmt.Errorf("invalid start-time format (use RFC3339, e.g. 2024-01-01T00:00:00Z): %w", err)
			}
		}
		if *endTimeFlag != "" {
			var err error
			endTime, err = time.Parse(time.RFC3339, *endTimeFlag)
			if err != nil {
				return fmt.Errorf("invalid end-time format (use RFC3339, e.g. 2024-01-01T00:00:00Z): %w", err)
			}
		}

		return admin.BackfillDeviceInterfaceCounters(
			log,
			*clickhouseAddrFlag, *clickhouseDatabaseFlag, *clickhouseUsernameFlag, *clickhousePasswordFlag,
			*influxURLFlag, *influxTokenFlag, *influxBucketFlag,
			admin.BackfillDeviceInterfaceCountersConfig{
				StartTime:     startTime,
				EndTime:       endTime,
				ChunkInterval: *chunkIntervalFlag,
				QueryDelay:    *queryDelayFlag,
				DryRun:        *dryRunFlag,
			},
		)
	}

	return nil
}
