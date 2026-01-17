package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
)

const (
	// DefaultMaxIterations is the maximum number of LLM round-trips before stopping.
	DefaultMaxIterations = 10
)

// Workflow orchestrates the v3 tool-calling workflow.
type Workflow struct {
	cfg           *workflow.Config
	prompts       *Prompts
	systemPrompt  string // Cached system prompt with schema
	tools         []workflow.ToolDefinition
	maxIterations int
}

// logInfo logs an info message if a logger is configured.
func (p *Workflow) logInfo(msg string, args ...any) {
	if p.cfg.Logger != nil {
		p.cfg.Logger.Info(msg, args...)
	}
}

// New creates a new v3 Workflow.
func New(cfg *workflow.Config) (*Workflow, error) {
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

	// Convert prompts provider to v3 Prompts
	prompts, ok := cfg.Prompts.(*Prompts)
	if !ok {
		return nil, fmt.Errorf("prompts must be *v3.Prompts")
	}

	// Convert tools to workflow.ToolDefinition format
	v3Tools := DefaultTools()
	tools := make([]workflow.ToolDefinition, len(v3Tools))
	for i, t := range v3Tools {
		var schema any
		if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
			return nil, fmt.Errorf("invalid tool schema for %s: %w", t.Name, err)
		}
		tools[i] = workflow.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		}
	}

	return &Workflow{
		cfg:           cfg,
		prompts:       prompts,
		tools:         tools,
		maxIterations: DefaultMaxIterations,
	}, nil
}

// Run executes the full workflow for a user question.
func (p *Workflow) Run(ctx context.Context, userQuestion string) (*workflow.WorkflowResult, error) {
	return p.RunWithHistory(ctx, userQuestion, nil)
}

// RunWithHistory executes the full workflow with conversation context.
func (p *Workflow) RunWithHistory(ctx context.Context, userQuestion string, history []workflow.ConversationMessage) (*workflow.WorkflowResult, error) {
	return p.RunWithProgress(ctx, userQuestion, history, nil)
}

// RunWithProgress executes the workflow with progress callbacks.
func (p *Workflow) RunWithProgress(ctx context.Context, userQuestion string, history []workflow.ConversationMessage, onProgress workflow.ProgressCallback) (*workflow.WorkflowResult, error) {
	startTime := time.Now()

	state := &LoopState{
		Metrics: &WorkflowMetrics{},
	}

	// Helper to notify progress
	notify := func(stage workflow.ProgressStage) {
		if onProgress != nil {
			onProgress(workflow.Progress{
				Stage: stage,
			})
		}
	}

	// Fetch schema once at the start
	schema, err := p.cfg.SchemaFetcher.FetchSchema(ctx)
	if err != nil {
		notify(workflow.StageError)
		return nil, fmt.Errorf("failed to fetch schema: %w", err)
	}

	// Build system prompt with schema
	systemPrompt := BuildSystemPrompt(p.prompts.System, schema, p.cfg.FormatContext)

	// Build initial messages
	messages := p.buildMessages(userQuestion, history)

	// Get tool LLM client
	toolLLM, ok := p.cfg.LLM.(workflow.ToolLLMClient)
	if !ok {
		return nil, fmt.Errorf("LLM client does not support tool calling")
	}

	// Tool-calling loop
	notify(workflow.StageExecuting)
	p.logInfo("workflow: starting tool loop", "question", userQuestion)

	for iteration := 0; iteration < p.maxIterations; iteration++ {
		state.Metrics.LoopIterations++

		// Check for context cancellation
		if ctx.Err() != nil {
			notify(workflow.StageError)
			return nil, ctx.Err()
		}

		// Call LLM with tools
		llmStart := time.Now()
		response, err := toolLLM.CompleteWithTools(ctx, systemPrompt, messages, p.tools, workflow.WithCacheControl())
		state.Metrics.LLMDuration += time.Since(llmStart)
		state.Metrics.LLMCalls++

		if err != nil {
			notify(workflow.StageError)
			return nil, fmt.Errorf("LLM call failed: %w", err)
		}

		state.Metrics.InputTokens += response.InputTokens
		state.Metrics.OutputTokens += response.OutputTokens

		p.logInfo("workflow: LLM response",
			"iteration", iteration+1,
			"stopReason", response.StopReason,
			"toolCalls", len(response.ToolCalls()))

		// Add assistant message to conversation
		messages = append(messages, p.responseToMessage(response))

		// Check if model is done (no tool calls)
		if !response.HasToolCalls() {
			// If the model hasn't executed any queries but is trying to answer,
			// force it to execute queries first (unless this is the last iteration)
			if len(state.ExecutedQueries) == 0 && state.Metrics.ThinkCalls > 0 && iteration < p.maxIterations-1 {
				p.logInfo("workflow: enforcing query execution",
					"iteration", iteration+1,
					"reason", "model tried to answer without executing queries")
				// Inject a reminder message and continue the loop
				messages = append(messages, workflow.ToolMessage{
					Role: "user",
					Content: []workflow.ToolContentBlock{
						{
							Type: "text",
							Text: "[System: You used the think tool but did not execute any SQL queries. For data questions, you MUST call execute_sql to get actual data before providing an answer. Please call execute_sql now with your planned queries.]",
						},
					},
				})
				continue
			}
			state.FinalAnswer = response.Text()
			break
		}

		// Process tool calls
		toolResults := make([]workflow.ToolContentBlock, 0)
		for _, call := range response.ToolCalls() {
			result, err := p.executeTool(ctx, call, state, onProgress)
			if err != nil {
				// Tool execution error - report back to model
				toolResults = append(toolResults, workflow.ToolContentBlock{
					Type:      "tool_result",
					ToolUseID: call.ID,
					Content:   fmt.Sprintf("Error: %s", err.Error()),
					IsError:   true,
				})
			} else {
				toolResults = append(toolResults, workflow.ToolContentBlock{
					Type:      "tool_result",
					ToolUseID: call.ID,
					Content:   result,
					IsError:   false,
				})
			}
		}

		// Add tool results as user message
		messages = append(messages, workflow.ToolMessage{
			Role:    "user",
			Content: toolResults,
		})

		// Warn model on penultimate iteration
		if iteration == p.maxIterations-2 {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, workflow.ToolContentBlock{
				Type: "text",
				Text: "[System: This is your second-to-last turn. Please wrap up your analysis and provide a final answer.]",
			})
		}
	}

	// Check if we hit max iterations without answer
	if state.FinalAnswer == "" {
		state.Metrics.Truncated = true

		// Force a finalization prompt to get a summary of what's known
		p.logInfo("workflow: forcing finalization", "reason", "max iterations reached without final answer")

		finalizationPrompt := "[System: You've reached the maximum number of iterations. Please provide your best answer now based on any data you've gathered. If you executed queries, summarize the results. If you haven't retrieved any data yet, acknowledge that you couldn't complete the analysis and explain what you were trying to do.]"

		messages = append(messages, workflow.ToolMessage{
			Role: "user",
			Content: []workflow.ToolContentBlock{
				{Type: "text", Text: finalizationPrompt},
			},
		})

		// Make one final LLM call to get the summary
		finalResponse, err := toolLLM.CompleteWithTools(ctx, systemPrompt, messages, p.tools, workflow.WithCacheControl())
		if err == nil {
			state.Metrics.LLMCalls++
			state.Metrics.InputTokens += finalResponse.InputTokens
			state.Metrics.OutputTokens += finalResponse.OutputTokens
			state.FinalAnswer = finalResponse.Text()
		}

		// If still no answer, use a generic message
		if state.FinalAnswer == "" {
			state.FinalAnswer = "I was unable to complete the analysis within the allowed iterations."
		}
	}

	state.Metrics.TotalDuration = time.Since(startTime)

	// Generate follow-up questions (non-blocking, best-effort)
	if state.FinalAnswer != "" {
		state.FollowUpQuestions = p.generateFollowUpQuestions(ctx, userQuestion, state.FinalAnswer)
	}

	// Convert to WorkflowResult
	result := state.ToWorkflowResult(userQuestion)

	notify(workflow.StageComplete)
	p.logInfo("workflow: complete",
		"classification", result.Classification,
		"iterations", state.Metrics.LoopIterations,
		"queries", len(state.ExecutedQueries),
		"truncated", state.Metrics.Truncated)

	return result, nil
}

// buildMessages constructs the initial message list from conversation history.
func (p *Workflow) buildMessages(userQuestion string, history []workflow.ConversationMessage) []workflow.ToolMessage {
	messages := make([]workflow.ToolMessage, 0, len(history)+1)

	// Add conversation history
	for _, msg := range history {
		messages = append(messages, workflow.ToolMessage{
			Role: msg.Role,
			Content: []workflow.ToolContentBlock{
				{Type: "text", Text: msg.Content},
			},
		})
	}

	// Add current user question
	messages = append(messages, workflow.ToolMessage{
		Role: "user",
		Content: []workflow.ToolContentBlock{
			{Type: "text", Text: userQuestion},
		},
	})

	return messages
}

// responseToMessage converts an LLM response to a ToolMessage for conversation history.
func (p *Workflow) responseToMessage(response *workflow.ToolLLMResponse) workflow.ToolMessage {
	content := make([]workflow.ToolContentBlock, len(response.Content))
	for i, block := range response.Content {
		content[i] = block
	}
	// Ensure content is never empty - the Anthropic API requires non-empty content
	// for assistant messages. This can happen when the model returns with stop_reason=end_turn
	// but no actual text or tool calls (e.g., outputTokens=2).
	if len(content) == 0 {
		content = []workflow.ToolContentBlock{
			{Type: "text", Text: "(considering...)"},
		}
	}
	return workflow.ToolMessage{
		Role:    "assistant",
		Content: content,
	}
}

// executeTool executes a single tool call and returns the result.
func (p *Workflow) executeTool(ctx context.Context, call workflow.ToolCallInfo, state *LoopState, onProgress workflow.ProgressCallback) (string, error) {
	switch call.Name {
	case "think":
		return p.executeThink(call.Parameters, state, onProgress)
	case "execute_sql":
		result, err := p.executeSQL(ctx, call.Parameters, state, onProgress)
		if err != nil {
			p.logInfo("workflow: execute_sql failed", "error", err, "params", call.Parameters)
		}
		return result, err
	default:
		p.logInfo("workflow: unknown tool called", "name", call.Name)
		return "", fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// executeThink handles the think tool - extracts reasoning and records it.
func (p *Workflow) executeThink(params map[string]any, state *LoopState, onProgress workflow.ProgressCallback) (string, error) {
	content, _ := params["content"].(string)
	if content != "" {
		state.ThinkingSteps = append(state.ThinkingSteps, content)
		state.Metrics.ThinkCalls++
		state.Metrics.ConsecutiveThinks++
		// Log full content for debugging (truncated version in summary log)
		p.logInfo("workflow: think",
			"thinkStep", state.Metrics.ThinkCalls,
			"consecutiveThinks", state.Metrics.ConsecutiveThinks,
			"contentLen", len(content),
			"preview", truncate(content, 200))
		if p.cfg.Logger != nil {
			p.cfg.Logger.Debug("workflow: think content", "full", content)
		}

		// Emit thinking progress event
		if onProgress != nil {
			onProgress(workflow.Progress{
				Stage:           workflow.StageThinking,
				ThinkingContent: content,
			})
		}
	}

	// Return progressively stronger messages based on consecutive think calls
	if state.Metrics.ConsecutiveThinks >= 3 {
		return "STOP PLANNING. You have called think 3+ times without executing any queries. Call execute_sql NOW with your queries. Do not call think again.", nil
	}
	if state.Metrics.ConsecutiveThinks >= 2 {
		return "Reasoning recorded. You have now called think twice without executing queries. You MUST call execute_sql next - do not call think again until you have data.", nil
	}
	// Return a directive message that reminds the model it needs to execute queries
	// This is important because the model sometimes hallucinates results after thinking
	return "Reasoning recorded. You have NOT retrieved any data yet. To get actual data, you MUST call execute_sql with your planned queries.", nil
}

// executeSQL handles the execute_sql tool - runs queries in parallel.
func (p *Workflow) executeSQL(ctx context.Context, params map[string]any, state *LoopState, onProgress workflow.ProgressCallback) (string, error) {
	queries, err := ParseQueries(params)
	if err != nil || len(queries) == 0 {
		return "", fmt.Errorf("no valid queries provided")
	}

	// Reset consecutive think counter since the model is now executing queries
	state.Metrics.ConsecutiveThinks = 0

	// Log each query question and SQL for debugging
	p.logInfo("workflow: executing SQL", "count", len(queries))
	for i, q := range queries {
		qNum := len(state.ExecutedQueries) + i + 1
		p.logInfo("workflow: query",
			"q", qNum,
			"question", q.Question,
			"sql", truncate(q.SQL, 200))
	}

	// Emit query started events for all queries
	if onProgress != nil {
		for _, q := range queries {
			onProgress(workflow.Progress{
				Stage:         workflow.StageQueryStarted,
				QueryQuestion: q.Question,
				QuerySQL:      q.SQL,
			})
		}
	}

	// Execute queries in parallel
	sqlStart := time.Now()
	results := make([]workflow.ExecutedQuery, len(queries))
	var wg sync.WaitGroup

	for i, q := range queries {
		wg.Add(1)
		go func(idx int, query QueryInput) {
			defer wg.Done()

			// Clean up SQL
			sql := strings.TrimSpace(query.SQL)
			sql = strings.TrimSuffix(sql, ";")

			// Execute query
			queryResult, err := p.cfg.Querier.Query(ctx, sql)
			if err != nil {
				state.Metrics.SQLErrors++
				results[idx] = workflow.ExecutedQuery{
					GeneratedQuery: workflow.GeneratedQuery{
						DataQuestion: workflow.DataQuestion{
							Question: query.Question,
						},
						SQL: sql,
					},
					Result: workflow.QueryResult{
						SQL:   sql,
						Error: err.Error(),
					},
				}
				// Emit query complete with error
				if onProgress != nil {
					onProgress(workflow.Progress{
						Stage:         workflow.StageQueryComplete,
						QueryQuestion: query.Question,
						QuerySQL:      sql,
						QueryError:    err.Error(),
					})
				}
				return
			}

			results[idx] = workflow.ExecutedQuery{
				GeneratedQuery: workflow.GeneratedQuery{
					DataQuestion: workflow.DataQuestion{
						Question: query.Question,
					},
					SQL: sql,
				},
				Result: queryResult,
			}

			// Emit query complete with success
			if onProgress != nil {
				onProgress(workflow.Progress{
					Stage:         workflow.StageQueryComplete,
					QueryQuestion: query.Question,
					QuerySQL:      sql,
					QueryRows:     queryResult.Count,
				})
			}
		}(i, q)
	}

	wg.Wait()
	state.Metrics.SQLDuration += time.Since(sqlStart)
	state.Metrics.SQLQueries += len(queries)

	// Log results for each query
	for i, q := range queries {
		qNum := len(state.ExecutedQueries) + i + 1
		result := results[i]
		if result.Result.Error != "" {
			p.logInfo("workflow: query result",
				"q", qNum,
				"question", q.Question,
				"error", result.Result.Error)
		} else {
			p.logInfo("workflow: query result",
				"q", qNum,
				"question", q.Question,
				"rows", result.Result.Count)
		}
	}

	// Track starting query number before appending
	startNum := len(state.ExecutedQueries)

	// Append to state
	state.ExecutedQueries = append(state.ExecutedQueries, results...)

	// Format results for model
	return formatQueryResults(queries, results, startNum), nil
}

// formatQueryResults formats query results for the model to consume.
// startNum is the number of queries already executed (0-indexed), so the first
// query in this batch will be numbered startNum+1.
func formatQueryResults(queries []QueryInput, results []workflow.ExecutedQuery, startNum int) string {
	var sb strings.Builder
	for i, q := range queries {
		sb.WriteString(fmt.Sprintf("## Q%d: %s\n\n", startNum+i+1, q.Question))
		result := results[i].Result
		if result.Error != "" {
			sb.WriteString(fmt.Sprintf("**Error:** %s\n\n", result.Error))
		} else {
			sb.WriteString(fmt.Sprintf("```sql\n%s\n```\n\n", result.SQL))
			sb.WriteString(fmt.Sprintf("**Rows:** %d\n\n", result.Count))
			if result.Formatted != "" {
				// Truncate if too long
				formatted := result.Formatted
				if len(formatted) > 5000 {
					formatted = formatted[:5000] + "\n... (truncated, " + fmt.Sprintf("%d", len(result.Formatted)-5000) + " more characters)"
				}
				sb.WriteString(formatted)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// truncate shortens a string for logging.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// RunWithCheckpoint executes the workflow with checkpoint callbacks for durability.
// The onCheckpoint callback is called after each loop iteration with the current state.
// Checkpoint errors are logged but don't fail the workflow (best-effort persistence).
func (p *Workflow) RunWithCheckpoint(
	ctx context.Context,
	userQuestion string,
	history []workflow.ConversationMessage,
	onProgress workflow.ProgressCallback,
	onCheckpoint CheckpointCallback,
) (*workflow.WorkflowResult, error) {
	startTime := time.Now()

	state := &LoopState{
		Metrics: &WorkflowMetrics{},
	}

	// Helper to notify progress
	notify := func(stage workflow.ProgressStage) {
		if onProgress != nil {
			onProgress(workflow.Progress{
				Stage: stage,
			})
		}
	}

	// Helper to checkpoint
	checkpoint := func(iteration int, messages []workflow.ToolMessage) {
		if onCheckpoint != nil {
			checkpointState := &CheckpointState{
				Iteration:       iteration,
				Messages:        messages,
				ThinkingSteps:   state.ThinkingSteps,
				ExecutedQueries: state.ExecutedQueries,
				Metrics:         state.Metrics,
			}
			if err := onCheckpoint(checkpointState); err != nil {
				p.logInfo("workflow: checkpoint failed", "iteration", iteration, "error", err)
			}
		}
	}

	// Fetch schema once at the start
	schema, err := p.cfg.SchemaFetcher.FetchSchema(ctx)
	if err != nil {
		notify(workflow.StageError)
		return nil, fmt.Errorf("failed to fetch schema: %w", err)
	}

	// Build system prompt with schema
	systemPrompt := BuildSystemPrompt(p.prompts.System, schema, p.cfg.FormatContext)

	// Build initial messages
	messages := p.buildMessages(userQuestion, history)

	// Get tool LLM client
	toolLLM, ok := p.cfg.LLM.(workflow.ToolLLMClient)
	if !ok {
		return nil, fmt.Errorf("LLM client does not support tool calling")
	}

	// Tool-calling loop
	notify(workflow.StageExecuting)
	p.logInfo("workflow: starting tool loop with checkpoint", "question", userQuestion)

	for iteration := 0; iteration < p.maxIterations; iteration++ {
		state.Metrics.LoopIterations++

		// Check for context cancellation
		if ctx.Err() != nil {
			notify(workflow.StageError)
			return nil, ctx.Err()
		}

		// Call LLM with tools
		llmStart := time.Now()
		response, err := toolLLM.CompleteWithTools(ctx, systemPrompt, messages, p.tools, workflow.WithCacheControl())
		state.Metrics.LLMDuration += time.Since(llmStart)
		state.Metrics.LLMCalls++

		if err != nil {
			notify(workflow.StageError)
			return nil, fmt.Errorf("LLM call failed: %w", err)
		}

		state.Metrics.InputTokens += response.InputTokens
		state.Metrics.OutputTokens += response.OutputTokens

		p.logInfo("workflow: LLM response",
			"iteration", iteration+1,
			"stopReason", response.StopReason,
			"toolCalls", len(response.ToolCalls()))

		// Add assistant message to conversation
		messages = append(messages, p.responseToMessage(response))

		// Check if model is done (no tool calls)
		if !response.HasToolCalls() {
			// If the model hasn't executed any queries but is trying to answer,
			// force it to execute queries first (unless this is the last iteration)
			if len(state.ExecutedQueries) == 0 && state.Metrics.ThinkCalls > 0 && iteration < p.maxIterations-1 {
				p.logInfo("workflow: enforcing query execution",
					"iteration", iteration+1,
					"reason", "model tried to answer without executing queries")
				// Inject a reminder message and continue the loop
				messages = append(messages, workflow.ToolMessage{
					Role: "user",
					Content: []workflow.ToolContentBlock{
						{
							Type: "text",
							Text: "[System: You used the think tool but did not execute any SQL queries. For data questions, you MUST call execute_sql to get actual data before providing an answer. Please call execute_sql now with your planned queries.]",
						},
					},
				})
				// Checkpoint after the reminder
				checkpoint(iteration, messages)
				continue
			}
			state.FinalAnswer = response.Text()
			break
		}

		// Process tool calls
		toolResults := make([]workflow.ToolContentBlock, 0)
		for _, call := range response.ToolCalls() {
			result, err := p.executeTool(ctx, call, state, onProgress)
			if err != nil {
				// Tool execution error - report back to model
				toolResults = append(toolResults, workflow.ToolContentBlock{
					Type:      "tool_result",
					ToolUseID: call.ID,
					Content:   fmt.Sprintf("Error: %s", err.Error()),
					IsError:   true,
				})
			} else {
				toolResults = append(toolResults, workflow.ToolContentBlock{
					Type:      "tool_result",
					ToolUseID: call.ID,
					Content:   result,
					IsError:   false,
				})
			}
		}

		// Add tool results as user message
		messages = append(messages, workflow.ToolMessage{
			Role:    "user",
			Content: toolResults,
		})

		// Warn model on penultimate iteration
		if iteration == p.maxIterations-2 {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, workflow.ToolContentBlock{
				Type: "text",
				Text: "[System: This is your second-to-last turn. Please wrap up your analysis and provide a final answer.]",
			})
		}

		// Checkpoint after each iteration (after tool results appended)
		checkpoint(iteration, messages)
	}

	// Check if we hit max iterations without answer
	if state.FinalAnswer == "" {
		state.Metrics.Truncated = true

		// Force a finalization prompt to get a summary of what's known
		p.logInfo("workflow: forcing finalization", "reason", "max iterations reached without final answer")

		finalizationPrompt := "[System: You've reached the maximum number of iterations. Please provide your best answer now based on any data you've gathered. If you executed queries, summarize the results. If you haven't retrieved any data yet, acknowledge that you couldn't complete the analysis and explain what you were trying to do.]"

		messages = append(messages, workflow.ToolMessage{
			Role: "user",
			Content: []workflow.ToolContentBlock{
				{Type: "text", Text: finalizationPrompt},
			},
		})

		// Make one final LLM call to get the summary
		finalResponse, err := toolLLM.CompleteWithTools(ctx, systemPrompt, messages, p.tools, workflow.WithCacheControl())
		if err == nil {
			state.Metrics.LLMCalls++
			state.Metrics.InputTokens += finalResponse.InputTokens
			state.Metrics.OutputTokens += finalResponse.OutputTokens
			state.FinalAnswer = finalResponse.Text()
		}

		// If still no answer, use a generic message
		if state.FinalAnswer == "" {
			state.FinalAnswer = "I was unable to complete the analysis within the allowed iterations."
		}
	}

	state.Metrics.TotalDuration = time.Since(startTime)

	// Generate follow-up questions (non-blocking, best-effort)
	if state.FinalAnswer != "" {
		state.FollowUpQuestions = p.generateFollowUpQuestions(ctx, userQuestion, state.FinalAnswer)
	}

	// Convert to WorkflowResult
	result := state.ToWorkflowResult(userQuestion)

	notify(workflow.StageComplete)
	p.logInfo("workflow: complete",
		"classification", result.Classification,
		"iterations", state.Metrics.LoopIterations,
		"queries", len(state.ExecutedQueries),
		"truncated", state.Metrics.Truncated)

	return result, nil
}

// ResumeFromCheckpoint resumes a workflow from a saved checkpoint state.
// The checkpoint contains the message history and accumulated state from prior execution.
func (p *Workflow) ResumeFromCheckpoint(
	ctx context.Context,
	userQuestion string,
	checkpoint *CheckpointState,
	onProgress workflow.ProgressCallback,
	onCheckpoint CheckpointCallback,
) (*workflow.WorkflowResult, error) {
	startTime := time.Now()

	// Restore state from checkpoint
	state := &LoopState{
		ThinkingSteps:   checkpoint.ThinkingSteps,
		ExecutedQueries: checkpoint.ExecutedQueries,
		Metrics:         checkpoint.Metrics,
	}
	if state.Metrics == nil {
		state.Metrics = &WorkflowMetrics{}
	}

	// Copy accumulated values from checkpoint metrics
	state.Metrics.LoopIterations = checkpoint.Iteration

	// Helper to notify progress
	notify := func(stage workflow.ProgressStage) {
		if onProgress != nil {
			onProgress(workflow.Progress{
				Stage: stage,
			})
		}
	}

	// Helper to persist checkpoint
	persistCheckpoint := func(iteration int, messages []workflow.ToolMessage) {
		if onCheckpoint != nil {
			checkpointState := &CheckpointState{
				Iteration:       iteration,
				Messages:        messages,
				ThinkingSteps:   state.ThinkingSteps,
				ExecutedQueries: state.ExecutedQueries,
				Metrics:         state.Metrics,
			}
			if err := onCheckpoint(checkpointState); err != nil {
				p.logInfo("workflow: checkpoint failed", "iteration", iteration, "error", err)
			}
		}
	}

	// Fetch schema
	schema, err := p.cfg.SchemaFetcher.FetchSchema(ctx)
	if err != nil {
		notify(workflow.StageError)
		return nil, fmt.Errorf("failed to fetch schema: %w", err)
	}

	// Build system prompt with schema
	systemPrompt := BuildSystemPrompt(p.prompts.System, schema, p.cfg.FormatContext)

	// Restore messages from checkpoint
	messages := checkpoint.Messages

	// Get tool LLM client
	toolLLM, ok := p.cfg.LLM.(workflow.ToolLLMClient)
	if !ok {
		return nil, fmt.Errorf("LLM client does not support tool calling")
	}

	// Continue tool-calling loop from checkpoint iteration
	notify(workflow.StageExecuting)
	p.logInfo("workflow: resuming from checkpoint",
		"question", userQuestion,
		"iteration", checkpoint.Iteration,
		"queries", len(checkpoint.ExecutedQueries))

	// Emit catch-up progress events for already-executed queries
	if onProgress != nil {
		for _, eq := range checkpoint.ExecutedQueries {
			onProgress(workflow.Progress{
				Stage:         workflow.StageQueryComplete,
				QueryQuestion: eq.GeneratedQuery.DataQuestion.Question,
				QuerySQL:      eq.Result.SQL,
				QueryRows:     eq.Result.Count,
				QueryError:    eq.Result.Error,
			})
		}
	}

	for iteration := checkpoint.Iteration; iteration < p.maxIterations; iteration++ {
		state.Metrics.LoopIterations++

		// Check for context cancellation
		if ctx.Err() != nil {
			notify(workflow.StageError)
			return nil, ctx.Err()
		}

		// Call LLM with tools
		llmStart := time.Now()
		response, err := toolLLM.CompleteWithTools(ctx, systemPrompt, messages, p.tools, workflow.WithCacheControl())
		state.Metrics.LLMDuration += time.Since(llmStart)
		state.Metrics.LLMCalls++

		if err != nil {
			notify(workflow.StageError)
			return nil, fmt.Errorf("LLM call failed: %w", err)
		}

		state.Metrics.InputTokens += response.InputTokens
		state.Metrics.OutputTokens += response.OutputTokens

		p.logInfo("workflow: LLM response (resumed)",
			"iteration", iteration+1,
			"stopReason", response.StopReason,
			"toolCalls", len(response.ToolCalls()))

		// Add assistant message to conversation
		messages = append(messages, p.responseToMessage(response))

		// Check if model is done (no tool calls)
		if !response.HasToolCalls() {
			if len(state.ExecutedQueries) == 0 && state.Metrics.ThinkCalls > 0 && iteration < p.maxIterations-1 {
				p.logInfo("workflow: enforcing query execution (resumed)",
					"iteration", iteration+1)
				messages = append(messages, workflow.ToolMessage{
					Role: "user",
					Content: []workflow.ToolContentBlock{
						{
							Type: "text",
							Text: "[System: You used the think tool but did not execute any SQL queries. For data questions, you MUST call execute_sql to get actual data before providing an answer. Please call execute_sql now with your planned queries.]",
						},
					},
				})
				persistCheckpoint(iteration, messages)
				continue
			}
			state.FinalAnswer = response.Text()
			break
		}

		// Process tool calls
		toolResults := make([]workflow.ToolContentBlock, 0)
		for _, call := range response.ToolCalls() {
			result, err := p.executeTool(ctx, call, state, onProgress)
			if err != nil {
				toolResults = append(toolResults, workflow.ToolContentBlock{
					Type:      "tool_result",
					ToolUseID: call.ID,
					Content:   fmt.Sprintf("Error: %s", err.Error()),
					IsError:   true,
				})
			} else {
				toolResults = append(toolResults, workflow.ToolContentBlock{
					Type:      "tool_result",
					ToolUseID: call.ID,
					Content:   result,
					IsError:   false,
				})
			}
		}

		// Add tool results as user message
		messages = append(messages, workflow.ToolMessage{
			Role:    "user",
			Content: toolResults,
		})

		// Warn model on penultimate iteration
		if iteration == p.maxIterations-2 {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, workflow.ToolContentBlock{
				Type: "text",
				Text: "[System: This is your second-to-last turn. Please wrap up your analysis and provide a final answer.]",
			})
		}

		// Checkpoint after each iteration
		persistCheckpoint(iteration, messages)
	}

	// Handle max iterations
	if state.FinalAnswer == "" {
		state.Metrics.Truncated = true
		p.logInfo("workflow: forcing finalization (resumed)")

		finalizationPrompt := "[System: You've reached the maximum number of iterations. Please provide your best answer now based on any data you've gathered.]"

		messages = append(messages, workflow.ToolMessage{
			Role: "user",
			Content: []workflow.ToolContentBlock{
				{Type: "text", Text: finalizationPrompt},
			},
		})

		finalResponse, err := toolLLM.CompleteWithTools(ctx, systemPrompt, messages, p.tools, workflow.WithCacheControl())
		if err == nil {
			state.Metrics.LLMCalls++
			state.Metrics.InputTokens += finalResponse.InputTokens
			state.Metrics.OutputTokens += finalResponse.OutputTokens
			state.FinalAnswer = finalResponse.Text()
		}

		if state.FinalAnswer == "" {
			state.FinalAnswer = "I was unable to complete the analysis within the allowed iterations."
		}
	}

	state.Metrics.TotalDuration = time.Since(startTime)

	// Generate follow-up questions (non-blocking, best-effort)
	if state.FinalAnswer != "" {
		state.FollowUpQuestions = p.generateFollowUpQuestions(ctx, userQuestion, state.FinalAnswer)
	}

	result := state.ToWorkflowResult(userQuestion)

	notify(workflow.StageComplete)
	p.logInfo("workflow: complete (resumed)",
		"classification", result.Classification,
		"iterations", state.Metrics.LoopIterations,
		"queries", len(state.ExecutedQueries))

	return result, nil
}

// GetFinalCheckpoint returns the final checkpoint state after completion.
// This is useful for persisting the final state before marking the workflow complete.
func GetFinalCheckpoint(
	messages []workflow.ToolMessage,
	state *LoopState,
) *CheckpointState {
	return &CheckpointState{
		Iteration:       state.Metrics.LoopIterations,
		Messages:        messages,
		ThinkingSteps:   state.ThinkingSteps,
		ExecutedQueries: state.ExecutedQueries,
		Metrics:         state.Metrics,
	}
}

// followUpSystemPrompt is the system prompt for generating follow-up questions.
const followUpSystemPrompt = `Given a Q&A exchange about network data, suggest 2-3 follow-up questions.

Rules:
- Questions should explore different angles or drill deeper into the data
- Do NOT suggest questions that are already answered by the response (e.g., if the response lists totals, don't ask "what's the total?")
- Output ONLY the questions, one per line
- No preamble, no numbering, no bullet points, no explanation
- Each line must be a complete question ending with ?`

// generateFollowUpQuestions generates follow-up question suggestions based on the Q&A.
// Uses a lightweight LLM call with just the question and answer as context.
func (p *Workflow) generateFollowUpQuestions(ctx context.Context, userQuestion, answer string) []string {
	// Get the LLM to use for follow-ups (defaults to main LLM if not configured)
	llm := p.cfg.FollowUpLLM
	if llm == nil {
		llm = p.cfg.LLM
	}

	userPrompt := fmt.Sprintf("User question: %s\n\nAssistant answer: %s", userQuestion, answer)

	response, err := llm.Complete(ctx, followUpSystemPrompt, userPrompt)
	if err != nil {
		p.logInfo("workflow: failed to generate follow-up questions", "error", err)
		return nil
	}

	// Parse response into individual questions (one per line, must end with ?)
	var questions []string
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		// Only include lines that look like questions
		if line != "" && strings.HasSuffix(line, "?") {
			questions = append(questions, line)
		}
	}

	// Limit to 3 questions max
	if len(questions) > 3 {
		questions = questions[:3]
	}

	p.logInfo("workflow: generated follow-up questions", "count", len(questions))
	return questions
}
