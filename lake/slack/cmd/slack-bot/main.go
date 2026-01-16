package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof" // Register pprof handlers
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
	v1 "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/v1"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	slackbot "github.com/malbeclabs/doublezero/lake/slack/internal/slack"
	"github.com/malbeclabs/doublezero/lake/utils/pkg/logger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	defaultMetricsAddr = "0.0.0.0:0"
	defaultHTTPAddr    = "0.0.0.0:3000"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// run starts the Slack bot.
//
// Required Slack Bot Token Scopes:
//   - chat:write - Post messages
//   - reactions:write - Add reactions
//   - channels:history - Read public channel messages (for channel mentions)
//   - groups:history - Read private channel messages (for private channel mentions)
//   - mpim:history - Read group DM messages (for group DM mentions and thread replies)
//   - channels:read - View public channel info (optional but recommended)
//   - groups:read - View private channel info (optional but recommended)
//   - im:history - Read DM history
//
// Required Event Subscriptions (Subscribe to bot events):
//   - app_mentions - Receive events when bot is mentioned in channels
//   - message.channels - Receive all messages in public channels (needed for thread replies)
//   - message.groups - Receive all messages in private channels (needed for thread replies)
//   - message.mpim - Receive all messages in group DMs (needed for thread replies)
//
// For DMs, the bot responds to all messages.
// For channels, the bot only responds when mentioned (@bot) or when replying in a thread where the root message mentioned the bot.
func run() error {
	verboseFlag := flag.Bool("verbose", false, "Enable verbose (debug) logging")
	enablePprofFlag := flag.Bool("enable-pprof", false, "Enable pprof server")
	metricsAddrFlag := flag.String("metrics-addr", defaultMetricsAddr, "Address to listen on for prometheus metrics")
	modeFlag := flag.String("mode", "", "Mode: 'socket' (dev) or 'http' (prod). Defaults to 'socket' if SLACK_APP_TOKEN is set, otherwise 'http'")
	httpAddrFlag := flag.String("http-addr", defaultHTTPAddr, "Address to listen on for HTTP events (production mode)")
	shutdownTimeoutFlag := flag.Duration("shutdown-timeout", 60*time.Second, "Maximum time to wait for in-flight operations to complete during graceful shutdown")

	// ClickHouse configuration flags (used as fallback if env vars not set)
	clickhouseAddrFlag := flag.String("clickhouse-addr", "", "ClickHouse server address (e.g., localhost:9000, or set CLICKHOUSE_ADDR_TCP env var)")
	clickhouseDatabaseFlag := flag.String("clickhouse-database", "default", "ClickHouse database name (or set CLICKHOUSE_DATABASE env var)")
	clickhouseUsernameFlag := flag.String("clickhouse-username", "default", "ClickHouse username (or set CLICKHOUSE_USERNAME env var)")
	clickhousePasswordFlag := flag.String("clickhouse-password", "", "ClickHouse password (or set CLICKHOUSE_PASSWORD env var)")

	flag.Parse()

	log := logger.New(*verboseFlag)

	// Load configuration
	cfg, err := slackbot.LoadFromEnv(*modeFlag, *httpAddrFlag, *metricsAddrFlag, *verboseFlag, *enablePprofFlag)
	if err != nil {
		return err
	}

	// Override config with flags if flags are provided (flags take precedence)
	if *clickhouseAddrFlag != "" {
		cfg.ClickhouseAddr = *clickhouseAddrFlag
	}
	if *clickhouseDatabaseFlag != "" && *clickhouseDatabaseFlag != "default" {
		cfg.ClickhouseDatabase = *clickhouseDatabaseFlag
	}
	if *clickhouseUsernameFlag != "" && *clickhouseUsernameFlag != "default" {
		cfg.ClickhouseUsername = *clickhouseUsernameFlag
	}
	if *clickhousePasswordFlag != "" {
		cfg.ClickhousePassword = *clickhousePasswordFlag
	}

	// Start pprof server if enabled
	if cfg.EnablePprof {
		go func() {
			log.Info("starting pprof server", "address", "localhost:6060")
			if err := http.ListenAndServe("localhost:6060", nil); err != nil {
				log.Error("failed to start pprof server", "error", err)
			}
		}()
	}

	// Start metrics server
	if cfg.MetricsAddr != "" {
		slackbot.BuildInfo.WithLabelValues(version, commit, date).Set(1)
		go func() {
			listener, err := net.Listen("tcp", cfg.MetricsAddr)
			if err != nil {
				log.Error("failed to start prometheus metrics server listener", "error", err)
				return
			}
			log.Info("prometheus metrics server listening", "address", listener.Addr().String())
			http.Handle("/metrics", promhttp.Handler())
			if err := http.Serve(listener, nil); err != nil {
				log.Error("failed to start prometheus metrics server", "error", err)
			}
		}()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Create ClickHouse client using config values
	clickhouseClient, err := clickhouse.NewClient(ctx, log, cfg.ClickhouseAddr, cfg.ClickhouseDatabase, cfg.ClickhouseUsername, cfg.ClickhousePassword, cfg.ClickhouseSecure)
	if err != nil {
		return fmt.Errorf("failed to create clickhouse client: %w", err)
	}
	defer clickhouseClient.Close()
	log.Info("ClickHouse client initialized", "addr", cfg.ClickhouseAddr, "database", cfg.ClickhouseDatabase)

	// Load pipeline prompts
	prompts, err := v1.LoadPrompts()
	if err != nil {
		return fmt.Errorf("failed to load pipeline prompts: %w", err)
	}

	// Create LLM client for the pipeline
	// Using Claude Sonnet 4.5 for good balance of speed and capability
	llmClient := pipeline.NewAnthropicLLMClient("claude-sonnet-4-20250514", 4096)

	// Create querier for the pipeline
	querier := slackbot.NewClickhouseQuerier(clickhouseClient)

	// Create schema fetcher for the pipeline (uses TCP, same as querier)
	schemaFetcher := slackbot.NewClickhouseSchemaFetcher(clickhouseClient, cfg.ClickhouseDatabase)

	// Create the analysis pipeline with Slack formatting context
	p, err := v1.New(&pipeline.Config{
		Logger:        log,
		LLM:           llmClient,
		Querier:       querier,
		SchemaFetcher: schemaFetcher,
		Prompts:       prompts,
		MaxTokens:     4096,
		MaxRetries:    4,
		FormatContext: prompts.Slack, // Apply Slack-specific formatting guidelines
	})
	if err != nil {
		return fmt.Errorf("failed to create pipeline: %w", err)
	}

	// Initialize Slack client
	slackClient := slackbot.NewClient(cfg.BotToken, cfg.AppToken, log)
	botUserID, err := slackClient.Initialize(ctx)
	if err != nil {
		log.Warn("slack auth test failed, continuing anyway", "error", err)
	}
	cfg.BotUserID = botUserID

	// Set up conversation manager
	convManager := slackbot.NewManager(log)
	convManager.StartCleanup(ctx)

	// Set up message processor
	msgProcessor := slackbot.NewProcessor(
		slackClient,
		p,
		convManager,
		log,
	)
	msgProcessor.StartCleanup(ctx)

	// Set up event handler
	eventHandler := slackbot.NewEventHandler(
		slackClient,
		msgProcessor,
		convManager,
		log,
		cfg.BotUserID,
		ctx,
	)
	eventHandler.StartCleanup(ctx)

	// Start bot based on mode
	if cfg.Mode == slackbot.ModeSocket {
		err = runSocketMode(ctx, slackClient.API(), eventHandler, log)
	} else {
		err = runHTTPMode(ctx, cfg.HTTPAddr, cfg.SigningSecret, eventHandler, log)
	}

	// If shutdown was initiated, wait for in-flight operations
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		log.Info("shutdown signal received, stopping new events and waiting for in-flight operations", "timeout", *shutdownTimeoutFlag)
		shutdownComplete := eventHandler.StopAcceptingNew()

		// Wait for in-flight operations with timeout
		waitDone := make(chan struct{})
		go func() {
			shutdownComplete()
			close(waitDone)
		}()

		select {
		case <-waitDone:
			log.Info("all in-flight operations completed")
		case <-time.After(*shutdownTimeoutFlag):
			log.Warn("timeout waiting for in-flight operations, proceeding with shutdown", "timeout", *shutdownTimeoutFlag)
		}
		log.Info("slack bot shutting down", "reason", err)
		return nil
	}
	return err
}

// runSocketMode runs the bot in Socket Mode (development)
func runSocketMode(
	ctx context.Context,
	api *slack.Client,
	eventHandler *slackbot.EventHandler,
	log *slog.Logger,
) error {
	client := socketmode.New(api)

	// Start the socketmode client in a goroutine
	go func() {
		if err := client.Run(); err != nil {
			log.Error("socketmode client error", "error", err)
		}
	}()

	// Handle events - this will return when ctx is cancelled
	return eventHandler.HandleSocketMode(ctx, client)
}

// runHTTPMode runs the bot in HTTP Mode (production)
func runHTTPMode(
	ctx context.Context,
	httpAddr string,
	signingSecret string,
	eventHandler *slackbot.EventHandler,
	log *slog.Logger,
) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/slack/events", func(w http.ResponseWriter, r *http.Request) {
		eventHandler.HandleHTTP(w, r, signingSecret)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ok")); err != nil {
			log.Error("failed to write readyz response", "error", err)
		}
	})

	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: mux,
	}

	go func() {
		log.Info("HTTP server listening for Slack events", "addr", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP server error", "error", err)
		}
	}()

	log.Info("bot running in HTTP mode (DMs and channel mentions, thread replies enabled)")
	<-ctx.Done()
	log.Info("shutdown signal received, stopping HTTP server from accepting new connections")

	// Stop accepting new events first
	eventHandler.StopAcceptingNew()

	// Shutdown HTTP server (stops accepting new connections)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("error shutting down HTTP server", "error", err)
	} else {
		log.Info("HTTP server stopped accepting new connections")
	}

	return ctx.Err()
}
