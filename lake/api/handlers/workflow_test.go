package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/handlers"
	apitesting "github.com/malbeclabs/doublezero/lake/api/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateWorkflowRun(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	// Create a session first (required for foreign key)
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	// Create a workflow run
	run, err := handlers.CreateWorkflowRun(ctx, sessionID, "What is 2+2?")
	require.NoError(t, err)
	require.NotNil(t, run)

	assert.NotEqual(t, uuid.Nil, run.ID)
	assert.Equal(t, sessionID, run.SessionID)
	assert.Equal(t, "running", run.Status)
	assert.Equal(t, "What is 2+2?", run.UserQuestion)
	assert.Equal(t, 0, run.Iteration)
	assert.NotNil(t, run.StartedAt)
}

func TestUpdateWorkflowCheckpoint(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	// Create session and workflow
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	run, err := handlers.CreateWorkflowRun(ctx, sessionID, "Test question")
	require.NoError(t, err)

	// Update checkpoint
	checkpoint := &handlers.WorkflowCheckpoint{
		Iteration:     1,
		ThinkingSteps: []string{"Step 1", "Step 2"},
		LLMCalls:      2,
		InputTokens:   100,
		OutputTokens:  50,
	}

	err = handlers.UpdateWorkflowCheckpoint(ctx, run.ID, checkpoint)
	require.NoError(t, err)

	// Verify update
	updatedRun, err := handlers.GetWorkflowRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, updatedRun.Iteration)
	assert.Equal(t, 2, updatedRun.LLMCalls)
	assert.Equal(t, 100, updatedRun.InputTokens)
	assert.Equal(t, 50, updatedRun.OutputTokens)
}

func TestCompleteWorkflowRun(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	// Create session and workflow
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	run, err := handlers.CreateWorkflowRun(ctx, sessionID, "Test question")
	require.NoError(t, err)

	// Complete workflow
	finalCheckpoint := &handlers.WorkflowCheckpoint{
		Iteration:     3,
		ThinkingSteps: []string{"Done"},
		LLMCalls:      5,
		InputTokens:   500,
		OutputTokens:  200,
	}

	err = handlers.CompleteWorkflowRun(ctx, run.ID, "The answer is 42", finalCheckpoint)
	require.NoError(t, err)

	// Verify completion
	completedRun, err := handlers.GetWorkflowRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", completedRun.Status)
	assert.NotNil(t, completedRun.FinalAnswer)
	assert.Equal(t, "The answer is 42", *completedRun.FinalAnswer)
	assert.NotNil(t, completedRun.CompletedAt)
}

func TestFailWorkflowRun(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	// Create session and workflow
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	run, err := handlers.CreateWorkflowRun(ctx, sessionID, "Test question")
	require.NoError(t, err)

	// Fail workflow
	err = handlers.FailWorkflowRun(ctx, run.ID, "Something went wrong")
	require.NoError(t, err)

	// Verify failure
	failedRun, err := handlers.GetWorkflowRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", failedRun.Status)
	assert.NotNil(t, failedRun.Error)
	assert.Equal(t, "Something went wrong", *failedRun.Error)
	assert.NotNil(t, failedRun.CompletedAt)
}

func TestCancelWorkflowRun(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	// Create session and workflow
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	run, err := handlers.CreateWorkflowRun(ctx, sessionID, "Test question")
	require.NoError(t, err)

	// Cancel workflow
	err = handlers.CancelWorkflowRun(ctx, run.ID)
	require.NoError(t, err)

	// Verify cancellation
	cancelledRun, err := handlers.GetWorkflowRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", cancelledRun.Status)
	assert.NotNil(t, cancelledRun.CompletedAt)
}

func TestGetWorkflowRun_NotFound(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	run, err := handlers.GetWorkflowRun(ctx, uuid.New())
	require.NoError(t, err)
	assert.Nil(t, run)
}

func TestGetRunningWorkflowForSession(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	// Create session
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	// No running workflows initially
	run, err := handlers.GetRunningWorkflowForSession(ctx, sessionID)
	require.NoError(t, err)
	assert.Nil(t, run)

	// Create a workflow
	createdRun, err := handlers.CreateWorkflowRun(ctx, sessionID, "Test question")
	require.NoError(t, err)

	// Now should find the running workflow
	run, err = handlers.GetRunningWorkflowForSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, createdRun.ID, run.ID)
	assert.Equal(t, "running", run.Status)

	// Complete the workflow
	err = handlers.CompleteWorkflowRun(ctx, createdRun.ID, "Done", &handlers.WorkflowCheckpoint{})
	require.NoError(t, err)

	// Should not find running workflow anymore
	run, err = handlers.GetRunningWorkflowForSession(ctx, sessionID)
	require.NoError(t, err)
	assert.Nil(t, run)
}

func TestGetLatestWorkflowForSession(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	// Create session
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	// Create first workflow and complete it
	run1, err := handlers.CreateWorkflowRun(ctx, sessionID, "First question")
	require.NoError(t, err)
	err = handlers.CompleteWorkflowRun(ctx, run1.ID, "First answer", &handlers.WorkflowCheckpoint{})
	require.NoError(t, err)

	// Create second workflow
	run2, err := handlers.CreateWorkflowRun(ctx, sessionID, "Second question")
	require.NoError(t, err)

	// Latest should be the second one
	latest, err := handlers.GetLatestWorkflowForSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, run2.ID, latest.ID)
}

func TestGetIncompleteWorkflows(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	// Clean up any existing workflows from other tests
	_, err := config.PgPool.Exec(ctx, "DELETE FROM workflow_runs")
	require.NoError(t, err)

	// Create sessions and workflows
	for i := 0; i < 3; i++ {
		sessionID := uuid.New()
		_, err := config.PgPool.Exec(ctx, `
			INSERT INTO sessions (id, type, name, content)
			VALUES ($1, 'chat', 'Test Session', '[]')
		`, sessionID)
		require.NoError(t, err)

		_, err = handlers.CreateWorkflowRun(ctx, sessionID, "Test question")
		require.NoError(t, err)
	}

	// Get incomplete workflows
	runs, err := handlers.GetIncompleteWorkflows(ctx)
	require.NoError(t, err)
	assert.Len(t, runs, 3)

	// All should be running
	for _, run := range runs {
		assert.Equal(t, "running", run.Status)
	}
}

func TestGetWorkflow_HTTPHandler(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	// Create session and workflow
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	run, err := handlers.CreateWorkflowRun(ctx, sessionID, "Test question")
	require.NoError(t, err)

	// Test HTTP handler
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/"+run.ID.String(), nil)
	req = withChiURLParams(req, map[string]string{"id": run.ID.String()})

	rr := httptest.NewRecorder()
	handlers.GetWorkflow(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.WorkflowRun
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, run.ID, response.ID)
}

func TestGetWorkflow_NotFound(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)

	req := httptest.NewRequest(http.MethodGet, "/api/workflows/"+uuid.New().String(), nil)
	req = withChiURLParams(req, map[string]string{"id": uuid.New().String()})

	rr := httptest.NewRecorder()
	handlers.GetWorkflow(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetWorkflow_InvalidID(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)

	req := httptest.NewRequest(http.MethodGet, "/api/workflows/not-a-uuid", nil)
	req = withChiURLParams(req, map[string]string{"id": "not-a-uuid"})

	rr := httptest.NewRecorder()
	handlers.GetWorkflow(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestGetWorkflowForSession_HTTPHandler(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	// Create session and workflow
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	run, err := handlers.CreateWorkflowRun(ctx, sessionID, "Test question")
	require.NoError(t, err)

	// Test HTTP handler
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID.String()+"/workflow", nil)
	req = withChiURLParams(req, map[string]string{"id": sessionID.String()})

	rr := httptest.NewRecorder()
	handlers.GetWorkflowForSession(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.WorkflowRun
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, run.ID, response.ID)
}

func TestGetWorkflowForSession_NoWorkflow(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	// Create session without workflow
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID.String()+"/workflow", nil)
	req = withChiURLParams(req, map[string]string{"id": sessionID.String()})

	rr := httptest.NewRecorder()
	handlers.GetWorkflowForSession(rr, req)

	// Should return 204 No Content
	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestGetWorkflowForSession_RunningOnly(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	// Create session
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	// Create and complete a workflow
	run, err := handlers.CreateWorkflowRun(ctx, sessionID, "Test question")
	require.NoError(t, err)
	err = handlers.CompleteWorkflowRun(ctx, run.ID, "Answer", &handlers.WorkflowCheckpoint{})
	require.NoError(t, err)

	// Request with status=running filter
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID.String()+"/workflow?status=running", nil)
	req = withChiURLParams(req, map[string]string{"id": sessionID.String()})

	rr := httptest.NewRecorder()
	handlers.GetWorkflowForSession(rr, req)

	// Should return 204 No Content (no running workflow)
	assert.Equal(t, http.StatusNoContent, rr.Code)

	// Without filter, should return the completed workflow
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID.String()+"/workflow", nil)
	req = withChiURLParams(req, map[string]string{"id": sessionID.String()})

	rr = httptest.NewRecorder()
	handlers.GetWorkflowForSession(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestWorkflowStep_JSONSerialization(t *testing.T) {
	step := handlers.WorkflowStep{
		ID:       "step-123",
		Type:     "sql_query",
		Question: "How many users?",
		SQL:      "SELECT COUNT(*) FROM users",
		Status:   "completed",
		Columns:  []string{"count"},
		Rows:     [][]any{{42}},
		Count:    1,
	}

	data, err := json.Marshal(step)
	require.NoError(t, err)

	var decoded handlers.WorkflowStep
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, step.ID, decoded.ID)
	assert.Equal(t, step.Type, decoded.Type)
	assert.Equal(t, step.Question, decoded.Question)
	assert.Equal(t, step.SQL, decoded.SQL)
	assert.Equal(t, step.Status, decoded.Status)
	assert.Equal(t, step.Columns, decoded.Columns)
	assert.Equal(t, step.Count, decoded.Count)
}
