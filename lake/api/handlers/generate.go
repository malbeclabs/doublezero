package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
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

const ollamaURL = "http://localhost:11434"
const ollamaModel = "mistral-nemo"
const maxValidationAttempts = 3

// Cached prompts for query generation
var (
	cachedPrompts     *pipeline.Prompts
	cachedPromptsOnce sync.Once
	cachedPromptsErr  error
)

func getGeneratePrompt() (string, error) {
	cachedPromptsOnce.Do(func() {
		cachedPrompts, cachedPromptsErr = pipeline.LoadPrompts()
	})
	if cachedPromptsErr != nil {
		return "", cachedPromptsErr
	}
	return cachedPrompts.Generate, nil
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

	// Fetch schema for context
	schema, err := fetchSchema()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GenerateResponse{Error: "Failed to fetch schema: " + err.Error()})
		return
	}

	// Determine provider
	useAnthropic := os.Getenv("ANTHROPIC_API_KEY") != ""
	provider := "ollama"
	if useAnthropic {
		provider = "anthropic"
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
		if useAnthropic {
			sql, err = generateWithAnthropic(schema, prompt, req.History)
		} else {
			sql, err = generateWithOllama(schema, prompt)
		}

		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(GenerateResponse{Error: err.Error(), Provider: provider, Attempts: attempts})
			return
		}

		// Clean up response
		sql = cleanSQL(sql)

		// Validate with EXPLAIN
		validationErr := validateQuery(sql)
		if validationErr == "" {
			// Query is valid
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(GenerateResponse{SQL: sql, Provider: provider, Attempts: attempts})
			return
		}

		// Store error for retry
		lastError = validationErr
	}

	// Max attempts reached, return last SQL with validation error
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GenerateResponse{
		SQL:      sql,
		Provider: provider,
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

	// Fetch schema for context
	schema, err := fetchSchema()
	if err != nil {
		sendEvent("error", "Failed to fetch schema: "+err.Error())
		return
	}

	// Determine provider
	useAnthropic := os.Getenv("ANTHROPIC_API_KEY") != ""
	provider := "ollama"
	if useAnthropic {
		provider = "anthropic"
	}

	sendEvent("status", fmt.Sprintf(`{"provider":"%s","status":"generating"}`, provider))

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
		if useAnthropic {
			err = streamWithAnthropic(schema, prompt, req.History, func(text string) {
				fullResponse.WriteString(text)
				sendEvent("token", escapeJSON(text))
			})
		} else {
			err = streamWithOllama(schema, prompt, func(text string) {
				fullResponse.WriteString(text)
				sendEvent("token", escapeJSON(text))
			})
		}

		if err != nil {
			sendEvent("error", err.Error())
			return
		}

		// Clean up response
		sql := cleanSQL(fullResponse.String())

		// Validate with EXPLAIN
		sendEvent("status", `{"status":"validating"}`)
		validationErr := validateQuery(sql)
		if validationErr == "" {
			// Query is valid
			sendEvent("done", fmt.Sprintf(`{"sql":"%s","provider":"%s","attempts":%d}`, escapeJSON(sql), provider, attempts))
			return
		}

		// Store error for retry
		lastError = validationErr
	}

	// Max attempts reached
	sql := cleanSQL(fullResponse.String())
	sendEvent("done", fmt.Sprintf(`{"sql":"%s","provider":"%s","attempts":%d,"error":"Query validation failed after %d attempts: %s"}`,
		escapeJSON(sql), provider, attempts, attempts, escapeJSON(lastError)))
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	// Remove surrounding quotes
	return string(b[1 : len(b)-1])
}

func streamWithAnthropic(schema, prompt string, history []HistoryMessage, onToken func(string)) error {
	client := anthropic.NewClient()
	systemPrompt := buildSystemPrompt(schema)

	// Build messages from history
	messages := buildAnthropicMessages(history, prompt)

	stream := client.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaude3_5Haiku20241022,
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

	return stream.Err()
}

func streamWithOllama(schema, prompt string, onToken func(string)) error {
	systemPrompt := buildSystemPrompt(schema)

	reqBody := map[string]any{
		"model":  ollamaModel,
		"prompt": prompt,
		"system": systemPrompt,
		"stream": true,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	resp, err := http.Post(ollamaURL+"/api/generate", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama error: %s", string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var chunk struct {
			Response string `json:"response"`
			Done     bool   `json:"done"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		if chunk.Response != "" {
			onToken(chunk.Response)
		}
		if chunk.Done {
			break
		}
	}

	return scanner.Err()
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

	resp, err := http.Post(clickhouseURL+"/", "text/plain", strings.NewReader(explainQuery))
	if err != nil {
		return "Failed to connect to ClickHouse: " + err.Error()
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "Failed to read response: " + err.Error()
	}

	if resp.StatusCode != http.StatusOK {
		// Extract error message
		errMsg := strings.TrimSpace(string(body))
		// Truncate long errors
		if len(errMsg) > 500 {
			errMsg = errMsg[:500] + "..."
		}
		return errMsg
	}

	return "" // Valid query
}

func generateWithAnthropic(schema, prompt string, history []HistoryMessage) (string, error) {
	client := anthropic.NewClient()

	systemPrompt := buildSystemPrompt(schema)

	// Build messages from history
	messages := buildAnthropicMessages(history, prompt)

	msg, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaude3_5Haiku20241022,
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Type: "text", Text: systemPrompt},
		},
		Messages: messages,
	})

	if err != nil {
		return "", err
	}

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

func generateWithOllama(schema, prompt string) (string, error) {
	systemPrompt := buildSystemPrompt(schema)

	reqBody := map[string]any{
		"model":  ollamaModel,
		"prompt": prompt,
		"system": systemPrompt,
		"stream": false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(ollamaURL+"/api/generate", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama error: %s", string(body))
	}

	var ollamaResp struct {
		Response string `json:"response"`
	}

	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return "", err
	}

	return ollamaResp.Response, nil
}

func buildSystemPrompt(schema string) string {
	// Load the unified GENERATE.md prompt
	generatePrompt, err := getGeneratePrompt()
	if err != nil {
		// Fall back to basic prompt if loading fails
		generatePrompt = "You are a SQL expert. Generate ClickHouse SQL queries based on the user's request."
	}

	// Add query editor specific instructions
	editorInstructions := `
## Query Editor Context

This is an interactive query editor. The user may provide:
- A natural language request to generate a new query
- A current query they want you to modify

Additional rules for the query editor:
- First, briefly explain your reasoning (1-3 sentences)
- Then provide the SQL query in a code block
- If a current query is provided, modify it based on the user's request (add filters, change columns, adjust limits, etc.) rather than starting from scratch
- If the user's request is unrelated to the current query, you may generate a new query
`

	return generatePrompt + editorInstructions + "\n\n## Database Schema\n\n```\n" + schema + "```"
}

func fetchSchema() (string, error) {
	// Fetch columns
	columnsQuery := `
		SELECT
			table,
			name,
			type
		FROM system.columns
		WHERE database = 'default'
		  AND table NOT LIKE 'stg_%'
		ORDER BY table, position
		FORMAT JSON
	`

	resp, err := http.Get(clickhouseURL + "/?query=" + url.QueryEscape(columnsQuery))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var columnsResp struct {
		Data []struct {
			Table string `json:"table"`
			Name  string `json:"name"`
			Type  string `json:"type"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &columnsResp); err != nil {
		return "", err
	}

	// Fetch view definitions
	viewsQuery := `
		SELECT
			name,
			as_select
		FROM system.tables
		WHERE database = 'default'
		  AND engine = 'View'
		  AND name NOT LIKE 'stg_%'
		FORMAT JSON
	`

	viewsResp, err := http.Get(clickhouseURL + "/?query=" + url.QueryEscape(viewsQuery))
	if err != nil {
		return "", err
	}
	defer viewsResp.Body.Close()

	viewsBody, err := io.ReadAll(viewsResp.Body)
	if err != nil {
		return "", err
	}

	var viewsData struct {
		Data []struct {
			Name     string `json:"name"`
			AsSelect string `json:"as_select"`
		} `json:"data"`
	}

	if err := json.Unmarshal(viewsBody, &viewsData); err != nil {
		return "", err
	}

	// Build view definitions map
	viewDefs := make(map[string]string)
	for _, v := range viewsData.Data {
		viewDefs[v.Name] = v.AsSelect
	}

	// Format schema as readable text
	var sb strings.Builder
	currentTable := ""
	for _, col := range columnsResp.Data {
		if col.Table != currentTable {
			if currentTable != "" {
				// Add view definition if this was a view
				if def, ok := viewDefs[currentTable]; ok {
					sb.WriteString("  Definition: " + def + "\n")
				}
				sb.WriteString("\n")
			}
			currentTable = col.Table
			if _, isView := viewDefs[col.Table]; isView {
				sb.WriteString(col.Table + " (VIEW):\n")
			} else {
				sb.WriteString(col.Table + ":\n")
			}
		}
		sb.WriteString("  - " + col.Name + " (" + col.Type + ")\n")
	}
	// Handle last table's view definition
	if def, ok := viewDefs[currentTable]; ok {
		sb.WriteString("  Definition: " + def + "\n")
	}

	return sb.String(), nil
}
