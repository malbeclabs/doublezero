package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

type AutoGenerateRequest struct {
	Prompt       string `json:"prompt"`
	CurrentQuery string `json:"currentQuery,omitempty"`
}

// AutoGenerateStream handles auto-detection of query mode and streams the generation.
// It first classifies the question as SQL or Cypher, then streams the appropriate query generation.
func AutoGenerateStream(w http.ResponseWriter, r *http.Request) {
	var req AutoGenerateRequest
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

	// Require Anthropic API key
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		sendEvent("error", "ANTHROPIC_API_KEY environment variable is not set")
		return
	}

	// Check if Neo4j is available - if not, default to SQL
	neo4jAvailable := config.Neo4jClient != nil

	// Classify the question
	var mode string
	if !neo4jAvailable {
		// Neo4j not available, default to SQL
		mode = "sql"
	} else {
		// Classify the question
		var err error
		mode, err = classifyQuestion(r.Context(), req.Prompt)
		if err != nil {
			// If classification fails, default to SQL
			mode = "sql"
		}
	}

	// Send mode event first
	sendEvent("mode", fmt.Sprintf(`{"mode":"%s"}`, mode))

	// Now delegate to the appropriate generator based on mode
	if mode == "cypher" {
		streamCypherGeneration(r.Context(), req, sendEvent)
	} else {
		streamSQLGeneration(r.Context(), req, sendEvent)
	}
}

// classifyQuestion uses a fast LLM call to classify if a question should use SQL or Cypher.
func classifyQuestion(ctx context.Context, question string) (string, error) {
	client := anthropic.NewClient()

	systemPrompt := `You are a query router for a network analytics system. Your job is to classify user questions into two categories:

CYPHER - Use for questions about:
- Network topology and structure (paths, routes, connectivity)
- Graph traversal (neighbors, reachability, hops)
- Impact analysis (what's affected if X goes down)
- Device/link relationships and connections
- Finding paths between devices or metros

SQL - Use for questions about:
- Time-series data and metrics (latency, bandwidth, errors over time)
- Aggregations and statistics (counts, averages, percentages)
- Historical analysis and trends
- Validator performance and stake data
- Traffic and utilization metrics

Respond with ONLY one word: either "sql" or "cypher"`

	start := time.Now()
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaude3_5Haiku20241022,
		MaxTokens: 10,
		System: []anthropic.TextBlockParam{
			{Type: "text", Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(question)),
		},
	})
	duration := time.Since(start)
	metrics.RecordAnthropicRequest("messages/classify", duration, err)

	if err != nil {
		return "", err
	}

	// Record token usage
	metrics.RecordAnthropicTokens(msg.Usage.InputTokens, msg.Usage.OutputTokens)

	for _, block := range msg.Content {
		if block.Type == "text" {
			response := strings.ToLower(strings.TrimSpace(block.Text))
			if response == "cypher" {
				return "cypher", nil
			}
			return "sql", nil
		}
	}

	// Default to SQL
	return "sql", nil
}

// streamSQLGeneration handles the SQL generation portion of auto-generate.
func streamSQLGeneration(ctx context.Context, req AutoGenerateRequest, sendEvent func(string, string)) {
	// Fetch schema using shared DBSchemaFetcher
	schemaFetcher := NewDBSchemaFetcher()
	schema, err := schemaFetcher.FetchSchema(ctx)
	if err != nil {
		sendEvent("error", internalError("Failed to fetch schema", err))
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
		err = streamWithAnthropic(ctx, schema, prompt, nil, func(text string) {
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

// streamCypherGeneration handles the Cypher generation portion of auto-generate.
func streamCypherGeneration(ctx context.Context, req AutoGenerateRequest, sendEvent func(string, string)) {
	// Check if Neo4j is available
	if config.Neo4jClient == nil {
		sendEvent("error", "Neo4j is not available")
		return
	}

	// Fetch Neo4j schema
	schemaFetcher := NewNeo4jSchemaFetcher()
	schema, err := schemaFetcher.FetchSchema(ctx)
	if err != nil {
		sendEvent("error", internalError("Failed to fetch Neo4j schema", err))
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
		err = streamCypherWithAnthropic(schema, prompt, nil, func(text string) {
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
		validationErr := validateCypherQuery(ctx, cypher)
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
