package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

// Classify determines the type of question and how it should be handled.
// This routes questions to the appropriate handling path.
func (p *Pipeline) Classify(ctx context.Context, userQuestion string) (*pipeline.ClassifyResult, error) {
	return p.ClassifyWithHistory(ctx, userQuestion, nil)
}

// ClassifyWithHistory classifies a question with conversation context.
func (p *Pipeline) ClassifyWithHistory(ctx context.Context, userQuestion string, history []pipeline.ConversationMessage) (*pipeline.ClassifyResult, error) {
	systemPrompt := p.prompts.Classify

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
				// Include executed queries if present (so classifier knows queries exist)
				if len(msg.ExecutedQueries) > 0 {
					historyText.WriteString(fmt.Sprintf("[%d SQL queries were executed]\n", len(msg.ExecutedQueries)))
				}
			}
		}
		historyText.WriteString("\n")
		userPrompt = fmt.Sprintf("%sQuestion to classify: %s", historyText.String(), userQuestion)
	} else {
		userPrompt = fmt.Sprintf("Question to classify: %s", userQuestion)
	}

	response, err := p.trackLLMCall(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	// Parse the JSON response
	result, err := parseClassifyResponse(response)
	if err != nil {
		// If parsing fails, default to data_analysis to be safe
		p.logInfo("v2 pipeline: classify parse failed, defaulting to data_analysis", "error", err)
		return &pipeline.ClassifyResult{
			Classification: pipeline.ClassificationDataAnalysis,
			Reasoning:      "Classification failed, defaulting to data analysis",
		}, nil
	}

	return result, nil
}

// parseClassifyResponse extracts the classification from the LLM response.
func parseClassifyResponse(response string) (*pipeline.ClassifyResult, error) {
	response = strings.TrimSpace(response)

	// Handle markdown code blocks if present
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) > 2 {
			endIdx := len(lines) - 1
			for i := len(lines) - 1; i >= 0; i-- {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") && i > 0 {
					endIdx = i
					break
				}
			}
			startIdx := 1
			response = strings.Join(lines[startIdx:endIdx], "\n")
		}
	}

	var result pipeline.ClassifyResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w (response: %s)", err, truncateForError(response))
	}

	// Validate classification
	switch result.Classification {
	case pipeline.ClassificationDataAnalysis, pipeline.ClassificationConversational, pipeline.ClassificationOutOfScope:
		// Valid
	default:
		return nil, fmt.Errorf("invalid classification: %s", result.Classification)
	}

	return &result, nil
}
