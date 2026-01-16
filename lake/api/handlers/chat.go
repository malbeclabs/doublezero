package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
	v1 "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/v1"
	v2 "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/v2"
)

// ChatMessage represents a single message in conversation history.
type ChatMessage struct {
	Role            string   `json:"role"`              // "user" or "assistant"
	Content         string   `json:"content"`
	ExecutedQueries []string `json:"executedQueries,omitempty"` // SQL from previous turns
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

	// Suggested follow-up questions
	FollowUpQuestions []string `json:"followUpQuestions,omitempty"`

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
	prompts, err := v1.LoadPrompts()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: internalError("Failed to load prompts", err)})
		return
	}

	// Create pipeline components
	llm := pipeline.NewAnthropicLLMClient(anthropic.ModelClaude3_5Haiku20241022, 4096)
	querier := NewDBQuerier()
	schemaFetcher := NewDBSchemaFetcher()

	// Create and run pipeline
	p, err := v1.New(&pipeline.Config{
		Logger:        slog.Default(),
		LLM:           llm,
		Querier:       querier,
		SchemaFetcher: schemaFetcher,
		Prompts:       prompts,
		MaxTokens:     4096,
	})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: internalError("Failed to initialize chat", err)})
		return
	}

	// Convert history to pipeline format
	var history []pipeline.ConversationMessage
	for _, msg := range req.History {
		history = append(history, pipeline.ConversationMessage{
			Role:            msg.Role,
			Content:         msg.Content,
			ExecutedQueries: msg.ExecutedQueries,
		})
	}

	result, err := p.RunWithHistory(r.Context(), req.Message, history)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: internalError("Chat processing failed", err)})
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
		Answer:            result.Answer,
		FollowUpQuestions: result.FollowUpQuestions,
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
				rowData = append(rowData, sanitizeValue(row[col]))
			}
			eqr.Rows = append(eqr.Rows, rowData)
		}

		resp.ExecutedQueries = append(resp.ExecutedQueries, eqr)
	}

	return resp
}

// sanitizeValue replaces non-JSON-serializable values (Inf, NaN) with nil.
func sanitizeValue(v any) any {
	switch val := v.(type) {
	case float64:
		if math.IsInf(val, 0) || math.IsNaN(val) {
			return nil
		}
	case float32:
		if math.IsInf(float64(val), 0) || math.IsNaN(float64(val)) {
			return nil
		}
	}
	return v
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
		jsonData, err := json.Marshal(data)
		if err != nil {
			slog.Error("Failed to marshal SSE event data", "eventType", eventType, "error", err)
			// Send an error event instead
			errorData, _ := json.Marshal(map[string]string{"error": "Failed to serialize response"})
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", string(errorData))
			flusher.Flush()
			return
		}
		slog.Debug("Sending SSE event", "eventType", eventType, "dataLen", len(jsonData))
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(jsonData))
		flusher.Flush()
	}

	// Check if we should use Anthropic
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		sendEvent("error", map[string]string{"error": "Chat requires ANTHROPIC_API_KEY to be set"})
		return
	}

	// Convert history to pipeline format
	var history []pipeline.ConversationMessage
	for _, msg := range req.History {
		history = append(history, pipeline.ConversationMessage{
			Role:            msg.Role,
			Content:         msg.Content,
			ExecutedQueries: msg.ExecutedQueries,
		})
	}

	ctx := r.Context()

	// Check which pipeline version to use
	version := pipeline.DefaultVersion()
	slog.Info("Using pipeline version", "version", version)

	if version == pipeline.VersionV2 {
		// v2 pipeline path - uses RunWithProgress with progress callbacks
		chatStreamV2(ctx, req, history, sendEvent)
		return
	}

	// v1 pipeline path - manual orchestration
	// Load prompts
	prompts, err := v1.LoadPrompts()
	if err != nil {
		sendEvent("error", map[string]string{"error": internalError("Failed to load prompts", err)})
		return
	}

	// Create pipeline components
	llm := pipeline.NewAnthropicLLMClient(anthropic.ModelClaude3_5Haiku20241022, 4096)
	querier := NewDBQuerier()
	schemaFetcher := NewDBSchemaFetcher()

	// Create pipeline
	p, err := v1.New(&pipeline.Config{
		Logger:        slog.Default(),
		LLM:           llm,
		Querier:       querier,
		SchemaFetcher: schemaFetcher,
		Prompts:       prompts,
		MaxTokens:     4096,
	})
	if err != nil {
		sendEvent("error", map[string]string{"error": internalError("Failed to initialize chat", err)})
		return
	}

	// Pre-step: Classify the question
	sendEvent("status", map[string]string{"step": "classifying", "message": "Understanding your question..."})

	classification, err := p.ClassifyWithHistory(ctx, req.Message, history)
	if err != nil {
		sendEvent("error", map[string]string{"error": internalError("Failed to classify question", err)})
		return
	}

	// Handle non-data-analysis classifications
	switch classification.Classification {
	case pipeline.ClassificationOutOfScope:
		answer := classification.DirectResponse
		if answer == "" {
			answer = "I'm a DoubleZero data analyst. I can help you with questions about the DZ network, devices, links, users, connected Solana validators, and performance metrics. What would you like to know?"
		}
		response := ChatResponse{Answer: answer}
		sendEvent("done", response)
		return

	case pipeline.ClassificationConversational:
		sendEvent("status", map[string]string{"step": "responding", "message": "Preparing response..."})
		answer, err := p.RespondWithHistory(ctx, req.Message, history)
		if err != nil {
			sendEvent("error", map[string]string{"error": internalError("Failed to generate response", err)})
			return
		}
		response := ChatResponse{Answer: answer}
		sendEvent("done", response)
		return
	}

	// Step 1: Decompose (only for data_analysis questions)
	sendEvent("status", map[string]string{"step": "decomposing", "message": "Breaking down your question..."})

	dataQuestions, err := p.DecomposeWithHistory(ctx, req.Message, history)
	if err != nil {
		sendEvent("error", map[string]string{"error": internalError("Failed to process question", err)})
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
			// Question numbers are 1-indexed for readability (Q1, Q2, ...)
			executed := p.GenerateAndExecuteWithRetry(ctx, question, idx+1)
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
	sendEvent("status", map[string]string{"step": "synthesizing", "message": "Preparing answer..."})

	// Send periodic heartbeats to keep connection alive through proxies (e.g., Cloudflare)
	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sendEvent("heartbeat", map[string]string{})
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	slog.Info("Starting synthesize", "message", req.Message, "queryCount", len(executedQueries))
	answer, err := p.Synthesize(ctx, req.Message, executedQueries)
	close(heartbeatDone)
	slog.Info("Synthesize completed", "answerLen", len(answer), "error", err)
	if err != nil {
		sendEvent("error", map[string]string{"error": internalError("Failed to generate answer", err)})
		return
	}

	// Generate follow-up suggestions (non-blocking, errors are logged but not returned)
	var followUpQuestions []string
	if followUps, err := p.GenerateFollowUps(ctx, req.Message, answer); err == nil {
		followUpQuestions = followUps
	}

	// Build final response
	result := &pipeline.PipelineResult{
		UserQuestion:      req.Message,
		DataQuestions:     dataQuestions,
		GeneratedQueries:  generatedQueries,
		ExecutedQueries:   executedQueries,
		Answer:            answer,
		FollowUpQuestions: followUpQuestions,
	}

	response := convertPipelineResult(result)
	slog.Info("Sending done event",
		"answerLen", len(response.Answer),
		"dataQuestionsCount", len(response.DataQuestions),
		"generatedQueriesCount", len(response.GeneratedQueries),
		"executedQueriesCount", len(response.ExecutedQueries),
		"followUpQuestionsCount", len(response.FollowUpQuestions),
		"hasError", response.Error != "",
	)
	sendEvent("done", response)
}

// chatStreamV2 handles the v2 pipeline streaming using RunWithProgress.
func chatStreamV2(ctx context.Context, req ChatRequest, history []pipeline.ConversationMessage, sendEvent func(string, any)) {
	// Load v2 prompts
	prompts, err := v2.LoadPrompts()
	if err != nil {
		sendEvent("error", map[string]string{"error": internalError("Failed to load prompts", err)})
		return
	}

	// Create pipeline components
	llm := pipeline.NewAnthropicLLMClient(anthropic.ModelClaude3_5Haiku20241022, 4096)
	querier := NewDBQuerier()
	schemaFetcher := NewDBSchemaFetcher()

	// Create v2 pipeline
	p, err := v2.New(&pipeline.Config{
		Logger:        slog.Default(),
		LLM:           llm,
		Querier:       querier,
		SchemaFetcher: schemaFetcher,
		Prompts:       prompts,
		MaxTokens:     4096,
	})
	if err != nil {
		sendEvent("error", map[string]string{"error": internalError("Failed to initialize chat", err)})
		return
	}

	// Track progress state for decomposed event
	var lastStage pipeline.ProgressStage
	var dataQuestions []pipeline.DataQuestion
	decomposedSent := false

	// Progress callback that emits SSE events
	onProgress := func(progress pipeline.Progress) {
		// Map stage to status message
		var message string
		switch progress.Stage {
		case pipeline.StageInterpreting:
			message = "Interpreting your question..."
		case pipeline.StageMapping:
			message = "Mapping to data..."
		case pipeline.StagePlanning:
			message = "Planning queries..."
		case pipeline.StageExecuting:
			message = "Executing queries..."
		case pipeline.StageInspecting:
			message = "Inspecting results..."
		case pipeline.StageSynthesizing:
			message = "Preparing answer..."
		default:
			message = "Processing..."
		}

		sendEvent("status", map[string]string{
			"step":    string(progress.Stage),
			"message": message,
		})

		// Send decomposed event when we have data questions and move to executing
		if progress.Stage == pipeline.StageExecuting && !decomposedSent && len(progress.DataQuestions) > 0 {
			dataQuestions = progress.DataQuestions
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
			decomposedSent = true
		}

		lastStage = progress.Stage
	}

	// Send periodic heartbeats to keep connection alive through proxies
	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sendEvent("heartbeat", map[string]string{})
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Run the v2 pipeline
	slog.Info("Starting v2 pipeline", "message", req.Message)
	result, err := p.RunWithProgress(ctx, req.Message, history, onProgress)
	close(heartbeatDone)

	if err != nil {
		slog.Error("v2 pipeline failed", "error", err, "message", req.Message)
		sendEvent("error", map[string]string{"error": fmt.Sprintf("Pipeline failed: %v", err)})
		return
	}

	slog.Info("v2 pipeline completed",
		"answerLen", len(result.Answer),
		"queryCount", len(result.ExecutedQueries),
		"lastStage", lastStage,
	)

	// Build and send the response
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
		json.NewEncoder(w).Encode(CompleteResponse{Error: internalError("Completion failed", err)})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CompleteResponse{Response: strings.TrimSpace(response)})
}
