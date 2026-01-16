package v2

import (
	"context"
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

// Respond generates a conversational response without querying data.
// This is used for follow-up questions, clarifications, and general conversation.
func (p *Pipeline) Respond(ctx context.Context, userQuestion string) (string, error) {
	return p.RespondWithHistory(ctx, userQuestion, nil)
}

// RespondWithHistory generates a conversational response with conversation context.
func (p *Pipeline) RespondWithHistory(ctx context.Context, userQuestion string, history []pipeline.ConversationMessage) (string, error) {
	systemPrompt := p.prompts.Respond

	// Build user prompt with conversation history
	var userPrompt strings.Builder

	if len(history) > 0 {
		userPrompt.WriteString("Previous conversation:\n")
		for _, msg := range history {
			if msg.Role == "user" {
				userPrompt.WriteString(fmt.Sprintf("User: %s\n", msg.Content))
			} else {
				// Include more of assistant responses for context in conversational mode
				content := msg.Content
				if len(content) > 1000 {
					content = content[:1000] + "..."
				}
				userPrompt.WriteString(fmt.Sprintf("Assistant: %s\n", content))
				// Include executed queries if present
				if len(msg.ExecutedQueries) > 0 {
					userPrompt.WriteString("Executed SQL queries:\n")
					for i, sql := range msg.ExecutedQueries {
						userPrompt.WriteString(fmt.Sprintf("Q%d: %s\n", i+1, sql))
					}
				}
			}
		}
		userPrompt.WriteString("\n")
	}

	userPrompt.WriteString(fmt.Sprintf("Current question: %s", userQuestion))

	response, err := p.trackLLMCall(ctx, systemPrompt, userPrompt.String())
	if err != nil {
		return "", fmt.Errorf("LLM completion failed: %w", err)
	}

	return strings.TrimSpace(response), nil
}
