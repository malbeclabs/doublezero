# Plan: V3 Tool-Calling Pipeline

**Status**: Draft
**Date**: 2025-01-16

## Problem

The current agent pipelines (v1 and v2) use explicit code-orchestrated workflows:
- **V1**: Linear `Classify → Decompose → Generate → Execute → Synthesize` (~400 lines)
- **V2**: Iterative `Classify → Interpret → Map → Plan → Execute → Inspect → Synthesize` (~600 lines)

This creates several issues:
1. **Rigidity** - Adding new capabilities (e.g., "look up entity IDs") requires code changes
2. **Complexity** - V2's iteration logic is ~200 lines of orchestration for what the model could decide naturally
3. **Maintenance** - Each stage has its own prompt, parsing logic, and error handling
4. **Brittleness** - The model can't adapt when something unexpected happens

## Proposal

Simplify to a **tool-calling loop** where the model drives the workflow. The orchestration code shrinks to a generic loop; the workflow becomes prompt-defined guidance.

```
┌─────────────────────────────────────────────┐
│                System Prompt                │
│  - Role & domain knowledge                  │
│  - Workflow guidance (from WORKFLOW.md)     │
│  - Tool descriptions                        │
└─────────────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────┐
│              Tool-Calling Loop              │
│                                             │
│  while not done:                            │
│    response = llm.complete(messages)        │
│    if response.has_tool_calls:              │
│      results = execute_tools(tool_calls)    │
│      messages.append(tool_results)          │
│    else:                                    │
│      return response.text  # final answer   │
└─────────────────────────────────────────────┘
```

## Tools

### Core Tools

| Tool | Description | Parameters |
|------|-------------|------------|
| `think` | Record reasoning, interpretation, or analysis plan. Streams to users. | `content: string` |
| `execute_sql` | Execute one or more SQL queries (runs in parallel) | `queries: [{question, sql}]` |

Two tools total. The `think` tool is a no-op that returns empty string - its purpose is to externalize the model's reasoning so it can be streamed to users. This makes the workflow stages (interpret, map, plan, inspect) visible without enforcing them in code.

### Auxiliary Tools (future, if needed)

| Tool | Description | Parameters |
|------|-------------|------------|
| `get_schema` | Fetch database schema on demand | none |
| `scratchpad_write` | Save intermediate notes/findings | `content: string` |
| `scratchpad_read` | Read saved notes | none |

These tools are deferred. Schema is included in the system prompt (see below), so `get_schema` is only needed if we want dynamic refresh. Scratchpad can be added if conversation context proves insufficient.

## Schema Strategy

**Decision: Include schema in system prompt with prompt caching.**

Rationale:
1. **Most questions need it** - Data analysis is the primary use case; conversational/out-of-scope is rare
2. **Prompt caching** - Anthropic's API caches the system prompt prefix. Schema is large (~15-20KB) but stable, so caching amortizes the cost across requests
3. **Latency** - No extra round trip; model can start reasoning immediately
4. **Simplicity** - No "let me fetch the schema first" step

### System Prompt Construction

```go
func BuildSystemPrompt(schema string, formatContext string) string {
    // Static part (role, workflow, domain knowledge)
    prompt := staticPrompt

    // Add schema
    prompt += fmt.Sprintf("\n\n# Database Schema\n\n%s", schema)

    // Add platform-specific formatting if provided
    if formatContext != "" {
        prompt += fmt.Sprintf("\n\n# Output Formatting\n\n%s", formatContext)
    }

    return prompt
}
```

Schema is fetched once at pipeline start and injected into the system prompt. For multi-turn conversations, we reuse the same schema unless explicitly refreshed.

**Prompt caching note:** Anthropic caches the system prompt prefix. Since our static content comes first, followed by schema, changes to schema will invalidate the cache. This is acceptable since schema changes are rare.

## System Prompt Structure

The system prompt embeds the workflow as guidance, not enforcement:

```markdown
# Role

You are a data analyst for the DoubleZero (DZ) network. You answer questions
by querying a ClickHouse database containing network telemetry and Solana
validator data.

# Tools

You have access to these tools:
- `think`: Record your reasoning (shown to users). Use liberally to explain your process.
- `execute_sql`: Run one or more SQL queries (executed in parallel)

The database schema is provided below - you don't need to fetch it.

# Workflow Guidance

When answering data questions, follow this process. Use the `think` tool at each
stage to record your reasoning - this helps users follow along.

## 1. Interpret
Use `think` to clarify what is actually being asked:
- What type of question? (descriptive, comparative, diagnostic, predictive)
- What entities and time windows are implied?
- What would a wrong answer look like?

## 2. Map to Data
Use `think` to translate to concrete data terms:
- Which tables/views are relevant?
- What is the unit of analysis?
- Are there known caveats or gaps?

If the data doesn't exist, say so explicitly.

## 3. Plan Queries
Use `think` to outline your query plan:
- Start with small validation queries (row counts, time coverage)
- Separate exploration from answer-producing queries
- Batch independent queries in a single `execute_sql` call for parallel execution

## 4. Execute and Inspect
After getting results, use `think` to assess:
- Check row counts against intuition
- Look for outliers or suspiciously clean results
- If results contradict expectations, investigate before proceeding

## 5. Iterate if Needed
Most good answers require refinement:
- Adjust filters after seeing real distributions
- Validate that metrics mean what the question assumes
- Only proceed when the pattern is robust

## 6. Synthesize
Turn data into an answer:
- State what the data shows, not what it implies
- Tie each claim to an observed metric
- Quantify uncertainty and blind spots

# Domain Knowledge

[Insert: ClickHouse syntax, pre-built views, business rules, common joins -
content from existing PLAN.md prompt]

# Response Format

When you have the final answer, respond in natural language with:
- Clear, direct answer to the question
- **Key data points with explicit references to which question/query they came from**
- Any caveats or limitations

## Claim Attribution

Every factual claim must reference its source question. This makes responses transparent
and verifiable. Use a consistent format like:

> "There are 150 validators on DZ [Q1], with total stake of ~12M SOL [Q2]."

Where [Q1], [Q2] reference the numbered data questions from your queries. This allows
users to trace any claim back to the specific query that produced it.

Do NOT wrap your final answer in tool calls.
```

## Parallelization

The `execute_sql` tool accepts multiple queries and runs them in parallel:

```json
{
  "tool": "execute_sql",
  "parameters": {
    "queries": [
      {"question": "How many validators are on DZ?", "sql": "SELECT COUNT(*) FROM..."},
      {"question": "What is the total stake?", "sql": "SELECT SUM(stake) FROM..."},
      {"question": "Which validators disconnected recently?", "sql": "SELECT * FROM..."}
    ]
  }
}
```

All queries execute concurrently and results are returned together. Single-query calls just use an array with one element. This gives us the same parallelization benefits as v1/v2 but with model-driven batching decisions.

## Tool Result Management

Tool results go into conversation history, so we need to manage size carefully. An accidental `SELECT *` on a large table could blow up context.

### Strategy: Truncate with Metadata

Limit results but provide enough metadata for the model to refine if needed:

```go
const (
    MaxRows       = 50    // Max rows to return
    MaxCellChars  = 500   // Truncate individual cell values
    MaxResultSize = 10000 // Total result string cap (chars)
)

type QueryResult struct {
    SQL         string
    Columns     []string
    Rows        []map[string]any  // Capped at MaxRows
    HasMore     bool              // True if more rows exist beyond MaxRows
    Error       string
}
```

**Note on row counts:** We don't know the exact total without a separate `COUNT(*)` query (expensive). Instead, we fetch `MaxRows + 1` and check if we got more:

```go
func (q *Querier) Query(ctx context.Context, sql string) (QueryResult, error) {
    // Fetch one extra row to detect if there are more
    limitedSQL := fmt.Sprintf("SELECT * FROM (%s) LIMIT %d", sql, MaxRows+1)
    rows, err := q.db.Query(ctx, limitedSQL)
    // ...

    result := QueryResult{SQL: sql, Columns: columns}
    if len(rows) > MaxRows {
        result.Rows = rows[:MaxRows]
        result.HasMore = true  // There are more rows
    } else {
        result.Rows = rows
        result.HasMore = false
    }
    return result, nil
}
```

This avoids the cost of `COUNT(*)` while still telling the model "there's more data if you need it."

### Result Formatting

Format results compactly for conversation history:

```
Query: SELECT vote_pubkey, activated_stake_sol FROM solana_validators_on_dz_current
Result: 50 rows (more available)

| vote_pubkey      | activated_stake_sol |
|------------------|---------------------|
| 7K8Hx...abc      | 45000.5             |
| 9Pqr2...def      | 38750.2             |
| ...              | ...                 |

[More rows available - add LIMIT, WHERE, or GROUP BY to refine]
```

Or when all results fit:

```
Query: SELECT COUNT(*) as total FROM solana_validators_on_dz_current
Result: 1 row

| total |
|-------|
| 156   |
```

### Why This Works

The model wrote the query - if results are too big or missing data, it can write a better one:
- Add `ORDER BY ... LIMIT 10` to get top N
- Add `GROUP BY` to aggregate instead of listing
- Add `WHERE` to filter down
- Use `COUNT(*)` to get totals without row data

Truncation is a **signal to refine**, not a loss of information.

### Guidance in System Prompt

Add to the prompt:

```markdown
## Query Results

Results are truncated to 50 rows. If you see "[N more rows truncated]":
- Consider if you need all rows, or just aggregates/top N
- Add LIMIT, GROUP BY, or WHERE clauses to get exactly what you need
- Use COUNT(*) if you just need totals
```

## Conversation History & Multi-Turn

### How It Works

The tool-calling loop naturally maintains context - each tool call and result stays in the message history. For multi-turn conversations:

```
Turn 1: User asks "how many validators on DZ?"
  → Model thinks, queries, answers "There are 156 validators"
  → History: [user_msg, think, execute_sql, tool_result, assistant_answer]

Turn 2: User asks "which ones have the most stake?"
  → Model sees prior context, knows we're talking about DZ validators
  → Can reference previous results or query fresh
```

### No Scratchpad Needed

The conversation history IS the scratchpad. The model can:
- Reference previous query results
- Build on prior reasoning
- Understand context from earlier turns

### Token Management

For long conversations, we need to manage context size:

```go
const (
    MaxHistoryTokens = 50000  // Reserve ~50K tokens for history
    MaxHistoryTurns  = 10     // Or cap by turn count
)

func truncateHistory(messages []Message, maxTokens int) []Message {
    // Keep system prompt (always)
    // Keep current user message (always)
    // Keep recent tool calls and results (prioritize)
    // Summarize or drop older turns if needed
}
```

**Strategy:**
1. Always keep the current turn's full context
2. For older turns, keep the final answer but drop intermediate tool calls/results
3. If still too long, summarize older answers into a "conversation summary" message

### Follow-up Questions Feel Connected

Follow-ups work naturally because the model sees the full conversation. When user clicks a suggested follow-up, it flows into the same context, so the model understands what "they" or "it" refers to.

## Loop Termination

### Max Iterations

```go
const (
    MaxToolCalls    = 15    // Max tool invocations per question
    MaxLLMRounds    = 10    // Max LLM API calls per question
)
```

### Graceful Degradation

When approaching limits, don't hard stop. Instead, inject guidance:

```go
func (p *Pipeline) runLoop(ctx context.Context, messages []Message) (*Result, error) {
    for round := 0; round < MaxLLMRounds; round++ {
        // On penultimate round, add guidance
        if round == MaxLLMRounds - 2 {
            messages = append(messages, Message{
                Role: "user",
                Content: "[System: You have one more round. Please provide your best answer with the information gathered so far. If analysis is incomplete, state what you found and what remains uncertain.]",
            })
        }

        response, err := p.llm.CompleteWithTools(ctx, messages, p.tools)
        // ... handle response
    }

    // If we exit the loop without an answer, synthesize from what we have
    return p.synthesizeBestEffort(messages)
}
```

### What Users See

```
🧠 Thinking: I've gathered partial data but need more queries to be certain...

⚠️ [Analysis limit reached - providing best answer with available data]

✅ Answer: Based on the data I was able to gather, there are approximately
   150 validators on DZ with ~12M SOL stake. I wasn't able to fully verify
   the regional breakdown due to query limits.
```

## Structured Data Extraction

The `Runner` interface expects structured output. We extract this from tool calls:

### Tracking During Loop

```go
type LoopState struct {
    ThinkingSteps   []string         // Content from think() calls
    ExecutedQueries []ExecutedQuery  // All SQL executed
    FinalAnswer     string           // Last assistant text (non-tool response)
}

func (p *Pipeline) runLoop(ctx context.Context, messages []Message) (*LoopState, error) {
    state := &LoopState{}

    for {
        response, err := p.llm.CompleteWithTools(ctx, messages, p.tools)
        if err != nil {
            return nil, err
        }

        // Extract from tool calls
        for _, call := range response.ToolCalls {
            switch call.Name {
            case "think":
                state.ThinkingSteps = append(state.ThinkingSteps, call.Parameters["content"].(string))
            case "execute_sql":
                queries := call.Parameters["queries"].([]QueryInput)
                results := p.executeQueries(ctx, queries)
                for i, q := range queries {
                    state.ExecutedQueries = append(state.ExecutedQueries, ExecutedQuery{
                        GeneratedQuery: GeneratedQuery{
                            DataQuestion: DataQuestion{Question: q.Question},
                            SQL:          q.SQL,
                        },
                        Result: results[i],
                    })
                }
            }
        }

        // Check for final answer
        if response.StopReason == "end_turn" && response.Content != "" {
            state.FinalAnswer = response.Content
            break
        }
    }

    return state, nil
}
```

### Mapping to PipelineResult

```go
func (state *LoopState) ToPipelineResult(userQuestion string) *pipeline.PipelineResult {
    result := &pipeline.PipelineResult{
        UserQuestion:    userQuestion,
        Classification:  state.InferClassification(),
        Answer:          state.FinalAnswer,
        ExecutedQueries: state.ExecutedQueries,
        Metrics:         state.Metrics,
    }

    // Extract DataQuestions and GeneratedQueries from ExecutedQueries
    for _, eq := range state.ExecutedQueries {
        result.DataQuestions = append(result.DataQuestions, eq.GeneratedQuery.DataQuestion)
        result.GeneratedQueries = append(result.GeneratedQueries, eq.GeneratedQuery)
    }

    return result
}
```

### Classification Detection

Classification is inferred from behavior rather than a separate LLM call:

```go
func (state *LoopState) InferClassification() Classification {
    // If model executed SQL queries, it's data analysis
    if len(state.ExecutedQueries) > 0 {
        return ClassificationDataAnalysis
    }

    // If model used think tool but no queries, check if it's explaining inability
    // (This catches "I don't have data for that" type responses)
    if len(state.ThinkingSteps) > 0 {
        // Could add heuristics here, but default to conversational
        return ClassificationConversational
    }

    // Direct response with no tools = conversational or out-of-scope
    // Could parse response for out-of-scope indicators, but conversational is safe default
    return ClassificationConversational
}
```

**Why this works:**
- **Data analysis** questions naturally lead to SQL queries
- **Conversational** questions (clarifications, capabilities) get direct answers
- **Out-of-scope** questions get polite redirects without queries

The model handles classification implicitly through its behavior. We just observe what it did.

## Claim Attribution in Responses

To maintain transparency and traceability, the agent must attribute each factual claim to its source question/query. This is similar to v1's behavior where references like `[Q1]`, `[Q2]` appear throughout the response.

### Why This Matters

1. **Transparency** - Users can see where each claim came from
2. **Verifiability** - Claims can be traced back to specific queries
3. **Trust** - Shows the agent isn't making up data
4. **Debugging** - Easier to identify which query produced incorrect results

### Implementation

The system prompt instructs the model to:
1. Number data questions as they're asked (Q1, Q2, etc.)
2. Reference these numbers in the final answer

Example response:
```
There are 150 validators currently participating in DZ [Q1], collectively staking
approximately 12M SOL [Q2]. Of these, 85% are in North America [Q3], with the
remaining 15% distributed across Europe and Asia.
```

The `execute_sql` tool already requires a `question` field for each query, which naturally creates the numbered questions for reference. The model just needs to maintain this numbering in its response.

### Prompt Guidance

The system prompt includes explicit guidance (in Response Format section):
- "Every factual claim must reference its source question"
- "Use a consistent format like [Q1], [Q2]"
- "This allows users to trace any claim back to the specific query"

## Follow-up Questions

### Approach: Dedicated LLM Call

Keep follow-ups separate from the main loop for cleaner separation:

```go
func (p *Pipeline) generateFollowUps(ctx context.Context, question string, answer string, queries []ExecutedQuery) ([]string, error) {
    prompt := fmt.Sprintf(`Based on this Q&A exchange, suggest 2-3 natural follow-up questions the user might ask.

Question: %s

Answer: %s

Queries executed: %v

Respond with a JSON array of follow-up questions. Make them specific and connected to the conversation.`,
        question, answer, formatQueriesForPrompt(queries))

    response, err := p.cfg.LLM.Complete(ctx, p.prompts.FollowUp, prompt)
    // Parse JSON array from response
}
```

### Why Separate?

1. **Cleaner main loop** - The agent focuses on answering, not meta-tasks
2. **Consistent quality** - Dedicated prompt tuned for good follow-ups
3. **Optional** - Can skip for Slack or other contexts where follow-ups aren't shown
4. **Cheaper** - Can use a smaller/faster model for follow-up generation

### Connected Feel

Follow-ups feel connected because we pass the full context (question, answer, queries) to the follow-up generator. It sees what was discussed and suggests relevant next steps:

```
User: "How many validators are on DZ?"
Answer: "There are 156 validators with 12.4M SOL stake..."

Follow-ups (generated with context):
- "Which validators have the most stake?"
- "How does this compare to last week?"
- "Are there any validators that recently disconnected?"
```

## Platform-Specific Formatting

### Approach: Append to System Prompt

Same as v2 - the `FormatContext` is appended to the system prompt via `BuildSystemPrompt(schema, formatContext)` (see Schema Strategy section above).

### Example: Slack Formatting

```markdown
# Output Formatting

You are responding in Slack. Follow these guidelines:
- Use Slack markdown: *bold*, _italic_, `code`, ```code blocks```
- Keep responses concise (Slack has character limits)
- Use bullet points for lists
- Avoid large tables - summarize instead
- Use emoji sparingly: ✅ ❌ ⚠️ for status
```

### Why This Works

- **Simple** - Just string concatenation
- **Flexible** - Any platform can define its own context
- **Cached** - Format context is part of the cacheable system prompt prefix
- **Tested** - This is exactly what v2 does and it works well

## SQL Error Handling

### Current Behavior

ClickHouse returns errors like:
```
Code: 62. DB::Exception: Syntax error: failed at position 45 (end of query):
WHER validator_count > 10. Expected one of: token, Comma, ...
```

### Enhanced Error Response

Wrap the error with hints for the model:

```go
func formatSQLError(sql string, chError string) string {
    var sb strings.Builder
    sb.WriteString("Query failed:\n")
    sb.WriteString(chError)
    sb.WriteString("\n\nHints:\n")

    // Pattern-match common errors and add hints
    switch {
    case strings.Contains(chError, "Syntax error"):
        sb.WriteString("- Check for typos in keywords (WHERE, SELECT, FROM)\n")
        sb.WriteString("- Verify column and table names exist in schema\n")
    case strings.Contains(chError, "Unknown identifier"):
        sb.WriteString("- Column may not exist - check schema for correct name\n")
        sb.WriteString("- May need table alias if using JOINs\n")
    case strings.Contains(chError, "Type mismatch"):
        sb.WriteString("- Check column types in schema\n")
        sb.WriteString("- May need CAST() or toString()\n")
    case strings.Contains(chError, "Memory limit"):
        sb.WriteString("- Query too expensive - add LIMIT or more filters\n")
        sb.WriteString("- Consider aggregating instead of selecting all rows\n")
    case strings.Contains(chError, "Timeout"):
        sb.WriteString("- Query took too long - add more specific filters\n")
        sb.WriteString("- Consider using a materialized view if available\n")
    }

    sb.WriteString("\nPlease fix the query and try again.")
    return sb.String()
}
```

### Model Iteration

The model naturally iterates on errors:

```
⚡ Executing: SELECT * FROM validators WHER status = 'active'

❌ Query failed: Syntax error at position 32...
   Hints:
   - Check for typos in keywords (WHERE, SELECT, FROM)

🧠 Thinking: I made a typo - "WHER" should be "WHERE". Let me fix that.

⚡ Executing: SELECT * FROM validators WHERE status = 'active'

✅ Results (156 rows)...
```

### System Prompt Guidance

Add to prompt:

```markdown
## Handling Query Errors

If a query fails:
1. Read the error message carefully
2. Check the hints provided
3. Verify table/column names against the schema
4. Fix the issue and retry
5. If stuck after 2-3 attempts, explain the issue to the user
```

## Streaming via SSE

### What Gets Streamed

Users see real-time updates as the agent works:

| Event Type | Payload | When |
|------------|---------|------|
| `thinking` | `{content: "..."}` | Each `think` tool call |
| `executing` | `{question: "...", sql: "..."}` | Each query in `execute_sql` |
| `result` | `{question: "...", rowCount: N, hasMore: bool}` | Query completes |
| `answer` | `{content: "..."}` | Final response |
| `followups` | `{questions: [...]}` | Follow-up generation completes |
| `error` | `{message: "..."}` | Any error |

### SSE Integration

The tool-calling loop emits events via a callback:

```go
type StreamEvent struct {
    Type    string         `json:"type"`
    Payload map[string]any `json:"payload"`
}

type StreamCallback func(event StreamEvent)

func (p *Pipeline) RunWithStream(
    ctx context.Context,
    question string,
    history []ConversationMessage,
    onEvent StreamCallback,
) (*PipelineResult, error) {
    // ... in the loop:
    for _, call := range response.ToolCalls {
        switch call.Name {
        case "think":
            onEvent(StreamEvent{
                Type:    "thinking",
                Payload: map[string]any{"content": call.Parameters["content"]},
            })
        case "execute_sql":
            for _, q := range queries {
                onEvent(StreamEvent{
                    Type:    "executing",
                    Payload: map[string]any{"question": q.Question, "sql": q.SQL},
                })
            }
            // Execute and stream results...
        }
    }
}
```

### HTTP Handler

```go
func (h *ChatHandler) HandleSSE(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")

    flusher := w.(http.Flusher)

    pipeline.RunWithStream(ctx, question, history, func(event StreamEvent) {
        data, _ := json.Marshal(event)
        fmt.Fprintf(w, "data: %s\n\n", data)
        flusher.Flush()
    })
}
```

### Structured Data via Tool Calls

**Decision: Keep it simple with just `think` and `execute_sql`.**

We extract structured data from tool calls rather than adding more tools:
- **DataQuestions** → from `execute_sql` query questions
- **ThinkingSteps** → from `think` content
- **Classification** → inferred from whether tools were called

Adding tools like `register_data_question()` or `record_interpretation()` would:
- Complicate the prompt with more tools to explain
- Risk model not using them correctly
- Add overhead without clear benefit

The query "question" field captures what we need for DataQuestions - a clear statement of what information is being sought. Think content is for user visibility and debugging, not structured extraction.

## Metrics & Observability

### Metrics Tracked

```go
type PipelineMetrics struct {
    // LLM usage
    LLMCalls       int           // Total API calls to LLM
    InputTokens    int           // Total input tokens
    OutputTokens   int           // Total output tokens

    // Tool usage
    ThinkCalls     int           // Number of think() invocations
    SQLQueries     int           // Total SQL queries executed
    SQLErrors      int           // Queries that returned errors

    // Timing
    TotalDuration  time.Duration // End-to-end time
    LLMDuration    time.Duration // Time spent in LLM calls
    SQLDuration    time.Duration // Time spent executing SQL

    // Loop behavior
    LoopIterations int           // LLM round-trips
    Truncated      bool          // Hit max iterations
}
```

### Recording Metrics

```go
func (p *Pipeline) runLoop(ctx context.Context, ...) (*LoopState, error) {
    metrics := &PipelineMetrics{}
    startTime := time.Now()

    for round := 0; round < MaxLLMRounds; round++ {
        metrics.LoopIterations++

        llmStart := time.Now()
        response, tokens, err := p.llm.CompleteWithTools(ctx, ...)
        metrics.LLMDuration += time.Since(llmStart)
        metrics.LLMCalls++
        metrics.InputTokens += tokens.Input
        metrics.OutputTokens += tokens.Output

        for _, call := range response.ToolCalls {
            switch call.Name {
            case "think":
                metrics.ThinkCalls++
            case "execute_sql":
                sqlStart := time.Now()
                results := p.executeQueries(ctx, queries)
                metrics.SQLDuration += time.Since(sqlStart)
                metrics.SQLQueries += len(queries)
                for _, r := range results {
                    if r.Error != "" {
                        metrics.SQLErrors++
                    }
                }
            }
        }
    }

    metrics.TotalDuration = time.Since(startTime)
    state.Metrics = metrics
    return state, nil
}
```

### Exposing Metrics

```go
// Include in PipelineResult for API response
type PipelineResult struct {
    // ... existing fields
    Metrics *PipelineMetrics `json:"metrics,omitempty"`
}

// Also emit to observability system
func (p *Pipeline) recordMetrics(metrics *PipelineMetrics, classification Classification) {
    // Prometheus, OpenTelemetry, or custom metrics
    pipelineRunsTotal.WithLabelValues(string(classification)).Inc()
    llmCallsHistogram.Observe(float64(metrics.LLMCalls))
    sqlQueriesHistogram.Observe(float64(metrics.SQLQueries))
    latencyHistogram.Observe(metrics.TotalDuration.Seconds())
}
```

## Cancellation Handling

### Context Checking

Check for cancellation at key points in the loop:

```go
func (p *Pipeline) runLoop(ctx context.Context, messages []Message) (*LoopState, error) {
    state := &LoopState{}

    for round := 0; round < MaxLLMRounds; round++ {
        // Check before LLM call
        if err := ctx.Err(); err != nil {
            return state, fmt.Errorf("cancelled: %w", err)
        }

        response, err := p.llm.CompleteWithTools(ctx, messages, p.tools)
        if err != nil {
            if ctx.Err() != nil {
                return state, fmt.Errorf("cancelled during LLM call: %w", ctx.Err())
            }
            return nil, err
        }

        // Check before SQL execution
        if err := ctx.Err(); err != nil {
            return state, fmt.Errorf("cancelled: %w", err)
        }

        // Execute tools...
    }

    return state, nil
}
```

### Partial Results on Cancel

If cancelled mid-way, return what we have:

```go
func (p *Pipeline) Run(ctx context.Context, question string) (*PipelineResult, error) {
    state, err := p.runLoop(ctx, messages)

    if err != nil && ctx.Err() != nil {
        // Cancelled - return partial result with note
        result := state.ToPipelineResult(question)
        result.Answer = "[Analysis cancelled]\n\n" + synthesizePartial(state)
        result.Cancelled = true
        return result, nil
    }

    // Normal completion...
}
```

## Implementation

### Directory Structure

```
agent/pkg/pipeline/v3/
├── pipeline.go      # Tool-calling loop (~150 lines)
├── tools.go         # Tool definitions and execution (~100 lines)
├── prompts.go       # Prompt loading
└── prompts/
    └── SYSTEM.md    # Combined system prompt with workflow guidance
```

### Pipeline Interface

V3 implements the same `Runner` interface as v1/v2:

```go
type Pipeline struct {
    cfg     *pipeline.Config
    tools   []Tool
}

func (p *Pipeline) Run(ctx context.Context, question string) (*pipeline.PipelineResult, error)
func (p *Pipeline) RunWithHistory(ctx context.Context, question string, history []ConversationMessage) (*pipeline.PipelineResult, error)
func (p *Pipeline) RunWithProgress(ctx context.Context, question string, history []ConversationMessage, onProgress ProgressCallback) (*pipeline.PipelineResult, error)
```

### Tool Definition (Anthropic JSON Schema Format)

Align with Anthropic's tool format for direct API compatibility:

```go
// Tool definition matches Anthropic's expected format
type Tool struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"input_schema"` // JSON Schema
    Execute     func(ctx context.Context, params map[string]any) (string, error) `json:"-"`
}

// Define tools with JSON Schema
var ThinkTool = Tool{
    Name:        "think",
    Description: "Record your reasoning, interpretation, or analysis plan. This is shown to users so they can follow your thought process.",
    InputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "content": {
                "type": "string",
                "description": "Your reasoning or analysis"
            }
        },
        "required": ["content"]
    }`),
    Execute: func(ctx context.Context, params map[string]any) (string, error) {
        return "", nil // No-op, just for streaming
    },
}

var ExecuteSQLTool = Tool{
    Name:        "execute_sql",
    Description: "Execute one or more SQL queries against the ClickHouse database. Queries run in parallel.",
    InputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "queries": {
                "type": "array",
                "items": {
                    "type": "object",
                    "properties": {
                        "question": {
                            "type": "string",
                            "description": "The data question this query answers, e.g. 'How many validators are on DZ?'"
                        },
                        "sql": {
                            "type": "string",
                            "description": "The SQL query to execute"
                        }
                    },
                    "required": ["question", "sql"]
                },
                "description": "List of queries to execute"
            }
        },
        "required": ["queries"]
    }`),
}
```

### LLM Client Changes

The `LLMClient` interface needs to support tool calling with system prompt for caching:

```go
type LLMClient interface {
    // Existing method for non-tool completions
    Complete(ctx context.Context, systemPrompt, userPrompt string, opts ...CompleteOption) (string, error)

    // New method for tool-calling
    // systemPrompt is separate to enable prompt caching
    CompleteWithTools(
        ctx context.Context,
        systemPrompt string,           // Separate for prompt caching
        messages []Message,
        tools []Tool,
        opts ...CompleteOption,
    ) (*ToolResponse, error)
}

// Message types aligned with Anthropic API
type Message struct {
    Role    string        `json:"role"` // "user" or "assistant"
    Content []ContentBlock `json:"content"`
}

type ContentBlock struct {
    Type      string         `json:"type"` // "text", "tool_use", "tool_result"
    Text      string         `json:"text,omitempty"`
    ID        string         `json:"id,omitempty"`         // For tool_use
    Name      string         `json:"name,omitempty"`       // For tool_use
    Input     map[string]any `json:"input,omitempty"`      // For tool_use
    ToolUseID string         `json:"tool_use_id,omitempty"` // For tool_result
    Content   string         `json:"content,omitempty"`    // For tool_result
}

type ToolResponse struct {
    StopReason   string         // "end_turn" or "tool_use"
    Content      []ContentBlock // May include both text and tool_use blocks
    InputTokens  int
    OutputTokens int
}

// Helper to extract tool calls from response
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

// Helper to extract text content from response
func (r *ToolResponse) Text() string {
    for _, block := range r.Content {
        if block.Type == "text" {
            return block.Text
        }
    }
    return ""
}
```

### Multiple Tool Calls Per Turn

Claude can call multiple tools in one response. Handle them all:

```go
func (p *Pipeline) processToolCalls(ctx context.Context, response *ToolResponse, state *LoopState, onEvent StreamCallback) []ContentBlock {
    var toolResults []ContentBlock

    for _, call := range response.ToolCalls() {
        var result string
        var err error

        switch call.Name {
        case "think":
            content := call.Parameters["content"].(string)
            state.ThinkingSteps = append(state.ThinkingSteps, content)
            onEvent(StreamEvent{Type: "thinking", Payload: map[string]any{"content": content}})
            result = "" // No-op

        case "execute_sql":
            queries := parseQueries(call.Parameters["queries"])
            for _, q := range queries {
                onEvent(StreamEvent{Type: "executing", Payload: map[string]any{"question": q.Question, "sql": q.SQL}})
            }
            results := p.executeQueries(ctx, queries)
            result = formatResults(results)
            // ... track in state
        }

        toolResults = append(toolResults, ContentBlock{
            Type:      "tool_result",
            ToolUseID: call.ID,
            Content:   result,
        })
    }

    return toolResults
}
```

### Progress Tracking

V3 emits progress based on tool calls rather than fixed stages:

| Event | Progress Stage | Streamed to User |
|-------|----------------|------------------|
| Loop start | `thinking` | - |
| `think` called | `thinking` | Yes - content shown in real-time |
| `execute_sql*` called | `executing` | Query question/SQL shown |
| Final response | `complete` | Final answer |

The `think` tool provides the primary mechanism for streaming progress. Users see the model's reasoning as it works through the problem, similar to how v2's discrete stages provided visibility.

**Example user experience:**

```
User: "Why did our stake share decrease?"

🧠 Thinking: This is a diagnostic question about stake share change. I need to:
   - Find current vs recent stake share
   - Identify validators that disconnected
   - Quantify their stake contribution

🧠 Thinking: I'll use solana_validators_disconnections to find validators
   that left recently, and calculate their stake impact.

⚡ Executing: SELECT vote_pubkey, activated_stake_sol, disconnected_ts
             FROM solana_validators_disconnections
             WHERE disconnected_ts > now() - INTERVAL 24 HOUR

🧠 Thinking: Found 2 validators that disconnected in the past 24h with
   combined stake of 450K SOL. Let me verify the stake share impact...

⚡ Executing: SELECT ... (stake share calculation)

✅ Answer: Your stake share decreased because 2 validators disconnected
   in the past 24 hours: validator ABC (300K SOL) and validator XYZ
   (150K SOL), reducing your total stake share by approximately 0.8%.
```

### Classification

V3 handles classification within the prompt rather than as a separate stage:

```markdown
# Handling Different Question Types

Before querying data, determine if this is:

1. **Data analysis** - Requires SQL queries. Proceed with the workflow.
2. **Conversational** - About your capabilities, clarifying previous answers, etc.
   Respond directly without querying data.
3. **Out of scope** - Not related to DZ network or Solana validators.
   Politely redirect to your domain.
```

The model naturally routes based on the question, eliminating the separate CLASSIFY stage.

## Migration Path

1. **Phase 1**: Implement v3 alongside v1/v2, selectable via `PIPELINE_VERSION=v3`
2. **Phase 2**: Run evals against v3, compare quality and cost
3. **Phase 3**: If v3 performs well, deprecate v1/v2

## Tradeoffs

### Advantages
- **~70% less orchestration code** (150 lines vs 600)
- **Flexible** - Model adapts to unexpected situations
- **Extensible** - Add tools without changing orchestration
- **Natural iteration** - Model decides when to refine

### Disadvantages
- **Less predictable** - Token usage and latency vary by question
- **Harder to debug** - Emergent behavior vs explicit stages
- **Prompt engineering** - Workflow quality depends on prompt clarity

### Mitigations
- **Max iterations cap** - Limit tool-calling rounds (default 10)
- **Logging** - Log all tool calls and responses for debugging
- **Cost tracking** - Track tokens per query for monitoring

## Success Criteria

1. **Evals pass** - V3 achieves same or better pass rate on existing evals
2. **Code reduction** - Orchestration code < 200 lines (vs ~1000 for v1+v2)
3. **Comparable cost** - Average tokens/query within 20% of v2
4. **Latency acceptable** - P95 latency < 30s for typical questions

## Open Questions

1. **Prompt caching with tools** - Verify that Anthropic's prompt caching works with tool-calling mode (system prompt should still be cacheable)
2. **Think tool verbosity** - How much should the model think? May need tuning to balance visibility vs token cost
3. **History summarization** - When truncating old turns, should we use LLM summarization or simple truncation? Summarization is smarter but adds latency/cost
4. **Follow-up model** - Can we use a smaller/cheaper model (Haiku) for follow-up generation since it's a simpler task?

## Decisions Made

- **Error recovery**: Model handles naturally with error hints; no explicit retry logic needed
- **Scratchpad**: Not needed; conversation history serves as scratchpad
- **Schema**: Included in system prompt with caching, not fetched via tool
- **Follow-ups**: Dedicated LLM call after main loop for cleaner separation
- **Platform formatting**: Append to system prompt (same as v2)
- **Structured data**: Extract from tool calls (questions → DataQuestions), don't add more tools
- **Classification**: Infer from behavior (SQL queries = data_analysis, no queries = conversational)
- **Row counts**: Use HasMore flag (fetch N+1) instead of expensive COUNT(*)
- **Tool schema**: Align with Anthropic JSON Schema format for direct API compatibility
- **System prompt**: Separate param in CompleteWithTools for prompt caching

## Tasks

### Core Implementation
- [ ] Add `CompleteWithTools` to `LLMClient` interface (with systemPrompt param)
- [ ] Implement Anthropic tool-calling in `anthropic.go` (aligned with API types)
- [ ] Create v3 package structure
- [ ] Implement tool definitions with JSON Schema (`think`, `execute_sql`)
- [ ] Implement tool-calling loop with multiple tools per turn support
- [ ] Implement graceful loop termination (penultimate round guidance)
- [ ] Implement structured data extraction (LoopState → PipelineResult)
- [ ] Implement classification inference (from tool usage behavior)
- [ ] Implement cancellation handling (context checking, partial results)

### Streaming
- [ ] Define SSE event types (thinking, executing, result, answer, followups, error)
- [ ] Implement `RunWithStream` with StreamCallback
- [ ] Update HTTP handler for SSE streaming

### Result & Error Handling
- [ ] Implement result truncation (MaxRows+1 fetch, HasMore flag)
- [ ] Implement cell value truncation (MaxCellChars)
- [ ] Implement SQL error hints (formatSQLError with pattern matching)

### Metrics
- [ ] Define PipelineMetrics struct (LLM calls, tokens, SQL queries, timing)
- [ ] Implement metrics collection in loop
- [ ] Add metrics to PipelineResult
- [ ] Integrate with observability system (Prometheus/OpenTelemetry)

### Context Management
- [ ] Implement conversation history truncation (token/turn limits)
- [ ] Implement follow-up question generation (dedicated LLM call)

### Prompts
- [ ] Write `SYSTEM.md` prompt (merge WORKFLOW.md + domain knowledge from PLAN.md)
- [ ] Add query results guidance to prompt
- [ ] Add error handling guidance to prompt
- [ ] Add classification guidance to prompt

### Integration
- [ ] Add v3 to factory with version selection
- [ ] Update frontend to display `think` content in real-time
- [ ] Update frontend to handle new SSE event types

### Validation
- [ ] Run evals, compare results with v2
- [ ] Test multi-turn conversations
- [ ] Test error recovery scenarios
- [ ] Test cancellation behavior
- [ ] Test streaming events
- [ ] Document findings
