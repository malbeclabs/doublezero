package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

// DecomposeResponse is the expected JSON response from the decompose step.
type DecomposeResponse struct {
	DataQuestions []struct {
		Question  string `json:"question"`
		Rationale string `json:"rationale"`
	} `json:"data_questions"`
	Error string `json:"error,omitempty"` // Set when the LLM couldn't understand the question
}

// Decompose breaks down a user question into specific data questions.
// This is Step 1 of the pipeline.
func (p *Pipeline) Decompose(ctx context.Context, userQuestion string) ([]pipeline.DataQuestion, error) {
	return p.DecomposeWithHistory(ctx, userQuestion, nil)
}

// DecomposeWithHistory breaks down a user question with conversation context.
func (p *Pipeline) DecomposeWithHistory(ctx context.Context, userQuestion string, history []pipeline.ConversationMessage) ([]pipeline.DataQuestion, error) {
	systemPrompt := p.prompts.Decompose

	// Build user prompt with conversation history for context
	var userPrompt string
	if len(history) > 0 {
		var historyText strings.Builder
		historyText.WriteString("Previous conversation:\n")
		for _, msg := range history {
			if msg.Role == "user" {
				historyText.WriteString(fmt.Sprintf("User: %s\n", msg.Content))
			} else {
				// Truncate long assistant responses to save context
				content := msg.Content
				if len(content) > 500 {
					content = content[:500] + "..."
				}
				historyText.WriteString(fmt.Sprintf("Assistant: %s\n", content))
			}
		}
		historyText.WriteString("\n")
		userPrompt = fmt.Sprintf("%sCurrent user question: %s\n\nRespond with JSON only.", historyText.String(), userQuestion)
	} else {
		userPrompt = fmt.Sprintf("User question: %s\n\nRespond with JSON only.", userQuestion)
	}

	response, err := p.trackLLMCall(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	// Parse the JSON response
	dataQuestions, err := parseDecomposeResponse(response)
	if err != nil {
		return nil, err // Error message is already user-friendly
	}

	if len(dataQuestions) == 0 {
		return nil, fmt.Errorf("no data questions generated")
	}

	return dataQuestions, nil
}

// parseDecomposeResponse extracts data questions from the LLM response.
func parseDecomposeResponse(response string) ([]pipeline.DataQuestion, error) {
	// Try to find JSON in the response (it might be wrapped in markdown code blocks)
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		// LLM didn't return valid JSON - log for debugging
		slog.Warn("Decompose: failed to extract JSON from response",
			"responseLen", len(response),
			"responsePreview", truncateString(response, 500))
		return nil, fmt.Errorf("I couldn't understand your question. Please try rephrasing it as a question about your data")
	}

	var parsed DecomposeResponse
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		slog.Warn("Decompose: failed to parse JSON",
			"error", err,
			"jsonPreview", truncateString(jsonStr, 500))
		return nil, fmt.Errorf("I couldn't understand your question. Please try rephrasing it as a question about your data")
	}

	// Check if the LLM explicitly reported an error (e.g., unclear question)
	if parsed.Error != "" {
		return nil, fmt.Errorf("%s", parsed.Error)
	}

	result := make([]pipeline.DataQuestion, 0, len(parsed.DataQuestions))
	for _, dq := range parsed.DataQuestions {
		if dq.Question == "" {
			continue
		}
		result = append(result, pipeline.DataQuestion{
			Question:  dq.Question,
			Rationale: dq.Rationale,
		})
	}

	return result, nil
}

// extractJSON finds and extracts JSON from a response that might contain markdown.
func extractJSON(response string) string {
	response = strings.TrimSpace(response)

	// Look for JSON in code blocks first (most reliable)
	if start := strings.Index(response, "```json"); start != -1 {
		start += 7 // len("```json")
		if end := strings.Index(response[start:], "```"); end != -1 {
			return strings.TrimSpace(response[start : start+end])
		}
	}

	// Look for JSON in generic code blocks
	if start := strings.Index(response, "```"); start != -1 {
		start += 3
		if end := strings.Index(response[start:], "```"); end != -1 {
			content := strings.TrimSpace(response[start : start+end])
			// Check if it looks like JSON
			if strings.HasPrefix(content, "{") {
				return content
			}
		}
	}

	// If it starts with {, assume it's raw JSON
	if strings.HasPrefix(response, "{") {
		return extractJSONObject(response, 0)
	}

	// Try to find JSON object anywhere in the response
	if start := strings.Index(response, "{"); start != -1 {
		return extractJSONObject(response, start)
	}

	return ""
}

// extractJSONObject extracts a complete JSON object starting at the given position,
// properly handling strings that may contain braces.
func extractJSONObject(s string, start int) string {
	if start >= len(s) || s[start] != '{' {
		return ""
	}

	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(s); i++ {
		c := s[i]

		if escaped {
			escaped = false
			continue
		}

		if c == '\\' && inString {
			escaped = true
			continue
		}

		if c == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}

	// If we get here, braces weren't balanced - return what we have
	return ""
}

// truncateString truncates a string to the given max length, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
