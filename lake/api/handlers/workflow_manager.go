package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
	v3 "github.com/malbeclabs/doublezero/lake/agent/pkg/workflow/v3"
	"github.com/malbeclabs/doublezero/lake/api/config"
)

// WorkflowEvent represents a progress event from a running workflow.
type WorkflowEvent struct {
	Type string // "thinking", "query_started", "query_done", "done", "error"
	Data any
}

// WorkflowSubscriber receives events from a running workflow.
type WorkflowSubscriber struct {
	Events chan WorkflowEvent
	Done   chan struct{}
}

// runningWorkflow tracks a workflow executing in the background.
type runningWorkflow struct {
	ID          uuid.UUID
	SessionID   uuid.UUID
	Question    string
	Format      string // Output format: "slack" for Slack-specific formatting
	Cancel      context.CancelFunc
	subscribers map[*WorkflowSubscriber]struct{}
	mu          sync.RWMutex
}

func (rw *runningWorkflow) addSubscriber(sub *WorkflowSubscriber) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	rw.subscribers[sub] = struct{}{}
}

func (rw *runningWorkflow) removeSubscriber(sub *WorkflowSubscriber) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	delete(rw.subscribers, sub)
}

func (rw *runningWorkflow) broadcast(event WorkflowEvent) {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	subCount := len(rw.subscribers)
	sent := 0
	for sub := range rw.subscribers {
		select {
		case sub.Events <- event:
			sent++
		default:
			// Subscriber buffer full, skip
			slog.Warn("Subscriber buffer full, skipping event", "workflow_id", rw.ID, "event_type", event.Type)
		}
	}
	slog.Debug("Broadcast event", "workflow_id", rw.ID, "event_type", event.Type, "subscribers", subCount, "sent", sent)
}

func (rw *runningWorkflow) closeAll() {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	for sub := range rw.subscribers {
		close(sub.Done)
	}
	rw.subscribers = make(map[*WorkflowSubscriber]struct{})
}

// WorkflowManager manages background workflow execution.
type WorkflowManager struct {
	mu       sync.RWMutex
	running  map[uuid.UUID]*runningWorkflow // workflowID -> running workflow
	bySession map[uuid.UUID]uuid.UUID       // sessionID -> workflowID
}

// Global workflow manager instance
var Manager = &WorkflowManager{
	running:   make(map[uuid.UUID]*runningWorkflow),
	bySession: make(map[uuid.UUID]uuid.UUID),
}

// SessionChatMessage represents a message in session content, matching the web's ChatMessage format.
type SessionChatMessage struct {
	ID              string               `json:"id"`
	Role            string               `json:"role"` // "user" or "assistant"
	Content         string               `json:"content"`
	WorkflowData    *SessionWorkflowData `json:"workflowData,omitempty"`
	ExecutedQueries []string             `json:"executedQueries,omitempty"`
	Status          string               `json:"status,omitempty"` // "streaming", "complete", "error"
	WorkflowID      string               `json:"workflowId,omitempty"`
}

// SessionWorkflowData contains workflow execution details for display in the web UI.
type SessionWorkflowData struct {
	DataQuestions     []DataQuestionResponse     `json:"dataQuestions,omitempty"`
	GeneratedQueries  []GeneratedQueryResponse   `json:"generatedQueries,omitempty"`
	ExecutedQueries   []ExecutedQueryResponse    `json:"executedQueries,omitempty"`
	FollowUpQuestions []string                   `json:"followUpQuestions,omitempty"`
	ProcessingSteps   []ClientProcessingStep     `json:"processingSteps,omitempty"`
}

// ClientProcessingStep matches the web's ProcessingStep format (different field names than WorkflowStep).
type ClientProcessingStep struct {
	Type    string   `json:"type"` // "thinking" or "query"
	Content string   `json:"content,omitempty"`
	// Query fields - note: "rows" is count, "data" is row data (matches web format)
	Question string   `json:"question,omitempty"`
	SQL      string   `json:"sql,omitempty"`
	Status   string   `json:"status,omitempty"`
	Rows     int      `json:"rows,omitempty"`     // Row count (web expects this as number)
	Columns  []string `json:"columns,omitempty"`
	Data     [][]any  `json:"data,omitempty"`     // Actual row data
	Error    string   `json:"error,omitempty"`
}

// toClientFormat converts a WorkflowStep to ClientProcessingStep format.
func (s WorkflowStep) toClientFormat() ClientProcessingStep {
	return ClientProcessingStep{
		Type:     s.Type,
		Content:  s.Content,
		Question: s.Question,
		SQL:      s.SQL,
		Status:   s.Status,
		Rows:     s.Count,   // Server's "Count" -> Client's "rows"
		Columns:  s.Columns,
		Data:     s.Rows,    // Server's "Rows" -> Client's "data"
		Error:    s.Error,
	}
}

// updateSessionContent updates the session's content field with the given messages.
func updateSessionContent(ctx context.Context, sessionID uuid.UUID, messages []SessionChatMessage) error {
	contentJSON, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("failed to marshal session content: %w", err)
	}

	_, err = config.PgPool.Exec(ctx, `
		UPDATE sessions SET content = $2, updated_at = NOW()
		WHERE id = $1
	`, sessionID, contentJSON)
	if err != nil {
		return fmt.Errorf("failed to update session content: %w", err)
	}
	return nil
}

// buildSessionMessages builds the session content messages from workflow state.
func buildSessionMessages(
	workflowID uuid.UUID,
	question string,
	answer string,
	status string,
	steps []WorkflowStep,
	executedQueries []workflow.ExecutedQuery,
) []SessionChatMessage {
	messages := []SessionChatMessage{
		{
			ID:      uuid.NewString(),
			Role:    "user",
			Content: question,
		},
	}

	// Build assistant message
	assistantMsg := SessionChatMessage{
		ID:         uuid.NewString(),
		Role:       "assistant",
		Content:    answer,
		Status:     status,
		WorkflowID: workflowID.String(),
	}

	// Add workflow data if we have steps or queries
	if len(steps) > 0 || len(executedQueries) > 0 {
		// Convert steps to client format
		clientSteps := make([]ClientProcessingStep, len(steps))
		for i, s := range steps {
			clientSteps[i] = s.toClientFormat()
		}

		workflowData := &SessionWorkflowData{
			ProcessingSteps: clientSteps,
		}

		// Convert executed queries
		var sqlQueries []string
		for _, eq := range executedQueries {
			workflowData.ExecutedQueries = append(workflowData.ExecutedQueries, ExecutedQueryResponse{
				Question: eq.GeneratedQuery.DataQuestion.Question,
				SQL:      eq.Result.SQL,
				Columns:  eq.Result.Columns,
				Rows:     convertRowsToArray(eq.Result),
				Count:    eq.Result.Count,
				Error:    eq.Result.Error,
			})
			sqlQueries = append(sqlQueries, eq.Result.SQL)
		}

		assistantMsg.WorkflowData = workflowData
		assistantMsg.ExecutedQueries = sqlQueries
	}

	messages = append(messages, assistantMsg)
	return messages
}

// StartWorkflow starts a new workflow in the background.
// Returns the workflow ID immediately - the workflow runs asynchronously.
// The format parameter controls output formatting: "slack" for Slack-specific formatting.
func (m *WorkflowManager) StartWorkflow(
	sessionID uuid.UUID,
	question string,
	history []workflow.ConversationMessage,
	format string,
) (uuid.UUID, error) {
	ctx := context.Background()

	// Ensure session exists (auto-create if needed for workflow persistence)
	if err := ensureSessionExists(ctx, sessionID); err != nil {
		return uuid.Nil, fmt.Errorf("failed to ensure session exists: %w", err)
	}

	// Create workflow run in database
	run, err := CreateWorkflowRun(ctx, sessionID, question)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create workflow: %w", err)
	}

	// Initialize session content with user message and streaming assistant message
	initialMessages := buildSessionMessages(run.ID, question, "", "streaming", nil, nil)
	if err := updateSessionContent(ctx, sessionID, initialMessages); err != nil {
		slog.Warn("Failed to initialize session content", "session_id", sessionID, "error", err)
	}

	// Create cancellable context for the workflow
	workflowCtx, cancel := context.WithCancel(context.Background())

	// Track the running workflow
	rw := &runningWorkflow{
		ID:          run.ID,
		SessionID:   sessionID,
		Question:    question,
		Format:      format,
		Cancel:      cancel,
		subscribers: make(map[*WorkflowSubscriber]struct{}),
	}

	m.mu.Lock()
	m.running[run.ID] = rw
	m.bySession[sessionID] = run.ID
	m.mu.Unlock()

	// Start workflow in background goroutine
	go m.runWorkflow(workflowCtx, rw, question, history)

	slog.Info("Started background workflow",
		"workflow_id", run.ID,
		"session_id", sessionID,
		"question", truncateLog(question, 50))

	return run.ID, nil
}

// Subscribe creates a subscriber to receive events from a workflow.
// Returns nil if the workflow is not running.
func (m *WorkflowManager) Subscribe(workflowID uuid.UUID) *WorkflowSubscriber {
	m.mu.RLock()
	rw, exists := m.running[workflowID]
	m.mu.RUnlock()

	if !exists {
		slog.Info("Subscribe: workflow not in running map", "workflow_id", workflowID)
		return nil
	}

	sub := &WorkflowSubscriber{
		Events: make(chan WorkflowEvent, 100),
		Done:   make(chan struct{}),
	}
	rw.addSubscriber(sub)
	slog.Info("Subscribe: added subscriber", "workflow_id", workflowID, "subscriber_count", len(rw.subscribers))
	return sub
}

// Unsubscribe removes a subscriber from a workflow.
func (m *WorkflowManager) Unsubscribe(workflowID uuid.UUID, sub *WorkflowSubscriber) {
	m.mu.RLock()
	rw, exists := m.running[workflowID]
	m.mu.RUnlock()

	if exists {
		rw.removeSubscriber(sub)
	}
}

// GetRunningWorkflowID returns the workflow ID for a session, if one is running.
func (m *WorkflowManager) GetRunningWorkflowID(sessionID uuid.UUID) (uuid.UUID, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, exists := m.bySession[sessionID]
	return id, exists
}

// IsRunning checks if a workflow is currently running in memory.
func (m *WorkflowManager) IsRunning(workflowID uuid.UUID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.running[workflowID]
	return exists
}

// CancelWorkflow cancels a running workflow.
func (m *WorkflowManager) CancelWorkflow(workflowID uuid.UUID) bool {
	m.mu.RLock()
	rw, exists := m.running[workflowID]
	m.mu.RUnlock()

	if !exists {
		return false
	}

	rw.Cancel()
	return true
}

// runWorkflow executes the workflow in the background.
func (m *WorkflowManager) runWorkflow(
	ctx context.Context,
	rw *runningWorkflow,
	question string,
	history []workflow.ConversationMessage,
) {
	defer func() {
		// Cleanup when done
		m.mu.Lock()
		delete(m.running, rw.ID)
		delete(m.bySession, rw.SessionID)
		m.mu.Unlock()
		rw.closeAll()
	}()

	// Load prompts
	prompts, err := v3.LoadPrompts()
	if err != nil {
		slog.Error("Background workflow failed to load prompts", "workflow_id", rw.ID, "error", err)
		m.failWorkflow(ctx, rw, fmt.Sprintf("Failed to load prompts: %v", err))
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

	// Apply format-specific context
	if rw.Format == "slack" {
		cfg.FormatContext = prompts.Slack
	}

	// Add Neo4j support if available
	if config.Neo4j != nil {
		cfg.GraphQuerier = NewNeo4jQuerier()
		cfg.GraphSchemaFetcher = NewNeo4jSchemaFetcher()
	}

	// Create workflow
	wf, err := v3.New(cfg)
	if err != nil {
		slog.Error("Background workflow failed to create workflow", "workflow_id", rw.ID, "error", err)
		m.failWorkflow(ctx, rw, fmt.Sprintf("Failed to create workflow: %v", err))
		return
	}

	// Track steps in execution order for unified timeline
	var steps []WorkflowStep

	// Track metrics from the last checkpoint (for final persistence)
	var lastLLMCalls, lastInputTokens, lastOutputTokens int

	// Progress callback - broadcast to subscribers and track steps
	onProgress := func(progress workflow.Progress) {
		switch progress.Stage {
		case workflow.StageThinking:
			// Track thinking step
			steps = append(steps, WorkflowStep{
				Type:    "thinking",
				Content: progress.ThinkingContent,
			})
			rw.broadcast(WorkflowEvent{
				Type: "thinking",
				Data: map[string]string{"content": progress.ThinkingContent},
			})
		case workflow.StageQueryStarted:
			rw.broadcast(WorkflowEvent{
				Type: "query_started",
				Data: map[string]string{
					"question": progress.QueryQuestion,
					"sql":      progress.QuerySQL,
				},
			})
		case workflow.StageQueryComplete:
			// Track query step with results
			status := "completed"
			if progress.QueryError != "" {
				status = "error"
			}
			steps = append(steps, WorkflowStep{
				Type:     "query",
				Question: progress.QueryQuestion,
				SQL:      progress.QuerySQL,
				Status:   status,
				Count:    progress.QueryRows, // Row count available during progress
				Error:    progress.QueryError,
			})
			rw.broadcast(WorkflowEvent{
				Type: "query_done",
				Data: map[string]any{
					"question": progress.QueryQuestion,
					"sql":      progress.QuerySQL,
					"rows":     progress.QueryRows,
					"error":    progress.QueryError,
				},
			})
		}
	}

	// Checkpoint callback - persist to database
	onCheckpoint := func(state *v3.CheckpointState) error {
		// Track latest metrics for final persistence
		lastLLMCalls = state.Metrics.LLMCalls
		lastInputTokens = state.Metrics.InputTokens
		lastOutputTokens = state.Metrics.OutputTokens

		checkpoint := &WorkflowCheckpoint{
			Iteration:       state.Iteration,
			Messages:        state.Messages,
			ThinkingSteps:   state.ThinkingSteps,
			ExecutedQueries: state.ExecutedQueries,
			Steps:           steps, // Include unified steps
			LLMCalls:        state.Metrics.LLMCalls,
			InputTokens:     state.Metrics.InputTokens,
			OutputTokens:    state.Metrics.OutputTokens,
		}
		if err := UpdateWorkflowCheckpoint(ctx, rw.ID, checkpoint); err != nil {
			return err
		}

		// Also update session content with current progress
		sessionMessages := buildSessionMessages(rw.ID, question, "", "streaming", steps, state.ExecutedQueries)
		if err := updateSessionContent(ctx, rw.SessionID, sessionMessages); err != nil {
			slog.Warn("Failed to update session content at checkpoint", "session_id", rw.SessionID, "error", err)
		}
		return nil
	}

	// Run the workflow
	slog.Info("Background workflow starting", "workflow_id", rw.ID)
	result, err := wf.RunWithCheckpoint(ctx, question, history, onProgress, onCheckpoint)

	if err != nil {
		if ctx.Err() != nil {
			// Context was cancelled
			slog.Info("Background workflow cancelled", "workflow_id", rw.ID)
			_ = CancelWorkflowRun(context.Background(), rw.ID)
			rw.broadcast(WorkflowEvent{
				Type: "error",
				Data: map[string]string{"error": "Workflow was cancelled"},
			})
		} else {
			slog.Error("Background workflow failed", "workflow_id", rw.ID, "error", err)
			m.failWorkflow(ctx, rw, err.Error())
		}
		return
	}

	// Build final steps with full row data from result
	finalSteps := buildFinalSteps(steps, result)

	// Build and broadcast the done event with steps
	response := convertWorkflowResult(result)
	response.Steps = finalSteps
	rw.broadcast(WorkflowEvent{
		Type: "done",
		Data: response,
	})

	// Mark workflow as completed (preserve metrics from last checkpoint)
	finalCheckpoint := &WorkflowCheckpoint{
		Iteration:       0,
		Messages:        nil,
		ThinkingSteps:   nil,
		ExecutedQueries: result.ExecutedQueries,
		Steps:           finalSteps,
		LLMCalls:        lastLLMCalls,
		InputTokens:     lastInputTokens,
		OutputTokens:    lastOutputTokens,
	}
	if err := CompleteWorkflowRun(context.Background(), rw.ID, result.Answer, finalCheckpoint); err != nil {
		slog.Warn("Failed to mark workflow as completed", "workflow_id", rw.ID, "error", err)
	}

	// Update session content with final answer and status 'complete'
	finalMessages := buildSessionMessages(rw.ID, question, result.Answer, "complete", finalSteps, result.ExecutedQueries)
	if err := updateSessionContent(context.Background(), rw.SessionID, finalMessages); err != nil {
		slog.Warn("Failed to update session content on completion", "session_id", rw.SessionID, "error", err)
	}

	slog.Info("Background workflow completed",
		"workflow_id", rw.ID,
		"answer_len", len(result.Answer),
		"queries", len(result.ExecutedQueries))
}

func (m *WorkflowManager) failWorkflow(ctx context.Context, rw *runningWorkflow, errMsg string) {
	rw.broadcast(WorkflowEvent{
		Type: "error",
		Data: map[string]string{"error": errMsg},
	})
	_ = FailWorkflowRun(context.Background(), rw.ID, errMsg)
}

// ResumeWorkflowBackground resumes an incomplete workflow in the background.
// This is called on server startup for workflows left in 'running' state.
func (m *WorkflowManager) ResumeWorkflowBackground(run *WorkflowRun) error {
	// Parse checkpoint state
	var messages []workflow.ToolMessage
	if err := json.Unmarshal(run.Messages, &messages); err != nil {
		return fmt.Errorf("failed to unmarshal messages: %w", err)
	}

	var thinkingSteps []string
	if err := json.Unmarshal(run.ThinkingSteps, &thinkingSteps); err != nil {
		return fmt.Errorf("failed to unmarshal thinking steps: %w", err)
	}

	var executedQueries []workflow.ExecutedQuery
	if err := json.Unmarshal(run.ExecutedQueries, &executedQueries); err != nil {
		return fmt.Errorf("failed to unmarshal executed queries: %w", err)
	}

	checkpoint := &v3.CheckpointState{
		Iteration:       run.Iteration,
		Messages:        messages,
		ThinkingSteps:   thinkingSteps,
		ExecutedQueries: executedQueries,
		Metrics: &v3.WorkflowMetrics{
			LLMCalls:     run.LLMCalls,
			InputTokens:  run.InputTokens,
			OutputTokens: run.OutputTokens,
		},
	}

	// Create cancellable context
	workflowCtx, cancel := context.WithCancel(context.Background())

	// Track the running workflow
	rw := &runningWorkflow{
		ID:          run.ID,
		SessionID:   run.SessionID,
		Question:    run.UserQuestion,
		Cancel:      cancel,
		subscribers: make(map[*WorkflowSubscriber]struct{}),
	}

	m.mu.Lock()
	m.running[run.ID] = rw
	m.bySession[run.SessionID] = run.ID
	m.mu.Unlock()

	// Start resume in background
	go m.resumeWorkflow(workflowCtx, rw, checkpoint)

	slog.Info("Resuming background workflow",
		"workflow_id", run.ID,
		"session_id", run.SessionID,
		"iteration", run.Iteration)

	return nil
}

func (m *WorkflowManager) resumeWorkflow(
	ctx context.Context,
	rw *runningWorkflow,
	checkpoint *v3.CheckpointState,
) {
	defer func() {
		m.mu.Lock()
		delete(m.running, rw.ID)
		delete(m.bySession, rw.SessionID)
		m.mu.Unlock()
		rw.closeAll()
	}()

	// Load prompts
	prompts, err := v3.LoadPrompts()
	if err != nil {
		slog.Error("Resume workflow failed to load prompts", "workflow_id", rw.ID, "error", err)
		m.failWorkflow(ctx, rw, fmt.Sprintf("Failed to load prompts: %v", err))
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

	// Apply format-specific context
	if rw.Format == "slack" {
		cfg.FormatContext = prompts.Slack
	}

	// Add Neo4j support if available
	if config.Neo4j != nil {
		cfg.GraphQuerier = NewNeo4jQuerier()
		cfg.GraphSchemaFetcher = NewNeo4jSchemaFetcher()
	}

	// Create workflow
	wf, err := v3.New(cfg)
	if err != nil {
		slog.Error("Resume workflow failed to create workflow", "workflow_id", rw.ID, "error", err)
		m.failWorkflow(ctx, rw, fmt.Sprintf("Failed to create workflow: %v", err))
		return
	}

	// Track steps in execution order for unified timeline
	var steps []WorkflowStep

	// Track metrics from the last checkpoint (for final persistence)
	var lastLLMCalls, lastInputTokens, lastOutputTokens int

	// Progress callback - broadcast to subscribers and track steps
	onProgress := func(progress workflow.Progress) {
		switch progress.Stage {
		case workflow.StageThinking:
			// Track thinking step
			steps = append(steps, WorkflowStep{
				Type:    "thinking",
				Content: progress.ThinkingContent,
			})
			rw.broadcast(WorkflowEvent{
				Type: "thinking",
				Data: map[string]string{"content": progress.ThinkingContent},
			})
		case workflow.StageQueryStarted:
			rw.broadcast(WorkflowEvent{
				Type: "query_started",
				Data: map[string]string{
					"question": progress.QueryQuestion,
					"sql":      progress.QuerySQL,
				},
			})
		case workflow.StageQueryComplete:
			// Track query step with results
			status := "completed"
			if progress.QueryError != "" {
				status = "error"
			}
			steps = append(steps, WorkflowStep{
				Type:     "query",
				Question: progress.QueryQuestion,
				SQL:      progress.QuerySQL,
				Status:   status,
				Count:    progress.QueryRows,
				Error:    progress.QueryError,
			})
			rw.broadcast(WorkflowEvent{
				Type: "query_done",
				Data: map[string]any{
					"question": progress.QueryQuestion,
					"sql":      progress.QuerySQL,
					"rows":     progress.QueryRows,
					"error":    progress.QueryError,
				},
			})
		}
	}

	// Checkpoint callback - persist to database
	onCheckpoint := func(state *v3.CheckpointState) error {
		// Track latest metrics for final persistence
		lastLLMCalls = state.Metrics.LLMCalls
		lastInputTokens = state.Metrics.InputTokens
		lastOutputTokens = state.Metrics.OutputTokens

		wfCheckpoint := &WorkflowCheckpoint{
			Iteration:       state.Iteration,
			Messages:        state.Messages,
			ThinkingSteps:   state.ThinkingSteps,
			ExecutedQueries: state.ExecutedQueries,
			Steps:           steps, // Include unified steps
			LLMCalls:        state.Metrics.LLMCalls,
			InputTokens:     state.Metrics.InputTokens,
			OutputTokens:    state.Metrics.OutputTokens,
		}
		if err := UpdateWorkflowCheckpoint(ctx, rw.ID, wfCheckpoint); err != nil {
			return err
		}

		// Also update session content with current progress
		sessionMessages := buildSessionMessages(rw.ID, rw.Question, "", "streaming", steps, state.ExecutedQueries)
		if err := updateSessionContent(ctx, rw.SessionID, sessionMessages); err != nil {
			slog.Warn("Failed to update session content at checkpoint", "session_id", rw.SessionID, "error", err)
		}
		return nil
	}

	// Resume the workflow
	slog.Info("Resuming workflow", "workflow_id", rw.ID)
	result, err := wf.ResumeFromCheckpoint(ctx, rw.Question, checkpoint, onProgress, onCheckpoint)

	if err != nil {
		if ctx.Err() != nil {
			slog.Info("Resume workflow cancelled", "workflow_id", rw.ID)
			_ = CancelWorkflowRun(context.Background(), rw.ID)
		} else {
			slog.Error("Resume workflow failed", "workflow_id", rw.ID, "error", err)
			m.failWorkflow(ctx, rw, err.Error())
		}
		return
	}

	// Build final steps with full row data from result
	finalSteps := buildFinalSteps(steps, result)

	// Broadcast done with steps
	response := convertWorkflowResult(result)
	response.Steps = finalSteps
	rw.broadcast(WorkflowEvent{
		Type: "done",
		Data: response,
	})

	// Mark complete (preserve metrics from last checkpoint)
	finalCheckpoint := &WorkflowCheckpoint{
		Iteration:       checkpoint.Iteration,
		Messages:        nil,
		ThinkingSteps:   nil,
		ExecutedQueries: result.ExecutedQueries,
		Steps:           finalSteps,
		LLMCalls:        lastLLMCalls,
		InputTokens:     lastInputTokens,
		OutputTokens:    lastOutputTokens,
	}
	if err := CompleteWorkflowRun(context.Background(), rw.ID, result.Answer, finalCheckpoint); err != nil {
		slog.Warn("Failed to mark resumed workflow as completed", "workflow_id", rw.ID, "error", err)
	}

	// Update session content with final answer and status 'complete'
	finalMessages := buildSessionMessages(rw.ID, rw.Question, result.Answer, "complete", finalSteps, result.ExecutedQueries)
	if err := updateSessionContent(context.Background(), rw.SessionID, finalMessages); err != nil {
		slog.Warn("Failed to update session content on completion", "session_id", rw.SessionID, "error", err)
	}

	slog.Info("Resume workflow completed",
		"workflow_id", rw.ID,
		"answer_len", len(result.Answer),
		"queries", len(result.ExecutedQueries))
}

// ResumeIncompleteWorkflows checks for and resumes any workflows that were
// interrupted (e.g., by a server restart).
func (m *WorkflowManager) ResumeIncompleteWorkflows() {
	// Wait for services to stabilize
	time.Sleep(5 * time.Second)

	ctx := context.Background()

	workflows, err := GetIncompleteWorkflows(ctx)
	if err != nil {
		slog.Error("Failed to get incomplete workflows", "error", err)
		return
	}

	if len(workflows) == 0 {
		slog.Info("No incomplete workflows to resume")
		return
	}

	slog.Info("Found incomplete workflows to resume", "count", len(workflows))

	for _, wf := range workflows {
		if err := m.ResumeWorkflowBackground(&wf); err != nil {
			slog.Error("Failed to resume workflow", "workflow_id", wf.ID, "error", err)
			// Mark as failed so we don't keep trying
			_ = FailWorkflowRun(ctx, wf.ID, fmt.Sprintf("Failed to resume: %v", err))
		}
	}
}

func truncateLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ensureSessionExists creates a session if it doesn't already exist.
// This allows workflows to be created even if the frontend hasn't persisted the session yet.
func ensureSessionExists(ctx context.Context, sessionID uuid.UUID) error {
	slog.Info("ensureSessionExists called", "session_id", sessionID)

	// Use INSERT ... ON CONFLICT DO NOTHING to avoid race conditions
	result, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Untitled', '[]')
		ON CONFLICT (id) DO NOTHING
	`, sessionID)
	if err != nil {
		slog.Error("ensureSessionExists failed", "session_id", sessionID, "error", err)
		return fmt.Errorf("failed to ensure session exists: %w", err)
	}
	slog.Info("ensureSessionExists completed", "session_id", sessionID, "rows_affected", result.RowsAffected())
	return nil
}

// buildFinalSteps enriches the tracked steps with full row data from the result.
// During progress tracking, we only have row counts. At completion, we can add full data.
func buildFinalSteps(steps []WorkflowStep, result *workflow.WorkflowResult) []WorkflowStep {
	// Build a map of executed queries by SQL for quick lookup
	queryBySQL := make(map[string]*workflow.ExecutedQuery)
	for i := range result.ExecutedQueries {
		eq := &result.ExecutedQueries[i]
		queryBySQL[eq.Result.SQL] = eq
	}

	// Enrich query steps with full row data
	finalSteps := make([]WorkflowStep, len(steps))
	for i, step := range steps {
		if step.Type == "query" {
			if eq, ok := queryBySQL[step.SQL]; ok {
				// Convert rows from map format to array format
				var rows [][]any
				for _, row := range eq.Result.Rows {
					rowData := make([]any, 0, len(eq.Result.Columns))
					for _, col := range eq.Result.Columns {
						rowData = append(rowData, sanitizeValue(row[col]))
					}
					rows = append(rows, rowData)
				}
				finalSteps[i] = WorkflowStep{
					Type:     "query",
					Question: step.Question,
					SQL:      step.SQL,
					Status:   step.Status,
					Columns:  eq.Result.Columns,
					Rows:     rows,
					Count:    eq.Result.Count,
					Error:    step.Error,
				}
			} else {
				finalSteps[i] = step
			}
		} else {
			finalSteps[i] = step
		}
	}
	return finalSteps
}
