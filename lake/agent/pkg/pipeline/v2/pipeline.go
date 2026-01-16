package v2

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

// DefaultMaxIterations is the default maximum number of iteration loops.
const DefaultMaxIterations = 3

// Pipeline orchestrates the iterative v2 question-answering process.
type Pipeline struct {
	cfg      *pipeline.Config
	prompts  *Prompts
	llmCalls atomic.Int32 // tracks LLM calls during a pipeline run
}

// logInfo logs an info message if a logger is configured.
func (p *Pipeline) logInfo(msg string, args ...any) {
	if p.cfg.Logger != nil {
		p.cfg.Logger.Info(msg, args...)
	}
}

// trackLLMCall increments the LLM call counter and calls the LLM.
func (p *Pipeline) trackLLMCall(ctx context.Context, systemPrompt, userPrompt string, opts ...pipeline.CompleteOption) (string, error) {
	p.llmCalls.Add(1)
	return p.cfg.LLM.Complete(ctx, systemPrompt, userPrompt, opts...)
}

// New creates a new v2 Pipeline.
func New(cfg *pipeline.Config) (*Pipeline, error) {
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
		cfg.MaxRetries = 3
	}

	// Convert prompts provider to v2 Prompts
	prompts, ok := cfg.Prompts.(*Prompts)
	if !ok {
		return nil, fmt.Errorf("prompts must be *v2.Prompts")
	}

	return &Pipeline{
		cfg:     cfg,
		prompts: prompts,
	}, nil
}

// Run executes the full pipeline for a user question.
func (p *Pipeline) Run(ctx context.Context, userQuestion string) (*pipeline.PipelineResult, error) {
	return p.RunWithHistory(ctx, userQuestion, nil)
}

// RunWithHistory executes the full pipeline with conversation context.
func (p *Pipeline) RunWithHistory(ctx context.Context, userQuestion string, history []pipeline.ConversationMessage) (*pipeline.PipelineResult, error) {
	return p.RunWithProgress(ctx, userQuestion, history, nil)
}

// RunWithProgress executes the pipeline with progress callbacks.
func (p *Pipeline) RunWithProgress(ctx context.Context, userQuestion string, history []pipeline.ConversationMessage, onProgress pipeline.ProgressCallback) (*pipeline.PipelineResult, error) {
	// Reset LLM call counter for this run
	p.llmCalls.Store(0)

	result := &pipeline.PipelineResult{
		UserQuestion: userQuestion,
	}

	// Helper to call progress callback if set
	notify := func(stage V2Stage, _ string) {
		if onProgress != nil {
			// Emit actual v2 stage names for proper frontend display
			var progressStage pipeline.ProgressStage
			switch stage {
			case StageClassifying:
				progressStage = pipeline.StageClassifying
			case StageResponding:
				progressStage = pipeline.StageSynthesizing // Map to synthesizing for frontend
			case StageInterpreting:
				progressStage = pipeline.StageInterpreting
			case StageMapping:
				progressStage = pipeline.StageMapping
			case StagePlanning:
				progressStage = pipeline.StagePlanning
			case StageExecuting:
				progressStage = pipeline.StageExecuting
			case StageInspecting:
				progressStage = pipeline.StageInspecting
			case StageSynthesizing:
				progressStage = pipeline.StageSynthesizing
			case StageComplete:
				progressStage = pipeline.StageComplete
			case StageError:
				progressStage = pipeline.StageError
			}
			onProgress(pipeline.Progress{
				Stage:          progressStage,
				Classification: result.Classification,
			})
		}
	}

	// Stage 0: Classify
	notify(StageClassifying, "Classifying question...")
	p.logInfo("v2 pipeline: classifying question")

	classification, err := p.ClassifyWithHistory(ctx, userQuestion, history)
	if err != nil {
		notify(StageError, "Classification failed")
		return nil, fmt.Errorf("classification failed: %w", err)
	}
	result.Classification = classification.Classification
	p.logInfo("v2 pipeline: question classified",
		"classification", classification.Classification,
		"reasoning", classification.Reasoning)

	// Handle non-data-analysis classifications
	switch classification.Classification {
	case pipeline.ClassificationOutOfScope:
		answer := classification.DirectResponse
		if answer == "" {
			answer = "I'm a DoubleZero data analyst. I can help you with questions about the DZ network, devices, links, users, connected Solana validators, and performance metrics. What would you like to know?"
		}
		result.Answer = answer
		notify(StageComplete, "Complete")
		p.logInfo("v2 pipeline: out of scope, returning direct response")
		return result, nil

	case pipeline.ClassificationConversational:
		notify(StageResponding, "Preparing response...")
		p.logInfo("v2 pipeline: conversational, generating response")

		answer, err := p.RespondWithHistory(ctx, userQuestion, history)
		if err != nil {
			notify(StageError, "Response generation failed")
			return nil, fmt.Errorf("response generation failed: %w", err)
		}
		result.Answer = answer
		notify(StageComplete, "Complete")
		p.logInfo("v2 pipeline: conversational response complete")
		return result, nil
	}

	// Data analysis path - continue with the full pipeline
	result.Classification = pipeline.ClassificationDataAnalysis

	// Fetch schema for context
	schema, err := p.cfg.SchemaFetcher.FetchSchema(ctx)
	if err != nil {
		notify(StageError, "Failed to fetch schema")
		return nil, fmt.Errorf("failed to fetch schema: %w", err)
	}

	// Stage 1: Interpret
	notify(StageInterpreting, "Interpreting question...")
	p.logInfo("v2 pipeline: interpreting question")

	interpretation, err := p.Interpret(ctx, userQuestion, history)
	if err != nil {
		notify(StageError, "Interpretation failed")
		return nil, fmt.Errorf("interpretation failed: %w", err)
	}
	p.logInfo("v2 pipeline: question interpreted",
		"type", interpretation.QuestionType,
		"entities", interpretation.Entities)

	// Stage 2: Map
	notify(StageMapping, "Mapping to data...")
	p.logInfo("v2 pipeline: mapping to data")

	dataMapping, err := p.Map(ctx, interpretation, schema)
	if err != nil {
		notify(StageError, "Mapping failed")
		return nil, fmt.Errorf("mapping failed: %w", err)
	}
	p.logInfo("v2 pipeline: data mapped",
		"tables", len(dataMapping.Tables),
		"unit", dataMapping.UnitOfAnalysis)

	// Iteration loop
	maxIterations := DefaultMaxIterations
	state := &IterationState{
		Iteration:     0,
		MaxIterations: maxIterations,
	}

	var executedQueries []pipeline.ExecutedQuery
	var inspection *InspectionResult
	var queryPlan *QueryPlan

	for state.Iteration < state.MaxIterations {
		state.Iteration++
		p.logInfo("v2 pipeline: iteration", "iteration", state.Iteration)

		// Stage 3: Plan
		notify(StagePlanning, fmt.Sprintf("Planning queries (iteration %d)...", state.Iteration))
		p.logInfo("v2 pipeline: planning queries")

		var err error
		queryPlan, err = p.Plan(ctx, interpretation, dataMapping, schema, state)
		if err != nil {
			notify(StageError, "Planning failed")
			return nil, fmt.Errorf("planning failed: %w", err)
		}
		p.logInfo("v2 pipeline: queries planned",
			"validation", len(queryPlan.ValidationQueries),
			"answer", len(queryPlan.AnswerQueries))

		// Stage 4: Execute
		notify(StageExecuting, "Executing queries...")
		p.logInfo("v2 pipeline: executing queries")

		executedQueries, err = p.Execute(ctx, queryPlan)
		if err != nil {
			notify(StageError, "Execution failed")
			return nil, fmt.Errorf("execution failed: %w", err)
		}
		p.logInfo("v2 pipeline: queries executed", "count", len(executedQueries))

		// Stage 5: Inspect
		notify(StageInspecting, "Inspecting results...")
		p.logInfo("v2 pipeline: inspecting results")

		inspection, err = p.Inspect(ctx, interpretation, executedQueries, state)
		if err != nil {
			notify(StageError, "Inspection failed")
			return nil, fmt.Errorf("inspection failed: %w", err)
		}
		p.logInfo("v2 pipeline: results inspected",
			"dataQualityOk", inspection.DataQualityOK,
			"shouldIterate", inspection.ShouldIterate,
			"confidence", inspection.Confidence)

		// Record iteration history
		state.History = append(state.History, IterationHistory{
			Iteration:  state.Iteration,
			Plan:       *queryPlan,
			Results:    executedQueries,
			Inspection: *inspection,
		})

		// Check if we should iterate
		if !inspection.ShouldIterate {
			break
		}

		// If we should iterate but are at max iterations, break with warning
		if state.Iteration >= state.MaxIterations {
			p.logInfo("v2 pipeline: max iterations reached", "iterations", state.Iteration)
			break
		}
	}

	// Stage 6: Synthesize
	notify(StageSynthesizing, "Synthesizing answer...")
	p.logInfo("v2 pipeline: synthesizing answer")

	answer, err := p.Synthesize(ctx, userQuestion, interpretation, executedQueries, inspection)
	if err != nil {
		notify(StageError, "Synthesis failed")
		return nil, fmt.Errorf("synthesis failed: %w", err)
	}
	result.Answer = answer
	result.ExecutedQueries = executedQueries

	// Convert executed queries to data questions and generated queries for compatibility
	for i, eq := range executedQueries {
		result.DataQuestions = append(result.DataQuestions, pipeline.DataQuestion{
			Question:  eq.GeneratedQuery.DataQuestion.Question,
			Rationale: eq.GeneratedQuery.DataQuestion.Rationale,
		})
		result.GeneratedQueries = append(result.GeneratedQueries, pipeline.GeneratedQuery{
			DataQuestion: result.DataQuestions[i],
			SQL:          eq.GeneratedQuery.SQL,
			Explanation:  eq.GeneratedQuery.Explanation,
		})
	}

	notify(StageComplete, "Complete")
	p.logInfo("v2 pipeline: complete", "iterations", state.Iteration)

	return result, nil
}
