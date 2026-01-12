package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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
	start := time.Now()
	slog.Info("Anthropic API call starting", "model", c.model, "maxTokens", c.maxTokens, "userPromptLen", len(userPrompt))

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

	duration := time.Since(start)
	if err != nil {
		slog.Error("Anthropic API call failed", "duration", duration, "error", err)
		return "", fmt.Errorf("anthropic API error: %w", err)
	}
	slog.Info("Anthropic API call completed", "duration", duration, "stopReason", msg.StopReason)

	// Extract text from response
	for _, block := range msg.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("no text content in response")
}
