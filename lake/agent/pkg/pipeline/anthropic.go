package pipeline

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

// AnthropicLLMClient implements LLMClient using the Anthropic API.
type AnthropicLLMClient struct {
	client    anthropic.Client
	model     anthropic.Model
	maxTokens int64
}

// NewAnthropicLLMClient creates a new Anthropic-based LLM client.
func NewAnthropicLLMClient(model anthropic.Model, maxTokens int64) *AnthropicLLMClient {
	return &AnthropicLLMClient{
		client:    anthropic.NewClient(),
		model:     model,
		maxTokens: maxTokens,
	}
}

// Complete sends a prompt to Claude and returns the response text.
func (c *AnthropicLLMClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	msg, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System: []anthropic.TextBlockParam{
			{Type: "text", Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic API error: %w", err)
	}

	// Extract text from response
	for _, block := range msg.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("no text content in response")
}
