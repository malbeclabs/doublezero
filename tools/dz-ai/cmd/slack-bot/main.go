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
	"github.com/malbeclabs/doublezero/lake/pkg/logger"
	mcpclient "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/client"
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
	defaultMetricsAddr = "0.0.0.0:8080"
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
	verboseFlag := flag.Bool("verbose", false, "enable verbose (debug) logging")
	mcpURL := flag.String("mcp", "", "MCP endpoint URL")
	enablePprofFlag := flag.Bool("enable-pprof", false, "enable pprof server")
	metricsAddrFlag := flag.String("metrics-addr", defaultMetricsAddr, "Address to listen on for prometheus metrics")
	modeFlag := flag.String("mode", "", "Mode: 'socket' (dev) or 'http' (prod). Defaults to 'socket' if SLACK_APP_TOKEN is set, otherwise 'http'")
	httpAddrFlag := flag.String("http-addr", defaultHTTPAddr, "Address to listen on for HTTP events (production mode)")
	maxRoundsFlag := flag.Int("max-rounds", 12, "Maximum number of rounds for the AI agent in normal mode")
	brainModeMaxRoundsFlag := flag.Int("brain-mode-max-rounds", 24, "Maximum number of rounds for the AI agent in brain mode (e.g. when the user asks for a detailed analysis)")
	flag.Parse()

	log := logger.New(*verboseFlag)

	// Handle MCP URL flag override
	mcpEndpoint := *mcpURL
	if mcpEndpoint != "" {
		os.Setenv("MCP_URL", mcpEndpoint)
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

	// Set up MCP client
	mcpClient, err := mcpclient.New(ctx, mcpclient.Config{
		Endpoint: cfg.MCPEndpoint,
		Logger:   log,
		Token:    cfg.MCPToken,
	})
	if err != nil {
		return fmt.Errorf("failed to create MCP client: %w", err)
	}
	defer mcpClient.Close()

	// Set up Anthropic client
	anthropicClient := anthropic.NewClient(option.WithAPIKey(cfg.AnthropicAPIKey))
	// Using Claude Sonnet 4.5 for better reasoning and proactive exploration
	// Haiku is cheaper but less capable for complex analysis
	// Alternative: anthropic.ModelClaudeHaiku4_5_20251001 // Cheaper but less capable

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
		mcpClient,
		&anthropicClient,
		convManager,
		log,
		*maxRoundsFlag,
		*brainModeMaxRoundsFlag,
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
	if errors.Is(err, context.Canceled) {
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
	log.Info("shutting down HTTP server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("error shutting down HTTP server", "error", err)
	}

	return nil
}
