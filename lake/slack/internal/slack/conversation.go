package slack

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
	"github.com/malbeclabs/doublezero/lake/utils/pkg/retry"
	"github.com/slack-go/slack"
)

const (
	maxHistoryMessages = 20 // Keep last N messages to avoid token limits
	maxConversations   = maxHistoryMessages * 10
)

const activeThreadsMaxAge = 24 * time.Hour

// Manager manages conversation history and active threads
type Manager struct {
	// Conversation history cache
	// Key: thread timestamp (or message timestamp if no thread)
	// Value: conversation history
	conversations   map[string][]workflow.ConversationMessage
	conversationsMu sync.RWMutex

	// Track active threads where the bot was mentioned
	// Key: channel:thread_timestamp, Value: timestamp when thread was activated
	activeThreads   map[string]time.Time
	activeThreadsMu sync.RWMutex

	log *slog.Logger
}

// NewManager creates a new conversation manager
func NewManager(log *slog.Logger) *Manager {
	return &Manager{
		conversations: make(map[string][]workflow.ConversationMessage),
		activeThreads: make(map[string]time.Time),
		log:           log,
	}
}

// StartCleanup starts a background goroutine to clean up old entries
func (m *Manager) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.cleanup()
			}
		}
	}()
}

func (m *Manager) cleanup() {
	now := time.Now()

	// Clean up old active threads
	m.activeThreadsMu.Lock()
	for threadKey, timestamp := range m.activeThreads {
		if now.Sub(timestamp) > activeThreadsMaxAge {
			delete(m.activeThreads, threadKey)
		}
	}
	activeCount := len(m.activeThreads)
	m.activeThreadsMu.Unlock()

	// Update active conversations metric
	ActiveConversations.Set(float64(activeCount))

	// Limit conversation cache size (keep most recent)
	m.conversationsMu.Lock()
	if len(m.conversations) > maxConversations {
		// Simple approach: clear all and let them rebuild (better would be LRU)
		m.conversations = make(map[string][]workflow.ConversationMessage)
	}
	m.conversationsMu.Unlock()
}

// MarkThreadActive marks a thread as active (bot was mentioned in root message)
func (m *Manager) MarkThreadActive(channelID, threadTS string) {
	threadKey := fmt.Sprintf("%s:%s", channelID, threadTS)
	m.activeThreadsMu.Lock()
	m.activeThreads[threadKey] = time.Now()
	activeCount := len(m.activeThreads)
	m.activeThreadsMu.Unlock()
	ActiveConversations.Set(float64(activeCount))
	m.log.Debug("marked thread as active", "thread_key", threadKey)
}

// IsThreadActive checks if a thread is active
func (m *Manager) IsThreadActive(channelID, threadTS string) bool {
	threadKey := fmt.Sprintf("%s:%s", channelID, threadTS)
	m.activeThreadsMu.RLock()
	_, active := m.activeThreads[threadKey]
	m.activeThreadsMu.RUnlock()
	return active
}

// HistoryFetcher fetches conversation history from Slack
type HistoryFetcher interface {
	FetchThreadHistory(ctx context.Context, api *slack.Client, channelID, threadTS, botUserID string) ([]workflow.ConversationMessage, error)
}

// GetConversationHistory gets conversation history for a thread, fetching from Slack if not cached
func (m *Manager) GetConversationHistory(
	ctx context.Context,
	api *slack.Client,
	channelID, messageTS, threadTS string,
	botUserID string,
	fetcher HistoryFetcher,
) ([]workflow.ConversationMessage, error) {
	// Determine thread key: use thread timestamp if in thread, otherwise use message timestamp
	threadKey := messageTS
	if threadTS != "" {
		threadKey = threadTS
	}

	// Check cache first
	m.conversationsMu.RLock()
	msgs, cached := m.conversations[threadKey]
	m.conversationsMu.RUnlock()

	if cached {
		return msgs, nil
	}

	// Fetch from Slack
	if threadTS != "" {
		// User is in a thread - fetch thread history
		threadMsgs, err := fetcher.FetchThreadHistory(ctx, api, channelID, threadKey, botUserID)
		if err != nil {
			m.log.Warn("failed to fetch thread history", "thread", threadKey, "error", err)
			msgs = []workflow.ConversationMessage{}
		} else {
			msgs = threadMsgs
		}
	} else {
		// Top-level message - start with empty history (new conversation)
		msgs = []workflow.ConversationMessage{}
		m.log.Debug("starting new conversation for top-level message", "message_ts", messageTS)
	}

	// Cache it
	m.conversationsMu.Lock()
	m.conversations[threadKey] = msgs
	m.conversationsMu.Unlock()

	return msgs, nil
}

// UpdateConversationHistory updates the conversation history cache
func (m *Manager) UpdateConversationHistory(threadKey string, msgs []workflow.ConversationMessage) {
	m.conversationsMu.Lock()
	m.conversations[threadKey] = msgs
	m.conversationsMu.Unlock()
}

// ClearConversation clears the conversation cache for a specific thread
func (m *Manager) ClearConversation(threadKey string) {
	m.conversationsMu.Lock()
	delete(m.conversations, threadKey)
	m.conversationsMu.Unlock()
	m.log.Debug("cleared conversation cache", "thread_key", threadKey)
}

// DefaultFetcher is the default implementation of HistoryFetcher
type DefaultFetcher struct {
	log *slog.Logger
}

// NewDefaultFetcher creates a new default fetcher
func NewDefaultFetcher(log *slog.Logger) *DefaultFetcher {
	return &DefaultFetcher{log: log}
}

// FetchThreadHistory fetches conversation history from Slack for a thread
func (f *DefaultFetcher) FetchThreadHistory(ctx context.Context, api *slack.Client, channelID, threadTS, botUserID string) ([]workflow.ConversationMessage, error) {
	params := &slack.GetConversationRepliesParameters{
		ChannelID: channelID,
		Timestamp: threadTS,
		Limit:     100, // Max messages to fetch
	}

	var allMessages []workflow.ConversationMessage
	var cursor string

	for {
		if cursor != "" {
			params.Cursor = cursor
		}

		var msgs []slack.Message
		var hasMore bool
		var nextCursor string
		var err error

		retryCfg := retry.DefaultConfig()
		err = retry.Do(ctx, retryCfg, func() error {
			msgs, hasMore, nextCursor, err = api.GetConversationRepliesContext(ctx, params)
			if err != nil {
				return err
			}
			return nil
		})

		if err != nil {
			return nil, fmt.Errorf("failed to get conversation replies after retries: %w", err)
		}

		f.log.Debug("fetchThreadHistory: got messages", "count", len(msgs), "thread_ts", threadTS)

		// Convert Slack messages to workflow conversation messages
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
				f.log.Debug("fetchThreadHistory: including bot message", "ts", msg.Timestamp, "bot_id", msg.BotID, "user", msg.User, "text_preview", TruncateString(plainText, 50))
				allMessages = append(allMessages, workflow.ConversationMessage{
					Role:    "assistant",
					Content: plainText,
				})
			} else {
				f.log.Debug("fetchThreadHistory: including message", "ts", msg.Timestamp, "bot_id", msg.BotID, "user", msg.User, "text_preview", TruncateString(plainText, 50))
				allMessages = append(allMessages, workflow.ConversationMessage{
					Role:    "user",
					Content: plainText,
				})
			}
		}

		if !hasMore {
			break
		}
		cursor = nextCursor
	}

	f.log.Debug("fetched thread history", "thread", threadTS, "messages", len(allMessages))
	if len(allMessages) == 0 {
		f.log.Warn("fetchThreadHistory: no messages found in thread", "thread_ts", threadTS)
	}
	return allMessages, nil
}

// stripMarkdown removes markdown formatting from text, converting it to plain text
func stripMarkdown(text string) string {
	text = regexp.MustCompile("(?s)```[a-zA-Z]*\\n?.*?```").ReplaceAllString(text, "")
	text = regexp.MustCompile("`[^`]+`").ReplaceAllString(text, "")
	text = regexp.MustCompile(`\[([^\]]+)\]\([^\)]+\)`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`\*\*([^\*]+)\*\*`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`\*([^\*]+)\*`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`__([^_]+)__`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`_([^_]+)_`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`^#{1,6}\s+`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`~~([^~]+)~~`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`\n\s*\n\s*\n`).ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)
	return text
}
