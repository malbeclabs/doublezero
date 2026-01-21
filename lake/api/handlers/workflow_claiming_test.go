package handlers_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/handlers"
	apitesting "github.com/malbeclabs/doublezero/lake/api/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimIncompleteWorkflow_SingleWorkflow(t *testing.T) {
	apitesting.SetupTestDB(t, testDB)
	ctx := t.Context()

	// Create a session first (required for foreign key)
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	// Create a workflow run
	run, err := handlers.CreateWorkflowRun(ctx, sessionID, "test question")
	require.NoError(t, err)
	require.NotNil(t, run)

	// Server 1 claims the workflow
	server1ID := "server-1"
	claimed, err := handlers.ClaimIncompleteWorkflow(ctx, server1ID, 5*time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, run.ID, claimed.ID)
	assert.Equal(t, &server1ID, claimed.ClaimedBy)
	assert.NotNil(t, claimed.ClaimedAt)
}

func TestClaimIncompleteWorkflow_AlreadyClaimed(t *testing.T) {
	apitesting.SetupTestDB(t, testDB)
	ctx := t.Context()

	// Create a session
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	// Create a workflow run
	run, err := handlers.CreateWorkflowRun(ctx, sessionID, "test question")
	require.NoError(t, err)

	// Server 1 claims the workflow
	server1ID := "server-1"
	claimed1, err := handlers.ClaimIncompleteWorkflow(ctx, server1ID, 5*time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed1)
	assert.Equal(t, run.ID, claimed1.ID)

	// Server 2 tries to claim - should get nothing (workflow already claimed)
	server2ID := "server-2"
	claimed2, err := handlers.ClaimIncompleteWorkflow(ctx, server2ID, 5*time.Minute)
	require.NoError(t, err)
	assert.Nil(t, claimed2, "second server should not be able to claim already-claimed workflow")
}

func TestClaimIncompleteWorkflow_StaleClaim(t *testing.T) {
	apitesting.SetupTestDB(t, testDB)
	ctx := t.Context()

	// Create a session
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	// Create a workflow run
	_, err = handlers.CreateWorkflowRun(ctx, sessionID, "test question")
	require.NoError(t, err)

	// Server 1 claims the workflow
	server1ID := "server-1"
	claimed1, err := handlers.ClaimIncompleteWorkflow(ctx, server1ID, 5*time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed1)

	// Simulate stale claim by backdating claimed_at and updated_at
	_, err = config.PgPool.Exec(ctx, `
		UPDATE workflow_runs
		SET claimed_at = NOW() - INTERVAL '10 minutes',
		    updated_at = NOW() - INTERVAL '10 minutes'
		WHERE id = $1
	`, claimed1.ID)
	require.NoError(t, err)

	// Server 2 should now be able to steal the workflow (stale claim)
	server2ID := "server-2"
	claimed2, err := handlers.ClaimIncompleteWorkflow(ctx, server2ID, 5*time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed2, "second server should steal stale-claimed workflow")
	assert.Equal(t, claimed1.ID, claimed2.ID)
	assert.Equal(t, &server2ID, claimed2.ClaimedBy)
}

func TestClaimIncompleteWorkflow_ActiveClaimWithProgress(t *testing.T) {
	apitesting.SetupTestDB(t, testDB)
	ctx := t.Context()

	// Create a session
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	// Create a workflow run
	_, err = handlers.CreateWorkflowRun(ctx, sessionID, "test question")
	require.NoError(t, err)

	// Server 1 claims the workflow
	server1ID := "server-1"
	claimed1, err := handlers.ClaimIncompleteWorkflow(ctx, server1ID, 5*time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed1)

	// Backdate claimed_at but keep updated_at recent (simulates active progress)
	_, err = config.PgPool.Exec(ctx, `
		UPDATE workflow_runs
		SET claimed_at = NOW() - INTERVAL '10 minutes',
		    updated_at = NOW()
		WHERE id = $1
	`, claimed1.ID)
	require.NoError(t, err)

	// Server 2 should NOT be able to steal (updated_at is recent)
	server2ID := "server-2"
	claimed2, err := handlers.ClaimIncompleteWorkflow(ctx, server2ID, 5*time.Minute)
	require.NoError(t, err)
	assert.Nil(t, claimed2, "second server should not steal workflow with recent progress")
}

func TestClaimIncompleteWorkflow_CompletedNotClaimable(t *testing.T) {
	apitesting.SetupTestDB(t, testDB)
	ctx := t.Context()

	// Create a session
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	// Create and immediately complete a workflow
	run, err := handlers.CreateWorkflowRun(ctx, sessionID, "test question")
	require.NoError(t, err)

	err = handlers.CompleteWorkflowRun(ctx, run.ID, "test answer", &handlers.WorkflowCheckpoint{})
	require.NoError(t, err)

	// Try to claim - should get nothing (workflow is completed)
	serverID := "server-1"
	claimed, err := handlers.ClaimIncompleteWorkflow(ctx, serverID, 5*time.Minute)
	require.NoError(t, err)
	assert.Nil(t, claimed, "completed workflow should not be claimable")
}

func TestClaimIncompleteWorkflow_FailedNotClaimable(t *testing.T) {
	apitesting.SetupTestDB(t, testDB)
	ctx := t.Context()

	// Create a session
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	// Create and immediately fail a workflow
	run, err := handlers.CreateWorkflowRun(ctx, sessionID, "test question")
	require.NoError(t, err)

	err = handlers.FailWorkflowRun(ctx, run.ID, "test error")
	require.NoError(t, err)

	// Try to claim - should get nothing (workflow is failed)
	serverID := "server-1"
	claimed, err := handlers.ClaimIncompleteWorkflow(ctx, serverID, 5*time.Minute)
	require.NoError(t, err)
	assert.Nil(t, claimed, "failed workflow should not be claimable")
}

func TestClaimIncompleteWorkflow_MultipleWorkflows(t *testing.T) {
	apitesting.SetupTestDB(t, testDB)
	ctx := t.Context()

	// Create sessions and workflows
	var workflowIDs []uuid.UUID
	for i := 0; i < 3; i++ {
		sessionID := uuid.New()
		_, err := config.PgPool.Exec(ctx, `
			INSERT INTO sessions (id, type, name, content)
			VALUES ($1, 'chat', 'Test Session', '[]')
		`, sessionID)
		require.NoError(t, err)

		run, err := handlers.CreateWorkflowRun(ctx, sessionID, "test question")
		require.NoError(t, err)
		workflowIDs = append(workflowIDs, run.ID)

		// Small delay to ensure different started_at times
		time.Sleep(10 * time.Millisecond)
	}

	// Server 1 claims workflows one by one
	server1ID := "server-1"
	var claimedByServer1 []uuid.UUID
	for {
		claimed, err := handlers.ClaimIncompleteWorkflow(ctx, server1ID, 5*time.Minute)
		require.NoError(t, err)
		if claimed == nil {
			break
		}
		claimedByServer1 = append(claimedByServer1, claimed.ID)
	}

	assert.Len(t, claimedByServer1, 3, "server 1 should claim all 3 workflows")

	// Verify claimed in started_at order (oldest first)
	assert.Equal(t, workflowIDs, claimedByServer1, "workflows should be claimed in started_at order")
}

func TestClaimIncompleteWorkflow_ConcurrentClaims(t *testing.T) {
	apitesting.SetupTestDB(t, testDB)
	ctx := t.Context()

	// Create a session and workflow
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, 'chat', 'Test Session', '[]')
	`, sessionID)
	require.NoError(t, err)

	_, err = handlers.CreateWorkflowRun(ctx, sessionID, "test question")
	require.NoError(t, err)

	// Simulate concurrent claims from multiple servers
	numServers := 5
	results := make(chan *handlers.WorkflowRun, numServers)

	for i := 0; i < numServers; i++ {
		go func(serverNum int) {
			serverID := uuid.NewString()
			claimed, err := handlers.ClaimIncompleteWorkflow(ctx, serverID, 5*time.Minute)
			if err != nil {
				t.Logf("server %d error: %v", serverNum, err)
			}
			results <- claimed
		}(i)
	}

	// Collect results
	var successCount int
	for i := 0; i < numServers; i++ {
		result := <-results
		if result != nil {
			successCount++
		}
	}

	assert.Equal(t, 1, successCount, "exactly one server should successfully claim the workflow")
}
