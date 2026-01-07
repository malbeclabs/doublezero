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

	// First pass: merge any code blocks that were split by the markdown converter
	blocks = mergeCodeBlocks(blocks)
	// Second pass: merge any tables that were split by the markdown converter
	blocks = mergeTables(blocks)

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
				// 4. Contains table markers (|)
				// Single-line blocks shouldn't be split as they're likely already atomic
				containsCodeMarkers := strings.Contains(text, "```")
				isSingleLine := !strings.Contains(text, "\n")
				containsList := containsListItems(text)
				containsTable := containsTable(text)

				if containsCodeMarkers || isSingleLine || containsList || containsTable {
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

// mergeCodeBlocks merges consecutive section blocks that are part of a code block.
// This handles cases where the markdown converter splits code blocks into multiple blocks.
func mergeCodeBlocks(blocks []slack.Block) []slack.Block {
	if len(blocks) == 0 {
		return blocks
	}

	var result []slack.Block
	var inCodeBlock bool
	var codeBlockBuilder strings.Builder
	var codeBlockType slack.MessageBlockType

	for _, block := range blocks {
		blockType := block.BlockType()

		// Handle rich_text blocks that might contain code blocks
		// The markdown converter creates rich_text blocks for code blocks
		// We need to convert them back to section blocks with markdown to preserve formatting
		if blockType == "rich_text" {
			if rtBlock, ok := block.(*slack.RichTextBlock); ok {
				// Extract text from rich text block by converting elements to markdown
				var textContent strings.Builder
				for _, element := range rtBlock.Elements {
					// Use type switch to handle different RichTextElement types
					switch elem := element.(type) {
					case *slack.RichTextSection:
						// RichTextSection contains a list of elements
						for _, subElem := range elem.Elements {
							switch sub := subElem.(type) {
							case *slack.RichTextSectionTextElement:
								// The markdown converter is adding double newlines when converting code blocks
								// Normalize them to single newlines immediately after extraction
								normalizedText := strings.ReplaceAll(sub.Text, "\n\n", "\n")
								textContent.WriteString(normalizedText)
							default:
								// For other types, try to handle if possible
							}
						}
					case *slack.RichTextPreformatted:
						// Preformatted text (code blocks) - convert back to markdown
						textContent.WriteString("```\n")
						for j, subElem := range elem.Elements {
							if textElem, ok := subElem.(*slack.RichTextSectionTextElement); ok {
								if j > 0 {
									// Add newline between elements to preserve line breaks
									textContent.WriteString("\n")
								}
								textContent.WriteString(textElem.Text)
							}
						}
						textContent.WriteString("\n```")
					}
				}

				text := textContent.String()

				if text != "" {
					// The markdown converter (slackutil.ConvertMarkdownTextToBlocks) is adding double newlines
					// when converting code blocks to RichTextSection. We've already normalized them during
					// extraction from RichTextSectionTextElement, so text should have single newlines now.
					// But double-check and normalize again just in case.
					text = strings.ReplaceAll(text, "\n\n", "\n")

					// Check if this looks like code block content (starts with headers like "CODE", has aligned columns, etc.)
					// If the markdown converter stripped the ``` markers, we need to add them back
					trimmed := strings.TrimSpace(text)
					lines := strings.Split(trimmed, "\n")
					// Detect code blocks: multiple lines, often starts with headers, has column alignment (multiple spaces)
					looksLikeCodeBlock := (len(lines) > 2 &&
						(strings.HasPrefix(trimmed, "CODE") ||
							strings.Contains(trimmed, "          ") || // Multiple spaces suggesting column alignment
							(len(lines) > 5))) // Many lines suggests formatted content

					if looksLikeCodeBlock && !strings.Contains(text, "```") {
						// This is code block content without markers - add them
						text = "```\n" + trimmed + "\n```"
					}

					// Convert to section block for processing
					sectionBlock := &slack.SectionBlock{
						Type:   slack.MBTSection,
						Text:   slack.NewTextBlockObject(slack.MarkdownType, text, false, false),
						Expand: true,
					}
					// Process this as a section block - fall through to section block processing
					block = sectionBlock
					blockType = slack.MBTSection
					// Continue processing as a section block (don't break/continue here)
				} else {
					// Can't extract text, keep as-is
					if inCodeBlock {
						mergedText := codeBlockBuilder.String()
						mergedBlock := &slack.SectionBlock{
							Type:   codeBlockType,
							Text:   slack.NewTextBlockObject(slack.MarkdownType, mergedText, false, false),
							Expand: true,
						}
						result = append(result, mergedBlock)
						codeBlockBuilder.Reset()
						inCodeBlock = false
					}
					result = append(result, block)
					continue
				}
			} else {
				// Not a rich text block we can process, keep as-is
				if inCodeBlock {
					mergedText := codeBlockBuilder.String()
					mergedBlock := &slack.SectionBlock{
						Type:   codeBlockType,
						Text:   slack.NewTextBlockObject(slack.MarkdownType, mergedText, false, false),
						Expand: true,
					}
					result = append(result, mergedBlock)
					codeBlockBuilder.Reset()
					inCodeBlock = false
				}
				result = append(result, block)
				continue
			}
		}

		if blockType != slack.MBTSection {
			// If we were building a code block, flush it first
			if inCodeBlock {
				mergedText := codeBlockBuilder.String()
				// Keep as MarkdownType so code blocks render properly in Slack
				mergedBlock := &slack.SectionBlock{
					Type:   codeBlockType,
					Text:   slack.NewTextBlockObject(slack.MarkdownType, mergedText, false, false),
					Expand: true,
				}
				result = append(result, mergedBlock)
				codeBlockBuilder.Reset()
				inCodeBlock = false
			}
			result = append(result, block)
			continue
		}

		sectionBlock := block.(*slack.SectionBlock)
		if sectionBlock.Text == nil || sectionBlock.Text.Text == "" {
			// Empty block - if we're in a code block, add newline; otherwise add as-is
			if inCodeBlock {
				codeBlockBuilder.WriteString("\n")
			} else {
				result = append(result, block)
			}
			continue
		}

		text := sectionBlock.Text.Text

		// Check if this block contains code block markers
		hasOpeningMarker := strings.Contains(text, "```")

		if hasOpeningMarker {
			// This block has code markers - track the state
			if !inCodeBlock {
				// Starting a new code block
				inCodeBlock = true
				codeBlockType = sectionBlock.Type
				if codeBlockBuilder.Len() > 0 {
					codeBlockBuilder.WriteString("\n")
				}
				codeBlockBuilder.WriteString(text)
			} else {
				// Already in a code block, this might be closing it
				codeBlockBuilder.WriteString("\n")
				codeBlockBuilder.WriteString(text)
			}

			// Count total markers in accumulated text to see if code block is closed
			totalMarkers := strings.Count(codeBlockBuilder.String(), "```")
			if totalMarkers%2 == 0 && totalMarkers > 0 {
				// Code block is complete (even number of markers = opened and closed)
				mergedText := codeBlockBuilder.String()
				// Keep as MarkdownType so code blocks render properly in Slack
				mergedBlock := &slack.SectionBlock{
					Type:   codeBlockType,
					Text:   slack.NewTextBlockObject(slack.MarkdownType, mergedText, false, false),
					Expand: true,
				}
				result = append(result, mergedBlock)
				codeBlockBuilder.Reset()
				inCodeBlock = false
			}
		} else if inCodeBlock {
			// We're in a code block and this block doesn't have markers
			// This is code content between the opening and closing markers
			// Always merge content when we're inside a code block
			codeBlockBuilder.WriteString("\n")
			codeBlockBuilder.WriteString(text)
		} else {
			// Not part of a code block, add as-is
			result = append(result, block)
		}
	}

	// Flush any remaining code block (in case it wasn't properly closed)
	if inCodeBlock {
		mergedText := codeBlockBuilder.String()
		// Keep as MarkdownType so code blocks render properly in Slack
		mergedBlock := &slack.SectionBlock{
			Type:   codeBlockType,
			Text:   slack.NewTextBlockObject(slack.MarkdownType, mergedText, false, false),
			Expand: true,
		}
		result = append(result, mergedBlock)
	}

	return result
}

// mergeTables merges consecutive section blocks that are part of a markdown table.
// This handles cases where the markdown converter splits tables into multiple blocks.
func mergeTables(blocks []slack.Block) []slack.Block {
	if len(blocks) == 0 {
		return blocks
	}

	var result []slack.Block
	var inTable bool
	var tableBuilder strings.Builder
	var tableType slack.MessageBlockType

	for _, block := range blocks {
		if block.BlockType() != slack.MBTSection {
			// If we were building a table, flush it first
			if inTable {
				mergedText := tableBuilder.String()
				mergedBlock := &slack.SectionBlock{
					Type:   tableType,
					Text:   slack.NewTextBlockObject(slack.MarkdownType, mergedText, false, false),
					Expand: true,
				}
				result = append(result, mergedBlock)
				tableBuilder.Reset()
				inTable = false
			}
			result = append(result, block)
			continue
		}

		sectionBlock := block.(*slack.SectionBlock)
		if sectionBlock.Text == nil || sectionBlock.Text.Text == "" {
			// Empty block - if we're in a table, add newline; otherwise add as-is
			if inTable {
				tableBuilder.WriteString("\n")
			} else {
				result = append(result, block)
			}
			continue
		}

		text := sectionBlock.Text.Text

		// Check if this block contains table markers (pipe characters)
		hasTableMarkers := strings.Contains(text, "|")

		if hasTableMarkers {
			// This block has table markers
			if !inTable {
				// Starting a new table
				inTable = true
				tableType = sectionBlock.Type
				if tableBuilder.Len() > 0 {
					tableBuilder.WriteString("\n")
				}
				tableBuilder.WriteString(text)
			} else {
				// Continuing a table
				tableBuilder.WriteString("\n")
				tableBuilder.WriteString(text)
			}
		} else if inTable {
			// We're in a table but this block doesn't have markers
			// Check if it's a continuation (empty line or separator line)
			trimmed := strings.TrimSpace(text)
			// Allow empty lines and separator-like lines to continue the table
			// But if we see substantial non-table content, end the table
			if trimmed == "" ||
				strings.HasPrefix(trimmed, "---") ||
				strings.HasPrefix(trimmed, "===") ||
				(len(trimmed) < 10 && (strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "="))) {
				// Likely a table separator or empty line within table
				tableBuilder.WriteString("\n")
				tableBuilder.WriteString(text)
			} else {
				// Not part of table, flush table and add this block
				mergedText := tableBuilder.String()
				mergedBlock := &slack.SectionBlock{
					Type:   tableType,
					Text:   slack.NewTextBlockObject(slack.MarkdownType, mergedText, false, false),
					Expand: true,
				}
				result = append(result, mergedBlock)
				tableBuilder.Reset()
				inTable = false
				result = append(result, block)
			}
		} else {
			// Not part of a table, add as-is
			result = append(result, block)
		}
	}

	// Flush any remaining table
	if inTable {
		mergedText := tableBuilder.String()
		mergedBlock := &slack.SectionBlock{
			Type:   tableType,
			Text:   slack.NewTextBlockObject(slack.MarkdownType, mergedText, false, false),
			Expand: true,
		}
		result = append(result, mergedBlock)
	}

	return result
}

// containsTable checks if text contains a markdown table (has pipe characters in table-like pattern)
func containsTable(text string) bool {
	lines := strings.Split(text, "\n")
	pipeLines := 0
	hasSeparator := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Check if line contains pipe characters (markdown table delimiter)
		if strings.Contains(trimmed, "|") {
			pipeLines++
			// Check for table separator lines (|---|---| or |:---|)
			if strings.HasPrefix(trimmed, "|") && (strings.Contains(trimmed, "---") || strings.Contains(trimmed, "===")) {
				hasSeparator = true
			}
		}
	}
	// If we have at least 2 lines with pipes, it's likely a table
	// Separator line makes it more certain
	return pipeLines >= 2 || (pipeLines >= 1 && hasSeparator)
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
	// The markdown converter should handle code blocks correctly, but we'll merge any that get split
	convertedBlocks, err := slackutil.ConvertMarkdownTextToBlocks(text)
	if err != nil {
		log.Debug("failed to convert markdown to blocks, using plain text", "error", err)
		return nil
	}

	// Set expand=true on all section blocks to prevent "see more" truncation
	// This also merges any code blocks or tables that were split
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
