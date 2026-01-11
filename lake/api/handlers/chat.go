package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

// ChatMessage represents a single message in conversation history.
type ChatMessage struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
}

// ChatRequest is the incoming request for a chat message.
type ChatRequest struct {
	Message string        `json:"message"`
	History []ChatMessage `json:"history"`
}

// DataQuestionResponse represents a decomposed data question.
type DataQuestionResponse struct {
	Question  string `json:"question"`
	Rationale string `json:"rationale"`
}

// GeneratedQueryResponse represents a generated SQL query.
type GeneratedQueryResponse struct {
	Question    string `json:"question"`
	SQL         string `json:"sql"`
	Explanation string `json:"explanation"`
}

// ExecutedQueryResponse represents an executed query with results.
type ExecutedQueryResponse struct {
	Question string   `json:"question"`
	SQL      string   `json:"sql"`
	Columns  []string `json:"columns"`
	Rows     [][]any  `json:"rows"`
	Count    int      `json:"count"`
	Error    string   `json:"error,omitempty"`
}

// ChatResponse is the full pipeline result returned to the UI.
type ChatResponse struct {
	// The final synthesized answer
	Answer string `json:"answer"`

	// Pipeline steps (for transparency)
	DataQuestions    []DataQuestionResponse    `json:"dataQuestions,omitempty"`
	GeneratedQueries []GeneratedQueryResponse  `json:"generatedQueries,omitempty"`
	ExecutedQueries  []ExecutedQueryResponse   `json:"executedQueries,omitempty"`

	// Error if pipeline failed
	Error string `json:"error,omitempty"`
}

func Chat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Check if we should use Anthropic
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: "Chat requires ANTHROPIC_API_KEY to be set"})
		return
	}

	// Load prompts
	prompts, err := pipeline.LoadPrompts()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: "Failed to load prompts: " + err.Error()})
		return
	}

	// Create pipeline components
	llm := pipeline.NewAnthropicLLMClient(anthropic.ModelClaude3_5Haiku20241022, 4096)
	querier := pipeline.NewHTTPQuerier(clickhouseURL)
	schemaFetcher := pipeline.NewHTTPSchemaFetcher(clickhouseURL)

	// Create and run pipeline
	p, err := pipeline.New(&pipeline.Config{
		Logger:        slog.Default(),
		LLM:           llm,
		Querier:       querier,
		SchemaFetcher: schemaFetcher,
		Prompts:       prompts,
		MaxTokens:     4096,
	})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: "Failed to create pipeline: " + err.Error()})
		return
	}

	// Convert history to pipeline format
	var history []pipeline.ConversationMessage
	for _, msg := range req.History {
		history = append(history, pipeline.ConversationMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	result, err := p.RunWithHistory(r.Context(), req.Message, history)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: err.Error()})
		return
	}

	// Convert pipeline result to response
	response := convertPipelineResult(result)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// convertPipelineResult converts the internal pipeline result to the API response format.
func convertPipelineResult(result *pipeline.PipelineResult) ChatResponse {
	resp := ChatResponse{
		Answer: result.Answer,
	}

	// Convert data questions
	for _, dq := range result.DataQuestions {
		resp.DataQuestions = append(resp.DataQuestions, DataQuestionResponse{
			Question:  dq.Question,
			Rationale: dq.Rationale,
		})
	}

	// Convert generated queries
	for _, gq := range result.GeneratedQueries {
		resp.GeneratedQueries = append(resp.GeneratedQueries, GeneratedQueryResponse{
			Question:    gq.DataQuestion.Question,
			SQL:         gq.SQL,
			Explanation: gq.Explanation,
		})
	}

	// Convert executed queries
	for _, eq := range result.ExecutedQueries {
		eqr := ExecutedQueryResponse{
			Question: eq.GeneratedQuery.DataQuestion.Question,
			SQL:      eq.Result.SQL,
			Columns:  eq.Result.Columns,
			Count:    eq.Result.Count,
			Error:    eq.Result.Error,
		}

		// Convert rows from map to array format for easier UI consumption
		for _, row := range eq.Result.Rows {
			rowData := make([]any, 0, len(eq.Result.Columns))
			for _, col := range eq.Result.Columns {
				rowData = append(rowData, row[col])
			}
			eqr.Rows = append(eqr.Rows, rowData)
		}

		resp.ExecutedQueries = append(resp.ExecutedQueries, eqr)
	}

	return resp
}

// ChatStream handles streaming chat requests with SSE progress updates.
func ChatStream(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
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
	sendEvent := func(eventType string, data any) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(jsonData))
		flusher.Flush()
	}

	// Check if we should use Anthropic
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		sendEvent("error", map[string]string{"error": "Chat requires ANTHROPIC_API_KEY to be set"})
		return
	}

	// Load prompts
	prompts, err := pipeline.LoadPrompts()
	if err != nil {
		sendEvent("error", map[string]string{"error": "Failed to load prompts: " + err.Error()})
		return
	}

	// Create pipeline components
	llm := pipeline.NewAnthropicLLMClient(anthropic.ModelClaude3_5Haiku20241022, 4096)
	querier := pipeline.NewHTTPQuerier(clickhouseURL)
	schemaFetcher := pipeline.NewHTTPSchemaFetcher(clickhouseURL)

	// Create pipeline
	p, err := pipeline.New(&pipeline.Config{
		Logger:        slog.Default(),
		LLM:           llm,
		Querier:       querier,
		SchemaFetcher: schemaFetcher,
		Prompts:       prompts,
		MaxTokens:     4096,
	})
	if err != nil {
		sendEvent("error", map[string]string{"error": "Failed to create pipeline: " + err.Error()})
		return
	}

	// Convert history to pipeline format
	var history []pipeline.ConversationMessage
	for _, msg := range req.History {
		history = append(history, pipeline.ConversationMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	ctx := r.Context()

	// Step 1: Decompose
	sendEvent("status", map[string]string{"step": "decomposing", "message": "Breaking down your question..."})

	dataQuestions, err := p.DecomposeWithHistory(ctx, req.Message, history)
	if err != nil {
		sendEvent("error", map[string]string{"error": err.Error()})
		return
	}

	// Send decomposed questions
	questions := make([]DataQuestionResponse, 0, len(dataQuestions))
	for _, dq := range dataQuestions {
		questions = append(questions, DataQuestionResponse{
			Question:  dq.Question,
			Rationale: dq.Rationale,
		})
	}
	sendEvent("decomposed", map[string]any{
		"count":     len(dataQuestions),
		"questions": questions,
	})

	// Step 2 & 3: Generate and execute queries in parallel
	sendEvent("status", map[string]string{"step": "executing", "message": fmt.Sprintf("Running %d queries...", len(dataQuestions))})

	executedQueries := make([]pipeline.ExecutedQuery, len(dataQuestions))
	var wg sync.WaitGroup
	var completedCount int
	var mu sync.Mutex

	for i, dq := range dataQuestions {
		wg.Add(1)
		go func(idx int, question pipeline.DataQuestion) {
			defer wg.Done()
			executed := p.GenerateAndExecuteWithRetry(ctx, question)
			executedQueries[idx] = executed

			mu.Lock()
			completedCount++
			// Send progress update
			sendEvent("query_progress", map[string]any{
				"completed": completedCount,
				"total":     len(dataQuestions),
				"question":  question.Question,
				"success":   executed.Result.Error == "",
				"rows":      executed.Result.Count,
			})
			mu.Unlock()
		}(i, dq)
	}
	wg.Wait()

	// Build generated queries from executed
	generatedQueries := make([]pipeline.GeneratedQuery, len(executedQueries))
	for i, eq := range executedQueries {
		generatedQueries[i] = eq.GeneratedQuery
	}

	// Step 4: Synthesize
	sendEvent("status", map[string]string{"step": "synthesizing", "message": "Generating answer..."})

	answer, err := p.Synthesize(ctx, req.Message, executedQueries)
	if err != nil {
		sendEvent("error", map[string]string{"error": "Synthesize failed: " + err.Error()})
		return
	}

	// Build final response
	result := &pipeline.PipelineResult{
		UserQuestion:     req.Message,
		DataQuestions:    dataQuestions,
		GeneratedQueries: generatedQueries,
		ExecutedQueries:  executedQueries,
		Answer:           answer,
	}

	response := convertPipelineResult(result)
	sendEvent("done", response)
}

// CompleteRequest is the request for a simple LLM completion.
type CompleteRequest struct {
	Message string `json:"message"`
}

// CompleteResponse is the response from a simple LLM completion.
type CompleteResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

// Complete handles simple LLM completion requests without the full pipeline.
// This is useful for tasks like generating titles.
func Complete(w http.ResponseWriter, r *http.Request) {
	var req CompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Check if we should use Anthropic
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CompleteResponse{Error: "Completion requires ANTHROPIC_API_KEY to be set"})
		return
	}

	// Create a simple LLM client
	llm := pipeline.NewAnthropicLLMClient(anthropic.ModelClaude3_5Haiku20241022, 256)

	// Simple completion with minimal system prompt
	response, err := llm.Complete(r.Context(), "You are a helpful assistant. Respond concisely.", req.Message)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CompleteResponse{Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CompleteResponse{Response: strings.TrimSpace(response)})
}
