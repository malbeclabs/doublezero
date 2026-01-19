package slack

import (
	"log/slog"
	"regexp"
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

// codeBlockPattern matches multi-line code blocks (```...```)
// Handles both ```lang\ncode``` and ```\ncode``` formats
var codeBlockPattern = regexp.MustCompile("(?s)```[a-zA-Z]*\n?(.*?)```")

// containsNestedList checks if text contains nested list items (indented list markers)
func containsNestedList(text string) bool {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		// Check for indented list items (2+ spaces or tab before - or *)
		if len(line) >= 3 {
			// Check for space-indented list items
			if (line[0] == ' ' || line[0] == '\t') && isListItem(line) {
				return true
			}
		}
	}
	return false
}

// ConvertMarkdownToBlocks converts markdown text to Slack blocks
func ConvertMarkdownToBlocks(text string, log *slog.Logger) []slack.Block {
	// Handle code blocks specially - the library tends to split them incorrectly
	// Extract code blocks and process text segments separately
	if strings.Contains(text, "```") {
		return convertMarkdownWithCodeBlocks(text, log)
	}

	// The slackutil library doesn't handle nested lists properly - nested items get dropped.
	// Fall back to plain mrkdwn text which preserves the structure with literal dashes.
	if containsNestedList(text) {
		log.Debug("detected nested list, using plain mrkdwn to preserve structure")
		return convertToMrkdwnSectionBlocks(text, log)
	}

	convertedBlocks, err := slackutil.ConvertMarkdownTextToBlocks(text)
	if err != nil {
		log.Debug("failed to convert markdown to blocks, using plain text", "error", err)
		return nil
	}

	// Set expand=true on all section blocks to prevent "see more" truncation
	return SetExpandOnSectionBlocks(convertedBlocks, log)
}

// headerPattern matches markdown headers (# to ######)
var headerPattern = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)

// convertToMrkdwnSectionBlocks converts markdown text to Slack section blocks using mrkdwn format.
// This preserves list structure that would be lost by the slackutil library for nested lists.
// Headers are converted to proper Slack header blocks for prominent display.
func convertToMrkdwnSectionBlocks(text string, log *slog.Logger) []slack.Block {
	return convertTextWithHeaders(text, func(segment string) []slack.Block {
		// Convert markdown formatting to mrkdwn (but not headers - those are handled separately)
		mrkdwn := convertMarkdownToMrkdwn(segment)

		// Split into paragraphs for better formatting
		paragraphs := strings.Split(mrkdwn, "\n\n")
		var blocks []slack.Block

		for _, para := range paragraphs {
			para = strings.TrimSpace(para)
			if para == "" {
				continue
			}

			textBlock := slack.NewTextBlockObject(slack.MarkdownType, para, false, false)
			sectionBlock := &slack.SectionBlock{
				Type:   slack.MBTSection,
				Text:   textBlock,
				Expand: true,
			}
			blocks = append(blocks, sectionBlock)
		}

		return blocks
	})
}

// convertTextWithHeaders extracts headers from text and converts them to proper Slack header blocks.
// Non-header segments are processed by the provided converter function.
func convertTextWithHeaders(text string, convertNonHeader func(string) []slack.Block) []slack.Block {
	var blocks []slack.Block

	// Find all header matches
	matches := headerPattern.FindAllStringSubmatchIndex(text, -1)
	if matches == nil {
		// No headers, process entire text
		return convertNonHeader(text)
	}

	lastEnd := 0
	for _, match := range matches {
		headerStart := match[0]
		headerEnd := match[1]
		// match[4] and match[5] are the header text capture group
		headerTextStart := match[4]
		headerTextEnd := match[5]

		// Process text before the header
		if headerStart > lastEnd {
			beforeText := strings.TrimSpace(text[lastEnd:headerStart])
			if beforeText != "" {
				blocks = append(blocks, convertNonHeader(beforeText)...)
			}
		}

		// Create header block
		headerText := strings.TrimSpace(text[headerTextStart:headerTextEnd])
		headerBlock := slack.NewHeaderBlock(
			slack.NewTextBlockObject(slack.PlainTextType, headerText, true, false),
		)
		blocks = append(blocks, headerBlock)

		lastEnd = headerEnd
	}

	// Process any remaining text after the last header
	if lastEnd < len(text) {
		afterText := strings.TrimSpace(text[lastEnd:])
		if afterText != "" {
			blocks = append(blocks, convertNonHeader(afterText)...)
		}
	}

	return blocks
}

// convertMarkdownToMrkdwn converts standard markdown formatting to Slack mrkdwn format.
// Note: Headers are NOT converted here - they should be extracted and converted to
// proper Slack header blocks by the caller using convertTextWithHeaders.
func convertMarkdownToMrkdwn(text string) string {
	// Order matters - process more specific patterns first

	// Convert bold: **text** or __text__ -> *text*
	boldPattern1 := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	text = boldPattern1.ReplaceAllString(text, "*$1*")
	boldPattern2 := regexp.MustCompile(`__([^_]+)__`)
	text = boldPattern2.ReplaceAllString(text, "*$1*")

	// Convert italic: *text* (single asterisk) -> _text_ (but only if not already bold)
	// This is tricky because * is used for both bold and italic in different contexts
	// In standard markdown, *text* is italic and **text** is bold
	// We've already converted **text** to *text*, so remaining single *text* should become _text_
	// But we need to be careful not to convert our newly created bold markers
	// Skip this for now as it's complex and the bold conversion handles most cases

	// Convert strikethrough: ~~text~~ -> ~text~
	strikePattern := regexp.MustCompile(`~~([^~]+)~~`)
	text = strikePattern.ReplaceAllString(text, "~$1~")

	// Convert inline code: `code` stays as `code` (Slack supports this)
	// No conversion needed

	// Convert links: [text](url) -> <url|text>
	linkPattern := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	text = linkPattern.ReplaceAllString(text, "<$2|$1>")

	return text
}

// convertMarkdownWithCodeBlocks handles text that contains code blocks
// by processing code blocks separately to prevent them from being split
func convertMarkdownWithCodeBlocks(text string, log *slog.Logger) []slack.Block {
	var result []slack.Block

	// Find all code block positions
	matches := codeBlockPattern.FindAllStringSubmatchIndex(text, -1)
	if matches == nil {
		// No proper code blocks found (maybe unclosed), fall back to regular conversion
		convertedBlocks, err := slackutil.ConvertMarkdownTextToBlocks(text)
		if err != nil {
			log.Debug("failed to convert markdown to blocks, using plain text", "error", err)
			return nil
		}
		return SetExpandOnSectionBlocks(convertedBlocks, log)
	}

	lastEnd := 0
	for _, match := range matches {
		// match[0] and match[1] are the full match start/end
		blockStart := match[0]
		blockEnd := match[1]

		// Process text before the code block
		if blockStart > lastEnd {
			beforeText := strings.TrimSpace(text[lastEnd:blockStart])
			if beforeText != "" {
				beforeBlocks, err := slackutil.ConvertMarkdownTextToBlocks(beforeText)
				if err == nil {
					result = append(result, SetExpandOnSectionBlocks(beforeBlocks, log)...)
				}
			}
		}

		// Extract just the code content (group 1) without the language specifier
		// match[2] and match[3] are the capture group (the actual code content)
		codeContent := text[match[2]:match[3]]

		// Create a section block for the code block
		// Use plain ``` markers - Slack mrkdwn doesn't support language specifiers
		codeBlock := "```\n" + codeContent + "```"
		codeTextBlock := slack.NewTextBlockObject(slack.MarkdownType, codeBlock, false, false)
		codeSectionBlock := &slack.SectionBlock{
			Type:   slack.MBTSection,
			Text:   codeTextBlock,
			Expand: true,
		}
		result = append(result, codeSectionBlock)

		lastEnd = blockEnd
	}

	// Process any remaining text after the last code block
	if lastEnd < len(text) {
		afterText := strings.TrimSpace(text[lastEnd:])
		if afterText != "" {
			afterBlocks, err := slackutil.ConvertMarkdownTextToBlocks(afterText)
			if err == nil {
				result = append(result, SetExpandOnSectionBlocks(afterBlocks, log)...)
			}
		}
	}

	return result
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
