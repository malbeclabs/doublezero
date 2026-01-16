package pipeline

import (
	"context"
	"log/slog"
)

// Config holds the configuration for the pipeline.
type Config struct {
	Logger        *slog.Logger
	LLM           LLMClient
	Querier       Querier
	SchemaFetcher SchemaFetcher
	Prompts       PromptsProvider
	MaxTokens     int64
	MaxRetries    int    // Max retries for failed queries (default 5)
	FormatContext string // Optional formatting context to append to synthesize/respond prompts (e.g., Slack formatting guidelines)
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
	Role            string   // "user" or "assistant"
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

// ProgressStage represents a stage in the pipeline execution.
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
)

// Progress represents the current state of pipeline execution.
type Progress struct {
	Stage          ProgressStage
	Classification Classification    // Set after classifying
	DataQuestions  []DataQuestion    // Set after decomposing
	QueriesTotal   int               // Total queries to execute
	QueriesDone    int               // Queries completed so far
	Error          error             // Set if an error occurred
}

// ProgressCallback is called at each stage of pipeline execution.
type ProgressCallback func(Progress)

// PipelineResult holds the complete result of running the pipeline.
type PipelineResult struct {
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
