package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
	"github.com/malbeclabs/doublezero/lake/api/config"
)

// WorkflowStep represents a single step in the workflow execution timeline.
// Steps are stored in execution order to preserve the interleaving of thinking and tool calls.
//
// Step types:
//   - "thinking": Model reasoning (Content field)
//   - "sql_query": SQL query execution (Question, SQL, Status, Columns, Rows, Count, Error)
//   - "cypher_query": Cypher query execution (Question, Cypher, Status, Nodes, Edges, Count, Error)
//   - "read_docs": Documentation lookup (Page, Status, Content, Error)
type WorkflowStep struct {
	ID   string `json:"id"`   // Unique identifier for this step
	Type string `json:"type"` // "thinking", "sql_query", "cypher_query", "read_docs"

	// For thinking steps
	Content string `json:"content,omitempty"`

	// For sql_query steps
	Question string   `json:"question,omitempty"`
	SQL      string   `json:"sql,omitempty"`
	Status   string   `json:"status,omitempty"` // "running", "completed", "error"
	Columns  []string `json:"columns,omitempty"`
	Rows     [][]any  `json:"rows,omitempty"`
	Count    int      `json:"count,omitempty"`
	Error    string   `json:"error,omitempty"`

	// For cypher_query steps
	Cypher string  `json:"cypher,omitempty"`
	Nodes  []any   `json:"nodes,omitempty"`
	Edges  []any   `json:"edges,omitempty"`

	// For read_docs steps
	Page string `json:"page,omitempty"`
}

// WorkflowRun represents a persistent workflow execution.
type WorkflowRun struct {
	ID           uuid.UUID       `json:"id"`
	SessionID    uuid.UUID       `json:"session_id"`
	Status       string          `json:"status"` // running, completed, failed, cancelled
	UserQuestion string          `json:"user_question"`

	// Checkpoint state
	Iteration       int             `json:"iteration"`
	Messages        json.RawMessage `json:"messages"`
	ThinkingSteps   json.RawMessage `json:"thinking_steps"`   // Legacy - kept for backward compatibility
	ExecutedQueries json.RawMessage `json:"executed_queries"` // Legacy - kept for backward compatibility
	Steps           json.RawMessage `json:"steps"`            // Unified timeline of all steps in order
	FinalAnswer     *string         `json:"final_answer,omitempty"`

	// Metrics
	LLMCalls     int `json:"llm_calls"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`

	// Timestamps
	StartedAt   time.Time  `json:"started_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// Distributed claiming (for multi-replica resumption)
	ClaimedBy *string    `json:"claimed_by,omitempty"`
	ClaimedAt *time.Time `json:"claimed_at,omitempty"`

	// Error tracking
	Error *string `json:"error,omitempty"`
}

// WorkflowCheckpoint represents the state to be checkpointed after each iteration.
type WorkflowCheckpoint struct {
	Iteration       int                      `json:"iteration"`
	Messages        []workflow.ToolMessage   `json:"messages"`
	ThinkingSteps   []string                 `json:"thinking_steps"`   // Legacy
	ExecutedQueries []workflow.ExecutedQuery `json:"executed_queries"` // Legacy
	Steps           []WorkflowStep           `json:"steps"`            // Unified timeline

	// Metrics
	LLMCalls     int `json:"llm_calls"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// CreateWorkflowRun creates a new workflow run in the database.
func CreateWorkflowRun(ctx context.Context, sessionID uuid.UUID, question string) (*WorkflowRun, error) {
	var run WorkflowRun
	err := config.PgPool.QueryRow(ctx, `
		INSERT INTO workflow_runs (session_id, user_question)
		VALUES ($1, $2)
		RETURNING id, session_id, status, user_question, iteration, messages, thinking_steps,
		          executed_queries, steps, final_answer, llm_calls, input_tokens, output_tokens,
		          started_at, updated_at, completed_at, claimed_by, claimed_at, error
	`, sessionID, question).Scan(
		&run.ID, &run.SessionID, &run.Status, &run.UserQuestion,
		&run.Iteration, &run.Messages, &run.ThinkingSteps, &run.ExecutedQueries, &run.Steps,
		&run.FinalAnswer, &run.LLMCalls, &run.InputTokens, &run.OutputTokens,
		&run.StartedAt, &run.UpdatedAt, &run.CompletedAt, &run.ClaimedBy, &run.ClaimedAt, &run.Error,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow run: %w", err)
	}
	return &run, nil
}

// UpdateWorkflowCheckpoint updates the checkpoint state of a workflow run.
func UpdateWorkflowCheckpoint(ctx context.Context, id uuid.UUID, checkpoint *WorkflowCheckpoint) error {
	messagesJSON, err := json.Marshal(checkpoint.Messages)
	if err != nil {
		return fmt.Errorf("failed to marshal messages: %w", err)
	}
	thinkingJSON, err := json.Marshal(checkpoint.ThinkingSteps)
	if err != nil {
		return fmt.Errorf("failed to marshal thinking steps: %w", err)
	}
	queriesJSON, err := json.Marshal(checkpoint.ExecutedQueries)
	if err != nil {
		return fmt.Errorf("failed to marshal executed queries: %w", err)
	}
	stepsJSON, err := json.Marshal(checkpoint.Steps)
	if err != nil {
		return fmt.Errorf("failed to marshal steps: %w", err)
	}

	_, err = config.PgPool.Exec(ctx, `
		UPDATE workflow_runs
		SET iteration = $2, messages = $3, thinking_steps = $4, executed_queries = $5, steps = $6,
		    llm_calls = $7, input_tokens = $8, output_tokens = $9, updated_at = NOW()
		WHERE id = $1
	`, id, checkpoint.Iteration, messagesJSON, thinkingJSON, queriesJSON, stepsJSON,
		checkpoint.LLMCalls, checkpoint.InputTokens, checkpoint.OutputTokens)
	if err != nil {
		return fmt.Errorf("failed to update workflow checkpoint: %w", err)
	}
	return nil
}

// CompleteWorkflowRun marks a workflow as completed with the final answer.
func CompleteWorkflowRun(ctx context.Context, id uuid.UUID, answer string, finalCheckpoint *WorkflowCheckpoint) error {
	messagesJSON, err := json.Marshal(finalCheckpoint.Messages)
	if err != nil {
		return fmt.Errorf("failed to marshal messages: %w", err)
	}
	thinkingJSON, err := json.Marshal(finalCheckpoint.ThinkingSteps)
	if err != nil {
		return fmt.Errorf("failed to marshal thinking steps: %w", err)
	}
	queriesJSON, err := json.Marshal(finalCheckpoint.ExecutedQueries)
	if err != nil {
		return fmt.Errorf("failed to marshal executed queries: %w", err)
	}
	stepsJSON, err := json.Marshal(finalCheckpoint.Steps)
	if err != nil {
		return fmt.Errorf("failed to marshal steps: %w", err)
	}

	_, err = config.PgPool.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'completed', final_answer = $2, completed_at = NOW(), updated_at = NOW(),
		    iteration = $3, messages = $4, thinking_steps = $5, executed_queries = $6, steps = $7,
		    llm_calls = $8, input_tokens = $9, output_tokens = $10
		WHERE id = $1
	`, id, answer, finalCheckpoint.Iteration, messagesJSON, thinkingJSON, queriesJSON, stepsJSON,
		finalCheckpoint.LLMCalls, finalCheckpoint.InputTokens, finalCheckpoint.OutputTokens)
	if err != nil {
		return fmt.Errorf("failed to complete workflow run: %w", err)
	}
	return nil
}

// FailWorkflowRun marks a workflow as failed with an error message.
func FailWorkflowRun(ctx context.Context, id uuid.UUID, errMsg string) error {
	_, err := config.PgPool.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'failed', error = $2, completed_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, id, errMsg)
	if err != nil {
		return fmt.Errorf("failed to fail workflow run: %w", err)
	}
	return nil
}

// CancelWorkflowRun marks a workflow as cancelled.
func CancelWorkflowRun(ctx context.Context, id uuid.UUID) error {
	_, err := config.PgPool.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'cancelled', completed_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("failed to cancel workflow run: %w", err)
	}
	return nil
}

// GetWorkflowRun retrieves a workflow run by ID.
func GetWorkflowRun(ctx context.Context, id uuid.UUID) (*WorkflowRun, error) {
	var run WorkflowRun
	err := config.PgPool.QueryRow(ctx, `
		SELECT id, session_id, status, user_question, iteration, messages, thinking_steps,
		       executed_queries, steps, final_answer, llm_calls, input_tokens, output_tokens,
		       started_at, updated_at, completed_at, claimed_by, claimed_at, error
		FROM workflow_runs
		WHERE id = $1
	`, id).Scan(
		&run.ID, &run.SessionID, &run.Status, &run.UserQuestion,
		&run.Iteration, &run.Messages, &run.ThinkingSteps, &run.ExecutedQueries, &run.Steps,
		&run.FinalAnswer, &run.LLMCalls, &run.InputTokens, &run.OutputTokens,
		&run.StartedAt, &run.UpdatedAt, &run.CompletedAt, &run.ClaimedBy, &run.ClaimedAt, &run.Error,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get workflow run: %w", err)
	}
	return &run, nil
}

// ClaimIncompleteWorkflow atomically claims a single incomplete workflow for resumption.
// This uses UPDATE ... RETURNING to ensure only one replica can claim a workflow.
// A workflow is claimable if:
// - status = 'running'
// - not claimed (claimed_at IS NULL), OR
// - claim is stale (claimed_at < NOW() - staleTimeout AND updated_at < NOW() - staleTimeout)
//
// The stale check uses updated_at to detect if the claiming server is still making progress.
// Returns nil if no workflow is available to claim.
func ClaimIncompleteWorkflow(ctx context.Context, serverID string, staleTimeout time.Duration) (*WorkflowRun, error) {
	var run WorkflowRun
	err := config.PgPool.QueryRow(ctx, `
		UPDATE workflow_runs
		SET claimed_by = $1, claimed_at = NOW(), updated_at = NOW()
		WHERE id = (
			SELECT id FROM workflow_runs
			WHERE status = 'running'
			  AND (
			    claimed_at IS NULL
			    OR (claimed_at < NOW() - $2::interval AND updated_at < NOW() - $2::interval)
			  )
			ORDER BY started_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, session_id, status, user_question, iteration, messages, thinking_steps,
		          executed_queries, steps, final_answer, llm_calls, input_tokens, output_tokens,
		          started_at, updated_at, completed_at, claimed_by, claimed_at, error
	`, serverID, staleTimeout).Scan(
		&run.ID, &run.SessionID, &run.Status, &run.UserQuestion,
		&run.Iteration, &run.Messages, &run.ThinkingSteps, &run.ExecutedQueries, &run.Steps,
		&run.FinalAnswer, &run.LLMCalls, &run.InputTokens, &run.OutputTokens,
		&run.StartedAt, &run.UpdatedAt, &run.CompletedAt, &run.ClaimedBy, &run.ClaimedAt, &run.Error,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil // No workflow available to claim
		}
		return nil, fmt.Errorf("failed to claim workflow: %w", err)
	}
	return &run, nil
}

// GetIncompleteWorkflows returns all workflows with status='running'.
// Note: For distributed resumption, use ClaimIncompleteWorkflow instead.
func GetIncompleteWorkflows(ctx context.Context) ([]WorkflowRun, error) {
	rows, err := config.PgPool.Query(ctx, `
		SELECT id, session_id, status, user_question, iteration, messages, thinking_steps,
		       executed_queries, steps, final_answer, llm_calls, input_tokens, output_tokens,
		       started_at, updated_at, completed_at, claimed_by, claimed_at, error
		FROM workflow_runs
		WHERE status = 'running'
		ORDER BY started_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get incomplete workflows: %w", err)
	}
	defer rows.Close()

	var runs []WorkflowRun
	for rows.Next() {
		var run WorkflowRun
		if err := rows.Scan(
			&run.ID, &run.SessionID, &run.Status, &run.UserQuestion,
			&run.Iteration, &run.Messages, &run.ThinkingSteps, &run.ExecutedQueries, &run.Steps,
			&run.FinalAnswer, &run.LLMCalls, &run.InputTokens, &run.OutputTokens,
			&run.StartedAt, &run.UpdatedAt, &run.CompletedAt, &run.ClaimedBy, &run.ClaimedAt, &run.Error,
		); err != nil {
			return nil, fmt.Errorf("failed to scan workflow run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate workflow runs: %w", err)
	}
	return runs, nil
}

// GetRunningWorkflowForSession returns the currently running workflow for a session, if any.
func GetRunningWorkflowForSession(ctx context.Context, sessionID uuid.UUID) (*WorkflowRun, error) {
	var run WorkflowRun
	err := config.PgPool.QueryRow(ctx, `
		SELECT id, session_id, status, user_question, iteration, messages, thinking_steps,
		       executed_queries, steps, final_answer, llm_calls, input_tokens, output_tokens,
		       started_at, updated_at, completed_at, claimed_by, claimed_at, error
		FROM workflow_runs
		WHERE session_id = $1 AND status = 'running'
		ORDER BY started_at DESC
		LIMIT 1
	`, sessionID).Scan(
		&run.ID, &run.SessionID, &run.Status, &run.UserQuestion,
		&run.Iteration, &run.Messages, &run.ThinkingSteps, &run.ExecutedQueries, &run.Steps,
		&run.FinalAnswer, &run.LLMCalls, &run.InputTokens, &run.OutputTokens,
		&run.StartedAt, &run.UpdatedAt, &run.CompletedAt, &run.ClaimedBy, &run.ClaimedAt, &run.Error,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get running workflow: %w", err)
	}
	return &run, nil
}

// GetLatestWorkflowForSession returns the most recent workflow for a session, regardless of status.
func GetLatestWorkflowForSession(ctx context.Context, sessionID uuid.UUID) (*WorkflowRun, error) {
	var run WorkflowRun
	err := config.PgPool.QueryRow(ctx, `
		SELECT id, session_id, status, user_question, iteration, messages, thinking_steps,
		       executed_queries, steps, final_answer, llm_calls, input_tokens, output_tokens,
		       started_at, updated_at, completed_at, claimed_by, claimed_at, error
		FROM workflow_runs
		WHERE session_id = $1
		ORDER BY started_at DESC
		LIMIT 1
	`, sessionID).Scan(
		&run.ID, &run.SessionID, &run.Status, &run.UserQuestion,
		&run.Iteration, &run.Messages, &run.ThinkingSteps, &run.ExecutedQueries, &run.Steps,
		&run.FinalAnswer, &run.LLMCalls, &run.InputTokens, &run.OutputTokens,
		&run.StartedAt, &run.UpdatedAt, &run.CompletedAt, &run.ClaimedBy, &run.ClaimedAt, &run.Error,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get latest workflow: %w", err)
	}
	return &run, nil
}

// HTTP Handlers

// GetWorkflowForSession handles GET /api/sessions/{id}/workflow
// Returns the most recent workflow for a session (running, completed, or failed).
// Use ?status=running to get only running workflows (legacy behavior).
func GetWorkflowForSession(w http.ResponseWriter, r *http.Request) {
	sessionIDStr := chi.URLParam(r, "id")
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	var run *WorkflowRun

	// Check for legacy behavior (only running workflows)
	if r.URL.Query().Get("status") == "running" {
		run, err = GetRunningWorkflowForSession(r.Context(), sessionID)
	} else {
		run, err = GetLatestWorkflowForSession(r.Context(), sessionID)
	}

	if err != nil {
		http.Error(w, internalError("Failed to get workflow", err), http.StatusInternalServerError)
		return
	}
	if run == nil {
		// No workflow - return 204 No Content
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

// GetWorkflow handles GET /api/workflows/{id}
func GetWorkflow(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid workflow ID", http.StatusBadRequest)
		return
	}

	run, err := GetWorkflowRun(r.Context(), id)
	if err != nil {
		http.Error(w, internalError("Failed to get workflow", err), http.StatusInternalServerError)
		return
	}
	if run == nil {
		http.Error(w, "Workflow not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

// WorkflowStreamResponse contains the events to emit for a workflow stream reconnection.
type WorkflowStreamResponse struct {
	// For completed workflows, contains the final response
	Status   string       `json:"status"`
	Response *ChatResponse `json:"response,omitempty"`
	Error    string       `json:"error,omitempty"`

	// For running workflows, contains catch-up events
	ThinkingSteps   []string        `json:"thinking_steps,omitempty"`
	ExecutedQueries json.RawMessage `json:"executed_queries,omitempty"`
}

// StreamWorkflow handles GET /api/workflows/{id}/stream
// This enables reconnection to running workflows or replaying completed ones.
func StreamWorkflow(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid workflow ID", http.StatusBadRequest)
		return
	}

	run, err := GetWorkflowRun(r.Context(), id)
	if err != nil {
		http.Error(w, internalError("Failed to get workflow", err), http.StatusInternalServerError)
		return
	}
	if run == nil {
		http.Error(w, "Workflow not found", http.StatusNotFound)
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

	sendEvent := func(eventType string, data any) {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(jsonData))
		flusher.Flush()
	}

	// Send workflow metadata
	sendEvent("workflow_status", map[string]any{
		"id":        run.ID,
		"status":    run.Status,
		"iteration": run.Iteration,
	})

	// Emit catch-up events from stored state
	// Prefer unified steps array (preserves interleaved order)
	var steps []WorkflowStep
	if err := json.Unmarshal(run.Steps, &steps); err == nil && len(steps) > 0 {
		for _, step := range steps {
			// Use stored ID if available, otherwise generate one for backwards compatibility
			stepID := step.ID
			if stepID == "" {
				stepID = uuid.New().String()
			}
			switch step.Type {
			case "thinking":
				sendEvent("thinking", map[string]string{"id": stepID, "content": step.Content})
			case "sql_query":
				sendEvent("sql_done", map[string]any{
					"id":       stepID,
					"question": step.Question,
					"sql":      step.SQL,
					"rows":     step.Count,
					"error":    step.Error,
				})
			case "cypher_query":
				sendEvent("cypher_done", map[string]any{
					"id":       stepID,
					"question": step.Question,
					"cypher":   step.Cypher,
					"rows":     step.Count,
					"error":    step.Error,
				})
			case "read_docs":
				sendEvent("read_docs_done", map[string]any{
					"id":      stepID,
					"page":    step.Page,
					"content": step.Content,
					"error":   step.Error,
				})
			case "query":
				// Legacy type - treat as SQL
				sendEvent("sql_done", map[string]any{
					"id":       stepID,
					"question": step.Question,
					"sql":      step.SQL,
					"rows":     step.Count,
					"error":    step.Error,
				})
			}
		}
	} else {
		// Fallback to legacy arrays (order not preserved)
		var thinkingSteps []string
		if err := json.Unmarshal(run.ThinkingSteps, &thinkingSteps); err == nil {
			for _, step := range thinkingSteps {
				stepID := uuid.New().String()
				sendEvent("thinking", map[string]string{"id": stepID, "content": step})
			}
		}

		var executedQueries []workflow.ExecutedQuery
		if err := json.Unmarshal(run.ExecutedQueries, &executedQueries); err == nil {
			for _, eq := range executedQueries {
				stepID := uuid.New().String()
				// Legacy queries are SQL queries
				sendEvent("sql_done", map[string]any{
					"id":       stepID,
					"question": eq.GeneratedQuery.DataQuestion.Question,
					"sql":      eq.Result.SQL,
					"rows":     eq.Result.Count,
					"error":    eq.Result.Error,
				})
			}
		}
	}

	// Parse legacy arrays for building response (still needed for completed workflows)
	var thinkingSteps []string
	_ = json.Unmarshal(run.ThinkingSteps, &thinkingSteps)
	var executedQueries []workflow.ExecutedQuery
	_ = json.Unmarshal(run.ExecutedQueries, &executedQueries)

	// Handle based on workflow status
	switch run.Status {
	case "completed":
		// Build the response from stored data
		response := ChatResponse{
			Answer:        "",
			ThinkingSteps: thinkingSteps,
			Steps:         steps, // Include unified steps
		}
		if run.FinalAnswer != nil {
			response.Answer = *run.FinalAnswer
		}

		// Extract executed queries for response
		for _, eq := range executedQueries {
			response.ExecutedQueries = append(response.ExecutedQueries, ExecutedQueryResponse{
				Question: eq.GeneratedQuery.DataQuestion.Question,
				SQL:      eq.Result.SQL,
				Columns:  eq.Result.Columns,
				Rows:     convertRowsToArray(eq.Result),
				Count:    eq.Result.Count,
				Error:    eq.Result.Error,
			})
		}

		sendEvent("done", response)

	case "failed":
		errorMsg := "Workflow failed"
		if run.Error != nil {
			errorMsg = *run.Error
		}
		sendEvent("error", map[string]string{"error": errorMsg})

	case "cancelled":
		sendEvent("error", map[string]string{"error": "Workflow was cancelled"})

	case "running":
		// Workflow is still running - subscribe to live events from the Manager
		sub := Manager.Subscribe(id)
		if sub == nil {
			// Workflow finished between DB check and subscribe - re-fetch status
			run, err := GetWorkflowRun(r.Context(), id)
			if err != nil || run == nil {
				sendEvent("error", map[string]string{"error": "Workflow not found"})
				return
			}
			// Recursively handle the new status
			if run.Status == "completed" && run.FinalAnswer != nil {
				// Re-parse steps from refetched workflow
				var refetchedSteps []WorkflowStep
				_ = json.Unmarshal(run.Steps, &refetchedSteps)
				response := ChatResponse{Answer: *run.FinalAnswer, ThinkingSteps: thinkingSteps, Steps: refetchedSteps}
				for _, eq := range executedQueries {
					response.ExecutedQueries = append(response.ExecutedQueries, ExecutedQueryResponse{
						Question: eq.GeneratedQuery.DataQuestion.Question,
						SQL:      eq.Result.SQL,
						Columns:  eq.Result.Columns,
						Rows:     convertRowsToArray(eq.Result),
						Count:    eq.Result.Count,
						Error:    eq.Result.Error,
					})
				}
				sendEvent("done", response)
			} else if run.Status == "failed" {
				errorMsg := "Workflow failed"
				if run.Error != nil {
					errorMsg = *run.Error
				}
				sendEvent("error", map[string]string{"error": errorMsg})
			} else {
				// Workflow is "running" in DB but not in this server's memory
				// Could be: running on another pod, or orphaned after restart
				// Don't mark as failed - just tell client to retry
				slog.Info("Workflow running in DB but not in memory, client should retry", "workflow_id", id)
				sendEvent("retry", map[string]string{"message": "Workflow is running, please retry"})
			}
			return
		}
		defer Manager.Unsubscribe(id, sub)

		sendEvent("live", map[string]string{"message": "Workflow is running"})
		slog.Info("StreamWorkflow: sent live event, entering event loop", "workflow_id", id)

		// Forward events from background workflow to SSE stream
		ctx := r.Context()
		heartbeatTicker := time.NewTicker(15 * time.Second)
		defer heartbeatTicker.Stop()

		for {
			select {
			case event, ok := <-sub.Events:
				if !ok {
					slog.Info("StreamWorkflow: events channel closed", "workflow_id", id)
					return
				}
				slog.Info("StreamWorkflow: received event", "workflow_id", id, "event_type", event.Type)
				sendEvent(event.Type, event.Data)
				if event.Type == "done" || event.Type == "error" {
					return
				}

			case <-sub.Done:
				slog.Info("StreamWorkflow: done channel signaled", "workflow_id", id)
				return

			case <-heartbeatTicker.C:
				slog.Debug("StreamWorkflow: sending heartbeat", "workflow_id", id)
				sendEvent("heartbeat", map[string]string{})

			case <-ctx.Done():
				// Client disconnected - workflow continues in background
				slog.Info("StreamWorkflow: client disconnected", "workflow_id", id)
				return
			}
		}
	}
}

// Helper to convert query result rows to array format for the response
func convertRowsToArray(result workflow.QueryResult) [][]any {
	var rows [][]any
	for _, row := range result.Rows {
		rowData := make([]any, 0, len(result.Columns))
		for _, col := range result.Columns {
			rowData = append(rowData, sanitizeValue(row[col]))
		}
		rows = append(rows, rowData)
	}
	return rows
}

