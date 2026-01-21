package slack

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_ai_slack_build_info",
			Help: "Build information of the DoubleZero AI Slack bot",
		},
		[]string{"version", "commit", "date"},
	)

	EventsReceivedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_ai_slack_events_received_total",
			Help: "Total number of Slack events received",
		},
		[]string{"event_type", "inner_event_type"},
	)

	EventsDuplicateTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "doublezero_ai_slack_events_duplicate_total",
			Help: "Total number of duplicate events skipped",
		},
	)

	MessagesProcessedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_ai_slack_messages_processed_total",
			Help: "Total number of messages processed",
		},
		[]string{"channel_type", "is_dm", "is_channel"},
	)

	MessagesIgnoredTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_ai_slack_messages_ignored_total",
			Help: "Total number of messages ignored",
		},
		[]string{"reason"},
	)

	MessageProcessingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "doublezero_ai_slack_message_processing_duration_seconds",
			Help:    "Duration of message processing",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 12), // 0.1s to ~205s (~3.4 minutes)
		},
		[]string{"effort_mode"},
	)

	AgentRounds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "doublezero_ai_slack_agent_rounds",
			Help:    "Number of rounds taken to generate a response",
			Buckets: []float64{1, 2, 3, 4, 5, 6, 8, 10, 12, 15, 20, 25, 30, 40, 50},
		},
		[]string{"effort_mode"},
	)

	MessagesPostedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_ai_slack_messages_posted_total",
			Help: "Total number of messages posted to Slack",
		},
		[]string{"status", "effort_mode"},
	)

	AgentErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_ai_slack_agent_errors_total",
			Help: "Total number of agent errors",
		},
		[]string{"error_type", "effort_mode"},
	)

	SlackAPIErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_ai_slack_api_errors_total",
			Help: "Total number of Slack API errors",
		},
		[]string{"operation"},
	)

	ConversationHistoryErrorsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "doublezero_ai_slack_conversation_history_errors_total",
			Help: "Total number of conversation history fetch errors",
		},
	)

	ActiveConversations = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "doublezero_ai_slack_active_conversations",
			Help: "Number of active conversations",
		},
	)
)
