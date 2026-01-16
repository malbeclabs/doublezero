package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

// Interpret analyzes the user's question and produces an analytical reframing.
func (p *Pipeline) Interpret(ctx context.Context, userQuestion string, history []pipeline.ConversationMessage) (*Interpretation, error) {
	systemPrompt := p.prompts.Interpret

	// Build user prompt with question and optional history context
	var userPrompt strings.Builder
	if len(history) > 0 {
		userPrompt.WriteString("## Conversation History\n\n")
		for _, msg := range history {
			userPrompt.WriteString(fmt.Sprintf("**%s**: %s\n\n", msg.Role, msg.Content))
		}
		userPrompt.WriteString("---\n\n")
	}
	userPrompt.WriteString("## Question\n\n")
	userPrompt.WriteString(userQuestion)

	response, err := p.trackLLMCall(ctx, systemPrompt, userPrompt.String())
	if err != nil {
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	// Parse JSON response
	interpretation, err := parseInterpretation(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse interpretation: %w", err)
	}

	return interpretation, nil
}

// parseInterpretation parses the LLM response into an Interpretation struct.
func parseInterpretation(response string) (*Interpretation, error) {
	response = strings.TrimSpace(response)

	// Handle markdown code blocks if present
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) > 2 {
			// Find the closing ```
			endIdx := len(lines) - 1
			for i := len(lines) - 1; i >= 0; i-- {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") && i > 0 {
					endIdx = i
					break
				}
			}
			// Skip the opening ``` line (and optional language identifier)
			startIdx := 1
			response = strings.Join(lines[startIdx:endIdx], "\n")
		}
	}

	var result Interpretation
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w (response: %s)", err, truncateForError(response))
	}

	return &result, nil
}

// truncateForError truncates a string for inclusion in error messages.
func truncateForError(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
