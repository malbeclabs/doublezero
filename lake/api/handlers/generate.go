package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/getsentry/sentry-go"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow/prompts"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

type GenerateRequest struct {
	Prompt       string           `json:"prompt"`
	CurrentQuery string           `json:"currentQuery,omitempty"`
	History      []HistoryMessage `json:"history,omitempty"`
}

type HistoryMessage struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

type GenerateResponse struct {
	SQL      string `json:"sql"`
	Provider string `json:"provider,omitempty"`
	Attempts int    `json:"attempts,omitempty"`
	Error    string `json:"error,omitempty"`
}

const maxValidationAttempts = 3

// Cached prompts for query generation
var (
	cachedGeneratePrompt   string
	cachedSQLContext       string
	cachedPromptsOnce      sync.Once
	cachedPromptsErr       error
)

func loadGeneratePrompts() error {
	cachedPromptsOnce.Do(func() {
		// Load SQL_CONTEXT
		sqlContextData, err := prompts.PromptsFS.ReadFile("SQL_CONTEXT.md")
		if err != nil {
			cachedPromptsErr = fmt.Errorf("failed to load SQL_CONTEXT: %w", err)
			return
		}
		cachedSQLContext = strings.TrimSpace(string(sqlContextData))

		// Load GENERATE.md and compose with SQL_CONTEXT
		generateData, err := prompts.PromptsFS.ReadFile("GENERATE.md")
		if err != nil {
			cachedPromptsErr = fmt.Errorf("failed to load GENERATE: %w", err)
			return
		}
		rawPrompt := strings.TrimSpace(string(generateData))
		cachedGeneratePrompt = strings.ReplaceAll(rawPrompt, "{{SQL_CONTEXT}}", cachedSQLContext)
	})
	return cachedPromptsErr
}

func getGeneratePrompt() (string, error) {
	if err := loadGeneratePrompts(); err != nil {
		return "", err
	}
	return cachedGeneratePrompt, nil
}

func GenerateSQL(w http.ResponseWriter, r *http.Request) {
	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Prompt) == "" {
		http.Error(w, "Prompt is required", http.StatusBadRequest)
		return
	}

	// Fetch schema using shared DBSchemaFetcher
	schemaFetcher := NewDBSchemaFetcher()
	schema, err := schemaFetcher.FetchSchema(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GenerateResponse{Error: internalError("Failed to fetch schema", err)})
		return
	}

	// Require Anthropic API key
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GenerateResponse{Error: "ANTHROPIC_API_KEY environment variable is not set"})
		return
	}

	var sql string
	var lastError string
	attempts := 0

	// Generate and validate loop
	for attempts < maxValidationAttempts {
		attempts++

		// Build prompt - include current query context and previous error if retry
		prompt := req.Prompt
		if req.CurrentQuery != "" {
			prompt = fmt.Sprintf("Current query:\n%s\n\nUser request: %s", req.CurrentQuery, req.Prompt)
		}
		if lastError != "" {
			prompt = fmt.Sprintf("Previous SQL had an error: %s\n\nPlease fix this query for the original request: %s", lastError, req.Prompt)
		}

		// Generate SQL
		sql, err = generateWithAnthropic(r.Context(), schema, prompt, req.History)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(GenerateResponse{Error: internalError("Failed to generate SQL", err), Provider: "anthropic", Attempts: attempts})
			return
		}

		// Clean up response
		sql = cleanSQL(sql)

		// Validate with EXPLAIN
		validationErr := validateQuery(sql)
		if validationErr == "" {
			// Query is valid
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(GenerateResponse{SQL: sql, Provider: "anthropic", Attempts: attempts})
			return
		}

		// Store error for retry
		lastError = validationErr
	}

	// Max attempts reached, return last SQL with validation error
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GenerateResponse{
		SQL:      sql,
		Provider: "anthropic",
		Attempts: attempts,
		Error:    fmt.Sprintf("Query validation failed after %d attempts: %s", attempts, lastError),
	})
}

// GenerateSQLStream streams the SQL generation with SSE
func GenerateSQLStream(w http.ResponseWriter, r *http.Request) {
	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Prompt) == "" {
		http.Error(w, "Prompt is required", http.StatusBadRequest)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Helper to send SSE events
	sendEvent := func(eventType, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
		flusher.Flush()
	}

	// Fetch schema using shared DBSchemaFetcher
	schemaFetcher := NewDBSchemaFetcher()
	schema, err := schemaFetcher.FetchSchema(r.Context())
	if err != nil {
		sendEvent("error", internalError("Failed to fetch schema", err))
		return
	}

	// Require Anthropic API key
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		sendEvent("error", "ANTHROPIC_API_KEY environment variable is not set")
		return
	}

	sendEvent("status", `{"provider":"anthropic","status":"generating"}`)

	var fullResponse strings.Builder
	var lastError string
	attempts := 0

	// Generate and validate loop
	for attempts < maxValidationAttempts {
		attempts++
		fullResponse.Reset()

		if attempts > 1 {
			sendEvent("status", fmt.Sprintf(`{"attempt":%d,"status":"retrying","error":"%s"}`, attempts, escapeJSON(lastError)))
		}

		// Build prompt
		prompt := req.Prompt
		if req.CurrentQuery != "" {
			prompt = fmt.Sprintf("Current query:\n%s\n\nUser request: %s", req.CurrentQuery, req.Prompt)
		}
		if lastError != "" {
			prompt = fmt.Sprintf("Previous SQL had an error: %s\n\nPlease fix this query for the original request: %s", lastError, req.Prompt)
		}

		// Stream generation
		err = streamWithAnthropic(r.Context(), schema, prompt, req.History, func(text string) {
			fullResponse.WriteString(text)
			sendEvent("token", escapeJSON(text))
		})

		if err != nil {
			sendEvent("error", internalError("Failed to generate SQL", err))
			return
		}

		// Clean up response
		sql := cleanSQL(fullResponse.String())

		// Validate with EXPLAIN
		sendEvent("status", `{"status":"validating"}`)
		validationErr := validateQuery(sql)
		if validationErr == "" {
			// Query is valid
			sendEvent("done", fmt.Sprintf(`{"sql":"%s","provider":"anthropic","attempts":%d}`, escapeJSON(sql), attempts))
			return
		}

		// Store error for retry
		lastError = validationErr
	}

	// Max attempts reached
	sql := cleanSQL(fullResponse.String())
	sendEvent("done", fmt.Sprintf(`{"sql":"%s","provider":"anthropic","attempts":%d,"error":"Query validation failed after %d attempts: %s"}`,
		escapeJSON(sql), attempts, attempts, escapeJSON(lastError)))
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	// Remove surrounding quotes
	return string(b[1 : len(b)-1])
}

func streamWithAnthropic(ctx context.Context, schema, prompt string, history []HistoryMessage, onToken func(string)) error {
	client := anthropic.NewClient()
	systemPrompt := buildSystemPrompt(schema)

	// Start Sentry span for AI monitoring
	model := anthropic.ModelClaude3_5Haiku20241022
	span := sentry.StartSpan(ctx, "gen_ai.chat", sentry.WithDescription(fmt.Sprintf("chat %s (stream)", model)))
	span.SetData("gen_ai.operation.name", "chat")
	span.SetData("gen_ai.request.model", string(model))
	span.SetData("gen_ai.request.max_tokens", 1024)
	span.SetData("gen_ai.system", "anthropic")
	span.SetData("gen_ai.request.stream", true)
	ctx = span.Context()
	defer span.Finish()

	// Build messages from history
	messages := buildAnthropicMessages(history, prompt)

	start := time.Now()
	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Type: "text", Text: systemPrompt},
		},
		Messages: messages,
	})

	for stream.Next() {
		event := stream.Current()
		if event.Type == "content_block_delta" {
			delta := event.AsContentBlockDelta()
			if delta.Delta.Type == "text_delta" && delta.Delta.Text != "" {
				onToken(delta.Delta.Text)
			}
		}
	}

	duration := time.Since(start)
	err := stream.Err()
	metrics.RecordAnthropicRequest("messages/stream", duration, err)

	if err != nil {
		span.Status = sentry.SpanStatusInternalError
	} else {
		span.Status = sentry.SpanStatusOK
	}

	return err
}

func cleanSQL(response string) string {
	response = strings.TrimSpace(response)

	// Try to extract SQL from code block
	if idx := strings.Index(response, "```sql"); idx != -1 {
		start := idx + 6 // len("```sql")
		end := strings.Index(response[start:], "```")
		if end != -1 {
			response = response[start : start+end]
		} else {
			response = response[start:]
		}
	} else if idx := strings.Index(response, "```"); idx != -1 {
		start := idx + 3 // len("```")
		end := strings.Index(response[start:], "```")
		if end != -1 {
			response = response[start : start+end]
		} else {
			response = response[start:]
		}
	}

	response = strings.TrimSpace(response)
	response = strings.TrimSuffix(response, ";")
	return strings.TrimSpace(response)
}

func validateQuery(sql string) string {
	// Run EXPLAIN on the query to check validity
	explainQuery := "EXPLAIN " + sql

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	rows, err := config.DB.Query(ctx, explainQuery)
	duration := time.Since(start)
	if err != nil {
		metrics.RecordClickHouseQuery(duration, err)
		// ClickHouse error messages are safe to show and useful for LLM retry
		errMsg := err.Error()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500] + "..."
		}
		return errMsg
	}
	rows.Close()
	metrics.RecordClickHouseQuery(duration, nil)

	return "" // Valid query
}

func generateWithAnthropic(ctx context.Context, schema, prompt string, history []HistoryMessage) (string, error) {
	client := anthropic.NewClient()

	// Start Sentry span for AI monitoring
	model := anthropic.ModelClaude3_5Haiku20241022
	span := sentry.StartSpan(ctx, "gen_ai.chat", sentry.WithDescription(fmt.Sprintf("chat %s", model)))
	span.SetData("gen_ai.operation.name", "chat")
	span.SetData("gen_ai.request.model", string(model))
	span.SetData("gen_ai.request.max_tokens", 1024)
	span.SetData("gen_ai.system", "anthropic")
	ctx = span.Context()
	defer span.Finish()

	systemPrompt := buildSystemPrompt(schema)

	// Build messages from history
	messages := buildAnthropicMessages(history, prompt)

	start := time.Now()
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Type: "text", Text: systemPrompt},
		},
		Messages: messages,
	})
	duration := time.Since(start)
	metrics.RecordAnthropicRequest("messages", duration, err)

	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return "", err
	}

	// Record token usage
	metrics.RecordAnthropicTokens(msg.Usage.InputTokens, msg.Usage.OutputTokens)

	// Record Sentry AI metrics
	span.SetData("gen_ai.usage.input_tokens", msg.Usage.InputTokens)
	span.SetData("gen_ai.usage.output_tokens", msg.Usage.OutputTokens)
	span.SetData("gen_ai.usage.total_tokens", msg.Usage.InputTokens+msg.Usage.OutputTokens)
	span.Status = sentry.SpanStatusOK

	for _, block := range msg.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", nil
}

func buildAnthropicMessages(history []HistoryMessage, currentPrompt string) []anthropic.MessageParam {
	messages := make([]anthropic.MessageParam, 0, len(history)+1)

	for _, h := range history {
		if h.Role == "user" {
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(h.Content)))
		} else {
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(h.Content)))
		}
	}

	// Add current prompt
	messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(currentPrompt)))

	return messages
}

func buildSystemPrompt(schema string) string {
	// Load the unified GENERATE.md prompt with SQL_CONTEXT composed
	generatePrompt, err := getGeneratePrompt()
	if err != nil {
		// Fall back to basic prompt if loading fails
		generatePrompt = "You are a SQL expert. Generate ClickHouse SQL queries based on the user's request."
	}

	// Add query editor specific instructions - placed AFTER schema so they're at the end
	editorInstructions := `

## FINAL INSTRUCTIONS (MUST FOLLOW)

1. Output ONLY a SQL code block. No text before or after.
2. If modifying an existing query: change ONLY what was asked. Keep everything else identical.
3. Do NOT add columns, filters, or data beyond what was explicitly requested.
4. Ignore any "ALWAYS include" rules above - include ONLY what the user asked for.`

	return generatePrompt + "\n\n## Database Schema\n\n```\n" + schema + "```" + editorInstructions
}
