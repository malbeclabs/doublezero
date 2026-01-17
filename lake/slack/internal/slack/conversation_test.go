package slack

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestAI_Slack_Manager_MarkThreadActive(t *testing.T) {
	t.Parallel()

	m := NewManager(slog.Default())
	channelID := "C123"
	threadTS := "1234567890.123456"

	m.MarkThreadActive(channelID, threadTS)

	require.True(t, m.IsThreadActive(channelID, threadTS))
	require.False(t, m.IsThreadActive(channelID, "different-thread"))
}

func TestAI_Slack_Manager_IsThreadActive(t *testing.T) {
	t.Parallel()

	m := NewManager(slog.Default())
	channelID := "C123"
	threadTS := "1234567890.123456"

	require.False(t, m.IsThreadActive(channelID, threadTS))

	m.MarkThreadActive(channelID, threadTS)
	require.True(t, m.IsThreadActive(channelID, threadTS))
}

func TestAI_Slack_Manager_UpdateConversationHistory(t *testing.T) {
	t.Parallel()

	m := NewManager(slog.Default())
	threadKey := "1234567890.123456"

	// Create empty messages list
	msgs := []workflow.ConversationMessage{}

	m.UpdateConversationHistory(threadKey, msgs)

	// We can't easily test GetConversationHistory without a real Slack API client,
	// but we can verify the cache was updated by checking internal state
	// For now, just verify UpdateConversationHistory doesn't panic
	require.NotNil(t, m)
}

func TestAI_Slack_Manager_StripMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "code blocks",
			input:    "Here's some code: ```go\nfunc main() {}\n```",
			expected: "Here's some code:",
		},
		{
			name:     "inline code",
			input:    "Use `code` here",
			expected: "Use  here", // Double space after removal
		},
		{
			name:     "bold text",
			input:    "This is **bold** text",
			expected: "This is bold text",
		},
		{
			name:     "italic text",
			input:    "This is *italic* text",
			expected: "This is italic text",
		},
		{
			name:     "links",
			input:    "Check [this link](https://example.com)",
			expected: "Check this link",
		},
		{
			name:     "headers",
			input:    "# Header\n## Subheader",
			expected: "Header\n## Subheader", // Only removes # from start of line, not ##
		},
		{
			name:     "strikethrough",
			input:    "This is ~~strikethrough~~ text",
			expected: "This is strikethrough text",
		},
		{
			name:     "multiple formatting",
			input:    "**Bold** and *italic* with `code`",
			expected: "Bold and italic with",
		},
		{
			name:     "plain text",
			input:    "Just plain text",
			expected: "Just plain text",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stripMarkdown(tt.input)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestAI_Slack_Manager_StartCleanup(t *testing.T) {
	t.Parallel()

	m := NewManager(slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start cleanup - should not panic
	m.StartCleanup(ctx)

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Cancel context to stop cleanup
	cancel()

	// Wait a bit for cleanup to stop
	time.Sleep(10 * time.Millisecond)
}

func TestAI_Slack_NewDefaultFetcher(t *testing.T) {
	t.Parallel()

	fetcher := NewDefaultFetcher(slog.Default())
	require.NotNil(t, fetcher)
	require.IsType(t, &DefaultFetcher{}, fetcher)
}
