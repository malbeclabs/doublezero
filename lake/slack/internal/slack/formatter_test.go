package slack

import (
	"log/slog"
	"strings"
	"testing"

	slackapi "github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

func TestAI_Slack_ConvertMarkdownToMrkdwn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "bold with double asterisks",
			input:    "This is **bold** text",
			expected: "This is *bold* text",
		},
		{
			name:     "bold with underscores",
			input:    "This is __bold__ text",
			expected: "This is *bold* text",
		},
		{
			name:     "strikethrough",
			input:    "This is ~~deleted~~ text",
			expected: "This is ~deleted~ text",
		},
		{
			name:     "link conversion",
			input:    "Check out [Google](https://google.com)",
			expected: "Check out <https://google.com|Google>",
		},
		{
			name:     "header conversion",
			input:    "### My Header",
			expected: "*My Header*",
		},
		{
			name:     "inline code preserved",
			input:    "Use `code` here",
			expected: "Use `code` here",
		},
		{
			name:     "list with bold items",
			input:    "- **Item 1**\n- **Item 2**",
			expected: "- *Item 1*\n- *Item 2*",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertMarkdownToMrkdwn(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

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

func TestAI_Slack_ConvertMarkdownToBlocks_NestedLists(t *testing.T) {
	t.Parallel()

	t.Run("nested list items are preserved", func(t *testing.T) {
		t.Parallel()
		input := `### Current Connection Summary

- **100 validators currently connected** to DZ infrastructure
- **Top validators by stake**:
  - validator1: 15.6M SOL
  - validator2: 14.0M SOL
  - validator3: 12.3M SOL`

		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		require.NotNil(t, blocks)
		require.Greater(t, len(blocks), 0)

		// Verify nested list items are present in the output
		foundNestedItems := false
		for _, block := range blocks {
			if block.BlockType() == "section" {
				sectionBlock := block.(*slackapi.SectionBlock)
				if sectionBlock.Text != nil {
					text := sectionBlock.Text.Text
					// Check that nested items are present
					if strings.Contains(text, "validator1") &&
						strings.Contains(text, "validator2") &&
						strings.Contains(text, "validator3") {
						foundNestedItems = true
					}
				}
			}
		}
		require.True(t, foundNestedItems, "nested list items should be preserved in output")
	})

	t.Run("simple list without nesting uses rich text blocks", func(t *testing.T) {
		t.Parallel()
		input := `- Item 1
- Item 2
- Item 3`

		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		require.NotNil(t, blocks)
		require.Greater(t, len(blocks), 0)
	})

	t.Run("containsNestedList detects indented lists", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			input    string
			expected bool
		}{
			{
				name:     "simple list",
				input:    "- Item 1\n- Item 2",
				expected: false,
			},
			{
				name:     "nested list with spaces",
				input:    "- Item 1\n  - Nested item",
				expected: true,
			},
			{
				name:     "nested list with tab",
				input:    "- Item 1\n\t- Nested item",
				expected: true,
			},
			{
				name:     "multiple levels",
				input:    "- Item\n  - Level 2\n    - Level 3",
				expected: true,
			},
			{
				name:     "no list",
				input:    "Just some text\nwith newlines",
				expected: false,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				result := containsNestedList(tt.input)
				require.Equal(t, tt.expected, result)
			})
		}
	})
}

func TestAI_Slack_ConvertMarkdownToBlocks_CodeBlocks(t *testing.T) {
	t.Parallel()

	t.Run("multi-line code block stays together", func(t *testing.T) {
		t.Parallel()
		input := "Here's a query:\n```sql\nSELECT *\nFROM users\nWHERE active = true;\n```\nThis is the result."

		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		// Verify we got blocks
		require.NotNil(t, blocks)
		require.Greater(t, len(blocks), 0)

		// Find the code block - it should contain the full SQL query
		// Note: language specifier (sql) is stripped since Slack doesn't support it
		foundCodeBlock := false
		for _, block := range blocks {
			if block.BlockType() == "section" {
				sectionBlock := block.(*slackapi.SectionBlock)
				if sectionBlock.Text != nil && strings.Contains(sectionBlock.Text.Text, "SELECT *") {
					// Verify the entire code block is in one section
					require.Contains(t, sectionBlock.Text.Text, "```")
					require.Contains(t, sectionBlock.Text.Text, "SELECT *")
					require.Contains(t, sectionBlock.Text.Text, "FROM users")
					require.Contains(t, sectionBlock.Text.Text, "WHERE active = true")
					// Language specifier should be stripped
					require.NotContains(t, sectionBlock.Text.Text, "sql")
					foundCodeBlock = true
				}
			}
		}
		require.True(t, foundCodeBlock, "should find a code block section")
	})

	t.Run("code block without language specifier", func(t *testing.T) {
		t.Parallel()
		input := "Example:\n```\nline 1\nline 2\nline 3\n```"

		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		require.NotNil(t, blocks)
		require.Greater(t, len(blocks), 0)

		// Find the code block
		foundCodeBlock := false
		for _, block := range blocks {
			if block.BlockType() == "section" {
				sectionBlock := block.(*slackapi.SectionBlock)
				if sectionBlock.Text != nil && strings.Contains(sectionBlock.Text.Text, "```") {
					require.Contains(t, sectionBlock.Text.Text, "line 1")
					require.Contains(t, sectionBlock.Text.Text, "line 2")
					require.Contains(t, sectionBlock.Text.Text, "line 3")
					foundCodeBlock = true
				}
			}
		}
		require.True(t, foundCodeBlock, "should find a code block section")
	})

	t.Run("multiple code blocks", func(t *testing.T) {
		t.Parallel()
		input := "First:\n```\ncode1\n```\nSecond:\n```\ncode2\n```"

		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		require.NotNil(t, blocks)

		// Count code blocks
		codeBlockCount := 0
		for _, block := range blocks {
			if block.BlockType() == "section" {
				sectionBlock := block.(*slackapi.SectionBlock)
				if sectionBlock.Text != nil && strings.Contains(sectionBlock.Text.Text, "```") {
					codeBlockCount++
				}
			}
		}
		require.Equal(t, 2, codeBlockCount, "should have 2 code blocks")
	})

	t.Run("text without code blocks", func(t *testing.T) {
		t.Parallel()
		input := "Just some plain text without any code blocks."

		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		// Should still work normally
		require.NotNil(t, blocks)
	})
}
