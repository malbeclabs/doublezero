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
			name:     "headers are NOT converted (handled separately)",
			input:    "### My Header",
			expected: "### My Header",
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
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertMarkdownToMrkdwn(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestAI_Slack_ConvertMarkdownToBlocks_Headers(t *testing.T) {
	t.Parallel()

	t.Run("header becomes header block type", func(t *testing.T) {
		t.Parallel()
		input := "### My Header\n\nSome content here."

		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		require.NotNil(t, blocks)
		require.GreaterOrEqual(t, len(blocks), 2)

		// First block should be a header block
		require.Equal(t, slackapi.MBTHeader, blocks[0].BlockType())
		headerBlock := blocks[0].(*slackapi.HeaderBlock)
		require.Equal(t, "My Header", headerBlock.Text.Text)
	})

	t.Run("multiple headers become header blocks", func(t *testing.T) {
		t.Parallel()
		input := `### Section 1

Content for section 1.

### Section 2

Content for section 2.`

		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		require.NotNil(t, blocks)

		// Count header blocks
		headerCount := 0
		for _, block := range blocks {
			if block.BlockType() == slackapi.MBTHeader {
				headerCount++
			}
		}
		require.Equal(t, 2, headerCount, "should have 2 header blocks")
	})

	t.Run("h1 through h6 all become header blocks", func(t *testing.T) {
		t.Parallel()
		inputs := []struct {
			markdown string
			expected string
		}{
			{"# H1 Header", "H1 Header"},
			{"## H2 Header", "H2 Header"},
			{"### H3 Header", "H3 Header"},
			{"#### H4 Header", "H4 Header"},
			{"##### H5 Header", "H5 Header"},
			{"###### H6 Header", "H6 Header"},
		}

		for _, tc := range inputs {
			blocks := ConvertMarkdownToBlocks(tc.markdown, slog.Default())
			require.NotNil(t, blocks)
			require.GreaterOrEqual(t, len(blocks), 1)
			require.Equal(t, slackapi.MBTHeader, blocks[0].BlockType())
			headerBlock := blocks[0].(*slackapi.HeaderBlock)
			require.Equal(t, tc.expected, headerBlock.Text.Text)
		}
	})

	t.Run("header with nested list still uses header blocks", func(t *testing.T) {
		t.Parallel()
		input := `### Summary

- Item 1
  - Nested item 1.1
  - Nested item 1.2
- Item 2`

		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		require.NotNil(t, blocks)
		require.GreaterOrEqual(t, len(blocks), 1)

		// First block should be a header block (not bold text)
		require.Equal(t, slackapi.MBTHeader, blocks[0].BlockType())
		headerBlock := blocks[0].(*slackapi.HeaderBlock)
		require.Equal(t, "Summary", headerBlock.Text.Text)

		// Nested list items should be preserved in rich_text blocks
		foundNestedItem := false
		for _, block := range blocks {
			if block.BlockType() == slackapi.MBTRichText {
				richTextBlock := block.(*slackapi.RichTextBlock)
				for _, element := range richTextBlock.Elements {
					if list, ok := element.(*slackapi.RichTextList); ok {
						for _, listElement := range list.Elements {
							if section, ok := listElement.(*slackapi.RichTextSection); ok {
								for _, sectionElement := range section.Elements {
									if textElement, ok := sectionElement.(*slackapi.RichTextSectionTextElement); ok {
										if strings.Contains(textElement.Text, "Nested item") {
											foundNestedItem = true
										}
									}
								}
							}
						}
					}
				}
			}
		}
		require.True(t, foundNestedItem, "nested list items should be preserved in rich_text blocks")
	})

	t.Run("header with emoji prefix", func(t *testing.T) {
		t.Parallel()
		input := "### 📊 Summary\n\nSome data here."

		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		require.NotNil(t, blocks)
		require.GreaterOrEqual(t, len(blocks), 1)
		require.Equal(t, slackapi.MBTHeader, blocks[0].BlockType())
		headerBlock := blocks[0].(*slackapi.HeaderBlock)
		require.Equal(t, "📊 Summary", headerBlock.Text.Text)
	})
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

		// First block should be a header
		require.Equal(t, slackapi.MBTHeader, blocks[0].BlockType())

		// Verify nested list items are present in rich_text blocks
		foundValidator1, foundValidator2, foundValidator3 := false, false, false
		for _, block := range blocks {
			if block.BlockType() == slackapi.MBTRichText {
				richTextBlock := block.(*slackapi.RichTextBlock)
				for _, element := range richTextBlock.Elements {
					if list, ok := element.(*slackapi.RichTextList); ok {
						for _, listElement := range list.Elements {
							if section, ok := listElement.(*slackapi.RichTextSection); ok {
								for _, sectionElement := range section.Elements {
									if textElement, ok := sectionElement.(*slackapi.RichTextSectionTextElement); ok {
										if strings.Contains(textElement.Text, "validator1") {
											foundValidator1 = true
										}
										if strings.Contains(textElement.Text, "validator2") {
											foundValidator2 = true
										}
										if strings.Contains(textElement.Text, "validator3") {
											foundValidator3 = true
										}
									}
								}
							}
						}
					}
				}
			}
		}
		require.True(t, foundValidator1 && foundValidator2 && foundValidator3,
			"nested list items should be preserved in rich_text blocks")
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
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				result := containsNestedList(tt.input)
				require.Equal(t, tt.expected, result)
			})
		}
	})
}

func TestAI_Slack_ParseInlineFormatting(t *testing.T) {
	t.Parallel()

	t.Run("plain text returns single element", func(t *testing.T) {
		t.Parallel()
		elements := parseInlineFormatting("plain text here")
		require.Len(t, elements, 1)
		require.Equal(t, "plain text here", elements[0].Text)
		require.Nil(t, elements[0].Style)
	})

	t.Run("bold text with double asterisks", func(t *testing.T) {
		t.Parallel()
		elements := parseInlineFormatting("this is **bold** text")
		require.Len(t, elements, 3)
		require.Equal(t, "this is ", elements[0].Text)
		require.Nil(t, elements[0].Style)
		require.Equal(t, "bold", elements[1].Text)
		require.NotNil(t, elements[1].Style)
		require.True(t, elements[1].Style.Bold)
		require.Equal(t, " text", elements[2].Text)
		require.Nil(t, elements[2].Style)
	})

	t.Run("inline code with backticks", func(t *testing.T) {
		t.Parallel()
		elements := parseInlineFormatting("run `npm install` command")
		require.Len(t, elements, 3)
		require.Equal(t, "run ", elements[0].Text)
		require.Equal(t, "npm install", elements[1].Text)
		require.NotNil(t, elements[1].Style)
		require.True(t, elements[1].Style.Code)
		require.Equal(t, " command", elements[2].Text)
	})

	t.Run("italic text with single asterisk", func(t *testing.T) {
		t.Parallel()
		elements := parseInlineFormatting("this is *italic* text")
		require.Len(t, elements, 3)
		require.Equal(t, "italic", elements[1].Text)
		require.NotNil(t, elements[1].Style)
		require.True(t, elements[1].Style.Italic)
	})

	t.Run("strikethrough with tildes", func(t *testing.T) {
		t.Parallel()
		elements := parseInlineFormatting("this is ~~deleted~~ text")
		require.Len(t, elements, 3)
		require.Equal(t, "deleted", elements[1].Text)
		require.NotNil(t, elements[1].Style)
		require.True(t, elements[1].Style.Strike)
	})

	t.Run("multiple formatting in one string", func(t *testing.T) {
		t.Parallel()
		elements := parseInlineFormatting("**bold** and `code` mixed")
		require.Len(t, elements, 4)
		require.Equal(t, "bold", elements[0].Text)
		require.True(t, elements[0].Style.Bold)
		require.Equal(t, " and ", elements[1].Text)
		require.Equal(t, "code", elements[2].Text)
		require.True(t, elements[2].Style.Code)
		require.Equal(t, " mixed", elements[3].Text)
	})

	t.Run("empty string returns nil", func(t *testing.T) {
		t.Parallel()
		elements := parseInlineFormatting("")
		require.Nil(t, elements)
	})
}

func TestAI_Slack_ConvertMarkdownToBlocks_InlineFormatting(t *testing.T) {
	t.Parallel()

	t.Run("nested list with bold text preserves formatting", func(t *testing.T) {
		t.Parallel()
		input := `- **Bold item**
  - Nested with `+ "`code`" + `
  - Normal nested`

		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		require.NotNil(t, blocks)
		require.Greater(t, len(blocks), 0)

		// Find rich_text block and verify formatting
		foundBold, foundCode := false, false
		for _, block := range blocks {
			if block.BlockType() == slackapi.MBTRichText {
				richTextBlock := block.(*slackapi.RichTextBlock)
				for _, element := range richTextBlock.Elements {
					if list, ok := element.(*slackapi.RichTextList); ok {
						for _, listElement := range list.Elements {
							if section, ok := listElement.(*slackapi.RichTextSection); ok {
								for _, sectionElement := range section.Elements {
									if textElement, ok := sectionElement.(*slackapi.RichTextSectionTextElement); ok {
										if textElement.Style != nil {
											if textElement.Style.Bold && textElement.Text == "Bold item" {
												foundBold = true
											}
											if textElement.Style.Code && textElement.Text == "code" {
												foundCode = true
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
		require.True(t, foundBold, "bold formatting should be preserved in rich_text blocks")
		require.True(t, foundCode, "inline code formatting should be preserved in rich_text blocks")
	})
}

func TestAI_Slack_ConvertMarkdownTablesToASCII(t *testing.T) {
	t.Parallel()

	t.Run("simple markdown table converts to ASCII", func(t *testing.T) {
		t.Parallel()
		input := `| Name | Value |
|------|-------|
| foo  | 123   |
| bar  | 456   |
`
		result := convertMarkdownTablesToASCII(input)

		// Should be wrapped in code block
		require.Contains(t, result, "```")

		// Should have ASCII box characters
		require.Contains(t, result, "+")
		require.Contains(t, result, "|")

		// Should contain the data
		require.Contains(t, result, "Name")
		require.Contains(t, result, "foo")
		require.Contains(t, result, "123")
	})

	t.Run("table with varying column widths", func(t *testing.T) {
		t.Parallel()
		input := `| Short | Much Longer Header |
|-------|-------------------|
| a     | b                 |
| longer text | c         |
`
		result := convertMarkdownTablesToASCII(input)

		// Columns should be padded to max width
		require.Contains(t, result, "```")
		require.Contains(t, result, "Short")
		require.Contains(t, result, "Much Longer Header")
		require.Contains(t, result, "longer text")
	})

	t.Run("text without tables passes through unchanged", func(t *testing.T) {
		t.Parallel()
		input := "Just some regular text\nwith multiple lines\n- and a list"

		result := convertMarkdownTablesToASCII(input)

		require.Equal(t, input, result)
	})

	t.Run("mixed content with table", func(t *testing.T) {
		t.Parallel()
		input := `Here is some text.

| Col1 | Col2 |
|------|------|
| a    | b    |

And more text after.`

		result := convertMarkdownTablesToASCII(input)

		// Should preserve text before and after
		require.Contains(t, result, "Here is some text.")
		require.Contains(t, result, "And more text after.")

		// Table should be converted
		require.Contains(t, result, "```")
		require.Contains(t, result, "+")
	})

	t.Run("table with trailing whitespace converts", func(t *testing.T) {
		t.Parallel()
		// Note: trailing space after "Metro |" on header row
		input := "| Validator | Stake | Metro | \n|-----------|-------|-------|\n| foo | 100 | NYC |\n"

		result := convertMarkdownTablesToASCII(input)

		// Should be wrapped in code block
		require.Contains(t, result, "```")
		require.Contains(t, result, "Validator")
		require.Contains(t, result, "foo")
	})

	t.Run("table renders correctly in blocks", func(t *testing.T) {
		t.Parallel()
		input := `### 📊 Results

| Metric | Value |
|--------|-------|
| Count  | 100   |
| Avg    | 50.5  |
`
		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		require.NotNil(t, blocks)
		require.Greater(t, len(blocks), 0)

		// Should have header block
		require.Equal(t, slackapi.MBTHeader, blocks[0].BlockType())

		// Should have code block with table
		foundTable := false
		for _, block := range blocks {
			if block.BlockType() == slackapi.MBTSection {
				sectionBlock := block.(*slackapi.SectionBlock)
				if sectionBlock.Text != nil && strings.Contains(sectionBlock.Text.Text, "```") {
					if strings.Contains(sectionBlock.Text.Text, "Metric") &&
						strings.Contains(sectionBlock.Text.Text, "Count") {
						foundTable = true
					}
				}
			}
		}
		require.True(t, foundTable, "table should be rendered as code block")
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
			if block.BlockType() == slackapi.MBTSection {
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
			if block.BlockType() == slackapi.MBTSection {
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
			if block.BlockType() == slackapi.MBTSection {
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

	t.Run("header with code block", func(t *testing.T) {
		t.Parallel()
		input := `### Query Results

Here's the SQL:
` + "```sql\nSELECT * FROM validators;\n```"

		blocks := ConvertMarkdownToBlocks(input, slog.Default())

		require.NotNil(t, blocks)

		// Should have a header block
		foundHeader := false
		foundCode := false
		for _, block := range blocks {
			if block.BlockType() == slackapi.MBTHeader {
				headerBlock := block.(*slackapi.HeaderBlock)
				if headerBlock.Text.Text == "Query Results" {
					foundHeader = true
				}
			}
			if block.BlockType() == slackapi.MBTSection {
				sectionBlock := block.(*slackapi.SectionBlock)
				if sectionBlock.Text != nil && strings.Contains(sectionBlock.Text.Text, "SELECT * FROM validators") {
					foundCode = true
				}
			}
		}
		require.True(t, foundHeader, "should have header block")
		require.True(t, foundCode, "should have code block")
	})
}

func TestAI_Slack_ConvertTextWithHeaders(t *testing.T) {
	t.Parallel()

	t.Run("no headers passes through to converter", func(t *testing.T) {
		t.Parallel()
		input := "Just some text without headers"
		called := false

		blocks := convertTextWithHeaders(input, func(text string) []slackapi.Block {
			called = true
			require.Equal(t, input, text)
			return []slackapi.Block{
				&slackapi.SectionBlock{
					Type: slackapi.MBTSection,
					Text: slackapi.NewTextBlockObject(slackapi.MarkdownType, text, false, false),
				},
			}
		})

		require.True(t, called)
		require.Len(t, blocks, 1)
	})

	t.Run("header at start creates header block first", func(t *testing.T) {
		t.Parallel()
		input := "### Header\n\nContent after"

		blocks := convertTextWithHeaders(input, func(text string) []slackapi.Block {
			return []slackapi.Block{
				&slackapi.SectionBlock{
					Type: slackapi.MBTSection,
					Text: slackapi.NewTextBlockObject(slackapi.MarkdownType, text, false, false),
				},
			}
		})

		require.GreaterOrEqual(t, len(blocks), 2)
		require.Equal(t, slackapi.MBTHeader, blocks[0].BlockType())
		headerBlock := blocks[0].(*slackapi.HeaderBlock)
		require.Equal(t, "Header", headerBlock.Text.Text)
	})

	t.Run("content before header is processed first", func(t *testing.T) {
		t.Parallel()
		input := "Content before\n\n### Header\n\nContent after"

		var segments []string
		blocks := convertTextWithHeaders(input, func(text string) []slackapi.Block {
			segments = append(segments, text)
			return []slackapi.Block{
				&slackapi.SectionBlock{
					Type: slackapi.MBTSection,
					Text: slackapi.NewTextBlockObject(slackapi.MarkdownType, text, false, false),
				},
			}
		})

		require.GreaterOrEqual(t, len(blocks), 3)
		require.Len(t, segments, 2) // before and after header

		// First segment should be content before header
		require.Contains(t, segments[0], "Content before")

		// Should have header block in the middle
		foundHeader := false
		for _, block := range blocks {
			if block.BlockType() == slackapi.MBTHeader {
				headerBlock := block.(*slackapi.HeaderBlock)
				if headerBlock.Text.Text == "Header" {
					foundHeader = true
				}
			}
		}
		require.True(t, foundHeader)
	})

	t.Run("multiple headers in sequence", func(t *testing.T) {
		t.Parallel()
		input := `### Header 1

Content 1

### Header 2

Content 2

### Header 3

Content 3`

		blocks := convertTextWithHeaders(input, func(text string) []slackapi.Block {
			return []slackapi.Block{
				&slackapi.SectionBlock{
					Type: slackapi.MBTSection,
					Text: slackapi.NewTextBlockObject(slackapi.MarkdownType, text, false, false),
				},
			}
		})

		// Count header blocks
		headerCount := 0
		headerTexts := []string{}
		for _, block := range blocks {
			if block.BlockType() == slackapi.MBTHeader {
				headerCount++
				headerBlock := block.(*slackapi.HeaderBlock)
				headerTexts = append(headerTexts, headerBlock.Text.Text)
			}
		}

		require.Equal(t, 3, headerCount)
		require.Equal(t, []string{"Header 1", "Header 2", "Header 3"}, headerTexts)
	})
}

func TestAI_Slack_NormalizeTwoWayArrow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "two-way arrow",
			input:    "nyc ↔ lon",
			expected: "nyc ⇔ lon",
		},
		{
			name:     "slack emoji",
			input:    "nyc :left_right_arrow: lon",
			expected: "nyc ⇔ lon",
		},
		{
			name:     "already correct",
			input:    "nyc ⇔ lon",
			expected: "nyc ⇔ lon",
		},
		{
			name:     "no arrow",
			input:    "just text",
			expected: "just text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := normalizeTwoWayArrow(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}
