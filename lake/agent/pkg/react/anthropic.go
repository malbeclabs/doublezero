package react

import (
	"context"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// AnthropicAgent implements LLMClient for Anthropic.
type AnthropicAgent struct {
	client          anthropic.Client
	model           anthropic.Model
	maxOutputTokens int64
	system          string
}

// NewAnthropicAgent creates a new Anthropic LLM client.
func NewAnthropicAgent(client anthropic.Client, model anthropic.Model, maxOutputTokens int64, system string) LLMClient {
	return &AnthropicAgent{
		client:          client,
		model:           model,
		maxOutputTokens: maxOutputTokens,
		system:          system,
	}
}

// Call sends messages to Anthropic and returns a response.
func (a *AnthropicAgent) Call(ctx context.Context, messages []Message, tools []Tool) (Response, error) {
	// Convert messages to Anthropic format
	anthropicMsgs := make([]anthropic.MessageParam, len(messages))
	for i, msg := range messages {
		param, ok := msg.ToParam().(anthropic.MessageParam)
		if !ok {
			return nil, fmt.Errorf("expected anthropic.MessageParam, got %T", msg.ToParam())
		}
		anthropicMsgs[i] = param
	}

	// Convert tools to Anthropic format
	anthropicTools := toAnthropicTools(tools)

	params := anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: a.maxOutputTokens,
		Messages:  anthropicMsgs,
		Tools:     anthropicTools,
	}
	if a.system != "" {
		// Enable prompt caching for the system prompt since it's static and reused.
		// This caches the system prompt (Role + Catalog + Slack guidelines), reducing
		// token usage and latency for subsequent requests. The cache has a 5-minute TTL
		// by default and automatically invalidates if the system prompt content changes.
		// Note: Content must be at least 1,024 tokens to be cacheable (our system
		// prompt should easily meet this requirement).
		params.System = []anthropic.TextBlockParam{
			{
				Text:         a.system,
				CacheControl: anthropic.NewCacheControlEphemeralParam(),
			},
		}
	}

	resp, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get response: %w", err)
	}

	return anthropicResponse{resp: resp}, nil
}

// ConvertToMessage converts an Anthropic message to a react.Message.
func (a *AnthropicAgent) ConvertToMessage(msg any) Message {
	switch m := msg.(type) {
	case anthropic.MessageParam:
		return AnthropicMessage{Msg: m}
	case AnthropicMessage:
		return m
	default:
		return AnthropicMessage{Msg: anthropic.MessageParam{}}
	}
}

// ConvertToolResults converts tool results to Anthropic messages.
func (a *AnthropicAgent) ConvertToolResults(toolUses []ToolUse, results []ToolResult) ([]Message, error) {
	toolResults := make([]anthropic.ContentBlockParamUnion, 0, len(results))
	for _, result := range results {
		toolResults = append(toolResults, anthropic.NewToolResultBlock(result.ID, result.Content, result.IsError))
	}

	msg := anthropic.NewUserMessage(toolResults...)
	return []Message{AnthropicMessage{Msg: msg}}, nil
}

// CreateUserMessage creates a user message in Anthropic format.
func (a *AnthropicAgent) CreateUserMessage(content string) Message {
	return AnthropicMessage{Msg: anthropic.NewUserMessage(anthropic.NewTextBlock(content))}
}

// AnthropicMessage wraps Anthropic's MessageParam to implement react.Message.
type AnthropicMessage struct {
	Msg anthropic.MessageParam
}

func (m AnthropicMessage) ToParam() any {
	return m.Msg
}

// anthropicResponse wraps Anthropic's response to implement react.Response.
type anthropicResponse struct {
	resp *anthropic.Message
}

func (r anthropicResponse) Content() []ContentBlock {
	blocks := make([]ContentBlock, len(r.resp.Content))
	for i, blk := range r.resp.Content {
		blocks[i] = anthropicContentBlock{blk}
	}
	return blocks
}

func (r anthropicResponse) ToMessage() Message {
	return AnthropicMessage{Msg: r.resp.ToParam()}
}

// anthropicContentBlock wraps Anthropic's ContentBlockUnion to implement react.ContentBlock.
type anthropicContentBlock struct {
	blk anthropic.ContentBlockUnion
}

func (b anthropicContentBlock) AsText() (string, bool) {
	text := b.blk.AsText()
	if text.Text == "" {
		return "", false
	}
	return text.Text, true
}

func (b anthropicContentBlock) AsToolUse() (string, string, []byte, bool) {
	tu := b.blk.AsToolUse()
	if tu.ID == "" || tu.Name == "" {
		return "", "", nil, false
	}
	return tu.ID, tu.Name, tu.Input, true
}

// toAnthropicTools converts tools to Anthropic tool parameters.
func toAnthropicTools(tools []Tool) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		props, _ := t.InputSchema["properties"].(map[string]any)
		required, _ := t.InputSchema["required"].([]string)
		toolParam := anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.Opt(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Type:       "object",
				Properties: props,
				Required:   required,
			},
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &toolParam})
	}
	return out
}
