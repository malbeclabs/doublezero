package slack

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/agent"
	mcpclient "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/client"
	"github.com/slack-go/slack/slackevents"
)

const (
	respondedMessagesMaxAge = 1 * time.Hour
)

// Processor processes Slack messages and generates responses
type Processor struct {
	slackClient     *Client
	mcpClient       *mcpclient.Client
	anthropicClient *anthropic.Client
	convManager     *Manager
	log             *slog.Logger

	// Track messages we've already responded to (by message timestamp) to prevent duplicate error messages
	respondedMessages   map[string]time.Time
	respondedMessagesMu sync.RWMutex
}

// NewProcessor creates a new message processor
func NewProcessor(
	slackClient *Client,
	mcpClient *mcpclient.Client,
	anthropicClient *anthropic.Client,
	convManager *Manager,
	log *slog.Logger,
) *Processor {
	return &Processor{
		slackClient:       slackClient,
		mcpClient:         mcpClient,
		anthropicClient:   anthropicClient,
		convManager:       convManager,
		log:               log,
		respondedMessages: make(map[string]time.Time),
	}
}

// StartCleanup starts a background goroutine to clean up old responded messages
func (p *Processor) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.cleanup()
			}
		}
	}()
}

func (p *Processor) cleanup() {
	now := time.Now()
	p.respondedMessagesMu.Lock()
	for msgKey, timestamp := range p.respondedMessages {
		if now.Sub(timestamp) > respondedMessagesMaxAge {
			delete(p.respondedMessages, msgKey)
		}
	}
	p.respondedMessagesMu.Unlock()
}

// HasResponded checks if we've already responded to a message
func (p *Processor) HasResponded(messageKey string) bool {
	p.respondedMessagesMu.RLock()
	_, responded := p.respondedMessages[messageKey]
	p.respondedMessagesMu.RUnlock()
	return responded
}

// MarkResponded marks a message as responded to
func (p *Processor) MarkResponded(messageKey string) {
	p.respondedMessagesMu.Lock()
	p.respondedMessages[messageKey] = time.Now()
	p.respondedMessagesMu.Unlock()
}

// ProcessMessage processes a single Slack message
func (p *Processor) ProcessMessage(
	ctx context.Context,
	ev *slackevents.MessageEvent,
	messageKey string,
	eventID string,
	isChannel bool,
) {
	startTime := time.Now()
	defer func() {
		MessageProcessingDuration.Observe(time.Since(startTime).Seconds())
	}()

	p.log.Info("replying to message",
		"channel", ev.Channel,
		"user", ev.User,
		"message_ts", ev.TimeStamp,
		"thread_ts", ev.ThreadTimeStamp,
		"text", ev.Text,
		"message_key", messageKey,
		"envelope_id", eventID,
		"is_channel", isChannel,
	)

	txt := strings.TrimSpace(ev.Text)

	// Remove bot mention from text for cleaner processing
	if isChannel {
		txt = p.slackClient.RemoveBotMention(txt)
	}

	// Always thread responses (both channels and DMs)
	// Determine thread key: use thread timestamp if in thread, otherwise use message timestamp
	threadKey := ev.TimeStamp
	if ev.ThreadTimeStamp != "" {
		threadKey = ev.ThreadTimeStamp
	}

	// Fetch conversation history from Slack if not cached
	fetcher := NewDefaultFetcher(p.log)
	msgs, err := p.convManager.GetConversationHistory(
		ctx,
		p.slackClient.API(),
		ev.Channel,
		ev.TimeStamp,
		ev.ThreadTimeStamp,
		p.slackClient.BotUserID(),
		fetcher,
	)
	if err != nil {
		p.log.Warn("failed to get conversation history", "error", err)
		ConversationHistoryErrorsTotal.Inc()
		msgs = []agent.Message{}
	}

	// Add user's current message to history
	userMsg := anthropicMessageAdapter{
		msg: anthropic.NewUserMessage(anthropic.NewTextBlock(txt)),
	}
	msgs = append(msgs, userMsg)

	// Don't trim here - the agent's internal trimming (KeepToolResultsRounds) handles
	// trimming in a way that preserves tool_use/tool_result pairs. Pre-trimming here
	// can break those pairs and cause API errors.

	requestAgent := agent.NewAnthropicAgent(&agent.AnthropicAgentConfig{
		Client:                *p.anthropicClient,
		Model:                 anthropic.ModelClaudeSonnet4_5_20250929,
		MaxTokens:             int64(4000),
		MaxRounds:             24,
		MaxToolResultLen:      10000,
		Logger:                p.log,
		System:                agent.SystemPrompt,
		KeepToolResultsRounds: 3, // Keep last 3 rounds of tool results during execution
	})

	// Add processing reaction to indicate thinking/typing
	if err := p.slackClient.AddProcessingReaction(ctx, ev.Channel, ev.TimeStamp); err != nil {
		// Error already logged in AddProcessingReaction
		SlackAPIErrorsTotal.WithLabelValues("add_reaction").Inc()
	}

	var output bytes.Buffer
	result, err := agent.RunAgent(ctx, requestAgent, p.mcpClient, msgs, &output)
	if err != nil {
		// If we get an error about invalid tool_use/tool_result pairs, clear the cache
		// This can happen if the cached conversation was corrupted
		errorType := "unknown"
		if strings.Contains(err.Error(), "tool_use_id") || strings.Contains(err.Error(), "tool_result") {
			errorType = "tool_use_pair"
			p.log.Warn("detected tool_use/tool_result pair error, clearing conversation cache", "thread_key", threadKey, "error", err)
			p.convManager.ClearConversation(threadKey)
		} else if strings.Contains(err.Error(), "mcp") || strings.Contains(err.Error(), "MCP") {
			errorType = "mcp_client"
			MCPClientErrorsTotal.WithLabelValues("run_agent").Inc()
		}
		AgentErrorsTotal.WithLabelValues(errorType).Inc()
		p.log.Error("agent error", "error", err, "message_ts", ev.TimeStamp, "envelope_id", eventID)

		// Mark as responded to prevent duplicate error messages
		p.MarkResponded(messageKey)

		// Provide user-friendly error message instead of raw error
		reply := SanitizeErrorMessage(err.Error())
		reply = normalizeTwoWayArrow(reply)

		// Post error response - always thread
		threadTS := ev.ThreadTimeStamp
		if threadTS == "" {
			threadTS = ev.TimeStamp
		}
		_, postErr := p.slackClient.PostMessage(ctx, ev.Channel, reply, nil, threadTS)
		if postErr != nil {
			SlackAPIErrorsTotal.WithLabelValues("post_message").Inc()
		} else {
			MessagesPostedTotal.WithLabelValues("error").Inc()
		}

		// Remove speech_balloon reaction after posting error
		time.Sleep(300 * time.Millisecond)
		if err := p.slackClient.RemoveProcessingReaction(ctx, ev.Channel, ev.TimeStamp); err != nil {
			SlackAPIErrorsTotal.WithLabelValues("remove_reaction").Inc()
		}
		return
	}

	// Only use FinalText - this is the clean, user-facing response
	reply := strings.TrimSpace(result.FinalText)

	// Fallback if still empty
	if reply == "" {
		reply = "I didn't get a response. Please try again."
	}

	// Normalize two-way arrows before rendering
	reply = normalizeTwoWayArrow(reply)

	p.log.Debug("agent response", "reply", reply)

	// Convert to blocks and set expand=true to prevent "see more" truncation
	blocks := ConvertMarkdownToBlocks(reply, p.log)

	// Determine thread timestamp for reply
	threadTS := ev.ThreadTimeStamp
	if threadTS == "" {
		threadTS = ev.TimeStamp
	}

	// Mark as responded before posting to prevent race conditions
	p.MarkResponded(messageKey)

	// Post message
	respTS, err := p.slackClient.PostMessage(ctx, ev.Channel, reply, blocks, threadTS)

	// Remove speech_balloon reaction after response is posted
	if err == nil {
		time.Sleep(300 * time.Millisecond)
		if err := p.slackClient.RemoveProcessingReaction(ctx, ev.Channel, ev.TimeStamp); err != nil {
			SlackAPIErrorsTotal.WithLabelValues("remove_reaction").Inc()
		}
	}

	if err != nil {
		SlackAPIErrorsTotal.WithLabelValues("post_message").Inc()
		MessagesPostedTotal.WithLabelValues("error").Inc()
		errorReply := "Sorry, I encountered an error. Please try again."
		errorReply = normalizeTwoWayArrow(errorReply)
		_, _ = p.slackClient.PostMessage(ctx, ev.Channel, errorReply, nil, threadTS)
	} else {
		MessagesPostedTotal.WithLabelValues("success").Inc()
		p.log.Info("reply posted successfully", "channel", ev.Channel, "thread_ts", threadKey, "reply_ts", respTS)

		// Update conversation history cache with FULL conversation including tool calls/results
		p.convManager.UpdateConversationHistory(threadKey, result.FullConversation)
	}
}
