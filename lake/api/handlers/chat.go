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
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
	v3 "github.com/malbeclabs/doublezero/lake/agent/pkg/workflow/v3"
	"github.com/malbeclabs/doublezero/lake/api/config"
)

// ChatMessage represents a single message in conversation history.
type ChatMessage struct {
	Role            string   `json:"role"`              // "user" or "assistant"
	Content         string   `json:"content"`
	ExecutedQueries []string `json:"executedQueries,omitempty"` // SQL from previous turns
}

// ChatRequest is the incoming request for a chat message.
type ChatRequest struct {
	Message     string        `json:"message"`
	History     []ChatMessage `json:"history"`
	SessionID   string        `json:"session_id,omitempty"`   // Optional session ID for workflow persistence
	Format      string        `json:"format,omitempty"`       // Output format: "slack" for Slack-specific formatting
	AnonymousID string        `json:"anonymous_id,omitempty"` // For anonymous users to prove session ownership
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

// ChatResponse is the full workflow result returned to the UI.
type ChatResponse struct {
	// The final synthesized answer
	Answer string `json:"answer"`

	// Workflow steps (for transparency)
	DataQuestions    []DataQuestionResponse    `json:"dataQuestions,omitempty"`
	GeneratedQueries []GeneratedQueryResponse  `json:"generatedQueries,omitempty"`
	ExecutedQueries  []ExecutedQueryResponse   `json:"executedQueries,omitempty"`

	// Thinking steps from the agent (for timeline display)
	ThinkingSteps []string `json:"thinking_steps,omitempty"`

	// Unified steps in execution order (thinking + queries interleaved)
	Steps []WorkflowStep `json:"steps,omitempty"`

	// Suggested follow-up questions
	FollowUpQuestions []string `json:"followUpQuestions,omitempty"`

	// Error if workflow failed
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
	prompts, err := v3.LoadPrompts()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: internalError("Failed to load prompts", err)})
		return
	}

	// Create workflow components
	llm := workflow.NewAnthropicLLMClient(anthropic.ModelClaude3_5Haiku20241022, 4096)
	querier := NewDBQuerier()
	schemaFetcher := NewDBSchemaFetcher()

	// Create workflow config
	cfg := &workflow.Config{
		Logger:        slog.Default(),
		LLM:           llm,
		Querier:       querier,
		SchemaFetcher: schemaFetcher,
		Prompts:       prompts,
		MaxTokens:     4096,
	}

	// Add Neo4j support if available
	if config.Neo4jClient != nil {
		cfg.GraphQuerier = NewNeo4jQuerier()
		cfg.GraphSchemaFetcher = NewNeo4jSchemaFetcher()
	}

	// Create and run workflow
	wf, err := v3.New(cfg)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: internalError("Failed to initialize chat", err)})
		return
	}

	// Convert history to workflow format
	var history []workflow.ConversationMessage
	for _, msg := range req.History {
		history = append(history, workflow.ConversationMessage{
			Role:            msg.Role,
			Content:         msg.Content,
			ExecutedQueries: msg.ExecutedQueries,
		})
	}

	result, err := wf.RunWithHistory(r.Context(), req.Message, history)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: internalError("Chat processing failed", err)})
		return
	}

	// Convert workflow result to response
	response := convertWorkflowResult(result)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// convertWorkflowResult converts the internal workflow result to the API response format.
func convertWorkflowResult(result *workflow.WorkflowResult) ChatResponse {
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

// QuotaExceededError represents a quota exceeded error response
type QuotaExceededError struct {
	Error     string `json:"error"`
	Remaining int    `json:"remaining"`
	ResetsAt  string `json:"resets_at"`
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

	// Get account and IP for quota checking
	ctx := r.Context()
	account := GetAccountFromContext(ctx)
	ip := GetIPFromRequest(r)

	// Check quota before processing
	remaining, err := CheckQuota(ctx, account, ip)
	if err != nil {
		// Handle specific errors
		if err == ErrKillSwitch {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(QuotaExceededError{
				Error:     "Service temporarily unavailable. Please try again later.",
				Remaining: 0,
				ResetsAt:  nextMidnightUTC().Format(time.RFC3339),
			})
			return
		}
		if err == ErrGlobalLimitExceeded {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(QuotaExceededError{
				Error:     "Service is currently at capacity. Please try again tomorrow.",
				Remaining: 0,
				ResetsAt:  nextMidnightUTC().Format(time.RFC3339),
			})
			return
		}
		slog.Error("Failed to check quota", "error", err)
		// Continue without quota check on other errors
	} else if remaining != nil && *remaining <= 0 {
		// Quota exceeded - return error before setting SSE headers
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(QuotaExceededError{
			Error:     "Daily question limit exceeded. Please sign in or try again tomorrow.",
			Remaining: 0,
			ResetsAt:  nextMidnightUTC().Format(time.RFC3339),
		})
		return
	}

	// Validate session ownership if session_id is provided
	if req.SessionID != "" {
		sessionUUID, err := uuid.Parse(req.SessionID)
		if err != nil {
			http.Error(w, "Invalid session_id", http.StatusBadRequest)
			return
		}

		// Check if the session exists and if caller owns it
		var accountID *uuid.UUID
		var anonymousID *string
		err = config.PgPool.QueryRow(ctx, `
			SELECT account_id, anonymous_id FROM sessions WHERE id = $1
		`, sessionUUID).Scan(&accountID, &anonymousID)

		if err != nil && err.Error() != "no rows in result set" {
			slog.Error("Failed to check session ownership", "session_id", req.SessionID, "error", err)
			http.Error(w, "Failed to verify session ownership", http.StatusInternalServerError)
			return
		}

		// If session exists, verify ownership
		if err == nil {
			owned := false
			if account != nil {
				// Authenticated user - must own via account_id or session must be orphaned
				owned = (accountID != nil && *accountID == account.ID) ||
					(accountID == nil && anonymousID == nil)
			} else if req.AnonymousID != "" {
				// Anonymous user - must own via anonymous_id or session must be orphaned
				owned = (anonymousID != nil && *anonymousID == req.AnonymousID) ||
					(accountID == nil && anonymousID == nil)
			} else {
				// No credentials - only orphaned sessions allowed
				owned = (accountID == nil && anonymousID == nil)
			}

			if !owned {
				http.Error(w, "Session not found", http.StatusNotFound)
				return
			}
		}
		// If session doesn't exist, allow (it will be created on sync)
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Add quota headers
	if remaining != nil {
		quota, _ := GetQuotaForAccount(ctx, account, ip)
		SetQuotaHeaders(w, quota)
	}

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

	// Record usage immediately (before starting workflow to prevent gaming)
	if err := IncrementQuestionCount(ctx, account, ip); err != nil {
		slog.Error("Failed to record usage", "error", err)
		// Continue even on error - don't block the user
	}

	// Convert history to workflow format
	var history []workflow.ConversationMessage
	for _, msg := range req.History {
		history = append(history, workflow.ConversationMessage{
			Role:            msg.Role,
			Content:         msg.Content,
			ExecutedQueries: msg.ExecutedQueries,
		})
	}

	// Use v3 workflow
	chatStreamV3(ctx, req, history, sendEvent)
}

// chatStreamV3 handles the v3 workflow streaming using background execution.
// The workflow runs in a background goroutine and continues even if the client disconnects.
func chatStreamV3(ctx context.Context, req ChatRequest, history []workflow.ConversationMessage, sendEvent func(string, any)) {
	// Validate session_id is provided (required for background execution)
	if req.SessionID == "" {
		sendEvent("error", map[string]string{"error": "session_id is required"})
		return
	}

	sessionUUID, err := uuid.Parse(req.SessionID)
	if err != nil {
		sendEvent("error", map[string]string{"error": "Invalid session_id"})
		return
	}

	// Start the workflow in background
	workflowID, err := Manager.StartWorkflow(sessionUUID, req.Message, history, req.Format)
	if err != nil {
		slog.Error("Failed to start background workflow", "session_id", req.SessionID, "error", err)
		// Don't expose internal errors to the UI
		sendEvent("error", map[string]string{"error": "Failed to start workflow. Please try again."})
		return
	}

	// Send workflow_started event immediately
	sendEvent("workflow_started", map[string]string{
		"workflow_id": workflowID.String(),
	})
	slog.Info("Started background workflow from chat", "workflow_id", workflowID, "session_id", req.SessionID)

	// Subscribe to workflow events
	sub := Manager.Subscribe(workflowID)
	if sub == nil {
		// Workflow already completed (shouldn't happen, but handle gracefully)
		sendEvent("error", map[string]string{"error": "Workflow not found"})
		return
	}
	defer Manager.Unsubscribe(workflowID, sub)

	// Send periodic heartbeats to keep connection alive through proxies
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	// Forward events from workflow to SSE stream
	for {
		select {
		case event, ok := <-sub.Events:
			if !ok {
				// Channel closed, workflow done
				return
			}
			sendEvent(event.Type, event.Data)
			// If this is the final event, we're done
			if event.Type == "done" || event.Type == "error" {
				return
			}

		case <-sub.Done:
			// Workflow completed (either success or error was already sent)
			return

		case <-heartbeatTicker.C:
			sendEvent("heartbeat", map[string]string{})

		case <-ctx.Done():
			// Client disconnected - workflow continues in background
			slog.Info("Client disconnected, workflow continues in background",
				"workflow_id", workflowID,
				"session_id", req.SessionID)
			return
		}
	}
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

// Complete handles simple LLM completion requests without the full workflow.
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
	llm := workflow.NewAnthropicLLMClient(anthropic.ModelClaude3_5Haiku20241022, 256)

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
