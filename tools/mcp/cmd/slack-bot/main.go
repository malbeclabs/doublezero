package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/tools/mcp/pkg/agent"
	mcpclient "github.com/malbeclabs/doublezero/tools/mcp/pkg/client"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	slackutil "github.com/takara2314/slack-go-util"
)

const (
	defaultMetricsAddr = "0.0.0.0:8080"
	defaultHTTPAddr    = "0.0.0.0:3000"
	maxHistoryMessages = 20 // Keep last N messages to avoid token limits
)

type anthropicMessageAdapter struct {
	msg anthropic.MessageParam
}

func (a anthropicMessageAdapter) ToParam() any {
	return a.msg
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	verboseFlag := flag.Bool("verbose", false, "enable verbose (debug) logging")
	mcpURL := flag.String("mcp", "", "MCP endpoint URL")
	enablePprofFlag := flag.Bool("enable-pprof", false, "enable pprof server")
	metricsAddrFlag := flag.String("metrics-addr", defaultMetricsAddr, "Address to listen on for prometheus metrics")
	modeFlag := flag.String("mode", "", "Mode: 'socket' (dev) or 'http' (prod). Defaults to 'socket' if SLACK_APP_TOKEN is set, otherwise 'http'")
	httpAddrFlag := flag.String("http-addr", defaultHTTPAddr, "Address to listen on for HTTP events (production mode)")
	flag.Parse()

	log := newLogger(*verboseFlag)

	botToken := os.Getenv("SLACK_BOT_TOKEN")
	if botToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN is required")
	}

	// Determine mode
	mode := *modeFlag
	if mode == "" {
		// Auto-detect: socket mode if app token is set, otherwise HTTP mode
		if os.Getenv("SLACK_APP_TOKEN") != "" {
			mode = "socket"
		} else {
			mode = "http"
		}
	}

	if mode != "socket" && mode != "http" {
		return fmt.Errorf("mode must be 'socket' or 'http', got: %s", mode)
	}

	var appToken, signingSecret string
	if mode == "socket" {
		appToken = os.Getenv("SLACK_APP_TOKEN")
		if appToken == "" {
			return fmt.Errorf("SLACK_APP_TOKEN is required for socket mode")
		}
		log.Info("running in socket mode (dev)")
	} else {
		signingSecret = os.Getenv("SLACK_SIGNING_SECRET")
		if signingSecret == "" {
			return fmt.Errorf("SLACK_SIGNING_SECRET is required for HTTP mode")
		}
		log.Info("running in HTTP mode (prod)", "http_addr", *httpAddrFlag)
	}

	anthropicAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicAPIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is required")
	}

	mcpEndpoint := *mcpURL
	if mcpEndpoint == "" {
		mcpEndpoint = os.Getenv("MCP_URL")
	}
	if mcpEndpoint == "" {
		return fmt.Errorf("MCP endpoint is required (use -mcp flag or MCP_URL env var)")
	}

	if *enablePprofFlag {
		go func() {
			log.Info("starting pprof server", "address", "localhost:6060")
			err := http.ListenAndServe("localhost:6060", nil)
			if err != nil {
				log.Error("failed to start pprof server", "error", err)
			}
		}()
	}

	var metricsServerErrCh = make(chan error, 1)
	if *metricsAddrFlag != "" {
		go func() {
			listener, err := net.Listen("tcp", *metricsAddrFlag)
			if err != nil {
				log.Error("failed to start prometheus metrics server listener", "error", err)
				metricsServerErrCh <- err
				return
			}
			log.Info("prometheus metrics server listening", "address", listener.Addr().String())
			http.Handle("/metrics", promhttp.Handler())
			if err := http.Serve(listener, nil); err != nil {
				log.Error("failed to start prometheus metrics server", "error", err)
				metricsServerErrCh <- err
				return
			}
		}()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Set up MCP client
	mcpToken := os.Getenv("MCP_TOKEN")
	mcpClient, err := mcpclient.New(ctx, mcpclient.Config{
		Endpoint: mcpEndpoint,
		Logger:   log,
		Token:    mcpToken,
	})
	if err != nil {
		return fmt.Errorf("failed to create MCP client: %w", err)
	}
	defer mcpClient.Close()

	// Set up Anthropic client
	anthropicClient := anthropic.NewClient(option.WithAPIKey(anthropicAPIKey))
	// Using Claude Sonnet 4.5 for better reasoning and proactive exploration
	// Haiku is cheaper but less capable for complex analysis
	// Alternative: anthropic.ModelClaudeHaiku4_5_20251001 // Cheaper but less capable

	// Initialize Slack API client
	var api *slack.Client
	if mode == "socket" {
		api = slack.New(botToken, slack.OptionAppLevelToken(appToken))
	} else {
		api = slack.New(botToken)
	}

	// Test auth and verify token works
	var botUserID string
	if authTest, err := api.AuthTestContext(ctx); err == nil {
		botUserID = authTest.UserID
		log.Info("slack auth test successful", "user_id", authTest.UserID, "team", authTest.Team, "bot_id", authTest.BotID)
	} else {
		log.Warn("slack auth test failed", "error", err)
	}

	// Cache for conversation history to avoid repeated API calls
	// Key: thread timestamp (or message timestamp if no thread)
	// Value: conversation history
	// Limited to maxHistoryMessages * 10 threads to prevent unbounded growth
	conversations := make(map[string][]agent.Message)
	var conversationsMu sync.RWMutex
	const maxConversations = maxHistoryMessages * 10

	// Track processed events by envelope ID to avoid reprocessing duplicates
	// Slack Socket Mode can retry events, so we deduplicate by envelope_id
	// Limited to prevent unbounded growth (events older than 1 hour are cleaned up)
	processedEvents := make(map[string]time.Time)
	var processedEventsMu sync.RWMutex
	const processedEventsMaxAge = 1 * time.Hour

	// Track messages we've already responded to (by message timestamp) to prevent duplicate error messages
	// This handles cases where the same message might be retried with different envelope_ids
	// Limited to prevent unbounded growth (messages older than 1 hour are cleaned up)
	respondedMessages := make(map[string]time.Time)
	var respondedMessagesMu sync.RWMutex
	const respondedMessagesMaxAge = 1 * time.Hour

	// Cleanup goroutine for maps
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				// Clean up old processed events
				processedEventsMu.Lock()
				for id, timestamp := range processedEvents {
					if now.Sub(timestamp) > processedEventsMaxAge {
						delete(processedEvents, id)
					}
				}
				processedEventsMu.Unlock()

				// Clean up old responded messages
				respondedMessagesMu.Lock()
				for msgKey, timestamp := range respondedMessages {
					if now.Sub(timestamp) > respondedMessagesMaxAge {
						delete(respondedMessages, msgKey)
					}
				}
				respondedMessagesMu.Unlock()

				// Limit conversation cache size (keep most recent)
				conversationsMu.Lock()
				if len(conversations) > maxConversations {
					// Simple approach: clear all and let them rebuild (better would be LRU)
					// For now, just clear if we exceed limit
					conversations = make(map[string][]agent.Message)
				}
				conversationsMu.Unlock()
			}
		}
	}()

	// Shared event handler
	handleEvent := func(e slackevents.EventsAPIEvent, eventID string) {
		if e.Type != slackevents.CallbackEvent {
			return
		}

		switch ev := e.InnerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			if ev.ChannelType != "im" {
				return
			} // DMs only
			if ev.SubType != "" {
				return
			} // ignore edits/joins/etc
			if ev.BotID != "" {
				return
			} // avoid loops
			txt := strings.TrimSpace(ev.Text)
			if txt == "" {
				return
			}

			log.Info("dm recv", "user", ev.User, "channel", ev.Channel, "ts", ev.TimeStamp, "thread_ts", ev.ThreadTimeStamp, "text", txt, "event_id", eventID)

			// Check if we've already responded to this message (prevent duplicate error messages)
			messageKey := fmt.Sprintf("%s:%s", ev.Channel, ev.TimeStamp)
			respondedMessagesMu.RLock()
			_, alreadyResponded := respondedMessages[messageKey]
			respondedMessagesMu.RUnlock()

			if alreadyResponded {
				log.Info("skipping already responded message", "message_ts", ev.TimeStamp, "event_id", eventID)
				return
			}

			// Process message in a goroutine to allow concurrent handling of multiple conversations
			go handleMessage(ctx, ev, messageKey, eventID, api, mcpClient, &anthropicClient, log, &conversations, &conversationsMu, &respondedMessages, &respondedMessagesMu, botUserID)
		}
	}

	if mode == "socket" {
		// Socket mode (dev)
		client := socketmode.New(api)

		go func() {
			for evt := range client.Events {
				switch evt.Type {
				case socketmode.EventTypeConnecting:
					log.Info("socketmode: connecting")
				case socketmode.EventTypeConnected:
					log.Info("socketmode: connected")
				case socketmode.EventTypeConnectionError:
					log.Error("socketmode: connection error", "error", evt.Data)
				case socketmode.EventTypeEventsAPI:
					e, ok := evt.Data.(slackevents.EventsAPIEvent)
					if !ok {
						continue
					}

					// Check if we've already processed this event (deduplication)
					envelopeID := evt.Request.EnvelopeID
					retryAttempt := evt.Request.RetryAttempt
					retryReason := evt.Request.RetryReason

					if envelopeID != "" {
						processedEventsMu.RLock()
						_, alreadyProcessed := processedEvents[envelopeID]
						processedEventsMu.RUnlock()

						if alreadyProcessed {
							log.Info("skipping duplicate event", "envelope_id", envelopeID, "retry_attempt", retryAttempt, "retry_reason", retryReason)
							client.Ack(*evt.Request)
							continue
						}

						// Mark as processed BEFORE processing to prevent race conditions
						processedEventsMu.Lock()
						processedEvents[envelopeID] = time.Now()
						processedEventsMu.Unlock()

						if retryAttempt > 0 {
							log.Info("processing retried event", "envelope_id", envelopeID, "retry_attempt", retryAttempt, "retry_reason", retryReason)
						}
					}

					client.Ack(*evt.Request)
					handleEvent(e, envelopeID)
				}
			}
		}()

		log.Info("DM-only bot running in socket mode (thread replies enabled)")
		if err := client.RunContext(ctx); err != nil {
			return fmt.Errorf("slack client error: %w", err)
		}
	} else {
		// HTTP mode (prod)
		mux := http.NewServeMux()
		mux.HandleFunc("/slack/events", func(w http.ResponseWriter, r *http.Request) {
			handleHTTPEvent(w, r, signingSecret, log, &processedEvents, &processedEventsMu, handleEvent)
		})

		httpServer := &http.Server{
			Addr:    *httpAddrFlag,
			Handler: mux,
		}

		go func() {
			log.Info("HTTP server listening for Slack events", "addr", *httpAddrFlag)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("HTTP server error", "error", err)
			}
		}()

		log.Info("DM-only bot running in HTTP mode (thread replies enabled)")
		<-ctx.Done()
		log.Info("shutting down HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("error shutting down HTTP server", "error", err)
		}
	}

	return nil
}

// handleMessage processes a single Slack message in a goroutine, allowing concurrent handling of multiple conversations
func handleMessage(
	ctx context.Context,
	ev *slackevents.MessageEvent,
	messageKey string,
	envelopeID string,
	api *slack.Client,
	mcpClient *mcpclient.Client,
	anthropicClient *anthropic.Client,
	log *slog.Logger,
	conversations *map[string][]agent.Message,
	conversationsMu *sync.RWMutex,
	respondedMessages *map[string]time.Time,
	respondedMessagesMu *sync.RWMutex,
	botUserID string,
) {
	txt := strings.TrimSpace(ev.Text)

	// Check if user wants thread reply (either already in thread or includes :thread: emoji)
	wantsThread := ev.ThreadTimeStamp != "" || strings.Contains(txt, ":thread:")

	// Determine thread key: use thread timestamp if in thread, otherwise use message timestamp
	// For non-thread replies, we still use message timestamp as key for conversation history
	threadKey := ev.TimeStamp
	if ev.ThreadTimeStamp != "" {
		threadKey = ev.ThreadTimeStamp
	}

	// Fetch conversation history from Slack if not cached
	conversationsMu.RLock()
	msgs, cached := (*conversations)[threadKey]
	conversationsMu.RUnlock()

	if !cached {
		// For threads, fetch thread history. For top-level messages, start fresh.
		if ev.ThreadTimeStamp != "" {
			// User is in a thread - fetch thread history
			threadMsgs, err := fetchThreadHistory(ctx, api, ev.Channel, threadKey, log, botUserID)
			if err != nil {
				log.Warn("failed to fetch thread history", "thread", threadKey, "error", err)
				msgs = []agent.Message{}
			} else {
				msgs = threadMsgs
			}
		} else {
			// Top-level message - start with empty history (new conversation)
			// Each top-level message is its own separate conversation
			msgs = []agent.Message{}
			log.Debug("starting new conversation for top-level message", "message_ts", ev.TimeStamp)
		}
		// Cache it
		conversationsMu.Lock()
		(*conversations)[threadKey] = msgs
		conversationsMu.Unlock()
	}

	// Create a new agent instance with system prompt for this request
	// (System prompt is per-request in Anthropic API)
	requestAgent := agent.NewAnthropicAgent(&agent.AnthropicAgentConfig{
		Client:           *anthropicClient,
		Model:            anthropic.ModelClaudeSonnet4_5_20250929,
		MaxTokens:        int64(4000),
		MaxRounds:        16,
		MaxToolResultLen: 10000,
		Logger:           log,
		System:           agent.SystemPrompt,
	})

	// Add user's current message to history
	userMsg := anthropicMessageAdapter{
		msg: anthropic.NewUserMessage(anthropic.NewTextBlock(txt)),
	}
	msgs = append(msgs, userMsg)

	// Trim history to avoid token limits - keep last N messages
	// This keeps recent context while staying within limits
	msgs = trimConversationHistory(msgs, maxHistoryMessages)

	// Add processing reaction to indicate thinking/typing
	// Note: Requires reactions:write scope in Slack app
	log.Info("adding processing reaction", "channel", ev.Channel, "timestamp", ev.TimeStamp)
	itemRef := slack.NewRefToMessage(ev.Channel, ev.TimeStamp)

	// Add speech_balloon reaction to show we're processing
	err := api.AddReactionContext(ctx, "speech_balloon", itemRef)
	if err != nil {
		if strings.Contains(err.Error(), "missing_scope") {
			log.Error("SCOPE ISSUE: reactions:write scope is missing from your bot token",
				"action_required", "1. Go to api.slack.com/apps -> Your App -> OAuth & Permissions",
				"action_required_2", "2. Verify 'reactions:write' is in Bot Token Scopes",
				"action_required_3", "3. If not there, add it and click 'Reinstall to Workspace'",
				"action_required_4", "4. Copy the NEW Bot User OAuth Token (even if it looks the same)",
				"action_required_5", "5. Update SLACK_BOT_TOKEN environment variable",
				"action_required_6", "6. Restart the bot")
		} else {
			log.Warn("failed to add reaction", "emoji", "speech_balloon", "error", err, "channel", ev.Channel)
		}
	} else {
		log.Info("successfully added reaction", "emoji", "speech_balloon", "channel", ev.Channel, "timestamp", ev.TimeStamp)
	}

	var output bytes.Buffer
	result, err := agent.RunAgent(ctx, requestAgent, mcpClient, msgs, &output)
	if err != nil {
		log.Error("agent error", "error", err, "message_ts", ev.TimeStamp, "envelope_id", envelopeID)

		// Mark as responded to prevent duplicate error messages
		respondedMessagesMu.Lock()
		(*respondedMessages)[messageKey] = time.Now()
		respondedMessagesMu.Unlock()

		// Provide user-friendly error message instead of raw error
		reply := sanitizeErrorMessage(err.Error())

		// Post error response - use same thread logic as successful replies
		errorOpts := []slack.MsgOption{
			slack.MsgOptionText(reply, false),
		}
		if wantsThread {
			if ev.ThreadTimeStamp != "" {
				errorOpts = append(errorOpts, slack.MsgOptionTS(ev.ThreadTimeStamp))
			} else {
				errorOpts = append(errorOpts, slack.MsgOptionTS(ev.TimeStamp))
			}
		}
		_, _, _ = api.PostMessageContext(ctx, ev.Channel, errorOpts...)

		// Remove speech_balloon reaction after posting error
		itemRef := slack.NewRefToMessage(ev.Channel, ev.TimeStamp)
		time.Sleep(300 * time.Millisecond)
		if err := api.RemoveReactionContext(ctx, "speech_balloon", itemRef); err != nil {
			log.Debug("failed to remove reaction (may not have been added)", "emoji", "speech_balloon", "error", err)
		}
		return
	}

	// Only use FinalText - this is the clean, user-facing response
	// Don't use output buffer as it may contain raw tool results or internal details
	reply := strings.TrimSpace(result.FinalText)

	// Filter out any potential internal markers or raw tool result data
	// Remove truncation notices that might have leaked through
	// reply = filterInternalMarkers(reply)

	// Fallback if still empty
	if reply == "" {
		reply = "I didn't get a response. Please try again."
	}

	fmt.Println("reply:\n", reply)

	// Convert to blocks and set expand=true to prevent "see more" truncation
	convertedBlocks, err := slackutil.ConvertMarkdownTextToBlocks(reply)
	var blocks []slack.Block

	if err != nil {
		log.Debug("failed to convert markdown to blocks, using plain text", "error", err)
		blocks = nil
	} else {
		// Set expand=true on all section blocks to prevent "see more" truncation
		blocks = setExpandOnSectionBlocks(convertedBlocks, log)
	}

	var msgOpts []slack.MsgOption
	// In DMs: reply outside thread by default, unless user is in a thread or includes :thread: emoji
	if wantsThread {
		if ev.ThreadTimeStamp != "" {
			// User is already in a thread, reply in that thread
			msgOpts = append(msgOpts, slack.MsgOptionTS(ev.ThreadTimeStamp))
		} else {
			// User included :thread: emoji, create a new thread
			msgOpts = append(msgOpts, slack.MsgOptionTS(ev.TimeStamp))
		}
	}
	// If wantsThread is false, don't add MsgOptionTS, so reply goes outside thread

	// Reactions are being removed automatically in the goroutine on the same schedule
	// We just need to clean up writing_hand and any remaining ones after posting

	// Post message - use blocks for short messages, plain text for longer ones
	if blocks != nil {
		msgOpts = append(msgOpts, slack.MsgOptionBlocks(blocks...))
	} else {
		msgOpts = append(msgOpts, slack.MsgOptionText(reply, false))
	}

	// Mark as responded before posting to prevent race conditions
	respondedMessagesMu.Lock()
	(*respondedMessages)[messageKey] = time.Now()
	respondedMessagesMu.Unlock()

	_, respTS, err := api.PostMessageContext(ctx, ev.Channel, msgOpts...)

	// Remove speech_balloon reaction after response is posted
	if err == nil {
		removeItemRef := slack.NewRefToMessage(ev.Channel, ev.TimeStamp)
		time.Sleep(300 * time.Millisecond)
		if err := api.RemoveReactionContext(ctx, "speech_balloon", removeItemRef); err != nil {
			log.Debug("failed to remove reaction (may not have been added)", "emoji", "speech_balloon", "error", err)
		} else {
			log.Debug("removed reaction", "channel", ev.Channel, "emoji", "speech_balloon")
		}
	}

	if err != nil {
		errorOpts := []slack.MsgOption{
			slack.MsgOptionText("Sorry, I encountered an error. Please try again.", false),
		}
		if wantsThread {
			if ev.ThreadTimeStamp != "" {
				errorOpts = append(errorOpts, slack.MsgOptionTS(ev.ThreadTimeStamp))
			} else {
				errorOpts = append(errorOpts, slack.MsgOptionTS(ev.TimeStamp))
			}
		}
		_, _, _ = api.PostMessageContext(ctx, ev.Channel, errorOpts...)
	} else {
		log.Info("dm reply ok", "channel", ev.Channel, "thread_ts", threadKey, "reply_ts", respTS)

		// Update conversation history cache with FULL conversation including tool calls/results
		conversationsMu.Lock()
		(*conversations)[threadKey] = result.FullConversation
		conversationsMu.Unlock()
	}
}

// setExpandOnSectionBlocks splits section blocks by paragraphs/newlines and sets expand=true
// to prevent "see more" truncation. Code blocks (containing ```) are never split.
func setExpandOnSectionBlocks(blocks []slack.Block, log *slog.Logger) []slack.Block {
	if blocks == nil {
		return nil
	}

	var result []slack.Block
	for _, block := range blocks {
		// Check block type using BlockType() method
		if block.BlockType() == slack.MBTSection {
			// Type assert to SectionBlock
			sectionBlock := block.(*slack.SectionBlock)

			// If there's text, check if it contains code blocks or looks like code
			if sectionBlock.Text != nil && sectionBlock.Text.Text != "" {
				text := sectionBlock.Text.Text

				// Don't split if:
				// 1. Contains code block markers (```)
				// 2. Is a single line (likely a code line or already properly formatted)
				// Single-line blocks shouldn't be split as they're likely already atomic
				containsCodeMarkers := strings.Contains(text, "```")
				isSingleLine := !strings.Contains(text, "\n")

				if containsCodeMarkers || isSingleLine {
					// Looks like code - keep as single block with expand=true
					expandedBlock := &slack.SectionBlock{
						Type:      sectionBlock.Type,
						Text:      sectionBlock.Text,
						BlockID:   sectionBlock.BlockID,
						Fields:    sectionBlock.Fields,
						Accessory: sectionBlock.Accessory,
						Expand:    true,
					}
					result = append(result, expandedBlock)
				} else {
					// No code blocks - split by paragraphs normally
					paragraphs := splitIntoParagraphs(text)
					for _, para := range paragraphs {
						paraTextBlock := slack.NewTextBlockObject(sectionBlock.Text.Type, para, false, false)
						paraBlock := &slack.SectionBlock{
							Type:      sectionBlock.Type,
							Text:      paraTextBlock,
							BlockID:   sectionBlock.BlockID,
							Fields:    nil,
							Accessory: nil,
							Expand:    true,
						}
						result = append(result, paraBlock)
					}
				}
			} else {
				// No text, just copy the block with expand=true
				expandedBlock := &slack.SectionBlock{
					Type:      sectionBlock.Type,
					Text:      sectionBlock.Text,
					BlockID:   sectionBlock.BlockID,
					Fields:    sectionBlock.Fields,
					Accessory: sectionBlock.Accessory,
					Expand:    true,
				}
				result = append(result, expandedBlock)
			}
		} else {
			// Not a section block, keep as-is
			result = append(result, block)
		}
	}
	return result
}

// splitIntoParagraphs splits text into paragraphs by double newlines, then by single newlines
func splitIntoParagraphs(text string) []string {
	var paragraphs []string

	// First try splitting by double newline
	paraSplit := strings.Split(text, "\n\n")
	for _, para := range paraSplit {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// If paragraph contains single newlines, split those too
		lines := strings.Split(para, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				paragraphs = append(paragraphs, line)
			}
		}
	}

	// If no paragraphs found (e.g., no newlines), use the whole text
	if len(paragraphs) == 0 {
		paragraphs = []string{text}
	}

	return paragraphs
}

func newLogger(verbose bool) *slog.Logger {
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	return slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level: logLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				t := a.Value.Time().UTC()
				a.Value = slog.StringValue(formatRFC3339Millis(t))
			}
			if s, ok := a.Value.Any().(string); ok && s == "" {
				return slog.Attr{}
			}
			return a
		},
	}))
}

func formatRFC3339Millis(t time.Time) string {
	t = t.UTC()
	base := t.Format("2006-01-02T15:04:05")
	ms := t.Nanosecond() / 1_000_000
	return fmt.Sprintf("%s.%03dZ", base, ms)
}

// createSlackBlocks creates Slack blocks from text
// For long messages, use plain text instead of blocks to avoid "see more" truncation
func createSlackBlocks(text string, log *slog.Logger) []slack.Block {
	// Slack's actual display limit for section blocks is around 1500-2000 chars before showing "see more"
	// For longer messages, it's better to use plain text which doesn't have this truncation
	const maxBlockTextLen = 1500 // Conservative limit to avoid "see more" truncation

	if len(text) <= maxBlockTextLen {
		textBlock := slack.NewTextBlockObject("mrkdwn", text, false, false)
		return []slack.Block{slack.NewSectionBlock(textBlock, nil, nil)}
	}

	// For longer messages, return nil to signal we should use plain text instead
	// This avoids the "see more" truncation that happens with multiple blocks
	if log != nil {
		log.Debug("message too long for blocks, will use plain text", "len", len(text))
	}
	return nil
}

// sanitizeErrorMessage converts raw error messages to user-friendly messages
func sanitizeErrorMessage(errMsg string) string {
	// Rate limit errors
	if strings.Contains(errMsg, "429") || strings.Contains(errMsg, "rate_limit_error") || strings.Contains(errMsg, "rate limit") {
		return "I'm currently experiencing high demand. Please try again in a moment."
	}

	// Connection errors (should be retried, but if they still fail, show generic message)
	if strings.Contains(errMsg, "connection closed") ||
		strings.Contains(errMsg, "EOF") ||
		strings.Contains(errMsg, "client is closing") ||
		strings.Contains(errMsg, "failed to get tools") ||
		strings.Contains(errMsg, "broken pipe") ||
		strings.Contains(errMsg, "connection reset") {
		return "I'm having trouble connecting to the data service. Please try again in a moment."
	}

	// Generic API errors
	if strings.Contains(errMsg, "failed to get response") || strings.Contains(errMsg, "POST") {
		return "I encountered an error processing your request. Please try again."
	}

	// Remove internal details like Request-IDs, URLs, etc.
	// Keep only the core error message
	lines := strings.Split(errMsg, "\n")
	var cleanLines []string
	for _, line := range lines {
		// Skip lines with internal details
		if strings.Contains(line, "Request-ID:") ||
			strings.Contains(line, "https://") ||
			strings.Contains(line, `"type":"error"`) ||
			strings.Contains(line, "POST \"") ||
			strings.Contains(line, "tools/list") ||
			strings.Contains(line, "calling \"") {
			continue
		}
		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}
		cleanLines = append(cleanLines, line)
	}

	if len(cleanLines) > 0 {
		return "Sorry, I encountered an error: " + strings.Join(cleanLines, " ")
	}

	return "Sorry, I encountered an error. Please try again."
}

// filterInternalMarkers removes internal markers and notices that shouldn't be shown to users
func filterInternalMarkers(text string) string {
	// Remove truncation notices
	text = strings.ReplaceAll(text, "[Result truncated from", "")
	text = strings.ReplaceAll(text, "characters to avoid token limits]", "")

	return strings.TrimSpace(text)
}

// fetchThreadHistory fetches conversation history from Slack for a thread
func fetchThreadHistory(ctx context.Context, api *slack.Client, channelID, threadTS string, log *slog.Logger, botUserID string) ([]agent.Message, error) {
	params := &slack.GetConversationRepliesParameters{
		ChannelID: channelID,
		Timestamp: threadTS,
		Limit:     100, // Max messages to fetch
	}

	var allMessages []agent.Message
	var cursor string

	for {
		if cursor != "" {
			params.Cursor = cursor
		}

		msgs, hasMore, nextCursor, err := api.GetConversationRepliesContext(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to get conversation replies: %w", err)
		}

		log.Debug("fetchThreadHistory: got messages", "count", len(msgs), "thread_ts", threadTS)

		// Convert Slack messages to agent messages
		// GetConversationReplies includes the parent message (the one that started the thread) as the first message
		// Include ALL messages in the thread (bot, user, or other) for full context
		for _, msg := range msgs {
			// Skip messages without text
			if strings.TrimSpace(msg.Text) == "" {
				continue
			}

			// Bot messages can be identified by BotID or by matching the bot's UserID
			isBotMessage := msg.BotID != "" || (botUserID != "" && msg.User == botUserID)

			// Strip markdown from previous messages for cleaner context
			plainText := stripMarkdown(msg.Text)

			if isBotMessage {
				// Bot message - include as assistant message for context
				// This allows the bot to see its original message when user replies in thread
				log.Debug("fetchThreadHistory: including bot message", "ts", msg.Timestamp, "bot_id", msg.BotID, "user", msg.User, "text_preview", truncateString(plainText, 50))
				allMessages = append(allMessages, anthropicMessageAdapter{
					msg: anthropic.NewAssistantMessage(anthropic.NewTextBlock(plainText)),
				})
			} else {
				// All other messages (user messages or any other messages) - include as user messages
				log.Debug("fetchThreadHistory: including message", "ts", msg.Timestamp, "bot_id", msg.BotID, "user", msg.User, "text_preview", truncateString(plainText, 50))
				allMessages = append(allMessages, anthropicMessageAdapter{
					msg: anthropic.NewUserMessage(anthropic.NewTextBlock(plainText)),
				})
			}
		}

		if !hasMore {
			break
		}
		cursor = nextCursor
	}

	log.Debug("fetched thread history", "thread", threadTS, "messages", len(allMessages))
	if len(allMessages) == 0 {
		log.Warn("fetchThreadHistory: no messages found in thread", "thread_ts", threadTS)
	}
	return allMessages, nil
}

// truncateString truncates a string to maxLen characters, adding "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// stripMarkdown removes markdown formatting from text, converting it to plain text
func stripMarkdown(text string) string {
	// Remove code blocks (```code``` or ```language\ncode\n```)
	// Match triple backticks with optional language identifier, then any content (including newlines) until closing triple backticks
	text = regexp.MustCompile("(?s)```[a-zA-Z]*\\n?.*?```").ReplaceAllString(text, "")
	// Remove inline code (`code`)
	text = regexp.MustCompile("`[^`]+`").ReplaceAllString(text, "")

	// Remove links but keep the text [text](url) -> text
	text = regexp.MustCompile(`\[([^\]]+)\]\([^\)]+\)`).ReplaceAllString(text, "$1")

	// Remove bold/italic markers (**text** or *text* or __text__ or _text_)
	text = regexp.MustCompile(`\*\*([^\*]+)\*\*`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`\*([^\*]+)\*`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`__([^_]+)__`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`_([^_]+)_`).ReplaceAllString(text, "$1")

	// Remove headers (# Header -> Header)
	text = regexp.MustCompile(`^#{1,6}\s+`).ReplaceAllString(text, "")

	// Remove strikethrough (~~text~~ -> text)
	text = regexp.MustCompile(`~~([^~]+)~~`).ReplaceAllString(text, "$1")

	// Clean up extra whitespace
	text = regexp.MustCompile(`\n\s*\n\s*\n`).ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)

	return text
}

// fetchRecentConversationHistory fetches recent conversation history from a channel
// to get context when user replies to bot messages outside of threads
func fetchRecentConversationHistory(ctx context.Context, api *slack.Client, channelID string, log *slog.Logger, botUserID string) ([]agent.Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     20, // Fetch last 20 messages to find recent bot messages
	}

	msgs, err := api.GetConversationHistoryContext(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation history: %w", err)
	}

	var allMessages []agent.Message
	// GetConversationHistory returns messages in reverse chronological order (newest first)
	// We want chronological order (oldest first) for conversation context
	// Process from oldest to newest by iterating in reverse
	for i := len(msgs.Messages) - 1; i >= 0; i-- {
		msg := msgs.Messages[i]

		// Skip messages without text
		if strings.TrimSpace(msg.Text) == "" {
			continue
		}

		// Include both user messages and bot messages
		// Bot messages can be identified by BotID or by matching the bot's UserID
		isBotMessage := msg.BotID != "" || (botUserID != "" && msg.User == botUserID)

		// Strip markdown from previous messages for cleaner context
		plainText := stripMarkdown(msg.Text)

		if isBotMessage {
			// Bot message - include as assistant message
			log.Debug("fetchRecentConversationHistory: including bot message", "ts", msg.Timestamp, "bot_id", msg.BotID, "user", msg.User)
			allMessages = append(allMessages, anthropicMessageAdapter{
				msg: anthropic.NewAssistantMessage(anthropic.NewTextBlock(plainText)),
			})
		} else if msg.User != "" {
			// User message
			allMessages = append(allMessages, anthropicMessageAdapter{
				msg: anthropic.NewUserMessage(anthropic.NewTextBlock(plainText)),
			})
		}
	}

	log.Debug("fetched recent conversation history", "channel", channelID, "messages", len(allMessages))
	return allMessages, nil
}

// trimConversationHistory keeps the first message (for context) and last N messages
// to balance context preservation with token limits
func trimConversationHistory(msgs []agent.Message, maxMessages int) []agent.Message {
	if len(msgs) <= maxMessages {
		return msgs
	}
	// Keep first message (initial context) + last N-1 messages
	trimmed := make([]agent.Message, 0, maxMessages)
	if len(msgs) > 0 {
		trimmed = append(trimmed, msgs[0]) // Keep first for context
	}
	start := len(msgs) - (maxMessages - 1)
	if start < 1 {
		start = 1
	}
	if start < len(msgs) {
		trimmed = append(trimmed, msgs[start:]...)
	}
	return trimmed
}

// handleHTTPEvent handles incoming HTTP requests from Slack Events API
func handleHTTPEvent(
	w http.ResponseWriter,
	r *http.Request,
	signingSecret string,
	log *slog.Logger,
	processedEvents *map[string]time.Time,
	processedEventsMu *sync.RWMutex,
	handleEvent func(slackevents.EventsAPIEvent, string),
) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error("failed to read request body", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Verify request signature
	if !verifySlackSignature(r, body, signingSecret) {
		log.Warn("invalid Slack signature")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Handle URL verification challenge
	var challengeResp struct {
		Type      string `json:"type"`
		Token     string `json:"token"`
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(body, &challengeResp); err == nil && challengeResp.Type == "url_verification" {
		log.Info("responding to URL verification challenge")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(challengeResp.Challenge))
		return
	}

	// Parse event
	event, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	if err != nil {
		log.Error("failed to parse event", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// For HTTP mode, we'll extract event ID from the inner event if it's a message event
	// Otherwise use a hash of the event data for deduplication
	var eventID string
	if event.Type == slackevents.CallbackEvent {
		if msgEv, ok := event.InnerEvent.Data.(*slackevents.MessageEvent); ok {
			// Use channel:timestamp as event ID for message events
			eventID = fmt.Sprintf("%s:%s", msgEv.Channel, msgEv.TimeStamp)
		} else {
			// For other events, create a hash from the event data
			eventData, _ := json.Marshal(event.InnerEvent.Data)
			eventID = fmt.Sprintf("%x", sha256.Sum256(eventData))
		}
	} else {
		// For non-callback events, use a hash
		eventData, _ := json.Marshal(event)
		eventID = fmt.Sprintf("%x", sha256.Sum256(eventData))
	}

	// Deduplicate events using event ID
	if eventID != "" {
		processedEventsMu.RLock()
		_, alreadyProcessed := (*processedEvents)[eventID]
		processedEventsMu.RUnlock()

		if alreadyProcessed {
			log.Info("skipping duplicate event", "event_id", eventID)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Mark as processed BEFORE processing to prevent race conditions
		processedEventsMu.Lock()
		(*processedEvents)[eventID] = time.Now()
		processedEventsMu.Unlock()
	}

	// Respond quickly to Slack (within 3 seconds)
	w.WriteHeader(http.StatusOK)

	// Process event asynchronously
	go handleEvent(event, eventID)
}

// verifySlackSignature verifies the Slack request signature
func verifySlackSignature(r *http.Request, body []byte, signingSecret string) bool {
	timestamp := r.Header.Get("X-Slack-Request-Timestamp")
	signature := r.Header.Get("X-Slack-Signature")

	if timestamp == "" || signature == "" {
		return false
	}

	// Check timestamp to prevent replay attacks (within 5 minutes)
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix()-ts > 300 {
		return false
	}

	// Create signature base string
	sigBase := fmt.Sprintf("v0:%s:%s", timestamp, string(body))

	// Compute HMAC
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(sigBase))
	expectedSig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	// Use constant-time comparison
	return hmac.Equal([]byte(signature), []byte(expectedSig))
}
