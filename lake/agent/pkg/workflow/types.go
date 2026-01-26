package workflow

import (
	"context"
	"log/slog"
)

// Context keys for workflow tracing
type ctxKeySessionID struct{}
type ctxKeyWorkflowID struct{}

// ContextWithWorkflowIDs adds session and workflow IDs to a context for tracing.
func ContextWithWorkflowIDs(ctx context.Context, sessionID, workflowID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeySessionID{}, sessionID)
	ctx = context.WithValue(ctx, ctxKeyWorkflowID{}, workflowID)
	return ctx
}

// SessionIDFromContext extracts the session ID from context, if present.
func SessionIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKeySessionID{}).(string)
	return id, ok
}

// WorkflowIDFromContext extracts the workflow ID from context, if present.
func WorkflowIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKeyWorkflowID{}).(string)
	return id, ok
}

// Config holds the configuration for the workflow.
type Config struct {
	Logger        *slog.Logger
	LLM           LLMClient
	FollowUpLLM   LLMClient // Optional LLM for generating follow-up questions (defaults to LLM if nil)
	Querier       Querier
	SchemaFetcher SchemaFetcher
	Prompts       PromptsProvider
	MaxTokens     int64
	MaxRetries    int    // Max retries for failed queries (default 5)
	FormatContext string // Optional formatting context to append to synthesize/respond prompts (e.g., Slack formatting guidelines)

	// Graph database support (optional)
	GraphQuerier       Querier       // Optional Neo4j querier for execute_cypher tool
	GraphSchemaFetcher SchemaFetcher // Optional Neo4j schema fetcher
}

// CompleteOptions holds options for LLM completion.
type CompleteOptions struct {
	CacheSystemPrompt bool // Enable prompt caching for the system prompt
}

// CompleteOption is a functional option for Complete.
type CompleteOption func(*CompleteOptions)

// WithCacheControl enables prompt caching for the system prompt.
// This marks the system prompt as cacheable, reducing costs for
// repeated calls with the same system prompt prefix.
func WithCacheControl() CompleteOption {
	return func(o *CompleteOptions) {
		o.CacheSystemPrompt = true
	}
}

// LLMClient is the interface for interacting with an LLM.
type LLMClient interface {
	// Complete sends a prompt and returns the response text.
	// Options can be passed to control caching behavior.
	Complete(ctx context.Context, systemPrompt, userPrompt string, opts ...CompleteOption) (string, error)
}

// ToolLLMClient extends LLMClient with tool-calling capabilities.
// Used by v3 workflow for agentic tool loops.
type ToolLLMClient interface {
	LLMClient

	// CompleteWithTools sends a request with tools and returns a response that may contain tool calls.
	// The systemPrompt is separate to enable prompt caching.
	// Returns the response, input tokens, output tokens, and any error.
	CompleteWithTools(
		ctx context.Context,
		systemPrompt string,
		messages []ToolMessage,
		tools []ToolDefinition,
		opts ...CompleteOption,
	) (*ToolLLMResponse, error)
}

// ToolMessage represents a message in a tool-calling conversation.
type ToolMessage struct {
	Role    string             `json:"role"` // "user" or "assistant"
	Content []ToolContentBlock `json:"content"`
}

// ToolContentBlock represents a block of content in a tool message.
type ToolContentBlock struct {
	Type      string         `json:"type"` // "text", "tool_use", "tool_result"
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`          // For tool_use
	Name      string         `json:"name,omitempty"`        // For tool_use
	Input     map[string]any `json:"input,omitempty"`       // For tool_use
	ToolUseID string         `json:"tool_use_id,omitempty"` // For tool_result
	Content   string         `json:"content,omitempty"`     // For tool_result
	IsError   bool           `json:"is_error,omitempty"`    // For tool_result
}

// ToolDefinition represents a tool that can be called by the LLM.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"` // JSON Schema for parameters
}

// ToolLLMResponse represents the response from a tool-calling LLM request.
type ToolLLMResponse struct {
	StopReason   string             // "end_turn" or "tool_use"
	Content      []ToolContentBlock // May include both text and tool_use blocks
	InputTokens  int
	OutputTokens int
}

// ToolCalls extracts tool calls from the response.
func (r *ToolLLMResponse) ToolCalls() []ToolCallInfo {
	var calls []ToolCallInfo
	for _, block := range r.Content {
		if block.Type == "tool_use" {
			calls = append(calls, ToolCallInfo{
				ID:         block.ID,
				Name:       block.Name,
				Parameters: block.Input,
			})
		}
	}
	return calls
}

// Text extracts text content from the response.
func (r *ToolLLMResponse) Text() string {
	for _, block := range r.Content {
		if block.Type == "text" {
			return block.Text
		}
	}
	return ""
}

// HasToolCalls returns true if the response contains tool calls.
// We check the actual content blocks instead of StopReason because the API
// can sometimes return tool_use blocks with stop_reason="end_turn".
func (r *ToolLLMResponse) HasToolCalls() bool {
	for _, block := range r.Content {
		if block.Type == "tool_use" {
			return true
		}
	}
	return false
}

// ToolCallInfo represents a tool invocation from the LLM.
type ToolCallInfo struct {
	ID         string
	Name       string
	Parameters map[string]any
}

// Querier executes SQL queries.
type Querier interface {
	// Query executes a SQL query and returns formatted results.
	Query(ctx context.Context, sql string) (QueryResult, error)
}

// SchemaFetcher retrieves database schema information.
type SchemaFetcher interface {
	// FetchSchema returns a formatted string describing the database schema.
	FetchSchema(ctx context.Context) (string, error)
}

// PromptsProvider provides access to prompt templates.
type PromptsProvider interface {
	// GetPrompt returns the prompt content for the given name.
	GetPrompt(name string) string
}

// QueryResult holds the result of a SQL query.
type QueryResult struct {
	SQL       string
	Columns   []string
	Rows      []map[string]any
	Count     int
	Error     string
	Formatted string // Human-readable formatted result
}

// DataQuestion represents a single data question to be answered.
type DataQuestion struct {
	Question  string // The data question in natural language
	Rationale string // Why this question helps answer the user's query
}

// GeneratedQuery represents a SQL query generated for a data question.
type GeneratedQuery struct {
	DataQuestion DataQuestion
	SQL          string
	Explanation  string // Brief explanation of what the query does
}

// ExecutedQuery represents an executed query with results.
type ExecutedQuery struct {
	GeneratedQuery GeneratedQuery
	Result         QueryResult
}

// ConversationMessage represents a message in conversation history.
type ConversationMessage struct {
	Role            string // "user" or "assistant"
	Content         string
	ExecutedQueries []string // SQL queries executed in this turn (assistant only)
}

// Classification represents the type of question being asked.
type Classification string

const (
	ClassificationDataAnalysis   Classification = "data_analysis"
	ClassificationConversational Classification = "conversational"
	ClassificationOutOfScope     Classification = "out_of_scope"
)

// ClassifyResult holds the result of question classification.
type ClassifyResult struct {
	Classification Classification `json:"classification"`
	Reasoning      string         `json:"reasoning"`
	DirectResponse string         `json:"direct_response,omitempty"`
}

// ProgressStage represents a stage in the workflow execution.
type ProgressStage string

const (
	// v1 stages
	StageClassifying  ProgressStage = "classifying"
	StageDecomposing  ProgressStage = "decomposing"
	StageDecomposed   ProgressStage = "decomposed"
	StageExecuting    ProgressStage = "executing"
	StageSynthesizing ProgressStage = "synthesizing"
	StageComplete     ProgressStage = "complete"
	StageError        ProgressStage = "error"

	// v2 stages
	StageInterpreting ProgressStage = "interpreting"
	StageMapping      ProgressStage = "mapping"
	StagePlanning     ProgressStage = "planning"
	StageInspecting   ProgressStage = "inspecting"

	// v3 stages
	StageThinking ProgressStage = "thinking" // Model is reasoning

	// Tool call stages - SQL
	StageSQLStarted  ProgressStage = "sql_started" // SQL query started
	StageSQLComplete ProgressStage = "sql_done"    // SQL query completed

	// Tool call stages - Cypher
	StageCypherStarted  ProgressStage = "cypher_started" // Cypher query started
	StageCypherComplete ProgressStage = "cypher_done"    // Cypher query completed

	// Tool call stages - ReadDocs
	StageReadDocsStarted  ProgressStage = "read_docs_started" // Reading docs started
	StageReadDocsComplete ProgressStage = "read_docs_done"    // Reading docs completed

	// Legacy v3 stages (for backwards compatibility during transition)
	StageQueryStarted  ProgressStage = "query_started" // Individual query started
	StageQueryComplete ProgressStage = "query_done"    // Individual query completed
)

// Progress represents the current state of workflow execution.
type Progress struct {
	Stage          ProgressStage
	Classification Classification // Set after classifying
	DataQuestions  []DataQuestion // Set after decomposing
	QueriesTotal   int            // Total queries to execute
	QueriesDone    int            // Queries completed so far
	Error          error          // Set if an error occurred

	// v3 fields
	ThinkingContent string // For StageThinking: the thinking content

	// SQL tool call fields
	SQLQuestion string // For StageSQLStarted/StageSQLComplete: the data question
	SQL         string // For StageSQLStarted/StageSQLComplete: the SQL query
	SQLRows     int    // For StageSQLComplete: row count
	SQLError    string // For StageSQLComplete: error if failed

	// Cypher tool call fields
	CypherQuestion string // For StageCypherStarted/StageCypherComplete: the data question
	Cypher         string // For StageCypherStarted/StageCypherComplete: the Cypher query
	CypherRows     int    // For StageCypherComplete: row count
	CypherError    string // For StageCypherComplete: error if failed

	// ReadDocs tool call fields
	DocsPage    string // For StageReadDocsStarted/StageReadDocsComplete: page name
	DocsContent string // For StageReadDocsComplete: content (truncated for progress)
	DocsError   string // For StageReadDocsComplete: error if failed

	// Legacy fields (for backwards compatibility)
	QueryQuestion string // For StageQueryStarted/StageQueryComplete: the query question
	QuerySQL      string // For StageQueryStarted/StageQueryComplete: the SQL
	QueryRows     int    // For StageQueryComplete: row count
	QueryError    string // For StageQueryComplete: error if failed
}

// ProgressCallback is called at each stage of workflow execution.
type ProgressCallback func(Progress)

// WorkflowResult holds the complete result of running the workflow.
type WorkflowResult struct {
	// Input
	UserQuestion string

	// Pre-step: Classification
	Classification Classification // How the question was classified

	// Step 1: Decomposition (only for data_analysis)
	DataQuestions []DataQuestion

	// Step 2: Generation (only for data_analysis)
	GeneratedQueries []GeneratedQuery

	// Step 3: Execution (only for data_analysis)
	ExecutedQueries []ExecutedQuery

	// Step 4: Synthesis / Response
	Answer string

	// Step 5: Follow-up suggestions (optional)
	FollowUpQuestions []string
}
