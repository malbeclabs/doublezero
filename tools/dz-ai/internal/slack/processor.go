package slack

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/lake/pkg/agent"
	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
	"github.com/slack-go/slack/slackevents"
)

const (
	respondedMessagesMaxAge = 1 * time.Hour
)

// Processor processes Slack messages and generates responses
type Processor struct {
	slackClient *Client
	agent       *agent.Agent
	brainAgent  *agent.Agent
	convManager *Manager
	log         *slog.Logger

	// Track messages we've already responded to (by message timestamp) to prevent duplicate error messages
	respondedMessages   map[string]time.Time
	respondedMessagesMu sync.RWMutex
}

// NewProcessor creates a new message processor
func NewProcessor(
	slackClient *Client,
	agent *agent.Agent,
	brainAgent *agent.Agent,
	convManager *Manager,
	log *slog.Logger,
) *Processor {
	return &Processor{
		slackClient:       slackClient,
		agent:             agent,
		brainAgent:        brainAgent,
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

// countRounds counts the number of rounds (assistant messages) in the conversation.
// Each round adds one assistant message to the conversation.
func countRounds(conversation []react.Message) int {
	count := 0
	for _, msg := range conversation {
		// Check if this is an assistant message by type asserting to anthropic.MessageParam
		if param := msg.ToParam(); param != nil {
			if msgParam, ok := param.(anthropic.MessageParam); ok {
				// MessageParamRole is a string type, so we can compare directly
				if string(msgParam.Role) == "assistant" {
					count++
				}
			}
		}
	}
	return count
}

// effortModeLabel returns "normal" or "brain" as a string for the effort_mode label.
func effortModeLabel(isBrainMode bool) string {
	if isBrainMode {
		return "brain"
	}
	return "normal"
}

// containsNonBotMention checks if the message text contains a user mention that is not the bot
func containsNonBotMention(text, botUserID string) bool {
	if botUserID == "" {
		return false
	}
	// Match mention patterns: <@USERID> or <@USERID|username>
	mentionRegex := regexp.MustCompile(`<@([A-Z0-9]+)(?:\|[^>]+)?>`)
	matches := mentionRegex.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		mentionedUserID := match[1]
		// Check if this mention is NOT the bot
		if mentionedUserID != botUserID {
			return true
		}
	}
	return false
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

	// Skip processing if in a thread and message contains another user being mentioned
	if ev.ThreadTimeStamp != "" && containsNonBotMention(ev.Text, p.slackClient.BotUserID()) {
		p.log.Info("skipping message in thread that contains non-bot mention",
			"channel", ev.Channel,
			"user", ev.User,
			"message_ts", ev.TimeStamp,
			"thread_ts", ev.ThreadTimeStamp,
			"text_preview", TruncateString(ev.Text, 100),
		)
		MessagesIgnoredTotal.WithLabelValues("thread_non_bot_mention").Inc()
		return
	}

	// Skip processing if message contains :mute: emoji
	if strings.Contains(ev.Text, ":mute:") {
		p.log.Info("skipping message with :mute: emoji",
			"channel", ev.Channel,
			"user", ev.User,
			"message_ts", ev.TimeStamp,
			"thread_ts", ev.ThreadTimeStamp,
			"text_preview", TruncateString(ev.Text, 100),
		)
		MessagesIgnoredTotal.WithLabelValues("mute_emoji").Inc()
		return
	}

	txt := strings.TrimSpace(ev.Text)

	// Remove bot mention from text for cleaner processing
	if isChannel {
		txt = p.slackClient.RemoveBotMention(txt)
	}

	// Check for brain mode (:brain: emoji)
	isBrainMode := strings.Contains(ev.Text, ":brain:")
	if isBrainMode {
		p.log.Info("brain mode activated")
	}

	effortModeLabel := effortModeLabel(isBrainMode)
	defer func() {
		MessageProcessingDuration.WithLabelValues(effortModeLabel).Observe(time.Since(startTime).Seconds())
	}()

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
		msgs = []react.Message{}
	}

	// Add user's current message to history
	userMsg := react.AnthropicMessage{
		Msg: anthropic.NewUserMessage(anthropic.NewTextBlock(txt)),
	}
	msgs = append(msgs, userMsg)

	// Select agent based on brain mode
	selectedAgent := p.agent
	if isBrainMode {
		selectedAgent = p.brainAgent
	}

	// Add processing reaction to indicate thinking/typing
	if err := p.slackClient.AddProcessingReaction(ctx, ev.Channel, ev.TimeStamp); err != nil {
		// Error already logged in AddProcessingReaction
		SlackAPIErrorsTotal.WithLabelValues("add_reaction").Inc()
	}

	var output bytes.Buffer
	// Use agent with conversation history
	result, err := selectedAgent.RunWithMessages(ctx, msgs, &output)
	if err != nil {
		errorType := "unknown"
		if strings.Contains(err.Error(), "tool_use_id") || strings.Contains(err.Error(), "tool_result") {
			errorType = "tool_use_pair"
			p.log.Warn("detected tool_use/tool_result pair error, clearing conversation cache", "thread_key", threadKey, "error", err)
			p.convManager.ClearConversation(threadKey)
		}
		AgentErrorsTotal.WithLabelValues(errorType, effortModeLabel).Inc()
		p.log.Error("agent error", "error", err, "message_ts", ev.TimeStamp, "envelope_id", eventID)

		p.MarkResponded(messageKey)

		reply := SanitizeErrorMessage(err.Error())
		reply = normalizeTwoWayArrow(reply)

		threadTS := ev.ThreadTimeStamp
		if threadTS == "" {
			threadTS = ev.TimeStamp
		}
		_, postErr := p.slackClient.PostMessage(ctx, ev.Channel, reply, nil, threadTS)
		if postErr != nil {
			SlackAPIErrorsTotal.WithLabelValues("post_message").Inc()
		} else {
			MessagesPostedTotal.WithLabelValues("error", effortModeLabel).Inc()
		}

		time.Sleep(300 * time.Millisecond)
		if err := p.slackClient.RemoveProcessingReaction(ctx, ev.Channel, ev.TimeStamp); err != nil {
			SlackAPIErrorsTotal.WithLabelValues("remove_reaction").Inc()
		}
		return
	}

	reply := strings.TrimSpace(result.FinalText)
	if reply == "" {
		reply = "I didn't get a response. Please try again."
	}
	reply = normalizeTwoWayArrow(reply)

	p.log.Debug("agent response", "reply", reply)

	blocks := ConvertMarkdownToBlocks(reply, p.log)

	// Append ISIS citation if ISIS tools were used
	if UsedISISTools(result.ToolsUsed) {
		blocks = AppendISISCitation(blocks)
	}

	threadTS := ev.ThreadTimeStamp
	if threadTS == "" {
		threadTS = ev.TimeStamp
	}

	p.MarkResponded(messageKey)

	respTS, err := p.slackClient.PostMessage(ctx, ev.Channel, reply, blocks, threadTS)

	if err == nil {
		time.Sleep(300 * time.Millisecond)
		if err := p.slackClient.RemoveProcessingReaction(ctx, ev.Channel, ev.TimeStamp); err != nil {
			SlackAPIErrorsTotal.WithLabelValues("remove_reaction").Inc()
		}
	}

	if err != nil {
		SlackAPIErrorsTotal.WithLabelValues("post_message").Inc()
		MessagesPostedTotal.WithLabelValues("error", effortModeLabel).Inc()
		errorReply := "Sorry, I encountered an error. Please try again."
		errorReply = normalizeTwoWayArrow(errorReply)
		_, _ = p.slackClient.PostMessage(ctx, ev.Channel, errorReply, nil, threadTS)
	} else {
		MessagesPostedTotal.WithLabelValues("success", effortModeLabel).Inc()
		p.log.Info("reply posted successfully", "channel", ev.Channel, "thread_ts", threadKey, "reply_ts", respTS)

		// Count rounds and record metric
		rounds := countRounds(result.FullConversation)
		AgentRounds.WithLabelValues(effortModeLabel).Observe(float64(rounds))

		// Update conversation history cache with FULL conversation including tool calls/results
		p.convManager.UpdateConversationHistory(threadKey, result.FullConversation)
	}
}
