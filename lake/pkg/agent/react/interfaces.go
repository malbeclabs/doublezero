package react

import (
	"context"
)

// Message represents a message in the conversation.
type Message interface {
	// ToParam converts the message to a provider-specific parameter type.
	ToParam() any
}

// Response represents a response from the LLM.
type Response interface {
	// Content returns the content blocks from the response.
	Content() []ContentBlock
	// ToMessage converts the response to a Message for the conversation history.
	ToMessage() Message
}

// ContentBlock represents a content block in a response.
type ContentBlock interface {
	// AsText returns text content if this is a text block.
	AsText() (text string, ok bool)
	// AsToolUse returns tool use information if this is a tool use block.
	AsToolUse() (id, name string, input []byte, ok bool)
}

// ToolUse represents a tool use request from the LLM.
type ToolUse struct {
	ID    string
	Name  string
	Input map[string]any
}

// RunResult contains the result of running an agent.
type RunResult struct {
	// FinalText is the final text response from the agent.
	FinalText string
	// FullConversation is the complete conversation history including tool calls and results.
	FullConversation []Message
}

// ToolClient is an interface for calling tools.
type ToolClient interface {
	// ListTools returns the available tools.
	ListTools(ctx context.Context) ([]Tool, error)
	// CallToolText calls a tool and returns the result as text.
	CallToolText(ctx context.Context, name string, args map[string]any) (result string, isError bool, err error)
}

// Tool represents an available tool.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// LLMClient is an interface for interacting with an LLM.
type LLMClient interface {
	// Call sends messages to the LLM and returns a response.
	Call(ctx context.Context, messages []Message, tools []Tool) (Response, error)
	// ConvertToMessage converts a provider-specific message to a Message.
	ConvertToMessage(msg any) Message
	// ConvertToolResults converts tool results to messages for the LLM.
	ConvertToolResults(toolUses []ToolUse, results []ToolResult) ([]Message, error)
}

// ToolResult represents the result of executing a tool.
type ToolResult struct {
	ID      string
	Content string
	IsError bool
}
