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

	t.Run("code block preserved as single block", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```go\nfunc main() {\n    fmt.Println(\"hello\")\n}\n```", false, false),
				nil, nil,
			),
		}
		got := SetExpandOnSectionBlocks(blocks, slog.Default())
		require.Len(t, got, 1)
		sectionBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.True(t, sectionBlock.Expand)
		require.Contains(t, sectionBlock.Text.Text, "```")
	})

	t.Run("code block with regular text", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "Here's some code:", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```go\nfunc main() {}\n```", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "That's the code.", false, false),
				nil, nil,
			),
		}
		got := SetExpandOnSectionBlocks(blocks, slog.Default())
		require.Len(t, got, 3)
		// Code block should be preserved
		sectionBlock, ok := got[1].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, sectionBlock.Text.Text, "```")
	})
}

func TestAI_Slack_mergeCodeBlocks(t *testing.T) {
	t.Parallel()

	t.Run("empty blocks", func(t *testing.T) {
		t.Parallel()
		got := mergeCodeBlocks([]slack.Block{})
		require.Empty(t, got)
	})

	t.Run("nil blocks", func(t *testing.T) {
		t.Parallel()
		got := mergeCodeBlocks(nil)
		require.Nil(t, got)
	})

	t.Run("code block split across multiple blocks - opening marker in first", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```go", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "func main() {", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "    fmt.Println(\"hello\")", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "}", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```", false, false),
				nil, nil,
			),
		}
		got := mergeCodeBlocks(blocks)
		require.Len(t, got, 1, "code block should be merged into single block")
		sectionBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, sectionBlock.Text.Text, "```go")
		require.Contains(t, sectionBlock.Text.Text, "func main()")
		require.Contains(t, sectionBlock.Text.Text, "fmt.Println")
		require.Contains(t, sectionBlock.Text.Text, "```")
		require.True(t, sectionBlock.Expand)
	})

	t.Run("code block split with language tag", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```python", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "def hello():", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "    print('world')", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```", false, false),
				nil, nil,
			),
		}
		got := mergeCodeBlocks(blocks)
		require.Len(t, got, 1)
		sectionBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, sectionBlock.Text.Text, "```python")
		require.Contains(t, sectionBlock.Text.Text, "def hello()")
		require.Contains(t, sectionBlock.Text.Text, "print('world')")
		require.Contains(t, sectionBlock.Text.Text, "```")
	})

	t.Run("multiple code blocks separated by text", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```go\nfunc a() {}\n```", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "Some text in between", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```python", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "def b():", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "    pass", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```", false, false),
				nil, nil,
			),
		}
		got := mergeCodeBlocks(blocks)
		require.Len(t, got, 3, "should have first code block, text, and merged second code block")
		// First block should be the complete first code block
		firstBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, firstBlock.Text.Text, "```go")
		// Second block should be the text
		secondBlock, ok := got[1].(*slack.SectionBlock)
		require.True(t, ok)
		require.Equal(t, "Some text in between", secondBlock.Text.Text)
		// Third block should be the merged second code block
		thirdBlock, ok := got[2].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, thirdBlock.Text.Text, "```python")
		require.Contains(t, thirdBlock.Text.Text, "def b()")
		require.Contains(t, thirdBlock.Text.Text, "pass")
		require.Contains(t, thirdBlock.Text.Text, "```")
	})

	t.Run("code block with empty lines", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```go", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "func test() {", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "    return true", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "}", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```", false, false),
				nil, nil,
			),
		}
		got := mergeCodeBlocks(blocks)
		require.Len(t, got, 1)
		sectionBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, sectionBlock.Text.Text, "```go")
		require.Contains(t, sectionBlock.Text.Text, "func test()")
		require.Contains(t, sectionBlock.Text.Text, "return true")
		require.Contains(t, sectionBlock.Text.Text, "```")
	})

	t.Run("non-section blocks interrupt code block", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```go", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "func test() {}", false, false),
				nil, nil,
			),
			slack.NewContextBlock("", slack.NewTextBlockObject(slack.PlainTextType, "Context block", false, false)),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```", false, false),
				nil, nil,
			),
		}
		got := mergeCodeBlocks(blocks)
		require.Len(t, got, 3, "code block should be flushed before context, then closing marker becomes new block")
		// First block should be the merged code block up to the context
		firstBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, firstBlock.Text.Text, "```go")
		require.Contains(t, firstBlock.Text.Text, "func test()")
		// Second block should be the context block
		require.Equal(t, slack.MBTContext, got[1].BlockType())
		// Third block should be the closing marker (treated as new incomplete code block)
		thirdBlock, ok := got[2].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, thirdBlock.Text.Text, "```")
	})

	t.Run("single-line code block", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```go\nfunc main() {}\n```", false, false),
				nil, nil,
			),
		}
		got := mergeCodeBlocks(blocks)
		require.Len(t, got, 1)
		sectionBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, sectionBlock.Text.Text, "```go")
		require.Contains(t, sectionBlock.Text.Text, "func main()")
		require.Contains(t, sectionBlock.Text.Text, "```")
	})

	t.Run("unclosed code block", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```go", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "func main() {}", false, false),
				nil, nil,
			),
		}
		got := mergeCodeBlocks(blocks)
		require.Len(t, got, 1, "unclosed code block should still be merged")
		sectionBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, sectionBlock.Text.Text, "```go")
		require.Contains(t, sectionBlock.Text.Text, "func main()")
	})

	t.Run("regular text blocks not merged", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "First paragraph", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "Second paragraph", false, false),
				nil, nil,
			),
		}
		got := mergeCodeBlocks(blocks)
		require.Len(t, got, 2, "regular text blocks should not be merged")
		firstBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Equal(t, "First paragraph", firstBlock.Text.Text)
		secondBlock, ok := got[1].(*slack.SectionBlock)
		require.True(t, ok)
		require.Equal(t, "Second paragraph", secondBlock.Text.Text)
	})

	t.Run("code block containing table - split across blocks", func(t *testing.T) {
		t.Parallel()
		// Simulate a code block with a table that gets split by markdown converter
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```text", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "LINK                              LOSS", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "lax001-dz002:sao001-dz002        100%", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "tyo001-dz002:sin001-dz002        100%", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "```", false, false),
				nil, nil,
			),
		}
		got := mergeCodeBlocks(blocks)
		require.Len(t, got, 1, "code block with table should be merged into single block")
		sectionBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, sectionBlock.Text.Text, "```text")
		require.Contains(t, sectionBlock.Text.Text, "LINK                              LOSS")
		require.Contains(t, sectionBlock.Text.Text, "lax001-dz002:sao001-dz002")
		require.Contains(t, sectionBlock.Text.Text, "tyo001-dz002:sin001-dz002")
		require.Contains(t, sectionBlock.Text.Text, "```")
		require.True(t, sectionBlock.Expand)
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

func TestAI_Slack_mergeTables(t *testing.T) {
	t.Parallel()

	t.Run("empty blocks", func(t *testing.T) {
		t.Parallel()
		got := mergeTables([]slack.Block{})
		require.Empty(t, got)
	})

	t.Run("nil blocks", func(t *testing.T) {
		t.Parallel()
		got := mergeTables(nil)
		require.Nil(t, got)
	})

	t.Run("table split across multiple blocks", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| DEVICE | INTERFACE | TRANSITIONS |", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "|---|---|---|", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| dgt-dzd-ams-ams1 | Ethernet22/1 | 1 |", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| dz-ch2-sw01 | Ethernet25/1 | 1 |", false, false),
				nil, nil,
			),
		}
		got := mergeTables(blocks)
		require.Len(t, got, 1, "table should be merged into single block")
		sectionBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, sectionBlock.Text.Text, "| DEVICE | INTERFACE | TRANSITIONS |")
		require.Contains(t, sectionBlock.Text.Text, "|---|---|---|")
		require.Contains(t, sectionBlock.Text.Text, "| dgt-dzd-ams-ams1 |")
		require.Contains(t, sectionBlock.Text.Text, "| dz-ch2-sw01 |")
		require.True(t, sectionBlock.Expand)
	})

	t.Run("table with header and separator", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| Column 1 | Column 2 |", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "|:---|:---:|", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| Value 1 | Value 2 |", false, false),
				nil, nil,
			),
		}
		got := mergeTables(blocks)
		require.Len(t, got, 1)
		sectionBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, sectionBlock.Text.Text, "| Column 1 | Column 2 |")
		require.Contains(t, sectionBlock.Text.Text, "|:---|:---:|")
		require.Contains(t, sectionBlock.Text.Text, "| Value 1 | Value 2 |")
	})

	t.Run("multiple tables separated by text", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| A | B |", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "|---|---|", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| 1 | 2 |", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "Some text between tables", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| X | Y |", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "|---|---|", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| 3 | 4 |", false, false),
				nil, nil,
			),
		}
		got := mergeTables(blocks)
		require.Len(t, got, 3, "should have first table, text, and second table")
		// First block should be the merged first table
		firstBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, firstBlock.Text.Text, "| A | B |")
		require.Contains(t, firstBlock.Text.Text, "| 1 | 2 |")
		// Second block should be the text
		secondBlock, ok := got[1].(*slack.SectionBlock)
		require.True(t, ok)
		require.Equal(t, "Some text between tables", secondBlock.Text.Text)
		// Third block should be the merged second table
		thirdBlock, ok := got[2].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, thirdBlock.Text.Text, "| X | Y |")
		require.Contains(t, thirdBlock.Text.Text, "| 3 | 4 |")
	})

	t.Run("table with empty lines", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| Header |", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| Data |", false, false),
				nil, nil,
			),
		}
		got := mergeTables(blocks)
		require.Len(t, got, 1)
		sectionBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, sectionBlock.Text.Text, "| Header |")
		require.Contains(t, sectionBlock.Text.Text, "| Data |")
	})

	t.Run("non-section blocks interrupt table", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| A | B |", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| 1 | 2 |", false, false),
				nil, nil,
			),
			slack.NewContextBlock("", slack.NewTextBlockObject(slack.PlainTextType, "Context", false, false)),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| 3 | 4 |", false, false),
				nil, nil,
			),
		}
		got := mergeTables(blocks)
		require.Len(t, got, 3, "table should be flushed before context, then new table block")
		// First block should be the merged table up to the context
		firstBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, firstBlock.Text.Text, "| A | B |")
		require.Contains(t, firstBlock.Text.Text, "| 1 | 2 |")
		// Second block should be the context block
		require.Equal(t, slack.MBTContext, got[1].BlockType())
		// Third block should be the new table row
		thirdBlock, ok := got[2].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, thirdBlock.Text.Text, "| 3 | 4 |")
	})

	t.Run("regular text blocks not merged", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "First paragraph", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "Second paragraph", false, false),
				nil, nil,
			),
		}
		got := mergeTables(blocks)
		require.Len(t, got, 2, "regular text blocks should not be merged")
	})

	t.Run("table detection in SetExpandOnSectionBlocks", func(t *testing.T) {
		t.Parallel()
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "| DEVICE | INTERFACE |\n|---|---|\n| dz-sw01 | Eth1/1 |", false, false),
				nil, nil,
			),
		}
		got := SetExpandOnSectionBlocks(blocks, slog.Default())
		require.Len(t, got, 1, "table should be kept as single block")
		sectionBlock, ok := got[0].(*slack.SectionBlock)
		require.True(t, ok)
		require.Contains(t, sectionBlock.Text.Text, "| DEVICE |")
		require.True(t, sectionBlock.Expand)
	})
}
