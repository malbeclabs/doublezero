// Package pipeline implements a multi-step question-answering pipeline.
// Unlike the ReAct agent which uses a single loop, this pipeline breaks
// the process into discrete steps: decompose, generate, execute, synthesize.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Config holds the configuration for the pipeline.
type Config struct {
	Logger        *slog.Logger
	LLM           LLMClient
	Querier       Querier
	SchemaFetcher SchemaFetcher
	Prompts       *Prompts
	MaxTokens     int64
	MaxRetries    int // Max retries for failed queries (default 2)
}

// LLMClient is the interface for interacting with an LLM.
type LLMClient interface {
	// Complete sends a prompt and returns the response text.
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
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

// QueryResult holds the result of a SQL query.
type QueryResult struct {
	SQL      string
	Columns  []string
	Rows     []map[string]any
	Count    int
	Error    string
	Formatted string // Human-readable formatted result
}

// DataQuestion represents a single data question to be answered.
type DataQuestion struct {
	Question    string // The data question in natural language
	Rationale   string // Why this question helps answer the user's query
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
	Role    string // "user" or "assistant"
	Content string
}

// PipelineResult holds the complete result of running the pipeline.
type PipelineResult struct {
	// Input
	UserQuestion string

	// Step 1: Decomposition
	DataQuestions []DataQuestion

	// Step 2: Generation
	GeneratedQueries []GeneratedQuery

	// Step 3: Execution
	ExecutedQueries []ExecutedQuery

	// Step 4: Synthesis
	Answer string
}

// Pipeline orchestrates the multi-step question-answering process.
type Pipeline struct {
	cfg *Config
	log *slog.Logger
}

// New creates a new Pipeline.
func New(cfg *Config) (*Pipeline, error) {
	if cfg.LLM == nil {
		return nil, fmt.Errorf("LLM client is required")
	}
	if cfg.Querier == nil {
		return nil, fmt.Errorf("querier is required")
	}
	if cfg.SchemaFetcher == nil {
		return nil, fmt.Errorf("schema fetcher is required")
	}
	if cfg.Prompts == nil {
		return nil, fmt.Errorf("prompts are required")
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 4
	}

	return &Pipeline{
		cfg: cfg,
		log: cfg.Logger,
	}, nil
}

// GenerateAndExecuteWithRetry generates SQL and executes it, retrying on errors.
func (p *Pipeline) GenerateAndExecuteWithRetry(ctx context.Context, dataQuestion DataQuestion) ExecutedQuery {
	// First attempt: generate and execute
	generated, err := p.Generate(ctx, dataQuestion)
	if err != nil {
		return ExecutedQuery{
			GeneratedQuery: GeneratedQuery{DataQuestion: dataQuestion},
			Result:         QueryResult{Error: fmt.Sprintf("generation failed: %v", err)},
		}
	}

	executed, _ := p.Execute(ctx, generated)

	// If no error, check for zero rows
	if executed.Result.Error == "" {
		// Handle zero-row results
		if executed.Result.Count == 0 {
			executed = p.handleZeroRowResult(ctx, dataQuestion, executed)
		}
		return executed
	}

	// Retry loop for errors
	for attempt := 1; attempt <= p.cfg.MaxRetries; attempt++ {
		if p.log != nil {
			p.log.Info("pipeline: retrying failed query",
				"question", dataQuestion.Question,
				"attempt", attempt,
				"error", executed.Result.Error)
		}

		// Regenerate with error context
		regenerated, err := p.RegenerateWithError(ctx, dataQuestion, executed.GeneratedQuery.SQL, executed.Result.Error)
		if err != nil {
			// Regeneration failed, keep the previous error
			continue
		}

		// Execute the regenerated query
		executed, _ = p.Execute(ctx, regenerated)

		// If successful, check for zero rows
		if executed.Result.Error == "" {
			if p.log != nil {
				p.log.Info("pipeline: retry succeeded",
					"question", dataQuestion.Question,
					"attempt", attempt)
			}
			// Handle zero-row results after successful retry
			if executed.Result.Count == 0 {
				executed = p.handleZeroRowResult(ctx, dataQuestion, executed)
			}
			return executed
		}
	}

	// All retries failed, return last result with error
	if p.log != nil {
		p.log.Info("pipeline: all retries failed",
			"question", dataQuestion.Question,
			"error", executed.Result.Error)
	}
	return executed
}

// handleZeroRowResult analyzes a zero-row result and potentially regenerates the query.
func (p *Pipeline) handleZeroRowResult(ctx context.Context, dataQuestion DataQuestion, executed ExecutedQuery) ExecutedQuery {
	// Analyze if zero rows is suspicious
	analysis, err := p.AnalyzeZeroResult(ctx, dataQuestion, executed.GeneratedQuery.SQL)
	if err != nil {
		if p.log != nil {
			p.log.Info("pipeline: zero-row analysis failed", "error", err)
		}
		return executed
	}

	if p.log != nil {
		p.log.Info("pipeline: zero-row analysis",
			"question", dataQuestion.Question,
			"suspicious", analysis.IsSuspicious,
			"reasoning", analysis.Reasoning)
	}

	// If not suspicious, return as-is
	if !analysis.IsSuspicious {
		return executed
	}

	// Regenerate with zero-row context
	if p.log != nil {
		p.log.Info("pipeline: regenerating query due to suspicious zero rows",
			"question", dataQuestion.Question,
			"suggestion", analysis.Suggestion)
	}

	regenerated, err := p.RegenerateWithZeroRows(ctx, dataQuestion, executed.GeneratedQuery.SQL, analysis)
	if err != nil {
		if p.log != nil {
			p.log.Info("pipeline: zero-row regeneration failed", "error", err)
		}
		return executed
	}

	// Execute the regenerated query
	newExecuted, _ := p.Execute(ctx, regenerated)

	// Return the new result (even if still zero rows - we tried)
	if p.log != nil {
		p.log.Info("pipeline: regenerated query executed",
			"question", dataQuestion.Question,
			"newRowCount", newExecuted.Result.Count)
	}

	return newExecuted
}

// Run executes the full pipeline for a user question.
func (p *Pipeline) Run(ctx context.Context, userQuestion string) (*PipelineResult, error) {
	return p.RunWithHistory(ctx, userQuestion, nil)
}

// RunWithHistory executes the full pipeline with conversation context.
func (p *Pipeline) RunWithHistory(ctx context.Context, userQuestion string, history []ConversationMessage) (*PipelineResult, error) {
	result := &PipelineResult{
		UserQuestion: userQuestion,
	}

	// Step 1: Decompose the question into data questions
	if p.log != nil {
		p.log.Info("pipeline: step 1 - decomposing question")
	}
	dataQuestions, err := p.DecomposeWithHistory(ctx, userQuestion, history)
	if err != nil {
		return nil, err // Error message is already user-friendly
	}
	result.DataQuestions = dataQuestions
	if p.log != nil {
		p.log.Info("pipeline: decomposed into data questions", "count", len(dataQuestions))
	}

	// Step 2 & 3: Generate SQL and execute queries (in parallel, with retries)
	if p.log != nil {
		p.log.Info("pipeline: step 2/3 - generating and executing queries", "count", len(dataQuestions))
	}
	executedQueries := make([]ExecutedQuery, len(dataQuestions))
	var wg sync.WaitGroup

	for i, dq := range dataQuestions {
		wg.Add(1)
		go func(idx int, question DataQuestion) {
			defer wg.Done()
			executed := p.GenerateAndExecuteWithRetry(ctx, question)
			executedQueries[idx] = executed
		}(i, dq)
	}
	wg.Wait()

	// Extract generated queries from executed results
	generatedQueries := make([]GeneratedQuery, len(executedQueries))
	for i, eq := range executedQueries {
		generatedQueries[i] = eq.GeneratedQuery
	}
	result.GeneratedQueries = generatedQueries
	result.ExecutedQueries = executedQueries

	successCount := 0
	for _, eq := range executedQueries {
		if eq.Result.Error == "" {
			successCount++
		}
	}
	if p.log != nil {
		p.log.Info("pipeline: queries completed", "total", len(executedQueries), "success", successCount)
	}

	// Step 4: Synthesize the answer
	if p.log != nil {
		p.log.Info("pipeline: step 4 - synthesizing answer")
	}
	answer, err := p.Synthesize(ctx, userQuestion, executedQueries)
	if err != nil {
		return nil, fmt.Errorf("synthesize failed: %w", err)
	}
	result.Answer = answer
	if p.log != nil {
		p.log.Info("pipeline: answer synthesized")
	}

	return result, nil
}
