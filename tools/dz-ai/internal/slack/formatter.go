package slack

import (
	"log/slog"
	"strings"
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
				// Single-line blocks shouldn't be split as they're likely already atomic
				containsCodeMarkers := strings.Contains(text, "```")
				isSingleLine := !strings.Contains(text, "\n")

				if containsCodeMarkers || isSingleLine {
					// Looks like code - keep as single block with expand=true
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
					// No code blocks - split by paragraphs normally
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

// splitIntoParagraphs splits text into paragraphs by double newlines, then by single newlines
func splitIntoParagraphs(text string) []string {
	var paragraphs []string

	// First try splitting by double newline
	paraSplit := strings.Split(text, "\n\n")
	for _, para := range paraSplit {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// If paragraph contains single newlines, split those too
		lines := strings.Split(para, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				paragraphs = append(paragraphs, line)
			}
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
