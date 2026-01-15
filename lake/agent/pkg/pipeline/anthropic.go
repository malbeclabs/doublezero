package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

// AnthropicLLMClient implements LLMClient using the Anthropic API.
type AnthropicLLMClient struct {
	client    anthropic.Client
	model     anthropic.Model
	maxTokens int64
	name      string // optional label for logging (e.g., "agent", "eval")
}

// NewAnthropicLLMClient creates a new Anthropic-based LLM client.
func NewAnthropicLLMClient(model anthropic.Model, maxTokens int64) *AnthropicLLMClient {
	return &AnthropicLLMClient{
		client:    anthropic.NewClient(),
		model:     model,
		maxTokens: maxTokens,
		name:      "agent",
	}
}

// NewAnthropicLLMClientWithName creates a new Anthropic-based LLM client with a custom name for logging.
func NewAnthropicLLMClientWithName(model anthropic.Model, maxTokens int64, name string) *AnthropicLLMClient {
	return &AnthropicLLMClient{
		client:    anthropic.NewClient(),
		model:     model,
		maxTokens: maxTokens,
		name:      name,
	}
}

// Complete sends a prompt to Claude and returns the response text.
func (c *AnthropicLLMClient) Complete(ctx context.Context, systemPrompt, userPrompt string, opts ...CompleteOption) (string, error) {
	// Apply options
	options := &CompleteOptions{}
	for _, opt := range opts {
		opt(options)
	}

	start := time.Now()
	slog.Info("Anthropic API call starting", "phase", c.name, "model", c.model, "maxTokens", c.maxTokens, "userPromptLen", len(userPrompt), "cacheEnabled", options.CacheSystemPrompt)

	// Build system prompt block with optional cache control
	systemBlock := anthropic.TextBlockParam{Type: "text", Text: systemPrompt}
	if options.CacheSystemPrompt {
		systemBlock.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}

	msg, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System: []anthropic.TextBlockParam{
			systemBlock,
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
	})

	duration := time.Since(start)
	if err != nil {
		slog.Error("Anthropic API call failed", "phase", c.name, "duration", duration, "error", err)
		metrics.RecordAnthropicRequest(c.name, duration, err)
		return "", fmt.Errorf("anthropic API error: %w", err)
	}

	// Log with cache metrics if available
	slog.Info("Anthropic API call completed",
		"phase", c.name,
		"duration", duration,
		"stopReason", msg.StopReason,
		"inputTokens", msg.Usage.InputTokens,
		"outputTokens", msg.Usage.OutputTokens,
		"cacheCreationInputTokens", msg.Usage.CacheCreationInputTokens,
		"cacheReadInputTokens", msg.Usage.CacheReadInputTokens,
	)

	// Record Prometheus metrics
	metrics.RecordAnthropicRequest(c.name, duration, nil)
	metrics.RecordAnthropicTokensWithCache(
		msg.Usage.InputTokens,
		msg.Usage.OutputTokens,
		msg.Usage.CacheCreationInputTokens,
		msg.Usage.CacheReadInputTokens,
	)

	// Extract text from response
	for _, block := range msg.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("no text content in response")
}
