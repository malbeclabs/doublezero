package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

const (
	// DefaultMaxIterations is the maximum number of LLM round-trips before stopping.
	DefaultMaxIterations = 10
)

// Pipeline orchestrates the v3 tool-calling pipeline.
type Pipeline struct {
	cfg           *pipeline.Config
	prompts       *Prompts
	systemPrompt  string // Cached system prompt with schema
	tools         []pipeline.ToolDefinition
	maxIterations int
}

// logInfo logs an info message if a logger is configured.
func (p *Pipeline) logInfo(msg string, args ...any) {
	if p.cfg.Logger != nil {
		p.cfg.Logger.Info(msg, args...)
	}
}

// New creates a new v3 Pipeline.
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

	// Convert prompts provider to v3 Prompts
	prompts, ok := cfg.Prompts.(*Prompts)
	if !ok {
		return nil, fmt.Errorf("prompts must be *v3.Prompts")
	}

	// Convert tools to pipeline.ToolDefinition format
	v3Tools := DefaultTools()
	tools := make([]pipeline.ToolDefinition, len(v3Tools))
	for i, t := range v3Tools {
		var schema any
		if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
			return nil, fmt.Errorf("invalid tool schema for %s: %w", t.Name, err)
		}
		tools[i] = pipeline.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		}
	}

	return &Pipeline{
		cfg:           cfg,
		prompts:       prompts,
		tools:         tools,
		maxIterations: DefaultMaxIterations,
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
	startTime := time.Now()

	state := &LoopState{
		Metrics: &PipelineMetrics{},
	}

	// Helper to notify progress
	notify := func(stage pipeline.ProgressStage) {
		if onProgress != nil {
			onProgress(pipeline.Progress{
				Stage: stage,
			})
		}
	}

	// Fetch schema once at the start
	schema, err := p.cfg.SchemaFetcher.FetchSchema(ctx)
	if err != nil {
		notify(pipeline.StageError)
		return nil, fmt.Errorf("failed to fetch schema: %w", err)
	}

	// Build system prompt with schema
	systemPrompt := BuildSystemPrompt(p.prompts.System, schema, p.cfg.FormatContext)

	// Build initial messages
	messages := p.buildMessages(userQuestion, history)

	// Get tool LLM client
	toolLLM, ok := p.cfg.LLM.(pipeline.ToolLLMClient)
	if !ok {
		return nil, fmt.Errorf("LLM client does not support tool calling")
	}

	// Tool-calling loop
	notify(pipeline.StageExecuting)
	p.logInfo("v3 pipeline: starting tool loop", "question", userQuestion)

	for iteration := 0; iteration < p.maxIterations; iteration++ {
		state.Metrics.LoopIterations++

		// Check for context cancellation
		if ctx.Err() != nil {
			notify(pipeline.StageError)
			return nil, ctx.Err()
		}

		// Call LLM with tools
		llmStart := time.Now()
		response, err := toolLLM.CompleteWithTools(ctx, systemPrompt, messages, p.tools, pipeline.WithCacheControl())
		state.Metrics.LLMDuration += time.Since(llmStart)
		state.Metrics.LLMCalls++

		if err != nil {
			notify(pipeline.StageError)
			return nil, fmt.Errorf("LLM call failed: %w", err)
		}

		state.Metrics.InputTokens += response.InputTokens
		state.Metrics.OutputTokens += response.OutputTokens

		p.logInfo("v3 pipeline: LLM response",
			"iteration", iteration+1,
			"stopReason", response.StopReason,
			"toolCalls", len(response.ToolCalls()))

		// Add assistant message to conversation
		messages = append(messages, p.responseToMessage(response))

		// Check if model is done (no tool calls)
		if !response.HasToolCalls() {
			state.FinalAnswer = response.Text()
			break
		}

		// Process tool calls
		toolResults := make([]pipeline.ToolContentBlock, 0)
		for _, call := range response.ToolCalls() {
			result, err := p.executeTool(ctx, call, state)
			if err != nil {
				// Tool execution error - report back to model
				toolResults = append(toolResults, pipeline.ToolContentBlock{
					Type:      "tool_result",
					ToolUseID: call.ID,
					Content:   fmt.Sprintf("Error: %s", err.Error()),
					IsError:   true,
				})
			} else {
				toolResults = append(toolResults, pipeline.ToolContentBlock{
					Type:      "tool_result",
					ToolUseID: call.ID,
					Content:   result,
					IsError:   false,
				})
			}
		}

		// Add tool results as user message
		messages = append(messages, pipeline.ToolMessage{
			Role:    "user",
			Content: toolResults,
		})

		// Warn model on penultimate iteration
		if iteration == p.maxIterations-2 {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, pipeline.ToolContentBlock{
				Type: "text",
				Text: "[System: This is your second-to-last turn. Please wrap up your analysis and provide a final answer.]",
			})
		}
	}

	// Check if we hit max iterations without answer
	if state.FinalAnswer == "" {
		state.Metrics.Truncated = true
		// Use last assistant text if available
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "assistant" {
				for _, block := range messages[i].Content {
					if block.Type == "text" && block.Text != "" {
						state.FinalAnswer = block.Text
						break
					}
				}
				if state.FinalAnswer != "" {
					break
				}
			}
		}
		if state.FinalAnswer == "" {
			state.FinalAnswer = "I was unable to complete the analysis within the allowed iterations."
		}
	}

	state.Metrics.TotalDuration = time.Since(startTime)

	// Convert to PipelineResult
	result := state.ToPipelineResult(userQuestion)

	notify(pipeline.StageComplete)
	p.logInfo("v3 pipeline: complete",
		"classification", result.Classification,
		"iterations", state.Metrics.LoopIterations,
		"queries", len(state.ExecutedQueries),
		"truncated", state.Metrics.Truncated)

	return result, nil
}

// buildMessages constructs the initial message list from conversation history.
func (p *Pipeline) buildMessages(userQuestion string, history []pipeline.ConversationMessage) []pipeline.ToolMessage {
	messages := make([]pipeline.ToolMessage, 0, len(history)+1)

	// Add conversation history
	for _, msg := range history {
		messages = append(messages, pipeline.ToolMessage{
			Role: msg.Role,
			Content: []pipeline.ToolContentBlock{
				{Type: "text", Text: msg.Content},
			},
		})
	}

	// Add current user question
	messages = append(messages, pipeline.ToolMessage{
		Role: "user",
		Content: []pipeline.ToolContentBlock{
			{Type: "text", Text: userQuestion},
		},
	})

	return messages
}

// responseToMessage converts an LLM response to a ToolMessage for conversation history.
func (p *Pipeline) responseToMessage(response *pipeline.ToolLLMResponse) pipeline.ToolMessage {
	content := make([]pipeline.ToolContentBlock, len(response.Content))
	for i, block := range response.Content {
		content[i] = block
	}
	return pipeline.ToolMessage{
		Role:    "assistant",
		Content: content,
	}
}

// executeTool executes a single tool call and returns the result.
func (p *Pipeline) executeTool(ctx context.Context, call pipeline.ToolCallInfo, state *LoopState) (string, error) {
	switch call.Name {
	case "think":
		return p.executeThink(call.Parameters, state)
	case "execute_sql":
		return p.executeSQL(ctx, call.Parameters, state)
	default:
		return "", fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// executeThink handles the think tool - extracts reasoning and records it.
func (p *Pipeline) executeThink(params map[string]any, state *LoopState) (string, error) {
	content, _ := params["content"].(string)
	if content != "" {
		state.ThinkingSteps = append(state.ThinkingSteps, content)
		state.Metrics.ThinkCalls++
		// Log full content for debugging (truncated version in summary log)
		p.logInfo("v3 pipeline: think",
			"thinkStep", state.Metrics.ThinkCalls,
			"contentLen", len(content),
			"preview", truncate(content, 200))
		if p.cfg.Logger != nil {
			p.cfg.Logger.Debug("v3 pipeline: think content", "full", content)
		}
	}
	// Think tool returns empty acknowledgment
	return "Thinking recorded.", nil
}

// executeSQL handles the execute_sql tool - runs queries in parallel.
func (p *Pipeline) executeSQL(ctx context.Context, params map[string]any, state *LoopState) (string, error) {
	queries, err := ParseQueries(params)
	if err != nil || len(queries) == 0 {
		return "", fmt.Errorf("no valid queries provided")
	}

	// Log each query question and SQL for debugging
	p.logInfo("v3 pipeline: executing SQL", "count", len(queries))
	for i, q := range queries {
		qNum := len(state.ExecutedQueries) + i + 1
		p.logInfo("v3 pipeline: query",
			"q", qNum,
			"question", q.Question,
			"sql", truncate(q.SQL, 200))
	}

	// Execute queries in parallel
	sqlStart := time.Now()
	results := make([]pipeline.ExecutedQuery, len(queries))
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
				results[idx] = pipeline.ExecutedQuery{
					GeneratedQuery: pipeline.GeneratedQuery{
						DataQuestion: pipeline.DataQuestion{
							Question: query.Question,
						},
						SQL: sql,
					},
					Result: pipeline.QueryResult{
						SQL:   sql,
						Error: err.Error(),
					},
				}
				return
			}

			results[idx] = pipeline.ExecutedQuery{
				GeneratedQuery: pipeline.GeneratedQuery{
					DataQuestion: pipeline.DataQuestion{
						Question: query.Question,
					},
					SQL: sql,
				},
				Result: queryResult,
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
			p.logInfo("v3 pipeline: query result",
				"q", qNum,
				"question", q.Question,
				"error", result.Result.Error)
		} else {
			p.logInfo("v3 pipeline: query result",
				"q", qNum,
				"question", q.Question,
				"rows", result.Result.Count)
		}
	}

	// Append to state
	state.ExecutedQueries = append(state.ExecutedQueries, results...)

	// Format results for model
	return formatQueryResults(queries, results), nil
}

// formatQueryResults formats query results for the model to consume.
func formatQueryResults(queries []QueryInput, results []pipeline.ExecutedQuery) string {
	var sb strings.Builder
	for i, q := range queries {
		sb.WriteString(fmt.Sprintf("## Q%d: %s\n\n", i+1, q.Question))
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
