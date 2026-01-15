// Package pipeline implements a multi-step question-answering pipeline.
// Unlike the ReAct agent which uses a single loop, this pipeline breaks
// the process into discrete steps: decompose, generate, execute, synthesize.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

// Config holds the configuration for the pipeline.
type Config struct {
	Logger        *slog.Logger
	LLM           LLMClient
	Querier       Querier
	SchemaFetcher SchemaFetcher
	Prompts       *Prompts
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
	Role            string   // "user" or "assistant"
	Content         string
	ExecutedQueries []string // SQL queries executed in this turn (assistant only)
}

// ProgressStage represents a stage in the pipeline execution.
type ProgressStage string

const (
	StageClassifying  ProgressStage = "classifying"
	StageDecomposing  ProgressStage = "decomposing"
	StageDecomposed   ProgressStage = "decomposed"
	StageExecuting    ProgressStage = "executing"
	StageSynthesizing ProgressStage = "synthesizing"
	StageComplete     ProgressStage = "complete"
	StageError        ProgressStage = "error"
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

// Pipeline orchestrates the multi-step question-answering process.
type Pipeline struct {
	cfg      *Config
	log      *slog.Logger
	llmCalls atomic.Int32 // tracks LLM calls during a pipeline run
}

// trackLLMCall increments the LLM call counter and calls the LLM.
func (p *Pipeline) trackLLMCall(ctx context.Context, systemPrompt, userPrompt string, opts ...CompleteOption) (string, error) {
	p.llmCalls.Add(1)
	return p.cfg.LLM.Complete(ctx, systemPrompt, userPrompt, opts...)
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
		cfg.MaxRetries = 5
	}

	return &Pipeline{
		cfg: cfg,
		log: cfg.Logger,
	}, nil
}

// GenerateAndExecuteWithRetry generates SQL and executes it, retrying on errors.
// The questionNum is a 1-indexed question identifier for logging (e.g., Q1, Q2).
func (p *Pipeline) GenerateAndExecuteWithRetry(ctx context.Context, dataQuestion DataQuestion, questionNum int) ExecutedQuery {
	// First attempt: generate and execute
	generated, err := p.Generate(ctx, dataQuestion)
	if err != nil {
		return ExecutedQuery{
			GeneratedQuery: GeneratedQuery{DataQuestion: dataQuestion},
			Result:         QueryResult{Error: fmt.Sprintf("generation failed: %v", err)},
		}
	}

	executed, _ := p.Execute(ctx, generated, questionNum)

	// If no error, check for zero rows
	if executed.Result.Error == "" {
		// Handle zero-row results
		if executed.Result.Count == 0 {
			executed = p.handleZeroRowResult(ctx, dataQuestion, executed, questionNum)
		}
		return executed
	}

	// Retry loop for errors
	for attempt := 1; attempt <= p.cfg.MaxRetries; attempt++ {
		if p.log != nil {
			p.log.Info("pipeline: retrying failed query",
				"q", questionNum,
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
		executed, _ = p.Execute(ctx, regenerated, questionNum)

		// If successful, check for zero rows
		if executed.Result.Error == "" {
			if p.log != nil {
				p.log.Info("pipeline: retry succeeded",
					"q", questionNum,
					"question", dataQuestion.Question,
					"attempt", attempt)
			}
			// Handle zero-row results after successful retry
			if executed.Result.Count == 0 {
				executed = p.handleZeroRowResult(ctx, dataQuestion, executed, questionNum)
			}
			return executed
		}
	}

	// All retries failed, return last result with error
	if p.log != nil {
		p.log.Info("pipeline: all retries failed",
			"q", questionNum,
			"question", dataQuestion.Question,
			"error", executed.Result.Error)
	}
	return executed
}

// handleZeroRowResult analyzes a zero-row result and potentially regenerates the query.
func (p *Pipeline) handleZeroRowResult(ctx context.Context, dataQuestion DataQuestion, executed ExecutedQuery, questionNum int) ExecutedQuery {
	// Analyze if zero rows is suspicious
	analysis, err := p.AnalyzeZeroResult(ctx, dataQuestion, executed.GeneratedQuery.SQL)
	if err != nil {
		if p.log != nil {
			p.log.Info("pipeline: zero-row analysis failed", "q", questionNum, "error", err)
		}
		return executed
	}

	if p.log != nil {
		p.log.Info("pipeline: zero-row analysis",
			"q", questionNum,
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
			"q", questionNum,
			"question", dataQuestion.Question,
			"suggestion", analysis.Suggestion)
	}

	regenerated, err := p.RegenerateWithZeroRows(ctx, dataQuestion, executed.GeneratedQuery.SQL, analysis)
	if err != nil {
		if p.log != nil {
			p.log.Info("pipeline: zero-row regeneration failed", "q", questionNum, "error", err)
		}
		return executed
	}

	// Execute the regenerated query
	newExecuted, _ := p.Execute(ctx, regenerated, questionNum)

	// Return the new result (even if still zero rows - we tried)
	if p.log != nil {
		p.log.Info("pipeline: regenerated query executed",
			"q", questionNum,
			"question", dataQuestion.Question,
			"newRowCount", newExecuted.Result.Count)
	}

	return newExecuted
}

// Run executes the full pipeline for a user question.
func (p *Pipeline) Run(ctx context.Context, userQuestion string) (*PipelineResult, error) {
	return p.RunWithHistory(ctx, userQuestion, nil)
}

// RunWithProgress executes the pipeline with progress callbacks.
func (p *Pipeline) RunWithProgress(ctx context.Context, userQuestion string, history []ConversationMessage, onProgress ProgressCallback) (*PipelineResult, error) {
	// Reset LLM call counter for this run
	p.llmCalls.Store(0)

	result := &PipelineResult{
		UserQuestion: userQuestion,
	}

	// Helper to call progress callback if set
	notify := func(progress Progress) {
		if onProgress != nil {
			onProgress(progress)
		}
	}

	// Pre-step: Classify the question
	notify(Progress{Stage: StageClassifying})

	if p.log != nil {
		p.log.Info("pipeline: classifying question")
	}
	classification, err := p.ClassifyWithHistory(ctx, userQuestion, history)
	if err != nil {
		notify(Progress{Stage: StageError, Error: err})
		return nil, fmt.Errorf("classification failed: %w", err)
	}
	result.Classification = classification.Classification

	// Handle non-data-analysis classifications
	switch classification.Classification {
	case ClassificationOutOfScope:
		if classification.DirectResponse != "" {
			result.Answer = classification.DirectResponse
		} else {
			result.Answer = "I'm a DoubleZero data analyst. I can help you with questions about the DZ network, devices, links, users, connected Solana validators, and performance metrics. What would you like to know?"
		}
		notify(Progress{Stage: StageComplete, Classification: classification.Classification})
		metrics.RecordPipelineRun(string(classification.Classification), int(p.llmCalls.Load()), 0)
		return result, nil

	case ClassificationConversational:
		if p.log != nil {
			p.log.Info("pipeline: handling conversational question")
		}
		answer, err := p.RespondWithHistory(ctx, userQuestion, history)
		if err != nil {
			notify(Progress{Stage: StageError, Error: err})
			return nil, fmt.Errorf("conversational response failed: %w", err)
		}
		result.Answer = answer
		notify(Progress{Stage: StageComplete, Classification: classification.Classification})
		metrics.RecordPipelineRun(string(classification.Classification), int(p.llmCalls.Load()), 0)
		return result, nil
	}

	// Step 1: Decompose
	notify(Progress{Stage: StageDecomposing, Classification: classification.Classification})

	if p.log != nil {
		p.log.Info("pipeline: step 1 - decomposing question")
	}
	dataQuestions, err := p.DecomposeWithHistory(ctx, userQuestion, history)
	if err != nil {
		notify(Progress{Stage: StageError, Error: err})
		return nil, err
	}
	result.DataQuestions = dataQuestions

	notify(Progress{
		Stage:          StageDecomposed,
		Classification: classification.Classification,
		DataQuestions:  dataQuestions,
		QueriesTotal:   len(dataQuestions),
	})

	// Step 2 & 3: Generate and execute queries
	notify(Progress{
		Stage:          StageExecuting,
		Classification: classification.Classification,
		DataQuestions:  dataQuestions,
		QueriesTotal:   len(dataQuestions),
		QueriesDone:    0,
	})

	if p.log != nil {
		p.log.Info("pipeline: step 2/3 - generating and executing queries", "count", len(dataQuestions))
	}

	executedQueries := make([]ExecutedQuery, len(dataQuestions))
	var wg sync.WaitGroup
	var queriesDone int
	var queriesMu sync.Mutex

	for i, dq := range dataQuestions {
		wg.Add(1)
		go func(idx int, question DataQuestion) {
			defer wg.Done()
			executed := p.GenerateAndExecuteWithRetry(ctx, question, idx+1)
			executedQueries[idx] = executed

			// Update progress
			queriesMu.Lock()
			queriesDone++
			currentDone := queriesDone
			queriesMu.Unlock()

			notify(Progress{
				Stage:          StageExecuting,
				Classification: classification.Classification,
				DataQuestions:  dataQuestions,
				QueriesTotal:   len(dataQuestions),
				QueriesDone:    currentDone,
			})
		}(i, dq)
	}
	wg.Wait()

	// Extract generated queries
	generatedQueries := make([]GeneratedQuery, len(executedQueries))
	for i, eq := range executedQueries {
		generatedQueries[i] = eq.GeneratedQuery
	}
	result.GeneratedQueries = generatedQueries
	result.ExecutedQueries = executedQueries

	// Step 4: Synthesize
	notify(Progress{
		Stage:          StageSynthesizing,
		Classification: classification.Classification,
		DataQuestions:  dataQuestions,
		QueriesTotal:   len(dataQuestions),
		QueriesDone:    len(dataQuestions),
	})

	if p.log != nil {
		p.log.Info("pipeline: step 4 - synthesizing answer")
	}
	answer, err := p.Synthesize(ctx, userQuestion, executedQueries)
	if err != nil {
		notify(Progress{Stage: StageError, Error: err})
		return nil, fmt.Errorf("synthesize failed: %w", err)
	}
	result.Answer = answer

	notify(Progress{
		Stage:          StageComplete,
		Classification: classification.Classification,
		DataQuestions:  dataQuestions,
		QueriesTotal:   len(dataQuestions),
		QueriesDone:    len(dataQuestions),
	})

	metrics.RecordPipelineRun(string(result.Classification), int(p.llmCalls.Load()), len(executedQueries))
	return result, nil
}

// RunWithHistory executes the full pipeline with conversation context.
func (p *Pipeline) RunWithHistory(ctx context.Context, userQuestion string, history []ConversationMessage) (*PipelineResult, error) {
	// Reset LLM call counter for this run
	p.llmCalls.Store(0)

	result := &PipelineResult{
		UserQuestion: userQuestion,
	}

	// Pre-step: Classify the question to determine routing
	if p.log != nil {
		p.log.Info("pipeline: classifying question")
	}
	classification, err := p.ClassifyWithHistory(ctx, userQuestion, history)
	if err != nil {
		return nil, fmt.Errorf("classification failed: %w", err)
	}
	result.Classification = classification.Classification
	if p.log != nil {
		p.log.Info("pipeline: question classified",
			"classification", classification.Classification,
			"reasoning", classification.Reasoning)
	}

	// Route based on classification
	switch classification.Classification {
	case ClassificationOutOfScope:
		// Return the direct response from classification
		if classification.DirectResponse != "" {
			result.Answer = classification.DirectResponse
		} else {
			result.Answer = "I'm a DoubleZero data analyst. I can help you with questions about the DZ network, devices, links, users, connected Solana validators, and performance metrics. What would you like to know?"
		}
		metrics.RecordPipelineRun(string(classification.Classification), int(p.llmCalls.Load()), 0)
		return result, nil

	case ClassificationConversational:
		// Handle conversational questions without querying data
		if p.log != nil {
			p.log.Info("pipeline: handling conversational question")
		}
		answer, err := p.RespondWithHistory(ctx, userQuestion, history)
		if err != nil {
			return nil, fmt.Errorf("conversational response failed: %w", err)
		}
		result.Answer = answer
		metrics.RecordPipelineRun(string(classification.Classification), int(p.llmCalls.Load()), 0)
		return result, nil

	case ClassificationDataAnalysis:
		// Continue with the full data analysis pipeline
		// (fall through to the rest of the function)
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
			// Question numbers are 1-indexed for readability (Q1, Q2, ...)
			executed := p.GenerateAndExecuteWithRetry(ctx, question, idx+1)
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

	metrics.RecordPipelineRun(string(result.Classification), int(p.llmCalls.Load()), len(executedQueries))
	return result, nil
}
