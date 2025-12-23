package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
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
	flag.Parse()

	log := newLogger(*verboseFlag)

	botToken, appToken := os.Getenv("SLACK_BOT_TOKEN"), os.Getenv("SLACK_APP_TOKEN")
	if botToken == "" || appToken == "" {
		return fmt.Errorf("set SLACK_BOT_TOKEN and SLACK_APP_TOKEN")
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
	mcpClient, err := mcpclient.New(ctx, mcpclient.Config{
		Endpoint: mcpEndpoint,
		Logger:   log,
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

	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))

	// Test auth and verify token works
	var botUserID string
	if authTest, err := api.AuthTestContext(ctx); err == nil {
		botUserID = authTest.UserID
		log.Info("slack auth test successful", "user_id", authTest.UserID, "team", authTest.Team, "bot_id", authTest.BotID)
	} else {
		log.Warn("slack auth test failed", "error", err)
	}

	// Try to test reaction API directly on startup to verify scope
	// We'll do a test reaction when we get the first message instead

	client := socketmode.New(api)

	// Cache for conversation history to avoid repeated API calls
	// Key: thread timestamp (or message timestamp if no thread)
	// Value: conversation history
	conversations := make(map[string][]agent.Message)
	var conversationsMu sync.RWMutex

	// Track processed events by envelope ID to avoid reprocessing duplicates
	// Slack Socket Mode can retry events, so we deduplicate by envelope_id
	processedEvents := make(map[string]bool)
	var processedEventsMu sync.RWMutex

	// Track messages we've already responded to (by message timestamp) to prevent duplicate error messages
	// This handles cases where the same message might be retried with different envelope_ids
	respondedMessages := make(map[string]bool)
	var respondedMessagesMu sync.RWMutex

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
					alreadyProcessed := processedEvents[envelopeID]
					processedEventsMu.RUnlock()

					if alreadyProcessed {
						log.Info("skipping duplicate event", "envelope_id", envelopeID, "retry_attempt", retryAttempt, "retry_reason", retryReason)
						client.Ack(*evt.Request)
						continue
					}

					// Mark as processed BEFORE processing to prevent race conditions
					processedEventsMu.Lock()
					processedEvents[envelopeID] = true
					processedEventsMu.Unlock()

					if retryAttempt > 0 {
						log.Info("processing retried event", "envelope_id", envelopeID, "retry_attempt", retryAttempt, "retry_reason", retryReason)
					}
				}

				client.Ack(*evt.Request)
				if e.Type != slackevents.CallbackEvent {
					continue
				}

				switch ev := e.InnerEvent.Data.(type) {
				case *slackevents.MessageEvent:
					if ev.ChannelType != "im" {
						continue
					} // DMs only
					if ev.SubType != "" {
						continue
					} // ignore edits/joins/etc
					if ev.BotID != "" {
						continue
					} // avoid loops
					txt := strings.TrimSpace(ev.Text)
					if txt == "" {
						continue
					}

					log.Info("dm recv", "user", ev.User, "channel", ev.Channel, "ts", ev.TimeStamp, "thread_ts", ev.ThreadTimeStamp, "text", txt, "envelope_id", envelopeID, "retry_attempt", retryAttempt)

					// Check if we've already responded to this message (prevent duplicate error messages)
					messageKey := fmt.Sprintf("%s:%s", ev.Channel, ev.TimeStamp)
					respondedMessagesMu.RLock()
					alreadyResponded := respondedMessages[messageKey]
					respondedMessagesMu.RUnlock()

					if alreadyResponded {
						log.Info("skipping already responded message", "message_ts", ev.TimeStamp, "envelope_id", envelopeID)
						continue
					}

					// Process message in a goroutine to allow concurrent handling of multiple conversations
					go handleMessage(ctx, ev, messageKey, envelopeID, api, mcpClient, &anthropicClient, log, &conversations, &conversationsMu, &respondedMessages, &respondedMessagesMu, botUserID)
				}
			}
		}
	}()

	log.Info("DM-only bot running (thread replies enabled)")
	if err := client.RunContext(ctx); err != nil {
		return fmt.Errorf("slack client error: %w", err)
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
	respondedMessages *map[string]bool,
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
		(*respondedMessages)[messageKey] = true
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
	(*respondedMessages)[messageKey] = true
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
// to prevent "see more" truncation. Each paragraph becomes its own section block.
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

			// If there's text, split it by paragraphs/newlines
			if sectionBlock.Text != nil && sectionBlock.Text.Text != "" {
				text := sectionBlock.Text.Text

				// Split by double newline (paragraphs) first, then by single newline
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

				// Create a section block for each paragraph
				for i, para := range paragraphs {
					paraTextBlock := slack.NewTextBlockObject(sectionBlock.Text.Type, para, false, false)
					paraBlock := &slack.SectionBlock{
						Type:      sectionBlock.Type,
						Text:      paraTextBlock,
						BlockID:   sectionBlock.BlockID,
						Fields:    nil,  // Don't copy fields to split blocks
						Accessory: nil,  // Don't copy accessory to split blocks
						Expand:    true, // Set expand to prevent "see more" truncation
					}
					result = append(result, paraBlock)

					// Add a small spacer between paragraphs (except after the last one)
					if i < len(paragraphs)-1 {
						// Add a divider or just skip - dividers might be too much
						// Actually, let's just leave them as separate blocks, Slack will space them
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
	// Remove code blocks (```code``` or `code`)
	text = regexp.MustCompile("(?s)```[^`]*```").ReplaceAllString(text, "")
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
