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

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/agent"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/prompts"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/react"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/tools"
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
	maxRoundsFlag := flag.Int("max-rounds", 16, "Maximum number of rounds for the AI agent in normal mode")
	brainModeMaxRoundsFlag := flag.Int("brain-mode-max-rounds", 32, "Maximum number of rounds for the AI agent in brain mode (e.g. when the user asks for a detailed analysis)")
	maxContextTokensFlag := flag.Int("max-context-tokens", 20000, "Maximum number of tokens for the AI agent context before compacting the conversation history")
	shutdownTimeoutFlag := flag.Duration("shutdown-timeout", 60*time.Second, "Maximum time to wait for in-flight operations to complete during graceful shutdown")

	// ClickHouse configuration flags (used as fallback if env vars not set)
	clickhouseAddrFlag := flag.String("clickhouse-addr", "", "ClickHouse server address (e.g., localhost:9000, or set CLICKHOUSE_ADDR env var)")
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

	// Create ClickHouse client using config values
	clickhouseClient, err := clickhouse.NewClient(ctx, log, cfg.ClickhouseAddr, cfg.ClickhouseDatabase, cfg.ClickhouseUsername, cfg.ClickhousePassword)
	if err != nil {
		return fmt.Errorf("failed to create clickhouse client: %w", err)
	}
	defer clickhouseClient.Close()

	// Create ClickHouse query tool with logger for verbose mode
	var toolClient react.ToolClient
	if cfg.Verbose {
		toolClient = tools.NewClickhouseQueryToolWithLogger(clickhouseClient, log)
	} else {
		toolClient = tools.NewClickhouseQueryTool(clickhouseClient)
	}
	log.Info("ClickHouse query tool initialized", "addr", cfg.ClickhouseAddr, "database", cfg.ClickhouseDatabase)

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
		SummaryPrompt:      prompts.Summary,
	})
	if err != nil {
		return fmt.Errorf("failed to create normal react agent: %w", err)
	}
	normalAgent := agent.NewAgent(&agent.AgentConfig{
		ReactAgent: normalReactAgent,
		LLMClient:  normalLLMClient,
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
		SummaryPrompt:      prompts.Summary,
	})
	if err != nil {
		return fmt.Errorf("failed to create brain react agent: %w", err)
	}
	brainAgent := agent.NewAgent(&agent.AgentConfig{
		ReactAgent: brainReactAgent,
		LLMClient:  brainLLMClient,
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
