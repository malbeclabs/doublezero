package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// GenerateFollowUps generates suggested follow-up questions based on the conversation.
func (p *Pipeline) GenerateFollowUps(ctx context.Context, userQuestion string, answer string) ([]string, error) {
	systemPrompt := p.cfg.Prompts.FollowUp

	// Build user prompt with the question and answer
	userPrompt := fmt.Sprintf("User question: %s\n\nAnswer provided:\n%s", userQuestion, answer)

	response, err := p.trackLLMCall(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	// Parse JSON response
	response = strings.TrimSpace(response)
	// Handle markdown code blocks if present
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) > 2 {
			response = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var questions []string
	if err := json.Unmarshal([]byte(response), &questions); err != nil {
		// If parsing fails, return empty slice rather than error
		if p.log != nil {
			p.log.Info("pipeline: failed to parse follow-up questions", "error", err, "response", response)
		}
		return nil, nil
	}

	// Limit to 3 questions max
	if len(questions) > 3 {
		questions = questions[:3]
	}

	return questions, nil
}
