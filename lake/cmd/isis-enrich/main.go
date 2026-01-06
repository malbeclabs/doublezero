// isis-enrich transforms ISIS LSP JSON data into structured markdown for LLM consumption.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	flag "github.com/spf13/pflag"

	"github.com/malbeclabs/doublezero/lake/pkg/isis"
	"github.com/malbeclabs/doublezero/lake/pkg/logger"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// CLI flags
	inputFlag := flag.StringP("input", "i", "", "Input JSON file or directory (auto-detects latest if directory)")
	outputFlag := flag.StringP("output", "o", "", "Output markdown file (default: stdout)")
	levelFlag := flag.IntP("level", "l", 2, "ISIS level to process (1 or 2)")
	verboseFlag := flag.BoolP("verbose", "v", false, "Print processing statistics to stderr")
	versionFlag := flag.Bool("version", false, "Print version information")

	// S3 fetch flags
	latestFlag := flag.Bool("latest", false, "Fetch latest ISIS JSON from S3 (requires network)")
	bucketFlag := flag.String("bucket", isis.DefaultBucket, "S3 bucket name (used with --latest)")
	regionFlag := flag.String("region", isis.DefaultRegion, "AWS region (used with --latest)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "isis-enrich - Transform ISIS JSON into LLM-optimized structured markdown\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  isis-enrich [--input FILE|DIR] [--output FILE] [--level 1|2] [--verbose]\n")
		fmt.Fprintf(os.Stderr, "  isis-enrich --latest [--bucket BUCKET] [--region REGION] [--output FILE]\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  # Fetch latest from S3\n")
		fmt.Fprintf(os.Stderr, "  isis-enrich --latest --output isis.md\n\n")
		fmt.Fprintf(os.Stderr, "  # Fetch from custom bucket\n")
		fmt.Fprintf(os.Stderr, "  isis-enrich --latest --bucket my-bucket --region eu-west-1\n\n")
		fmt.Fprintf(os.Stderr, "  # Process specific file\n")
		fmt.Fprintf(os.Stderr, "  isis-enrich --input data/2026-01-06T15-42-13Z_upload_data.json -o isis.md\n\n")
		fmt.Fprintf(os.Stderr, "  # Auto-detect latest file in directory\n")
		fmt.Fprintf(os.Stderr, "  isis-enrich --input data/ -o isis.md\n\n")
		fmt.Fprintf(os.Stderr, "  # Output to stdout\n")
		fmt.Fprintf(os.Stderr, "  isis-enrich --input data/\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *versionFlag {
		fmt.Printf("isis-enrich %s (commit: %s, built: %s)\n", version, commit, date)
		return nil
	}

	log := logger.New(*verboseFlag)

	// Validate level
	if *levelFlag != 1 && *levelFlag != 2 {
		return fmt.Errorf("invalid ISIS level: %d (must be 1 or 2)", *levelFlag)
	}

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Create enricher
	enricher, err := isis.NewEnricher(isis.EnricherConfig{
		Level: *levelFlag,
	})
	if err != nil {
		return fmt.Errorf("failed to create enricher: %w", err)
	}

	var result *isis.Result

	if *latestFlag {
		// Fetch from S3
		result, err = fetchFromS3(ctx, enricher, log, *bucketFlag, *regionFlag)
		if err != nil {
			return err
		}
	} else {
		// Process local file
		result, err = processLocalFile(ctx, enricher, log, *inputFlag)
		if err != nil {
			return err
		}
	}

	log.Info("processed",
		"routers", result.Stats.TotalRouters,
		"links", result.Stats.TotalLinks,
		"sr_enabled", result.Stats.SREnabledRouters,
	)

	// Output
	if *outputFlag != "" {
		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(*outputFlag), 0o755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}

		if err := os.WriteFile(*outputFlag, []byte(result.Markdown), 0o644); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
		log.Info("written to", "path", *outputFlag)
	} else {
		fmt.Print(result.Markdown)
	}

	return nil
}

// fetchFromS3 fetches the latest ISIS JSON from S3 and enriches it.
func fetchFromS3(ctx context.Context, enricher *isis.Enricher, log *slog.Logger, bucket, region string) (*isis.Result, error) {
	fetcher := isis.NewS3Fetcher(isis.S3FetcherConfig{
		Bucket: bucket,
		Region: region,
	})

	log.Info("fetching latest from S3", "bucket", bucket, "region", region)

	fetchResult, err := fetcher.FetchLatest(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from S3: %w", err)
	}
	defer fetchResult.Body.Close()

	log.Info("fetched", "key", fetchResult.Key, "timestamp", fetchResult.Timestamp)

	result, err := enricher.EnrichFromReader(ctx, fetchResult.Body, fetchResult.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to enrich: %w", err)
	}

	return result, nil
}

// processLocalFile processes a local JSON file or directory.
func processLocalFile(ctx context.Context, enricher *isis.Enricher, log *slog.Logger, inputFlag string) (*isis.Result, error) {
	inputPath := inputFlag
	if inputPath == "" {
		// Check positional argument
		if flag.NArg() > 0 {
			inputPath = flag.Arg(0)
		} else {
			return nil, fmt.Errorf("input file or directory required (use --input or --latest)")
		}
	}

	// Check if input is a directory
	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, fmt.Errorf("input not found: %s", inputPath)
	}

	if info.IsDir() {
		latestFile, err := isis.FindLatestJSON(inputPath)
		if err != nil {
			return nil, fmt.Errorf("failed to find JSON files: %w", err)
		}
		if latestFile == "" {
			return nil, fmt.Errorf("no JSON files found in directory: %s", inputPath)
		}
		log.Info("auto-detected latest file", "path", latestFile)
		inputPath = latestFile
	}

	log.Info("processing file", "path", inputPath)

	result, err := enricher.EnrichFromFile(ctx, inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to enrich: %w", err)
	}

	return result, nil
}
