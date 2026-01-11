package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Classification represents the type of question being asked.
type Classification string

const (
	ClassificationDataAnalysis  Classification = "data_analysis"
	ClassificationConversational Classification = "conversational"
	ClassificationOutOfScope    Classification = "out_of_scope"
)

// ClassifyResult holds the result of question classification.
type ClassifyResult struct {
	Classification Classification `json:"classification"`
	Reasoning      string         `json:"reasoning"`
	DirectResponse string         `json:"direct_response,omitempty"`
}

// Classify determines the type of question and how it should be handled.
// This is a pre-step that runs before decomposition to route questions appropriately.
func (p *Pipeline) Classify(ctx context.Context, userQuestion string) (*ClassifyResult, error) {
	return p.ClassifyWithHistory(ctx, userQuestion, nil)
}

// ClassifyWithHistory classifies a question with conversation context.
func (p *Pipeline) ClassifyWithHistory(ctx context.Context, userQuestion string, history []ConversationMessage) (*ClassifyResult, error) {
	systemPrompt := p.cfg.Prompts.Classify

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
		userPrompt = fmt.Sprintf("%sQuestion to classify: %s", historyText.String(), userQuestion)
	} else {
		userPrompt = fmt.Sprintf("Question to classify: %s", userQuestion)
	}

	response, err := p.cfg.LLM.Complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	// Parse the JSON response
	result, err := parseClassifyResponse(response)
	if err != nil {
		// If parsing fails, default to data_analysis to be safe
		if p.log != nil {
			p.log.Info("pipeline: classify parse failed, defaulting to data_analysis", "error", err)
		}
		return &ClassifyResult{
			Classification: ClassificationDataAnalysis,
			Reasoning:      "Classification failed, defaulting to data analysis",
		}, nil
	}

	return result, nil
}

// parseClassifyResponse extracts the classification from the LLM response.
func parseClassifyResponse(response string) (*ClassifyResult, error) {
	// Try to find JSON in the response
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var result ClassifyResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Validate classification
	switch result.Classification {
	case ClassificationDataAnalysis, ClassificationConversational, ClassificationOutOfScope:
		// Valid
	default:
		return nil, fmt.Errorf("invalid classification: %s", result.Classification)
	}

	return &result, nil
}
