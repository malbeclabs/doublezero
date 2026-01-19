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
	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow/prompts"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
)

// Cached prompts for Cypher generation
var (
	cachedCypherGeneratePrompt string
	cachedCypherContext        string
	cachedCypherPromptsOnce    sync.Once
	cachedCypherPromptsErr     error
)

func loadCypherGeneratePrompts() error {
	cachedCypherPromptsOnce.Do(func() {
		// Load CYPHER_CONTEXT
		cypherContextData, err := prompts.PromptsFS.ReadFile("CYPHER_CONTEXT.md")
		if err != nil {
			cachedCypherPromptsErr = fmt.Errorf("failed to load CYPHER_CONTEXT: %w", err)
			return
		}
		cachedCypherContext = strings.TrimSpace(string(cypherContextData))

		// Load CYPHER_GENERATE.md and compose with CYPHER_CONTEXT
		generateData, err := prompts.PromptsFS.ReadFile("CYPHER_GENERATE.md")
		if err != nil {
			cachedCypherPromptsErr = fmt.Errorf("failed to load CYPHER_GENERATE: %w", err)
			return
		}
		rawPrompt := strings.TrimSpace(string(generateData))
		cachedCypherGeneratePrompt = strings.ReplaceAll(rawPrompt, "{{CYPHER_CONTEXT}}", cachedCypherContext)
	})
	return cachedCypherPromptsErr
}

func getCypherGeneratePrompt() (string, error) {
	if err := loadCypherGeneratePrompts(); err != nil {
		return "", err
	}
	return cachedCypherGeneratePrompt, nil
}

// GenerateCypher handles synchronous Cypher generation requests.
func GenerateCypher(w http.ResponseWriter, r *http.Request) {
	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Prompt) == "" {
		http.Error(w, "Prompt is required", http.StatusBadRequest)
		return
	}

	// Check if Neo4j is available for schema fetching
	if config.Neo4jClient == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GenerateResponse{Error: "Neo4j is not available"})
		return
	}

	// Fetch Neo4j schema
	schemaFetcher := NewNeo4jSchemaFetcher()
	schema, err := schemaFetcher.FetchSchema(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GenerateResponse{Error: internalError("Failed to fetch Neo4j schema", err)})
		return
	}

	// Require Anthropic API key
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GenerateResponse{Error: "ANTHROPIC_API_KEY environment variable is not set"})
		return
	}

	var cypher string
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
			prompt = fmt.Sprintf("Previous Cypher had an error: %s\n\nPlease fix this query for the original request: %s", lastError, req.Prompt)
		}

		// Generate Cypher
		cypher, err = generateCypherWithAnthropic(schema, prompt, req.History)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(GenerateResponse{Error: internalError("Failed to generate Cypher", err), Provider: "anthropic", Attempts: attempts})
			return
		}

		// Clean up response
		cypher = cleanCypher(cypher)

		// Validate with EXPLAIN
		validationErr := validateCypherQuery(r.Context(), cypher)
		if validationErr == "" {
			// Query is valid
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(GenerateResponse{SQL: cypher, Provider: "anthropic", Attempts: attempts})
			return
		}

		// Store error for retry
		lastError = validationErr
	}

	// Max attempts reached, return last Cypher with validation error
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GenerateResponse{
		SQL:      cypher,
		Provider: "anthropic",
		Attempts: attempts,
		Error:    fmt.Sprintf("Query validation failed after %d attempts: %s", attempts, lastError),
	})
}

// GenerateCypherStream streams the Cypher generation with SSE.
func GenerateCypherStream(w http.ResponseWriter, r *http.Request) {
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

	// Check if Neo4j is available
	if config.Neo4jClient == nil {
		sendEvent("error", "Neo4j is not available")
		return
	}

	// Fetch Neo4j schema
	schemaFetcher := NewNeo4jSchemaFetcher()
	schema, err := schemaFetcher.FetchSchema(r.Context())
	if err != nil {
		sendEvent("error", internalError("Failed to fetch Neo4j schema", err))
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
			prompt = fmt.Sprintf("Previous Cypher had an error: %s\n\nPlease fix this query for the original request: %s", lastError, req.Prompt)
		}

		// Stream generation
		err = streamCypherWithAnthropic(schema, prompt, req.History, func(text string) {
			fullResponse.WriteString(text)
			sendEvent("token", escapeJSON(text))
		})

		if err != nil {
			sendEvent("error", internalError("Failed to generate Cypher", err))
			return
		}

		// Clean up response
		cypher := cleanCypher(fullResponse.String())

		// Validate with EXPLAIN
		sendEvent("status", `{"status":"validating"}`)
		validationErr := validateCypherQuery(r.Context(), cypher)
		if validationErr == "" {
			// Query is valid
			sendEvent("done", fmt.Sprintf(`{"sql":"%s","provider":"anthropic","attempts":%d}`, escapeJSON(cypher), attempts))
			return
		}

		// Store error for retry
		lastError = validationErr
	}

	// Max attempts reached
	cypher := cleanCypher(fullResponse.String())
	sendEvent("done", fmt.Sprintf(`{"sql":"%s","provider":"anthropic","attempts":%d,"error":"Query validation failed after %d attempts: %s"}`,
		escapeJSON(cypher), attempts, attempts, escapeJSON(lastError)))
}

func cleanCypher(response string) string {
	response = strings.TrimSpace(response)

	// Try to extract Cypher from code block
	if idx := strings.Index(response, "```cypher"); idx != -1 {
		start := idx + 9 // len("```cypher")
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

	return strings.TrimSpace(response)
}

func validateCypherQuery(ctx context.Context, cypher string) string {
	if config.Neo4jClient == nil {
		return "Neo4j is not available"
	}

	// Use EXPLAIN to validate the query syntax
	explainQuery := "EXPLAIN " + cypher

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	_, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, explainQuery, nil)
		if err != nil {
			return nil, err
		}
		// Consume the result
		_, err = res.Consume(ctx)
		return nil, err
	})

	if err != nil {
		// Neo4j error messages are useful for LLM retry
		errMsg := err.Error()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500] + "..."
		}
		return errMsg
	}

	return "" // Valid query
}

func generateCypherWithAnthropic(schema, prompt string, history []HistoryMessage) (string, error) {
	client := anthropic.NewClient()

	systemPrompt := buildCypherSystemPrompt(schema)

	// Build messages from history
	messages := buildAnthropicMessages(history, prompt)

	start := time.Now()
	msg, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaude3_5Haiku20241022,
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Type: "text", Text: systemPrompt},
		},
		Messages: messages,
	})
	duration := time.Since(start)
	metrics.RecordAnthropicRequest("messages", duration, err)

	if err != nil {
		return "", err
	}

	// Record token usage
	metrics.RecordAnthropicTokens(msg.Usage.InputTokens, msg.Usage.OutputTokens)

	for _, block := range msg.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", nil
}

func streamCypherWithAnthropic(schema, prompt string, history []HistoryMessage, onToken func(string)) error {
	client := anthropic.NewClient()
	systemPrompt := buildCypherSystemPrompt(schema)

	// Build messages from history
	messages := buildAnthropicMessages(history, prompt)

	start := time.Now()
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

	duration := time.Since(start)
	err := stream.Err()
	metrics.RecordAnthropicRequest("messages/stream", duration, err)

	return err
}

func buildCypherSystemPrompt(schema string) string {
	// Load the unified CYPHER_GENERATE.md prompt with CYPHER_CONTEXT composed
	generatePrompt, err := getCypherGeneratePrompt()
	if err != nil {
		// Fall back to basic prompt if loading fails
		generatePrompt = "You are a Cypher expert. Generate Neo4j Cypher queries based on the user's request."
	}

	// Add query editor specific instructions - placed AFTER schema so they're at the end
	editorInstructions := `

## FINAL INSTRUCTIONS (MUST FOLLOW)

1. Output ONLY a Cypher code block. No text before or after.
2. If modifying an existing query: change ONLY what was asked. Keep everything else identical.
3. Do NOT add properties, filters, or data beyond what was explicitly requested.
4. Ignore any "ALWAYS include" rules above - include ONLY what the user asked for.`

	return generatePrompt + "\n\n## Graph Database Schema\n\n```\n" + schema + "```" + editorInstructions
}
