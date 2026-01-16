package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

// Classify determines the type of question and how it should be handled.
// This is a pre-step that runs before decomposition to route questions appropriately.
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
		p.logInfo("pipeline: classify parse failed, defaulting to data_analysis", "error", err)
		return &pipeline.ClassifyResult{
			Classification: pipeline.ClassificationDataAnalysis,
			Reasoning:      "Classification failed, defaulting to data analysis",
		}, nil
	}

	return result, nil
}

// parseClassifyResponse extracts the classification from the LLM response.
func parseClassifyResponse(response string) (*pipeline.ClassifyResult, error) {
	// Try to find JSON in the response
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var result pipeline.ClassifyResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
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
