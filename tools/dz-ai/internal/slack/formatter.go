package slack

import (
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/slack-go/slack"
	slackutil "github.com/takara2314/slack-go-util"
)

// SetExpandOnSectionBlocks splits section blocks by paragraphs/newlines and sets expand=true
// to prevent "see more" truncation. Code blocks (containing ```) are never split.
func SetExpandOnSectionBlocks(blocks []slack.Block, log *slog.Logger) []slack.Block {
	if blocks == nil {
		return nil
	}

	var result []slack.Block
	for _, block := range blocks {
		if block.BlockType() == slack.MBTSection {
			sectionBlock := block.(*slack.SectionBlock)

			// If there's text, check if it contains code blocks or looks like code
			if sectionBlock.Text != nil && sectionBlock.Text.Text != "" {
				text := sectionBlock.Text.Text

				// Don't split if:
				// 1. Contains code block markers (```)
				// 2. Is a single line (likely a code line or already properly formatted)
				// 3. Contains list items (lists should be kept together)
				// Single-line blocks shouldn't be split as they're likely already atomic
				containsCodeMarkers := strings.Contains(text, "```")
				isSingleLine := !strings.Contains(text, "\n")
				containsList := containsListItems(text)

				if containsCodeMarkers || isSingleLine || containsList {
					// Looks like code or contains lists - keep as single block with expand=true
					expandedBlock := &slack.SectionBlock{
						Type:      sectionBlock.Type,
						Text:      sectionBlock.Text,
						BlockID:   sectionBlock.BlockID,
						Fields:    sectionBlock.Fields,
						Accessory: sectionBlock.Accessory,
						Expand:    true,
					}
					result = append(result, expandedBlock)
				} else {
					// No code blocks or lists - split by paragraphs normally
					paragraphs := splitIntoParagraphs(text)
					for _, para := range paragraphs {
						paraTextBlock := slack.NewTextBlockObject(sectionBlock.Text.Type, para, false, false)
						paraBlock := &slack.SectionBlock{
							Type:      sectionBlock.Type,
							Text:      paraTextBlock,
							BlockID:   sectionBlock.BlockID,
							Fields:    nil,
							Accessory: nil,
							Expand:    true,
						}
						result = append(result, paraBlock)
					}
				}
			} else {
				// No text, just copy the block with expand=true
				expandedBlock := &slack.SectionBlock{
					Type:      sectionBlock.Type,
					Text:      sectionBlock.Text,
					BlockID:   sectionBlock.BlockID,
					Fields:    sectionBlock.Fields,
					Accessory: sectionBlock.Accessory,
					Expand:    true,
				}
				result = append(result, expandedBlock)
			}
		} else {
			// Not a section block, keep as-is
			result = append(result, block)
		}
	}
	return result
}

// containsListItems checks if text contains any list items
func containsListItems(text string) bool {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if isListItem(line) {
			return true
		}
	}
	return false
}

// isListItem checks if a line is a list item (bullet or numbered)
func isListItem(line string) bool {
	trimmed := strings.TrimSpace(line)
	// Check for bullet lists: - or * at start (after whitespace)
	if len(trimmed) > 0 && (trimmed[0] == '-' || trimmed[0] == '*') {
		// Make sure it's not just a dash in text - should have space after
		if len(trimmed) > 1 && (trimmed[1] == ' ' || trimmed[1] == '\t') {
			return true
		}
	}
	// Check for numbered lists: digit(s) followed by . or )
	if len(trimmed) > 0 && trimmed[0] >= '0' && trimmed[0] <= '9' {
		// Look for . or ) after digits
		for i := 1; i < len(trimmed) && i < 10; i++ {
			if trimmed[i] == '.' || trimmed[i] == ')' {
				// Should have space after
				if i+1 < len(trimmed) && (trimmed[i+1] == ' ' || trimmed[i+1] == '\t') {
					return true
				}
			}
			if trimmed[i] < '0' || trimmed[i] > '9' {
				break
			}
		}
	}
	return false
}

// splitIntoParagraphs splits text into paragraphs by double newlines, preserving list structures
func splitIntoParagraphs(text string) []string {
	var paragraphs []string

	// First try splitting by double newline
	paraSplit := strings.Split(text, "\n\n")
	for _, para := range paraSplit {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// Check if this paragraph contains a list
		lines := strings.Split(para, "\n")
		inList := false
		var currentList []string
		var currentParagraph strings.Builder

		for _, line := range lines {
			lineTrimmed := strings.TrimSpace(line)
			if lineTrimmed == "" {
				// Empty line - if we're in a list, end it; otherwise continue
				if inList && len(currentList) > 0 {
					paragraphs = append(paragraphs, strings.Join(currentList, "\n"))
					currentList = nil
					inList = false
				}
				continue
			}

			// Check if this line is a list item
			if isListItem(line) {
				// If we were building a regular paragraph, save it first
				if !inList && currentParagraph.Len() > 0 {
					paragraphs = append(paragraphs, strings.TrimSpace(currentParagraph.String()))
					currentParagraph.Reset()
				}
				inList = true
				currentList = append(currentList, line)
			} else {
				// Not a list item
				if inList {
					// We were in a list, but this line isn't a list item
					// Check if it's a continuation (indented line that's part of the list)
					// For now, we'll end the list when we hit a non-list item
					if len(currentList) > 0 {
						paragraphs = append(paragraphs, strings.Join(currentList, "\n"))
						currentList = nil
					}
					inList = false
				}
				// Add to current paragraph
				if currentParagraph.Len() > 0 {
					currentParagraph.WriteString("\n")
				}
				currentParagraph.WriteString(line)
			}
		}

		// Flush any remaining content
		if inList && len(currentList) > 0 {
			paragraphs = append(paragraphs, strings.Join(currentList, "\n"))
		} else if currentParagraph.Len() > 0 {
			paragraphs = append(paragraphs, strings.TrimSpace(currentParagraph.String()))
		}
	}

	// If no paragraphs found (e.g., no newlines), use the whole text
	if len(paragraphs) == 0 {
		paragraphs = []string{text}
	}

	return paragraphs
}

// ConvertMarkdownToBlocks converts markdown text to Slack blocks
func ConvertMarkdownToBlocks(text string, log *slog.Logger) []slack.Block {
	convertedBlocks, err := slackutil.ConvertMarkdownTextToBlocks(text)
	if err != nil {
		log.Debug("failed to convert markdown to blocks, using plain text", "error", err)
		return nil
	}

	// Set expand=true on all section blocks to prevent "see more" truncation
	return SetExpandOnSectionBlocks(convertedBlocks, log)
}

// SanitizeErrorMessage converts raw error messages to user-friendly messages
func SanitizeErrorMessage(errMsg string) string {
	// Rate limit errors
	if strings.Contains(errMsg, "429") || strings.Contains(errMsg, "rate_limit_error") || strings.Contains(errMsg, "rate limit") {
		return "I'm currently experiencing high demand. Please try again in a moment."
	}

	// Connection errors (should be retried, but if they still fail, show generic message)
	if strings.Contains(errMsg, "connection closed") ||
		strings.Contains(errMsg, "EOF") ||
		strings.Contains(errMsg, "client is closing") ||
		strings.Contains(errMsg, "failed to get tools") ||
		strings.Contains(errMsg, "broken pipe") ||
		strings.Contains(errMsg, "connection reset") {
		return "I'm having trouble connecting to the data service. Please try again in a moment."
	}

	// SQL-related errors (should be handled by agent, but fallback here)
	if strings.Contains(errMsg, "SQL validation failed") ||
		strings.Contains(errMsg, "query execution failed") ||
		strings.Contains(errMsg, "SQLSTATE") ||
		strings.Contains(errMsg, "Binder Error") ||
		strings.Contains(errMsg, "unknown statement") {
		return "I encountered an issue processing your query. Please try rephrasing your question or providing more specific details."
	}

	// Generic API errors
	if strings.Contains(errMsg, "failed to get response") || strings.Contains(errMsg, "POST") {
		return "I encountered an error processing your request. Please try again."
	}

	// Remove internal details like Request-IDs, URLs, etc.
	// Keep only the core error message
	lines := strings.Split(errMsg, "\n")
	var cleanLines []string
	for _, line := range lines {
		// Skip lines with internal details
		if strings.Contains(line, "Request-ID:") ||
			strings.Contains(line, "https://") ||
			strings.Contains(line, `"type":"error"`) ||
			strings.Contains(line, "POST \"") ||
			strings.Contains(line, "tools/list") ||
			strings.Contains(line, "calling \"") {
			continue
		}
		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}
		cleanLines = append(cleanLines, line)
	}

	if len(cleanLines) > 0 {
		return "Sorry, I encountered an error: " + strings.Join(cleanLines, " ")
	}

	return "Sorry, I encountered an error. Please try again."
}

// normalizeTwoWayArrow replaces the two-way arrow (↔) and :left_right_arrow: emoji with the double arrow (⇔) and removes variation selectors
func normalizeTwoWayArrow(s string) string {
	// First replace the Slack emoji :left_right_arrow: with ⇔
	s = strings.ReplaceAll(s, ":left_right_arrow:", "⇔")

	var b strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Variation_Selector, r) {
			continue
		}
		if r == '↔' {
			b.WriteRune('⇔')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// UsedISISTools checks if any ISIS tools were used in the tool list.
func UsedISISTools(toolsUsed []string) bool {
	for _, tool := range toolsUsed {
		if strings.HasPrefix(tool, "isis_") {
			return true
		}
	}
	return false
}

// AppendISISCitation appends an ISIS verification context block to the given blocks.
// If blocks is nil or empty, it creates a new slice with just the context block.
func AppendISISCitation(blocks []slack.Block) []slack.Block {
	timestamp := time.Now().UTC().Format("2006-01-02 15:04 UTC")
	contextText := ":mag: _Verified against ISIS topology (refreshed: " + timestamp + ")_"

	contextBlock := slack.NewContextBlock(
		"",
		slack.NewTextBlockObject(slack.MarkdownType, contextText, false, false),
	)

	if blocks == nil {
		return []slack.Block{contextBlock}
	}
	return append(blocks, contextBlock)
}
