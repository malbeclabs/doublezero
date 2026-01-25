package v3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
)

// queryMarkerPattern matches [Q1], [Q2], etc. - markers that should only appear
// when queries have actually been executed.
var queryMarkerPattern = regexp.MustCompile(`\[Q\d+\]`)

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
	// Include graph tools if GraphQuerier is configured
	var v3Tools []Tool
	if cfg.GraphQuerier != nil {
		v3Tools = DefaultToolsWithGraph()
	} else {
		v3Tools = DefaultTools()
	}
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

	// Fetch SQL schema once at the start
	sqlSchema, err := p.cfg.SchemaFetcher.FetchSchema(ctx)
	if err != nil {
		notify(workflow.StageError)
		return nil, fmt.Errorf("failed to fetch SQL schema: %w", err)
	}

	// Fetch graph schema if available
	var graphSchema string
	if p.cfg.GraphSchemaFetcher != nil {
		graphSchema, err = p.cfg.GraphSchemaFetcher.FetchSchema(ctx)
		if err != nil {
			p.logInfo("workflow: failed to fetch graph schema", "error", err)
			// Continue without graph schema - it's optional
		}
	}

	// Build system prompt with schemas (format context is applied only during synthesis)
	systemPrompt := BuildSystemPromptWithGraph(p.prompts.System, sqlSchema, graphSchema, p.prompts.CypherContext, "")

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

	// Track the last text response during the working phase (for conversational responses)
	var lastTextResponse string
	var loopComplete bool

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

		// Track text output (for conversational responses without queries)
		if text := response.Text(); text != "" {
			lastTextResponse = text
			// Emit thinking progress for text output during working phase
			if response.HasToolCalls() && onProgress != nil {
				onProgress(workflow.Progress{
					Stage:           workflow.StageThinking,
					ThinkingContent: text,
				})
			}
		}

		// Check if model is done (no tool calls)
		if !response.HasToolCalls() {
			loopComplete = true
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

	// Handle truncated execution (hit max iterations while still making tool calls)
	if !loopComplete {
		state.Metrics.Truncated = true
		p.logInfo("workflow: hit max iterations", "queries", len(state.ExecutedQueries))
	}

	// Synthesis phase: generate the final user-facing answer
	if len(state.ExecutedQueries) > 0 {
		// Data analysis: run synthesis to produce clean answer from query results
		state.FinalAnswer, err = p.synthesizeAnswer(ctx, toolLLM, systemPrompt, messages, state, userQuestion)
		if err != nil {
			p.logInfo("workflow: synthesis failed, using last response", "error", err)
			state.FinalAnswer = lastTextResponse
		}
	} else {
		// Conversational response: use the direct text output
		state.FinalAnswer = lastTextResponse
	}

	// Final fallback if still no answer
	if state.FinalAnswer == "" {
		state.FinalAnswer = "I was unable to complete the analysis within the allowed iterations."
	}

	// Detect and handle fabrication (query markers without actual queries)
	p.handleFabricationDetection(ctx, toolLLM, systemPrompt, messages, state, userQuestion, onProgress)

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
		"truncated", state.Metrics.Truncated,
		"fabricated", state.Metrics.Fabricated)

	return result, nil
}

// buildMessages constructs the initial message list from conversation history.
func (p *Workflow) buildMessages(userQuestion string, history []workflow.ConversationMessage) []workflow.ToolMessage {
	messages := make([]workflow.ToolMessage, 0, len(history)+1)

	// Add conversation history, skipping messages with empty content
	// (e.g., streaming placeholders that were persisted but never completed)
	for _, msg := range history {
		if msg.Content == "" {
			continue
		}
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

// handleFabricationDetection checks if the response contains query markers without actual queries.
// If fabrication is detected, it retries once with a correction message.
func (p *Workflow) handleFabricationDetection(
	ctx context.Context,
	toolLLM workflow.ToolLLMClient,
	systemPrompt string,
	messages []workflow.ToolMessage,
	state *LoopState,
	userQuestion string,
	onProgress workflow.ProgressCallback,
) {
	// Only check if no queries were executed but response references them
	if len(state.ExecutedQueries) > 0 || !queryMarkerPattern.MatchString(state.FinalAnswer) {
		return
	}

	p.logInfo("workflow: detected fabricated query references, retrying")

	// Add correction message asking the model to try again properly
	messages = append(messages, workflow.ToolMessage{
		Role: "user",
		Content: []workflow.ToolContentBlock{{
			Type: "text",
			Text: "[System: Your response referenced query results (e.g., [Q1]) but you did not actually execute any queries. Please use the execute_sql or execute_cypher tools to retrieve real data before answering. Do not fabricate data.]",
		}},
	})

	// One more LLM call to give the model another chance
	retryResponse, retryErr := toolLLM.CompleteWithTools(ctx, systemPrompt, messages, p.tools)
	if retryErr == nil {
		state.Metrics.LLMCalls++
		state.Metrics.InputTokens += retryResponse.InputTokens
		state.Metrics.OutputTokens += retryResponse.OutputTokens

		// Process any tool calls from the retry
		if retryResponse.HasToolCalls() {
			messages = append(messages, p.responseToMessage(retryResponse))
			for _, call := range retryResponse.ToolCalls() {
				result, err := p.executeTool(ctx, call, state, onProgress)
				toolResult := workflow.ToolContentBlock{
					Type:      "tool_result",
					ToolUseID: call.ID,
					IsError:   err != nil,
				}
				if err != nil {
					toolResult.Content = fmt.Sprintf("Error: %s", err.Error())
				} else {
					toolResult.Content = result
				}
				messages = append(messages, workflow.ToolMessage{
					Role:    "user",
					Content: []workflow.ToolContentBlock{toolResult},
				})
			}
			// If we got queries, synthesize a new answer
			if len(state.ExecutedQueries) > 0 {
				state.FinalAnswer, _ = p.synthesizeAnswer(ctx, toolLLM, systemPrompt, messages, state, userQuestion)
			}
		} else if text := retryResponse.Text(); text != "" {
			state.FinalAnswer = text
		}
	}

	// If still fabricating after retry, mark it and show error
	if len(state.ExecutedQueries) == 0 && queryMarkerPattern.MatchString(state.FinalAnswer) {
		p.logInfo("workflow: fabrication persisted after retry")
		state.FinalAnswer = "I was unable to retrieve the data needed to answer your question. Please try rephrasing your question."
		state.Metrics.Fabricated = true
	}
}

// executeTool executes a single tool call and returns the result.
func (p *Workflow) executeTool(ctx context.Context, call workflow.ToolCallInfo, state *LoopState, onProgress workflow.ProgressCallback) (string, error) {
	switch call.Name {
	case "execute_sql":
		result, err := p.executeSQL(ctx, call.Parameters, state, onProgress)
		if err != nil {
			p.logInfo("workflow: execute_sql failed", "error", err, "params", call.Parameters)
		}
		return result, err
	case "execute_cypher":
		result, err := p.executeCypher(ctx, call.Parameters, state, onProgress)
		if err != nil {
			p.logInfo("workflow: execute_cypher failed", "error", err, "params", call.Parameters)
		}
		return result, err
	case "read_docs":
		result, err := p.readDocs(ctx, call.Parameters, onProgress)
		if err != nil {
			p.logInfo("workflow: read_docs failed", "error", err, "params", call.Parameters)
		}
		return result, err
	default:
		p.logInfo("workflow: unknown tool called", "name", call.Name)
		return "", fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// executeSQL handles the execute_sql tool - runs queries in parallel.
func (p *Workflow) executeSQL(ctx context.Context, params map[string]any, state *LoopState, onProgress workflow.ProgressCallback) (string, error) {
	queries, err := ParseQueries(params)
	if err != nil {
		return "", fmt.Errorf("failed to parse queries: %w", err)
	}
	if len(queries) == 0 {
		return "", fmt.Errorf("no valid queries provided")
	}

	// Log each query question and SQL for debugging
	p.logInfo("workflow: executing SQL", "count", len(queries))
	for i, q := range queries {
		qNum := len(state.ExecutedQueries) + i + 1
		p.logInfo("workflow: query",
			"q", qNum,
			"question", q.Question,
			"sql", truncate(q.SQL, 200))
	}

	// Emit SQL started events for all queries
	if onProgress != nil {
		for _, q := range queries {
			onProgress(workflow.Progress{
				Stage:       workflow.StageSQLStarted,
				SQLQuestion: q.Question,
				SQL:         q.SQL,
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
				state.Metrics.QueryErrors++
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
				// Emit SQL complete with error
				if onProgress != nil {
					onProgress(workflow.Progress{
						Stage:       workflow.StageSQLComplete,
						SQLQuestion: query.Question,
						SQL:         sql,
						SQLError:    err.Error(),
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

			// Emit SQL complete (includes error if query failed)
			if onProgress != nil {
				onProgress(workflow.Progress{
					Stage:       workflow.StageSQLComplete,
					SQLQuestion: query.Question,
					SQL:         sql,
					SQLRows:     queryResult.Count,
					SQLError:    queryResult.Error,
				})
			}
		}(i, q)
	}

	wg.Wait()
	state.Metrics.QueryDuration += time.Since(sqlStart)
	state.Metrics.Queries += len(queries)

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

// executeCypher handles the execute_cypher tool - runs Cypher queries in parallel.
func (p *Workflow) executeCypher(ctx context.Context, params map[string]any, state *LoopState, onProgress workflow.ProgressCallback) (string, error) {
	if p.cfg.GraphQuerier == nil {
		return "", fmt.Errorf("graph database not configured")
	}

	queries, err := ParseCypherQueries(params)
	if err != nil {
		return "", fmt.Errorf("failed to parse Cypher queries: %w", err)
	}
	if len(queries) == 0 {
		return "", fmt.Errorf("no valid Cypher queries provided")
	}

	// Log each query question and Cypher for debugging
	p.logInfo("workflow: executing Cypher", "count", len(queries))
	for i, q := range queries {
		qNum := len(state.ExecutedQueries) + i + 1
		p.logInfo("workflow: cypher query",
			"q", qNum,
			"question", q.Question,
			"cypher", truncate(q.Cypher, 200))
	}

	// Emit Cypher started events for all queries
	if onProgress != nil {
		for _, q := range queries {
			onProgress(workflow.Progress{
				Stage:          workflow.StageCypherStarted,
				CypherQuestion: q.Question,
				Cypher:         q.Cypher,
			})
		}
	}

	// Execute queries in parallel
	cypherStart := time.Now()
	results := make([]workflow.ExecutedQuery, len(queries))
	var wg sync.WaitGroup

	for i, q := range queries {
		wg.Add(1)
		go func(idx int, query CypherQueryInput) {
			defer wg.Done()

			// Clean up Cypher
			cypher := strings.TrimSpace(query.Cypher)

			// Execute query using GraphQuerier
			queryResult, err := p.cfg.GraphQuerier.Query(ctx, cypher)
			if err != nil {
				state.Metrics.QueryErrors++
				results[idx] = workflow.ExecutedQuery{
					GeneratedQuery: workflow.GeneratedQuery{
						DataQuestion: workflow.DataQuestion{
							Question: query.Question,
						},
						SQL: cypher, // Store Cypher in SQL field for compatibility
					},
					Result: workflow.QueryResult{
						SQL:   cypher,
						Error: err.Error(),
					},
				}
				// Emit Cypher complete with error
				if onProgress != nil {
					onProgress(workflow.Progress{
						Stage:          workflow.StageCypherComplete,
						CypherQuestion: query.Question,
						Cypher:         cypher,
						CypherError:    err.Error(),
					})
				}
				return
			}

			results[idx] = workflow.ExecutedQuery{
				GeneratedQuery: workflow.GeneratedQuery{
					DataQuestion: workflow.DataQuestion{
						Question: query.Question,
					},
					SQL: cypher,
				},
				Result: queryResult,
			}

			// Emit Cypher complete
			if onProgress != nil {
				onProgress(workflow.Progress{
					Stage:          workflow.StageCypherComplete,
					CypherQuestion: query.Question,
					Cypher:         cypher,
					CypherRows:     queryResult.Count,
					CypherError:    queryResult.Error,
				})
			}
		}(i, q)
	}

	wg.Wait()
	state.Metrics.QueryDuration += time.Since(cypherStart)
	state.Metrics.Queries += len(queries)

	// Log results for each query
	for i, q := range queries {
		qNum := len(state.ExecutedQueries) + i + 1
		result := results[i]
		if result.Result.Error != "" {
			p.logInfo("workflow: cypher result",
				"q", qNum,
				"question", q.Question,
				"error", result.Result.Error)
		} else {
			p.logInfo("workflow: cypher result",
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
	return formatCypherQueryResults(queries, results, startNum), nil
}

// formatCypherQueryResults formats Cypher query results for the model to consume.
func formatCypherQueryResults(queries []CypherQueryInput, results []workflow.ExecutedQuery, startNum int) string {
	var sb strings.Builder
	for i, q := range queries {
		sb.WriteString(fmt.Sprintf("## Q%d: %s\n\n", startNum+i+1, q.Question))
		result := results[i].Result
		if result.Error != "" {
			sb.WriteString(fmt.Sprintf("**Error:** %s\n\n", result.Error))
		} else {
			sb.WriteString(fmt.Sprintf("```cypher\n%s\n```\n\n", result.SQL))
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

// docsBaseURL is the base URL for fetching raw documentation from GitHub.
const docsBaseURL = "https://raw.githubusercontent.com/malbeclabs/docs/main/docs/"

// readDocs handles the read_docs tool - fetches documentation from GitHub.
func (p *Workflow) readDocs(ctx context.Context, params map[string]any, onProgress workflow.ProgressCallback) (string, error) {
	input, err := ParseReadDocsInput(params)
	if err != nil {
		return "", fmt.Errorf("failed to parse read_docs input: %w", err)
	}

	p.logInfo("workflow: reading docs", "page", input.Page)

	// Emit read_docs started event
	if onProgress != nil {
		onProgress(workflow.Progress{
			Stage:    workflow.StageReadDocsStarted,
			DocsPage: input.Page,
		})
	}

	// Build URL and fetch
	url := docsBaseURL + input.Page + ".md"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		// Emit read_docs complete with error
		if onProgress != nil {
			onProgress(workflow.Progress{
				Stage:     workflow.StageReadDocsComplete,
				DocsPage:  input.Page,
				DocsError: err.Error(),
			})
		}
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Emit read_docs complete with error
		if onProgress != nil {
			onProgress(workflow.Progress{
				Stage:     workflow.StageReadDocsComplete,
				DocsPage:  input.Page,
				DocsError: err.Error(),
			})
		}
		return "", fmt.Errorf("failed to fetch docs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("docs page not found: %s (status %d)", input.Page, resp.StatusCode)
		// Emit read_docs complete with error
		if onProgress != nil {
			onProgress(workflow.Progress{
				Stage:     workflow.StageReadDocsComplete,
				DocsPage:  input.Page,
				DocsError: errMsg,
			})
		}
		return "", errors.New(errMsg)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		// Emit read_docs complete with error
		if onProgress != nil {
			onProgress(workflow.Progress{
				Stage:     workflow.StageReadDocsComplete,
				DocsPage:  input.Page,
				DocsError: err.Error(),
			})
		}
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	content := string(body)

	// Truncate if too long (docs shouldn't be huge, but just in case)
	if len(content) > 10000 {
		content = content[:10000] + "\n\n... (truncated)"
	}

	p.logInfo("workflow: docs fetched", "page", input.Page, "length", len(content))

	// Emit read_docs complete
	if onProgress != nil {
		// Truncate content for progress (just first 200 chars)
		progressContent := content
		if len(progressContent) > 200 {
			progressContent = progressContent[:200] + "..."
		}
		onProgress(workflow.Progress{
			Stage:       workflow.StageReadDocsComplete,
			DocsPage:    input.Page,
			DocsContent: progressContent,
		})
	}

	return fmt.Sprintf("# Documentation: %s\n\n%s", input.Page, content), nil
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

	// Fetch SQL schema once at the start
	sqlSchema, err := p.cfg.SchemaFetcher.FetchSchema(ctx)
	if err != nil {
		notify(workflow.StageError)
		return nil, fmt.Errorf("failed to fetch SQL schema: %w", err)
	}

	// Fetch graph schema if available
	var graphSchema string
	if p.cfg.GraphSchemaFetcher != nil {
		graphSchema, err = p.cfg.GraphSchemaFetcher.FetchSchema(ctx)
		if err != nil {
			p.logInfo("workflow: failed to fetch graph schema", "error", err)
			// Continue without graph schema - it's optional
		}
	}

	// Build system prompt with schemas (format context is applied only during synthesis)
	systemPrompt := BuildSystemPromptWithGraph(p.prompts.System, sqlSchema, graphSchema, p.prompts.CypherContext, "")

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

	// Track the last text response during the working phase (for conversational responses)
	var lastTextResponse string
	var loopComplete bool

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

		// Track text output (for conversational responses without queries)
		if text := response.Text(); text != "" {
			lastTextResponse = text
			// Emit thinking progress for text output during working phase
			if response.HasToolCalls() && onProgress != nil {
				onProgress(workflow.Progress{
					Stage:           workflow.StageThinking,
					ThinkingContent: text,
				})
			}
		}

		// Check if model is done (no tool calls)
		if !response.HasToolCalls() {
			loopComplete = true
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

	// Handle truncated execution (hit max iterations while still making tool calls)
	if !loopComplete {
		state.Metrics.Truncated = true
		p.logInfo("workflow: hit max iterations", "queries", len(state.ExecutedQueries))
	}

	// Synthesis phase: generate the final user-facing answer
	if len(state.ExecutedQueries) > 0 {
		// Data analysis: run synthesis to produce clean answer from query results
		state.FinalAnswer, err = p.synthesizeAnswer(ctx, toolLLM, systemPrompt, messages, state, userQuestion)
		if err != nil {
			p.logInfo("workflow: synthesis failed, using last response", "error", err)
			state.FinalAnswer = lastTextResponse
		}
	} else {
		// Conversational response: use the direct text output
		state.FinalAnswer = lastTextResponse
	}

	// Final fallback if still no answer
	if state.FinalAnswer == "" {
		state.FinalAnswer = "I was unable to complete the analysis within the allowed iterations."
	}

	// Detect and handle fabrication (query markers without actual queries)
	p.handleFabricationDetection(ctx, toolLLM, systemPrompt, messages, state, userQuestion, onProgress)

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
		"truncated", state.Metrics.Truncated,
		"fabricated", state.Metrics.Fabricated)

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

	// Fetch SQL schema
	sqlSchema, err := p.cfg.SchemaFetcher.FetchSchema(ctx)
	if err != nil {
		notify(workflow.StageError)
		return nil, fmt.Errorf("failed to fetch SQL schema: %w", err)
	}

	// Fetch graph schema if available
	var graphSchema string
	if p.cfg.GraphSchemaFetcher != nil {
		graphSchema, err = p.cfg.GraphSchemaFetcher.FetchSchema(ctx)
		if err != nil {
			p.logInfo("workflow: failed to fetch graph schema", "error", err)
			// Continue without graph schema - it's optional
		}
	}

	// Build system prompt with schemas (format context is applied only during synthesis)
	systemPrompt := BuildSystemPromptWithGraph(p.prompts.System, sqlSchema, graphSchema, p.prompts.CypherContext, "")

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
	// Note: Legacy checkpoints only have SQL queries, so we emit StageSQLComplete
	if onProgress != nil {
		for _, eq := range checkpoint.ExecutedQueries {
			onProgress(workflow.Progress{
				Stage:       workflow.StageSQLComplete,
				SQLQuestion: eq.GeneratedQuery.DataQuestion.Question,
				SQL:         eq.Result.SQL,
				SQLRows:     eq.Result.Count,
				SQLError:    eq.Result.Error,
			})
		}
	}

	// Track the last text response during the working phase (for conversational responses)
	var lastTextResponse string
	var loopComplete bool

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

		// Track text output (for conversational responses without queries)
		if text := response.Text(); text != "" {
			lastTextResponse = text
			// Emit thinking progress for text output during working phase
			if response.HasToolCalls() && onProgress != nil {
				onProgress(workflow.Progress{
					Stage:           workflow.StageThinking,
					ThinkingContent: text,
				})
			}
		}

		// Check if model is done (no tool calls)
		if !response.HasToolCalls() {
			loopComplete = true
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

	// Handle truncated execution (hit max iterations while still making tool calls)
	if !loopComplete {
		state.Metrics.Truncated = true
		p.logInfo("workflow: hit max iterations (resumed)", "queries", len(state.ExecutedQueries))
	}

	// Synthesis phase: generate the final user-facing answer
	if len(state.ExecutedQueries) > 0 {
		// Data analysis: run synthesis to produce clean answer from query results
		state.FinalAnswer, err = p.synthesizeAnswer(ctx, toolLLM, systemPrompt, messages, state, userQuestion)
		if err != nil {
			p.logInfo("workflow: synthesis failed, using last response", "error", err)
			state.FinalAnswer = lastTextResponse
		}
	} else {
		// Conversational response: use the direct text output
		state.FinalAnswer = lastTextResponse
	}

	// Final fallback if still no answer
	if state.FinalAnswer == "" {
		state.FinalAnswer = "I was unable to complete the analysis within the allowed iterations."
	}

	// Detect and handle fabrication (query markers without actual queries)
	p.handleFabricationDetection(ctx, toolLLM, systemPrompt, messages, state, userQuestion, onProgress)

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
		"queries", len(state.ExecutedQueries),
		"fabricated", state.Metrics.Fabricated)

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

// baseSynthesisPrompt is the base prompt for the synthesis phase.
// It asks the model to produce a clean, user-facing answer from the data gathered.
// The %s placeholder is for the user's original question.
const baseSynthesisPrompt = `You have finished gathering data. Now answer the user's question: %q

CRITICAL RULES:
1. Start directly with the answer - no preamble like "Based on the data..." or "Here's what I found..."
2. Include [Q1], [Q2] references to cite which query each claim comes from
3. Use tables for multi-attribute lists (validators, devices, links)
4. Keep it concise but thorough

BE HONEST ABOUT FAILURES:
- If your queries returned errors or no data, say so clearly - don't make up an answer
- If you couldn't retrieve the data needed, say "I wasn't able to retrieve the data needed to answer this question" and briefly explain what went wrong
- NEVER invent data or provide estimates based on "typical" values or prior knowledge`

// synthesizeAnswer makes a final LLM call to produce a clean user-facing answer.
// This is the "synthesis phase" that separates working notes from the final response.
func (p *Workflow) synthesizeAnswer(ctx context.Context, llm workflow.ToolLLMClient, systemPrompt string, messages []workflow.ToolMessage, state *LoopState, userQuestion string) (string, error) {
	p.logInfo("workflow: starting synthesis phase", "queries", len(state.ExecutedQueries))

	// Build synthesis prompt, appending format context if configured
	synthesisPrompt := fmt.Sprintf(baseSynthesisPrompt, userQuestion)
	if p.cfg.FormatContext != "" {
		synthesisPrompt += "\n\n# Output Formatting\n\n" + p.cfg.FormatContext
	}

	// Add synthesis prompt to messages
	synthesisMessages := make([]workflow.ToolMessage, len(messages)+1)
	copy(synthesisMessages, messages)
	synthesisMessages[len(synthesisMessages)-1] = workflow.ToolMessage{
		Role: "user",
		Content: []workflow.ToolContentBlock{
			{Type: "text", Text: synthesisPrompt},
		},
	}

	// Make synthesis LLM call (no tools - just produce the answer)
	llmStart := time.Now()
	response, err := llm.CompleteWithTools(ctx, systemPrompt, synthesisMessages, nil, workflow.WithCacheControl())
	state.Metrics.LLMDuration += time.Since(llmStart)
	state.Metrics.LLMCalls++

	if err != nil {
		return "", fmt.Errorf("synthesis LLM call failed: %w", err)
	}

	state.Metrics.InputTokens += response.InputTokens
	state.Metrics.OutputTokens += response.OutputTokens

	answer := response.Text()
	p.logInfo("workflow: synthesis complete", "answerLen", len(answer))

	return answer, nil
}

// followUpSystemPrompt is the system prompt for generating follow-up questions.
const followUpSystemPrompt = `Given a Q&A exchange about DZ network data, suggest 2-3 follow-up questions.

Rules:
- Questions should be related to DZ (DoubleZero) network data and analytics
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
