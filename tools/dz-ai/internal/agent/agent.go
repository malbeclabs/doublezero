package agent

import (
	"context"
	"encoding/json"
	"io"

	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/client"
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
	// This can be used to maintain context across multiple agent runs.
	FullConversation []Message
}

// Agent is an interface for LLM agents that can use tools.
type Agent interface {
	// Run executes the tool calling loop, sending messages to the LLM and executing tools as needed.
	// It returns the final text response and full conversation history, or an error if max rounds is exceeded.
	Run(ctx context.Context, mcpClient *client.Client, initialMessages []Message, output io.Writer) (*RunResult, error)
}

// RunAgent runs the generic tool calling loop with any Agent implementation.
func RunAgent(ctx context.Context, agent Agent, mcpClient *client.Client, initialMessages []Message, output io.Writer) (*RunResult, error) {
	return agent.Run(ctx, mcpClient, initialMessages, output)
}

// extractToolUses extracts tool use requests from response content blocks.
func extractToolUses(content []ContentBlock) []ToolUse {
	var toolUses []ToolUse
	for _, blk := range content {
		id, name, inputBytes, ok := blk.AsToolUse()
		if !ok || id == "" || name == "" {
			continue
		}
		// Parse input JSON
		var input map[string]any
		if err := json.Unmarshal(inputBytes, &input); err != nil {
			continue
		}
		toolUses = append(toolUses, ToolUse{
			ID:    id,
			Name:  name,
			Input: input,
		})
	}
	return toolUses
}
