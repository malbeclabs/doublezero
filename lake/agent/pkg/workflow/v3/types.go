package v3

import (
	"encoding/json"
	"time"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
)

// V3Stage represents a stage in the v3 workflow execution.
type V3Stage string

const (
	StageThinking  V3Stage = "thinking"
	StageExecuting V3Stage = "executing"
	StageComplete  V3Stage = "complete"
	StageError     V3Stage = "error"
)

// Tool represents a tool that can be called by the LLM.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolCall represents a tool invocation from the LLM.
type ToolCall struct {
	ID         string
	Name       string
	Parameters map[string]any
}

// QueryInput represents a single query in an execute_sql tool call.
type QueryInput struct {
	Question string `json:"question"`
	SQL      string `json:"sql"`
}

// StreamEvent represents an event to be streamed to the client.
type StreamEvent struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

// StreamCallback is called for each event during workflow execution.
type StreamCallback func(event StreamEvent)

// LoopState tracks state during the tool-calling loop.
type LoopState struct {
	ThinkingSteps     []string                 // Content from think() calls
	ExecutedQueries   []workflow.ExecutedQuery // All SQL executed
	FinalAnswer       string                   // Last assistant text (non-tool response)
	FollowUpQuestions []string                 // Suggested follow-up questions
	Metrics           *WorkflowMetrics         // Metrics collected during execution
}

// InferClassification determines classification based on tool usage behavior.
func (state *LoopState) InferClassification() workflow.Classification {
	// If model executed SQL queries, it's data analysis
	if len(state.ExecutedQueries) > 0 {
		return workflow.ClassificationDataAnalysis
	}

	// Direct response with no queries = conversational or out-of-scope
	return workflow.ClassificationConversational
}

// ToWorkflowResult converts LoopState to a WorkflowResult.
func (state *LoopState) ToWorkflowResult(userQuestion string) *workflow.WorkflowResult {
	result := &workflow.WorkflowResult{
		UserQuestion:      userQuestion,
		Classification:    state.InferClassification(),
		Answer:            state.FinalAnswer,
		ExecutedQueries:   state.ExecutedQueries,
		FollowUpQuestions: state.FollowUpQuestions,
	}

	// Extract DataQuestions and GeneratedQueries from ExecutedQueries
	for _, eq := range state.ExecutedQueries {
		result.DataQuestions = append(result.DataQuestions, eq.GeneratedQuery.DataQuestion)
		result.GeneratedQueries = append(result.GeneratedQueries, eq.GeneratedQuery)
	}

	return result
}

// WorkflowMetrics tracks metrics during workflow execution.
type WorkflowMetrics struct {
	// LLM usage
	LLMCalls     int // Total API calls to LLM
	InputTokens  int // Total input tokens
	OutputTokens int // Total output tokens

	// Tool usage
	Queries      int // Total queries executed (SQL + Cypher)
	QueryErrors  int // Queries that returned errors

	// Timing
	TotalDuration time.Duration // End-to-end time
	LLMDuration   time.Duration // Time spent in LLM calls
	QueryDuration time.Duration // Time spent executing queries

	// Loop behavior
	LoopIterations int  // LLM round-trips
	Truncated      bool // Hit max iterations
}

// CheckpointState captures the state of the workflow at a point in time.
// This is used for durable workflow persistence and resumption.
type CheckpointState struct {
	// Current iteration number (0-indexed)
	Iteration int

	// Message history (all messages exchanged with the LLM)
	Messages []workflow.ToolMessage

	// Accumulated state from tool calls
	ThinkingSteps   []string
	ExecutedQueries []workflow.ExecutedQuery

	// Metrics at checkpoint time
	Metrics *WorkflowMetrics
}

// CheckpointCallback is called after each iteration to persist state.
// Errors from the callback are logged but don't fail the workflow.
type CheckpointCallback func(state *CheckpointState) error

// Message represents a message in the conversation.
type Message struct {
	Role    string         `json:"role"` // "user" or "assistant"
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents a block of content in a message.
type ContentBlock struct {
	Type      string         `json:"type"` // "text", "tool_use", "tool_result"
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`           // For tool_use
	Name      string         `json:"name,omitempty"`         // For tool_use
	Input     map[string]any `json:"input,omitempty"`        // For tool_use
	ToolUseID string         `json:"tool_use_id,omitempty"`  // For tool_result
	Content   string         `json:"content,omitempty"`      // For tool_result (when Type is tool_result)
	IsError   bool           `json:"is_error,omitempty"`     // For tool_result
}

// NewTextBlock creates a text content block.
func NewTextBlock(text string) ContentBlock {
	return ContentBlock{Type: "text", Text: text}
}

// NewToolResultBlock creates a tool result content block.
func NewToolResultBlock(toolUseID string, content string, isError bool) ContentBlock {
	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   content,
		IsError:   isError,
	}
}

// ToolResponse represents the response from an LLM tool-calling request.
type ToolResponse struct {
	StopReason   string         // "end_turn" or "tool_use"
	Content      []ContentBlock // May include both text and tool_use blocks
	InputTokens  int
	OutputTokens int
}

// ToolCalls extracts tool calls from the response.
func (r *ToolResponse) ToolCalls() []ToolCall {
	var calls []ToolCall
	for _, block := range r.Content {
		if block.Type == "tool_use" {
			calls = append(calls, ToolCall{
				ID:         block.ID,
				Name:       block.Name,
				Parameters: block.Input,
			})
		}
	}
	return calls
}

// Text extracts text content from the response.
func (r *ToolResponse) Text() string {
	for _, block := range r.Content {
		if block.Type == "text" {
			return block.Text
		}
	}
	return ""
}

// HasToolCalls returns true if the response contains tool calls.
func (r *ToolResponse) HasToolCalls() bool {
	return r.StopReason == "tool_use"
}
