package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/malbeclabs/doublezero/controlplane/s3-uploader/internal/config"
	"github.com/malbeclabs/doublezero/controlplane/s3-uploader/internal/uploader"
)

var (
	configPath  = flag.String("config", "/mnt/flash/s3_uploader_config.toml", "Path to configuration file")
	customKey   = flag.String("key", "", "Custom S3 key (skips automatic timestamping)")
	bucket      = flag.String("bucket", "", "AWS S3 bucket name (overrides config file)")
	region      = flag.String("region", "", "AWS region (overrides config file)")
	verbose     = flag.Bool("verbose", false, "Enable verbose logging")
	showVersion = flag.Bool("version", false, "Print version information and exit")

	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		os.Exit(0)
	}

	// Check for file argument
	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "[ERROR] Missing required argument: FILE\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options] FILE\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	filePath := flag.Arg(0)

	// Initialize logger
	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

	log.Info("[OK] S3 Uploader starting...")

	// Validate file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Error("[ERROR] File not found", "path", filePath)
		os.Exit(1)
	}

	// Load configuration
	log.Info("[OK] Loading configuration")

	// Check if config file exists, if not specified or doesn't exist, rely on env vars
	cfgPath := *configPath
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) && cfgPath == "/mnt/flash/s3_uploader_config.toml" {
		// Default config path doesn't exist, that's OK - we'll use env vars
		cfgPath = ""
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Error("[ERROR] Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Apply CLI overrides
	cfg.ApplyOverrides(bucket, region)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Error("[ERROR] Invalid configuration", "error", err)
		os.Exit(1)
	}

	log.Info("[OK] Configuration validated")
	log.Info("[OK] Bucket", "bucket", cfg.AWS.Bucket)
	log.Info("[OK] Region", "region", cfg.AWS.Region)

	// Create context with cancellation
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Create uploader
	up, err := uploader.New(ctx, cfg, log)
	if err != nil {
		log.Error("[ERROR] Failed to initialize S3 uploader", "error", err)
		os.Exit(1)
	}

	// Upload file
	var keyPtr *string
	if *customKey != "" {
		keyPtr = customKey
	}

	s3URL, err := up.Upload(ctx, filePath, keyPtr)
	if err != nil {
		log.Error("[ERROR] Failed to upload file", "error", err)
		os.Exit(1)
	}

	log.Info("[OK] Upload successful!")
	log.Info("[OK] S3 URL", "url", s3URL)
}
