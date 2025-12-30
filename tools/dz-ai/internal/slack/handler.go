package slack

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const processedEventsMaxAge = 1 * time.Hour

// EventHandler handles Slack events
type EventHandler struct {
	slackClient *Client
	processor   *Processor
	convManager *Manager
	log         *slog.Logger
	botUserID   string
	shutdownCtx context.Context // Main shutdown context for graceful cancellation

	// Track processed events by envelope ID to avoid reprocessing duplicates
	processedEvents   map[string]time.Time
	processedEventsMu sync.RWMutex

	// Graceful shutdown coordination
	inFlightOps  sync.WaitGroup // Tracks in-flight message processing operations
	shuttingDown sync.RWMutex   // Protects shutdown flag
	acceptingNew bool           // Whether we're still accepting new events
}

// NewEventHandler creates a new event handler
func NewEventHandler(
	slackClient *Client,
	processor *Processor,
	convManager *Manager,
	log *slog.Logger,
	botUserID string,
	shutdownCtx context.Context,
) *EventHandler {
	return &EventHandler{
		slackClient:     slackClient,
		processor:       processor,
		convManager:     convManager,
		log:             log,
		botUserID:       botUserID,
		shutdownCtx:     shutdownCtx,
		processedEvents: make(map[string]time.Time),
		acceptingNew:    true,
	}
}

// StartCleanup starts a background goroutine to clean up old processed events
func (h *EventHandler) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.cleanup()
			}
		}
	}()
}

// StopAcceptingNew stops accepting new events and returns a function to wait for in-flight operations
func (h *EventHandler) StopAcceptingNew() func() {
	h.shuttingDown.Lock()
	h.acceptingNew = false
	h.shuttingDown.Unlock()
	h.log.Info("stopped accepting new events, waiting for in-flight operations to complete")
	return h.inFlightOps.Wait
}

// isAcceptingNew checks if we're still accepting new events
func (h *EventHandler) isAcceptingNew() bool {
	h.shuttingDown.RLock()
	defer h.shuttingDown.RUnlock()
	return h.acceptingNew
}

func (h *EventHandler) cleanup() {
	now := time.Now()
	h.processedEventsMu.Lock()
	for id, timestamp := range h.processedEvents {
		if now.Sub(timestamp) > processedEventsMaxAge {
			delete(h.processedEvents, id)
		}
	}
	h.processedEventsMu.Unlock()
}

// HandleEvent handles a Slack Events API event
func (h *EventHandler) HandleEvent(ctx context.Context, e slackevents.EventsAPIEvent, eventID string) {
	h.log.Info("event received", "type", e.Type, "inner_event_type", e.InnerEvent.Type)
	EventsReceivedTotal.WithLabelValues(e.Type, e.InnerEvent.Type).Inc()
	if e.Type != slackevents.CallbackEvent {
		h.log.Debug("ignoring non-callback event", "type", e.Type)
		return
	}

	// Handle app_mentions event (more reliable for channel mentions)
	if e.InnerEvent.Type == "app_mention" {
		if ev, ok := e.InnerEvent.Data.(*slackevents.AppMentionEvent); ok {
			h.handleAppMention(ctx, ev, eventID)
			return
		}
	}

	// Handle message events
	switch ev := e.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		h.handleMessageEvent(ctx, ev, eventID)
	}
}

// handleAppMention handles app_mention events
func (h *EventHandler) handleAppMention(ctx context.Context, ev *slackevents.AppMentionEvent, eventID string) {
	h.log.Info("app_mention event received", "user", ev.User, "channel", ev.Channel, "ts", ev.TimeStamp, "thread_ts", ev.ThreadTimeStamp, "text_preview", TruncateString(ev.Text, 100))

	// AppMentionEvent is always from a channel (not DM), so determine channel type from channel ID
	isChannel := true

	// Mark this thread as active only if it's a top-level message (not a reply in an existing thread)
	if ev.ThreadTimeStamp == "" {
		threadKey := ev.TimeStamp
		h.convManager.MarkThreadActive(ev.Channel, threadKey)
		h.log.Debug("marked thread as active from app_mention (root message)", "thread_key", fmt.Sprintf("%s:%s", ev.Channel, threadKey), "message_ts", ev.TimeStamp)
	} else {
		h.log.Debug("app_mention in existing thread, not marking as active (only root mentions activate threads)", "thread_ts", ev.ThreadTimeStamp)
	}

	// Convert AppMentionEvent to MessageEvent-like structure for processing
	msgEv := &slackevents.MessageEvent{
		Type:            "message",
		User:            ev.User,
		Text:            ev.Text,
		TimeStamp:       ev.TimeStamp,
		ThreadTimeStamp: ev.ThreadTimeStamp,
		Channel:         ev.Channel,
		EventTimeStamp:  ev.EventTimeStamp,
	}

	messageKey := fmt.Sprintf("%s:%s", msgEv.Channel, msgEv.TimeStamp)
	if h.processor.HasResponded(messageKey) {
		h.log.Info("skipping already responded app_mention", "message_ts", msgEv.TimeStamp, "event_id", eventID)
		MessagesIgnoredTotal.WithLabelValues("already_responded").Inc()
		return
	}

	// Mark as responded BEFORE starting goroutine to prevent race condition
	h.processor.MarkResponded(messageKey)

	// Track metrics
	MessagesProcessedTotal.WithLabelValues("channel", "false", "true").Inc()

	// Track in-flight operation for graceful shutdown
	// Always use background context for message processing to allow operations to complete
	// during graceful shutdown. The WaitGroup handles shutdown coordination.
	h.inFlightOps.Add(1)
	go func() {
		defer h.inFlightOps.Done()
		// Use background context so shutdown cancellation doesn't interrupt in-flight operations
		h.processor.ProcessMessage(context.Background(), msgEv, messageKey, eventID, isChannel)
	}()
}

// handleMessageEvent handles message events
func (h *EventHandler) handleMessageEvent(ctx context.Context, ev *slackevents.MessageEvent, eventID string) {
	if ev.SubType != "" {
		h.log.Debug("ignoring message with subtype", "subtype", ev.SubType, "channel", ev.Channel, "channel_type", ev.ChannelType)
		MessagesIgnoredTotal.WithLabelValues("subtype").Inc()
		return
	} // ignore edits/joins/etc
	if ev.BotID != "" {
		h.log.Debug("ignoring bot message", "bot_id", ev.BotID, "channel", ev.Channel)
		MessagesIgnoredTotal.WithLabelValues("bot_message").Inc()
		return
	} // avoid loops
	txt := strings.TrimSpace(ev.Text)
	if txt == "" {
		h.log.Debug("ignoring empty message", "channel", ev.Channel, "channel_type", ev.ChannelType)
		MessagesIgnoredTotal.WithLabelValues("empty").Inc()
		return
	}

	isDM := ev.ChannelType == "im"
	isChannel := ev.ChannelType == "channel" || ev.ChannelType == "group" || ev.ChannelType == "mpim"

	h.log.Info("message event received", "channel", ev.Channel, "channel_type", ev.ChannelType, "is_dm", isDM, "is_channel", isChannel, "thread_ts", ev.ThreadTimeStamp, "text_preview", TruncateString(txt, 100), "bot_user_id", h.botUserID)

	// For channels, respond if bot is mentioned OR if replying in an active thread (where bot was mentioned in root message)
	if isChannel {
		botMentioned := h.slackClient.IsBotMentioned(ev.Text)

		// If bot is mentioned in a top-level message, skip this message event - app_mention event will handle it instead
		if botMentioned && ev.ThreadTimeStamp == "" {
			h.log.Debug("skipping message event with bot mention in top-level message (app_mention event will handle it)", "channel", ev.Channel, "message_ts", ev.TimeStamp)
			return
		}

		h.log.Info("processing channel message", "channel", ev.Channel, "thread_ts", ev.ThreadTimeStamp, "is_thread_reply", ev.ThreadTimeStamp != "", "bot_mentioned", botMentioned)

		inActiveThread := false
		if ev.ThreadTimeStamp != "" {
			h.log.Info("checking if message is in active thread", "thread_ts", ev.ThreadTimeStamp, "channel", ev.Channel, "message_ts", ev.TimeStamp)
			// First check in-memory cache
			inActiveThread = h.convManager.IsThreadActive(ev.Channel, ev.ThreadTimeStamp)

			if inActiveThread {
				h.log.Info("thread found in cache (active)", "thread_ts", ev.ThreadTimeStamp, "channel", ev.Channel)
			} else {
				h.log.Info("thread not in cache, checking root message", "thread_ts", ev.ThreadTimeStamp, "channel", ev.Channel)
			}

			// If not in cache, check the root message to see if bot was mentioned there
			if !inActiveThread && h.botUserID != "" {
				h.log.Info("fetching root message to check for bot mention", "thread_ts", ev.ThreadTimeStamp, "bot_user_id", h.botUserID)
				rootMentioned, err := h.slackClient.CheckRootMessageMentioned(ctx, ev.Channel, ev.ThreadTimeStamp, h.botUserID)
				if err != nil {
					h.log.Warn("failed to check root message for mention", "thread_ts", ev.ThreadTimeStamp, "error", err)
				} else if rootMentioned {
					// Mark as active for future checks
					h.convManager.MarkThreadActive(ev.Channel, ev.ThreadTimeStamp)
					inActiveThread = true
					h.log.Info("root message mentions bot, marking thread as active", "thread_ts", ev.ThreadTimeStamp, "channel", ev.Channel)
				} else {
					h.log.Info("root message does not mention bot", "thread_ts", ev.ThreadTimeStamp, "channel", ev.Channel)
				}
			}

			if inActiveThread {
				h.log.Info("message in active thread (root was mentioned)", "thread_ts", ev.ThreadTimeStamp, "channel", ev.Channel)
			}
		}

		if !botMentioned && !inActiveThread {
			h.log.Debug("bot not mentioned and not in active thread, ignoring", "channel", ev.Channel, "channel_type", ev.ChannelType, "thread_ts", ev.ThreadTimeStamp, "text_preview", TruncateString(txt, 50))
			MessagesIgnoredTotal.WithLabelValues("not_mentioned").Inc()
			return // Not mentioned and not in active thread, ignore
		}

		// If bot was mentioned in a top-level message (not a thread reply), mark this thread as active
		if botMentioned && ev.ThreadTimeStamp == "" {
			threadKey := ev.TimeStamp
			h.convManager.MarkThreadActive(ev.Channel, threadKey)
			h.log.Debug("marked thread as active (root message mentioned)", "thread_key", fmt.Sprintf("%s:%s", ev.Channel, threadKey), "message_ts", ev.TimeStamp)
		}

		if botMentioned {
			h.log.Info("channel mention recv", "user", ev.User, "channel", ev.Channel, "channel_type", ev.ChannelType, "ts", ev.TimeStamp, "thread_ts", ev.ThreadTimeStamp, "text", txt, "event_id", eventID)
		} else {
			h.log.Info("channel thread reply recv (root was mentioned)", "user", ev.User, "channel", ev.Channel, "channel_type", ev.ChannelType, "ts", ev.TimeStamp, "thread_ts", ev.ThreadTimeStamp, "text", txt, "event_id", eventID)
		}
	} else if isDM {
		h.log.Info("dm recv", "user", ev.User, "channel", ev.Channel, "ts", ev.TimeStamp, "thread_ts", ev.ThreadTimeStamp, "text", txt, "event_id", eventID)
	} else {
		// Unknown channel type, skip
		MessagesIgnoredTotal.WithLabelValues("unknown_channel_type").Inc()
		return
	}

	// Check if we've already responded to this message (prevent duplicate error messages)
	messageKey := fmt.Sprintf("%s:%s", ev.Channel, ev.TimeStamp)
	if h.processor.HasResponded(messageKey) {
		h.log.Info("skipping already responded message", "message_ts", ev.TimeStamp, "event_id", eventID)
		MessagesIgnoredTotal.WithLabelValues("already_responded").Inc()
		return
	}

	// Mark as responded BEFORE starting goroutine to prevent race condition
	h.processor.MarkResponded(messageKey)

	// Track metrics
	channelType := ev.ChannelType
	if channelType == "" {
		channelType = "unknown"
	}
	MessagesProcessedTotal.WithLabelValues(channelType, fmt.Sprintf("%v", isDM), fmt.Sprintf("%v", isChannel)).Inc()

	// Track in-flight operation for graceful shutdown
	// Always use background context for message processing to allow operations to complete
	// during graceful shutdown. The WaitGroup handles shutdown coordination.
	h.inFlightOps.Add(1)
	go func() {
		defer h.inFlightOps.Done()
		// Use background context so shutdown cancellation doesn't interrupt in-flight operations
		h.processor.ProcessMessage(context.Background(), ev, messageKey, eventID, isChannel)
	}()
}

// HandleSocketMode handles events from Socket Mode
func (h *EventHandler) HandleSocketMode(ctx context.Context, client *socketmode.Client) error {
	h.log.Info("bot running in socket mode (DMs and channel mentions, thread replies enabled)")

	for {
		select {
		case <-ctx.Done():
			h.log.Info("shutting down socket mode handler", "error", ctx.Err())
			return ctx.Err()
		case evt, ok := <-client.Events:
			if !ok {
				h.log.Info("socket mode client events channel closed")
				return nil
			}
			// Check if we're still accepting new events
			if !h.isAcceptingNew() {
				h.log.Info("not accepting new events, shutting down")
				return ctx.Err()
			}
			h.log.Info("socketmode: event received", "event_type", evt.Type)
			switch evt.Type {
			case socketmode.EventTypeConnecting:
				h.log.Info("socketmode: connecting")
			case socketmode.EventTypeConnected:
				h.log.Info("socketmode: connected")
			case socketmode.EventTypeConnectionError:
				h.log.Error("socketmode: connection error", "error", evt.Data)
			case socketmode.EventTypeEventsAPI:
				h.log.Info("socketmode: EventsAPI event received", "inner_event_type", func() string {
					if e, ok := evt.Data.(slackevents.EventsAPIEvent); ok {
						return e.InnerEvent.Type
					}
					return "unknown"
				}())
				e, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					h.log.Warn("socketmode: EventsAPI event data is not EventsAPIEvent", "data_type", fmt.Sprintf("%T", evt.Data))
					continue
				}

				h.log.Info("socketmode: processing EventsAPI event", "event_type", e.Type, "inner_event_type", e.InnerEvent.Type)

				// Check if we've already processed this event (deduplication)
				envelopeID := evt.Request.EnvelopeID
				retryAttempt := evt.Request.RetryAttempt
				retryReason := evt.Request.RetryReason

				if envelopeID != "" {
					h.processedEventsMu.RLock()
					_, alreadyProcessed := h.processedEvents[envelopeID]
					h.processedEventsMu.RUnlock()

					if alreadyProcessed {
						h.log.Info("skipping duplicate event", "envelope_id", envelopeID, "retry_attempt", retryAttempt, "retry_reason", retryReason)
						EventsDuplicateTotal.Inc()
						client.Ack(*evt.Request)
						continue
					}

					// Mark as processed BEFORE processing to prevent race conditions
					h.processedEventsMu.Lock()
					h.processedEvents[envelopeID] = time.Now()
					h.processedEventsMu.Unlock()

					if retryAttempt > 0 {
						h.log.Info("processing retried event", "envelope_id", envelopeID, "retry_attempt", retryAttempt, "retry_reason", retryReason)
					}
				}

				client.Ack(*evt.Request)
				// Use background context so shutdown cancellation doesn't interrupt in-flight operations
				// The WaitGroup handles graceful shutdown coordination
				h.HandleEvent(context.Background(), e, envelopeID)
			}
		}
	}
}

// HandleHTTP handles HTTP requests from Slack Events API
func (h *EventHandler) HandleHTTP(w http.ResponseWriter, r *http.Request, signingSecret string) {
	h.log.Info("HTTP event request received", "method", r.Method, "path", r.URL.Path, "content_type", r.Header.Get("Content-Type"))
	if r.Method != http.MethodPost {
		h.log.Warn("HTTP event: method not allowed", "method", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.log.Error("failed to read request body", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Verify request signature
	if !VerifySlackSignature(r, body, signingSecret) {
		h.log.Warn("invalid Slack signature")
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
		h.log.Info("responding to URL verification challenge")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(challengeResp.Challenge)); err != nil {
			h.log.Error("failed to write challenge response", "error", err)
		}
		return
	}

	// Parse event
	event, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	if err != nil {
		h.log.Error("failed to parse event", "error", err)
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
		h.processedEventsMu.RLock()
		_, alreadyProcessed := h.processedEvents[eventID]
		h.processedEventsMu.RUnlock()

		if alreadyProcessed {
			h.log.Info("skipping duplicate event", "event_id", eventID)
			EventsDuplicateTotal.Inc()
			w.WriteHeader(http.StatusOK)
			return
		}

		// Mark as processed BEFORE processing to prevent race conditions
		h.processedEventsMu.Lock()
		h.processedEvents[eventID] = time.Now()
		h.processedEventsMu.Unlock()
	}

	// Check if we're still accepting new events
	if !h.isAcceptingNew() {
		h.log.Info("not accepting new events, returning 503")
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte("Service is shutting down")); err != nil {
			h.log.Error("failed to write shutdown response", "error", err)
		}
		return
	}

	// Respond quickly to Slack (within 3 seconds)
	w.WriteHeader(http.StatusOK)

	// Process event asynchronously
	// Always use background context so shutdown cancellation doesn't interrupt in-flight operations
	// The WaitGroup handles graceful shutdown coordination
	go h.HandleEvent(context.Background(), event, eventID)
}
