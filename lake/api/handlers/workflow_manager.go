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

// StartWorkflow starts a new workflow in the background.
// Returns the workflow ID immediately - the workflow runs asynchronously.
func (m *WorkflowManager) StartWorkflow(
	sessionID uuid.UUID,
	question string,
	history []workflow.ConversationMessage,
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

	// Create cancellable context for the workflow
	workflowCtx, cancel := context.WithCancel(context.Background())

	// Track the running workflow
	rw := &runningWorkflow{
		ID:          run.ID,
		SessionID:   sessionID,
		Question:    question,
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

	// Create workflow
	wf, err := v3.New(&workflow.Config{
		Logger:        slog.Default(),
		LLM:           llm,
		Querier:       querier,
		SchemaFetcher: schemaFetcher,
		Prompts:       prompts,
		MaxTokens:     4096,
	})
	if err != nil {
		slog.Error("Background workflow failed to create workflow", "workflow_id", rw.ID, "error", err)
		m.failWorkflow(ctx, rw, fmt.Sprintf("Failed to create workflow: %v", err))
		return
	}

	// Progress callback - broadcast to subscribers
	onProgress := func(progress workflow.Progress) {
		switch progress.Stage {
		case workflow.StageThinking:
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
		checkpoint := &WorkflowCheckpoint{
			Iteration:       state.Iteration,
			Messages:        state.Messages,
			ThinkingSteps:   state.ThinkingSteps,
			ExecutedQueries: state.ExecutedQueries,
			LLMCalls:        state.Metrics.LLMCalls,
			InputTokens:     state.Metrics.InputTokens,
			OutputTokens:    state.Metrics.OutputTokens,
		}
		return UpdateWorkflowCheckpoint(ctx, rw.ID, checkpoint)
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

	// Build and broadcast the done event
	response := convertWorkflowResult(result)
	rw.broadcast(WorkflowEvent{
		Type: "done",
		Data: response,
	})

	// Mark workflow as completed
	finalCheckpoint := &WorkflowCheckpoint{
		Iteration:       0,
		Messages:        nil,
		ThinkingSteps:   nil,
		ExecutedQueries: result.ExecutedQueries,
		LLMCalls:        0,
		InputTokens:     0,
		OutputTokens:    0,
	}
	if err := CompleteWorkflowRun(context.Background(), rw.ID, result.Answer, finalCheckpoint); err != nil {
		slog.Warn("Failed to mark workflow as completed", "workflow_id", rw.ID, "error", err)
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

	// Create workflow
	wf, err := v3.New(&workflow.Config{
		Logger:        slog.Default(),
		LLM:           llm,
		Querier:       querier,
		SchemaFetcher: schemaFetcher,
		Prompts:       prompts,
		MaxTokens:     4096,
	})
	if err != nil {
		slog.Error("Resume workflow failed to create workflow", "workflow_id", rw.ID, "error", err)
		m.failWorkflow(ctx, rw, fmt.Sprintf("Failed to create workflow: %v", err))
		return
	}

	// Progress callback
	onProgress := func(progress workflow.Progress) {
		switch progress.Stage {
		case workflow.StageThinking:
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

	// Checkpoint callback
	onCheckpoint := func(state *v3.CheckpointState) error {
		wfCheckpoint := &WorkflowCheckpoint{
			Iteration:       state.Iteration,
			Messages:        state.Messages,
			ThinkingSteps:   state.ThinkingSteps,
			ExecutedQueries: state.ExecutedQueries,
			LLMCalls:        state.Metrics.LLMCalls,
			InputTokens:     state.Metrics.InputTokens,
			OutputTokens:    state.Metrics.OutputTokens,
		}
		return UpdateWorkflowCheckpoint(ctx, rw.ID, wfCheckpoint)
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

	// Broadcast done
	response := convertWorkflowResult(result)
	rw.broadcast(WorkflowEvent{
		Type: "done",
		Data: response,
	})

	// Mark complete
	finalCheckpoint := &WorkflowCheckpoint{
		Iteration:       checkpoint.Iteration,
		Messages:        nil,
		ThinkingSteps:   nil,
		ExecutedQueries: result.ExecutedQueries,
	}
	if err := CompleteWorkflowRun(context.Background(), rw.ID, result.Answer, finalCheckpoint); err != nil {
		slog.Warn("Failed to mark resumed workflow as completed", "workflow_id", rw.ID, "error", err)
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
