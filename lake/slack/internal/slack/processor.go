package slack

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
	"github.com/slack-go/slack/slackevents"
)

const (
	respondedMessagesMaxAge = 1 * time.Hour
)

// Processor processes Slack messages and generates responses
type Processor struct {
	slackClient *Client
	pipeline    *pipeline.Pipeline
	convManager *Manager
	log         *slog.Logger

	// Track messages we've already responded to (by message timestamp) to prevent duplicate error messages
	respondedMessages   map[string]time.Time
	respondedMessagesMu sync.RWMutex
}

// NewProcessor creates a new message processor
func NewProcessor(
	slackClient *Client,
	pipeline *pipeline.Pipeline,
	convManager *Manager,
	log *slog.Logger,
) *Processor {
	return &Processor{
		slackClient:       slackClient,
		pipeline:          pipeline,
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

// formatThinkingMessage formats the thinking message based on progress
func formatThinkingMessage(progress pipeline.Progress) string {
	var sb strings.Builder

	switch progress.Stage {
	case pipeline.StageClassifying:
		sb.WriteString(":hourglass: *Thinking...*\n")
		sb.WriteString("_Analyzing your question..._")

	case pipeline.StageDecomposing:
		sb.WriteString(":hourglass: *Thinking...*\n")
		sb.WriteString("_Breaking down into data questions..._")

	case pipeline.StageDecomposed:
		sb.WriteString(":hourglass: *Thinking...*\n")
		sb.WriteString("_Identified data questions:_\n")
		for i, q := range progress.DataQuestions {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, q.Question))
		}

	case pipeline.StageExecuting:
		sb.WriteString(":hourglass: *Thinking...*\n")
		sb.WriteString("_Identified data questions:_\n")
		for i, q := range progress.DataQuestions {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, q.Question))
		}
		sb.WriteString(fmt.Sprintf("\n_Running queries... (%d/%d)_", progress.QueriesDone, progress.QueriesTotal))

	case pipeline.StageSynthesizing:
		sb.WriteString(":hourglass: *Thinking...*\n")
		sb.WriteString("_Identified data questions:_\n")
		for i, q := range progress.DataQuestions {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, q.Question))
		}
		sb.WriteString(fmt.Sprintf("\n:white_check_mark: _Queries complete (%d/%d)_\n", progress.QueriesDone, progress.QueriesTotal))
		sb.WriteString("_Preparing answer..._")

	case pipeline.StageComplete:
		// For data_analysis, show summary
		if progress.Classification == pipeline.ClassificationDataAnalysis && len(progress.DataQuestions) > 0 {
			sb.WriteString(":brain: *Analysis complete*\n")
			sb.WriteString("_Answered by querying:_\n")
			for i, q := range progress.DataQuestions {
				sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, q.Question))
			}
		}
		// For conversational/out_of_scope, we don't show anything (just answer)

	case pipeline.StageError:
		sb.WriteString(":x: *Error*\n")
		if progress.Error != nil {
			sb.WriteString(fmt.Sprintf("_%s_", progress.Error.Error()))
		}
	}

	return sb.String()
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

	defer func() {
		MessageProcessingDuration.WithLabelValues("pipeline").Observe(time.Since(startTime).Seconds())
	}()

	// Always thread responses (both channels and DMs)
	// Determine thread key: use thread timestamp if in thread, otherwise use message timestamp
	threadKey := ev.TimeStamp
	threadTS := ev.ThreadTimeStamp
	if threadTS == "" {
		threadTS = ev.TimeStamp
	}
	if ev.ThreadTimeStamp != "" {
		threadKey = ev.ThreadTimeStamp
	}

	// Fetch conversation history from Slack if not cached
	fetcher := NewDefaultFetcher(p.log)
	history, err := p.convManager.GetConversationHistory(
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
		history = []pipeline.ConversationMessage{}
	}

	// Post initial thinking message
	thinkingTS, err := p.slackClient.PostMessage(ctx, ev.Channel, ":hourglass: *Thinking...*\n_Analyzing your question..._", nil, threadTS)
	if err != nil {
		p.log.Warn("failed to post thinking message", "error", err)
		SlackAPIErrorsTotal.WithLabelValues("post_message").Inc()
		// Continue anyway - we can still process without the thinking message
	}

	// Track last progress stage to avoid redundant updates
	var lastStage pipeline.ProgressStage
	var lastQueriesDone int
	var thinkingMu sync.Mutex

	// Progress callback to update thinking message
	onProgress := func(progress pipeline.Progress) {
		thinkingMu.Lock()
		defer thinkingMu.Unlock()

		// Skip if same stage and same query count (avoid redundant updates)
		if progress.Stage == lastStage && progress.QueriesDone == lastQueriesDone {
			return
		}
		lastStage = progress.Stage
		lastQueriesDone = progress.QueriesDone

		// Don't update on complete - we'll handle that separately
		if progress.Stage == pipeline.StageComplete {
			return
		}

		// Update thinking message
		if thinkingTS != "" {
			thinkingText := formatThinkingMessage(progress)
			if err := p.slackClient.UpdateMessage(ctx, ev.Channel, thinkingTS, thinkingText, nil); err != nil {
				p.log.Debug("failed to update thinking message", "error", err)
			}
		}
	}

	// Run the pipeline with progress callbacks
	result, err := p.pipeline.RunWithProgress(ctx, txt, history, onProgress)
	if err != nil {
		AgentErrorsTotal.WithLabelValues("pipeline", "pipeline").Inc()
		p.log.Error("pipeline error", "error", err, "message_ts", ev.TimeStamp, "envelope_id", eventID)

		p.MarkResponded(messageKey)

		// Update thinking message to show error
		if thinkingTS != "" {
			errorText := fmt.Sprintf(":x: *Error*\n_%s_", SanitizeErrorMessage(err.Error()))
			if err := p.slackClient.UpdateMessage(ctx, ev.Channel, thinkingTS, errorText, nil); err != nil {
				p.log.Debug("failed to update thinking message with error", "error", err)
			}
		}

		MessagesPostedTotal.WithLabelValues("error", "pipeline").Inc()
		return
	}

	reply := strings.TrimSpace(result.Answer)
	if reply == "" {
		reply = "I didn't get a response. Please try again."
	}
	reply = normalizeTwoWayArrow(reply)

	p.log.Debug("pipeline response",
		"reply", reply,
		"classification", result.Classification,
		"data_questions", len(result.DataQuestions))

	// For data analysis, update thinking message with summary
	if result.Classification == pipeline.ClassificationDataAnalysis && len(result.DataQuestions) > 0 && thinkingTS != "" {
		summaryText := formatThinkingMessage(pipeline.Progress{
			Stage:          pipeline.StageComplete,
			Classification: result.Classification,
			DataQuestions:  result.DataQuestions,
			QueriesTotal:   len(result.DataQuestions),
			QueriesDone:    len(result.DataQuestions),
		})
		if err := p.slackClient.UpdateMessage(ctx, ev.Channel, thinkingTS, summaryText, nil); err != nil {
			p.log.Debug("failed to update thinking message with summary", "error", err)
		}
	} else if thinkingTS != "" {
		// For conversational/out_of_scope, delete the thinking message so only the answer shows
		if err := p.slackClient.DeleteMessage(ctx, ev.Channel, thinkingTS); err != nil {
			p.log.Debug("failed to delete thinking message", "error", err)
		}
	}

	// Post the final answer
	blocks := ConvertMarkdownToBlocks(reply, p.log)

	p.MarkResponded(messageKey)

	respTS, err := p.slackClient.PostMessage(ctx, ev.Channel, reply, blocks, threadTS)

	if err != nil {
		SlackAPIErrorsTotal.WithLabelValues("post_message").Inc()
		MessagesPostedTotal.WithLabelValues("error", "pipeline").Inc()
		errorReply := "Sorry, I encountered an error. Please try again."
		errorReply = normalizeTwoWayArrow(errorReply)
		_, _ = p.slackClient.PostMessage(ctx, ev.Channel, errorReply, nil, threadTS)
	} else {
		MessagesPostedTotal.WithLabelValues("success", "pipeline").Inc()
		p.log.Info("reply posted successfully", "channel", ev.Channel, "thread_ts", threadKey, "reply_ts", respTS)

		// Extract SQL queries from executed queries
		var executedSQL []string
		for _, eq := range result.ExecutedQueries {
			executedSQL = append(executedSQL, eq.GeneratedQuery.SQL)
		}

		// Update conversation history with the new exchange
		newHistory := append(history,
			pipeline.ConversationMessage{Role: "user", Content: txt},
			pipeline.ConversationMessage{Role: "assistant", Content: result.Answer, ExecutedQueries: executedSQL},
		)
		p.convManager.UpdateConversationHistory(threadKey, newHistory)
	}
}
