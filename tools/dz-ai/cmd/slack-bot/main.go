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

	"database/sql"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/malbeclabs/doublezero/lake/pkg/agent"
	"github.com/malbeclabs/doublezero/lake/pkg/agent/prompts"
	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
	"github.com/malbeclabs/doublezero/lake/pkg/agent/tools"
	"github.com/malbeclabs/doublezero/lake/pkg/isis"
	"github.com/malbeclabs/doublezero/lake/pkg/logger"
	"github.com/malbeclabs/doublezero/lake/pkg/querier"
	slackbot "github.com/malbeclabs/doublezero/tools/dz-ai/internal/slack"
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
	lakeQuerierURIFlag := flag.String("lake-querier-uri", "", "Lake querier URI (PostgreSQL connection string to querier service)")
	enablePprofFlag := flag.Bool("enable-pprof", false, "Enable pprof server")
	metricsAddrFlag := flag.String("metrics-addr", defaultMetricsAddr, "Address to listen on for prometheus metrics")
	modeFlag := flag.String("mode", "", "Mode: 'socket' (dev) or 'http' (prod). Defaults to 'socket' if SLACK_APP_TOKEN is set, otherwise 'http'")
	httpAddrFlag := flag.String("http-addr", defaultHTTPAddr, "Address to listen on for HTTP events (production mode)")
	maxRoundsFlag := flag.Int("max-rounds", 16, "Maximum number of rounds for the AI agent in normal mode")
	brainModeMaxRoundsFlag := flag.Int("brain-mode-max-rounds", 32, "Maximum number of rounds for the AI agent in brain mode (e.g. when the user asks for a detailed analysis)")
	maxContextTokensFlag := flag.Int("max-context-tokens", 20000, "Maximum number of tokens for the AI agent context before compacting the conversation history")
	shutdownTimeoutFlag := flag.Duration("shutdown-timeout", 60*time.Second, "Maximum time to wait for in-flight operations to complete during graceful shutdown")
	flag.Parse()

	log := logger.New(*verboseFlag)

	// Handle lake querier URI flag override
	lakeQuerierURI := *lakeQuerierURIFlag
	if lakeQuerierURI == "" {
		lakeQuerierURI = os.Getenv("LAKE_QUERIER_URI")
	}
	if lakeQuerierURI != "" {
		os.Setenv("LAKE_QUERIER_URI", lakeQuerierURI)
	}

	// Load configuration
	cfg, err := slackbot.LoadFromEnv(*modeFlag, *httpAddrFlag, *metricsAddrFlag, *verboseFlag, *enablePprofFlag)
	if err != nil {
		return err
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

	// Set up Anthropic client
	anthropicClient := anthropic.NewClient(option.WithAPIKey(cfg.AnthropicAPIKey))
	// Using Claude Sonnet 4.5 for better reasoning and proactive exploration
	// Haiku is cheaper but less capable for complex analysis
	// Alternative: anthropic.ModelClaudeHaiku4_5_20251001 // Cheaper but less capable

	// Load prompts and build system prompt with Slack-specific guidelines
	prompts, err := prompts.Load()
	if err != nil {
		return fmt.Errorf("failed to load prompts: %w", err)
	}
	systemPrompt := prompts.BuildSlackSystemPrompt()

	// Set up tool client using MultiToolClient
	// Create querier from lake querier URI (PostgreSQL connection string to querier service)
	pgQuerier, err := newPostgresQuerier(ctx, cfg.LakeQuerierURI, log)
	if err != nil {
		return fmt.Errorf("failed to create querier: %w", err)
	}
	defer pgQuerier.Close()

	querierClient := tools.NewQuerierToolClient(pgQuerier)

	// Create ISIS tool client
	isisClient, err := tools.NewISISToolClient(tools.ISISToolClientConfig{})
	if err != nil {
		return fmt.Errorf("failed to create ISIS tool client: %w", err)
	}

	// Build multi-tool client with optional memvid
	var toolClient react.ToolClient
	if cfg.MemvidBrainPath != "" {
		memvidClient := tools.NewMemvidToolClient(tools.MemvidConfig{
			BinaryPath: "memvid",
			BrainPath:  cfg.MemvidBrainPath,
		})
		toolClient, err = tools.NewMultiToolClient(querierClient, isisClient, memvidClient)
		if err != nil {
			return fmt.Errorf("failed to create multi-tool client: %w", err)
		}
		log.Info("tool client initialized", "tools", "query, isis, memvid")
	} else {
		toolClient, err = tools.NewMultiToolClient(querierClient, isisClient)
		if err != nil {
			return fmt.Errorf("failed to create multi-tool client: %w", err)
		}
		log.Warn("memvid disabled: MEMVID_BRAIN_PATH not set")
		log.Info("tool client initialized", "tools", "query, isis")
	}

	// Start ISIS syncer goroutine if memvid is enabled
	if cfg.MemvidBrainPath != "" {
		go runISISSyncer(ctx, cfg.MemvidBrainPath, log)
	}

	// Create normal agent (Haiku model, normal maxRounds)
	normalModel := anthropic.ModelClaudeHaiku4_5
	normalLLMClient := react.NewAnthropicAgent(anthropicClient, normalModel, int64(4000), systemPrompt)
	normalReactAgent, err := react.NewAgent(&react.Config{
		Logger:             log,
		LLM:                normalLLMClient,
		ToolClient:         toolClient,
		MaxRounds:          *maxRoundsFlag,
		MaxContextTokens:   *maxContextTokensFlag,
		FinalizationPrompt: prompts.Finalization,
	})
	if err != nil {
		return fmt.Errorf("failed to create normal react agent: %w", err)
	}
	normalAgent := agent.NewAgent(&agent.AgentConfig{
		ReactAgent: normalReactAgent,
	})

	// Create brain mode agent (Sonnet model, brainModeMaxRounds)
	brainModel := anthropic.ModelClaudeSonnet4_5
	brainLLMClient := react.NewAnthropicAgent(anthropicClient, brainModel, int64(4000), systemPrompt)
	brainReactAgent, err := react.NewAgent(&react.Config{
		Logger:             log,
		LLM:                brainLLMClient,
		ToolClient:         toolClient,
		MaxRounds:          *brainModeMaxRoundsFlag,
		MaxContextTokens:   *maxContextTokensFlag,
		FinalizationPrompt: prompts.Finalization,
	})
	if err != nil {
		return fmt.Errorf("failed to create brain react agent: %w", err)
	}
	brainAgent := agent.NewAgent(&agent.AgentConfig{
		ReactAgent: brainReactAgent,
	})

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
		normalAgent,
		brainAgent,
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

// postgresQuerier implements tools.Querier by connecting to a querier service via PostgreSQL wire protocol.
type postgresQuerier struct {
	db  *sql.DB
	log *slog.Logger
}

// newPostgresQuerier creates a new PostgreSQL querier that connects to the querier service.
func newPostgresQuerier(ctx context.Context, connStr string, log *slog.Logger) (*postgresQuerier, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Test the connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &postgresQuerier{
		db:  db,
		log: log,
	}, nil
}

func (p *postgresQuerier) Close() error {
	return p.db.Close()
}

func (p *postgresQuerier) Query(ctx context.Context, sqlQuery string) (querier.QueryResponse, error) {
	rows, err := p.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return querier.QueryResponse{}, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return querier.QueryResponse{}, fmt.Errorf("failed to get columns: %w", err)
	}

	// Get column types
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return querier.QueryResponse{}, fmt.Errorf("failed to get column types: %w", err)
	}

	// Build column type information
	colTypeInfo := make([]querier.ColumnType, len(columns))
	for i, colType := range columnTypes {
		colTypeInfo[i] = querier.ColumnType{
			Name:             colType.Name(),
			DatabaseTypeName: colType.DatabaseTypeName(),
			ScanType:         "",
		}
		if colType.ScanType() != nil {
			colTypeInfo[i].ScanType = colType.ScanType().String()
		}
	}

	var resultRows []querier.QueryRow
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return querier.QueryResponse{}, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(querier.QueryRow)
		for i, col := range columns {
			val := values[i]
			if val == nil {
				row[col] = nil
			} else {
				switch v := val.(type) {
				case []byte:
					row[col] = string(v)
				default:
					row[col] = val
				}
			}
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return querier.QueryResponse{}, fmt.Errorf("error iterating rows: %w", err)
	}

	return querier.QueryResponse{
		Columns:     columns,
		ColumnTypes: colTypeInfo,
		Rows:        resultRows,
		Count:       len(resultRows),
	}, nil
}

// runISISSyncer periodically syncs ISIS topology from S3 to memvid.
// Runs every ISIS_SYNC_INTERVAL (default: 5h).
func runISISSyncer(ctx context.Context, brainPath string, log *slog.Logger) {
	// Panic isolation - don't crash the bot
	defer func() {
		if r := recover(); r != nil {
			log.Error("isis syncer panic recovered", "error", r, "component", "isis_syncer")
		}
	}()

	syncLog := log.With("component", "isis_syncer")

	syncInterval := 5 * time.Hour
	if s := os.Getenv("ISIS_SYNC_INTERVAL"); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			syncInterval = d
		}
	}

	fetcher := isis.NewS3Fetcher(isis.S3FetcherConfig{})
	enricher, err := isis.NewEnricher(isis.EnricherConfig{Level: 2})
	if err != nil {
		syncLog.Error("failed to create enricher", "error", err)
		return
	}

	memvid := tools.NewMemvidToolClient(tools.MemvidConfig{
		BinaryPath: "memvid",
		BrainPath:  brainPath,
		Timeout:    2 * time.Minute,
	})

	// Initial sync
	syncLog.Info("performing initial ISIS sync")
	if err := syncISISTopology(ctx, fetcher, enricher, memvid, syncLog); err != nil {
		syncLog.Error("initial sync failed", "error", err)
	}

	// Periodic sync
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()

	syncLog.Info("ISIS syncer started", "interval", syncInterval)

	for {
		select {
		case <-ctx.Done():
			syncLog.Info("ISIS syncer stopped")
			return
		case <-ticker.C:
			if err := syncISISTopology(ctx, fetcher, enricher, memvid, syncLog); err != nil {
				syncLog.Error("scheduled sync failed", "error", err)
			}
		}
	}
}

func syncISISTopology(ctx context.Context, fetcher *isis.S3Fetcher, enricher *isis.Enricher, memvid *tools.MemvidToolClient, log *slog.Logger) error {
	fetchResult, err := fetcher.FetchLatest(ctx)
	if err != nil {
		return fmt.Errorf("fetch from S3: %w", err)
	}
	defer fetchResult.Body.Close()

	result, err := enricher.EnrichFromReader(ctx, fetchResult.Body, fetchResult.Timestamp)
	if err != nil {
		return fmt.Errorf("enrich: %w", err)
	}

	now := time.Now().UTC()
	title := fmt.Sprintf("ISIS Network Topology %s", now.Format(time.RFC3339))
	snapshotTag := fmt.Sprintf("snapshot:%s", now.Format("2006-01-02"))

	output, isErr, err := memvid.CallToolText(ctx, "memory_save", map[string]any{
		"content": result.Markdown,
		"title":   title,
		"tags":    []any{"isis", "topology", snapshotTag},
	})
	if err != nil {
		return fmt.Errorf("memvid call: %w", err)
	}
	if isErr {
		return fmt.Errorf("memvid error: %s", output)
	}

	log.Info("ISIS sync completed",
		"routers", result.Stats.TotalRouters,
		"links", result.Stats.TotalLinks,
	)
	return nil
}
