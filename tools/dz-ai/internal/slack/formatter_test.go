package slack

import (
	"log/slog"
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

func TestAI_Slack_SanitizeErrorMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		errMsg   string
		want     string
		contains string
	}{
		{
			name:     "rate limit error",
			errMsg:   "rate_limit_error: too many requests",
			want:     "I'm currently experiencing high demand. Please try again in a moment.",
			contains: "",
		},
		{
			name:     "rate limit 429",
			errMsg:   "HTTP 429: rate limit exceeded",
			want:     "I'm currently experiencing high demand. Please try again in a moment.",
			contains: "",
		},
		{
			name:     "connection closed",
			errMsg:   "connection closed by peer",
			want:     "I'm having trouble connecting to the data service. Please try again in a moment.",
			contains: "",
		},
		{
			name:     "EOF error",
			errMsg:   "EOF error occurred",
			want:     "I'm having trouble connecting to the data service. Please try again in a moment.",
			contains: "",
		},
		{
			name:     "failed to get tools",
			errMsg:   "failed to get tools from server",
			want:     "I'm having trouble connecting to the data service. Please try again in a moment.",
			contains: "",
		},
		{
			name:     "generic API error",
			errMsg:   "failed to get response from API",
			want:     "I encountered an error processing your request. Please try again.",
			contains: "",
		},
		{
			name:     "error with internal details",
			errMsg:   "Error occurred\nRequest-ID: abc123\nhttps://api.example.com/error\nActual error message",
			want:     "Sorry, I encountered an error: Error occurred Actual error message",
			contains: "",
		},
		{
			name:     "error with only internal details",
			errMsg:   "Request-ID: abc123\nhttps://api.example.com/error\nPOST \"https://api.example.com\"",
			want:     "I encountered an error processing your request. Please try again.",
			contains: "",
		},
		{
			name:     "generic error",
			errMsg:   "something went wrong",
			want:     "Sorry, I encountered an error: something went wrong",
			contains: "",
		},
		{
			name:     "empty error",
			errMsg:   "",
			want:     "Sorry, I encountered an error. Please try again.",
			contains: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := SanitizeErrorMessage(tt.errMsg)
			if tt.contains != "" {
				require.Contains(t, got, tt.contains)
			} else {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestAI_Slack_SetExpandOnSectionBlocks(t *testing.T) {
	t.Parallel()

	t.Run("nil blocks", func(t *testing.T) {
		t.Parallel()
		got := SetExpandOnSectionBlocks(nil, slog.Default())
		require.Nil(t, got)
	})

	t.Run("empty blocks", func(t *testing.T) {
		t.Parallel()
		got := SetExpandOnSectionBlocks(nil, slog.Default())
		require.Nil(t, got)
	})
}

func TestAI_Slack_UsedISISTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		toolsUsed []string
		want      bool
	}{
		{
			name:      "nil tools",
			toolsUsed: nil,
			want:      false,
		},
		{
			name:      "empty tools",
			toolsUsed: []string{},
			want:      false,
		},
		{
			name:      "no ISIS tools",
			toolsUsed: []string{"query_data", "get_schema", "memvid_search"},
			want:      false,
		},
		{
			name:      "isis_refresh tool",
			toolsUsed: []string{"query_data", "isis_refresh"},
			want:      true,
		},
		{
			name:      "isis_lookup tool",
			toolsUsed: []string{"isis_lookup", "get_schema"},
			want:      true,
		},
		{
			name:      "multiple ISIS tools",
			toolsUsed: []string{"isis_refresh", "isis_lookup", "isis_neighbors"},
			want:      true,
		},
		{
			name:      "ISIS tool only",
			toolsUsed: []string{"isis_refresh"},
			want:      true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := UsedISISTools(tt.toolsUsed)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestAI_Slack_AppendISISCitation(t *testing.T) {
	t.Parallel()

	t.Run("nil blocks", func(t *testing.T) {
		t.Parallel()
		got := AppendISISCitation(nil)
		require.Len(t, got, 1)
		require.Equal(t, slack.MBTContext, got[0].BlockType())
	})

	t.Run("empty blocks", func(t *testing.T) {
		t.Parallel()
		got := AppendISISCitation([]slack.Block{})
		require.Len(t, got, 1)
		require.Equal(t, slack.MBTContext, got[0].BlockType())
	})

	t.Run("append to existing blocks", func(t *testing.T) {
		t.Parallel()
		existing := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "Hello", false, false),
				nil, nil,
			),
		}
		got := AppendISISCitation(existing)
		require.Len(t, got, 2)
		require.Equal(t, slack.MBTSection, got[0].BlockType())
		require.Equal(t, slack.MBTContext, got[1].BlockType())
	})

	t.Run("context block contains ISIS verification text", func(t *testing.T) {
		t.Parallel()
		got := AppendISISCitation(nil)
		require.Len(t, got, 1)
		contextBlock, ok := got[0].(*slack.ContextBlock)
		require.True(t, ok, "expected context block")
		require.Len(t, contextBlock.ContextElements.Elements, 1)
		textObj, ok := contextBlock.ContextElements.Elements[0].(*slack.TextBlockObject)
		require.True(t, ok, "expected text block object")
		require.Contains(t, textObj.Text, "Verified against ISIS topology")
		require.Contains(t, textObj.Text, ":mag:")
	})
}
