package slack

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAI_Slack_APIClient_apiError(t *testing.T) {
	t.Parallel()

	err := &apiError{
		statusCode: 503,
		message:    "Service Unavailable",
	}

	assert.Equal(t, "API error: Service Unavailable (status 503)", err.Error())
	assert.Equal(t, 503, err.StatusCode())
}

func TestAI_Slack_APIClient_NewAPIClient(t *testing.T) {
	t.Parallel()

	client := NewAPIClient("http://localhost:8080", slog.Default())

	require.NotNil(t, client)
	assert.Equal(t, "http://localhost:8080", client.baseURL)
	assert.NotNil(t, client.httpClient)
	assert.NotNil(t, client.httpClient.Transport)
}

func TestAI_Slack_APIClient_NewAPIClient_TrimsTrailingSlash(t *testing.T) {
	t.Parallel()

	client := NewAPIClient("http://localhost:8080/", slog.Default())

	assert.Equal(t, "http://localhost:8080", client.baseURL)
}

func TestAI_Slack_APIClient_ChatStream_RetriesOnConnectionFailure(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt < 3 {
			// Simulate connection being closed (server restart scenario)
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
		}
		// Third attempt succeeds
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: done\n")
		fmt.Fprint(w, `data: {"answer":"test response","dataQuestions":[],"executedQueries":[]}`+"\n\n")
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, slog.Default())
	ctx := context.Background()

	result, err := client.ChatStream(ctx, "test message", nil, "test-session", func(p workflow.Progress) {})

	require.NoError(t, err)
	assert.Equal(t, "test response", result.Answer)
	assert.Equal(t, int32(3), attempts.Load(), "expected 3 attempts")
}

func TestAI_Slack_APIClient_ChatStream_RetriesOn5xxErrors(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "Service Unavailable")
			return
		}
		// Third attempt succeeds
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: done\n")
		fmt.Fprint(w, `data: {"answer":"recovered","dataQuestions":[],"executedQueries":[]}`+"\n\n")
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, slog.Default())
	ctx := context.Background()

	result, err := client.ChatStream(ctx, "test", nil, "", func(p workflow.Progress) {})

	require.NoError(t, err)
	assert.Equal(t, "recovered", result.Answer)
	assert.Equal(t, int32(3), attempts.Load(), "expected 3 attempts")
}

func TestAI_Slack_APIClient_ChatStream_NoRetryOn4xxErrors(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Bad Request")
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, slog.Default())
	ctx := context.Background()

	_, err := client.ChatStream(ctx, "test", nil, "", func(p workflow.Progress) {})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 400")
	assert.Equal(t, int32(1), attempts.Load(), "expected only 1 attempt (no retry on 4xx)")
}

func TestAI_Slack_APIClient_ChatStream_FailsAfterMaxRetries(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Internal Server Error")
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, slog.Default())
	ctx := context.Background()

	_, err := client.ChatStream(ctx, "test", nil, "", func(p workflow.Progress) {})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
	assert.Equal(t, int32(3), attempts.Load(), "expected 3 attempts before failing")
}

func TestAI_Slack_APIClient_ChatStream_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		// Slow response to allow context cancellation
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.ChatStream(ctx, "test", nil, "", func(p workflow.Progress) {})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}

func TestAI_Slack_APIClient_ChatStream_SuccessfulStreaming(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send workflow started
		fmt.Fprint(w, "event: workflow_started\n")
		fmt.Fprint(w, "data: {}\n\n")

		// Send thinking
		fmt.Fprint(w, "event: thinking\n")
		fmt.Fprint(w, "data: {}\n\n")

		// Send query_started
		fmt.Fprint(w, "event: query_started\n")
		fmt.Fprint(w, `data: {"question":"test question","sql":"SELECT 1"}`+"\n\n")

		// Send query_done
		fmt.Fprint(w, "event: query_done\n")
		fmt.Fprint(w, `data: {"question":"test question","sql":"SELECT 1","rows":1}`+"\n\n")

		// Send done
		fmt.Fprint(w, "event: done\n")
		fmt.Fprint(w, `data: {"answer":"Final answer","dataQuestions":[{"question":"test question","rationale":"testing"}],"executedQueries":[{"question":"test question","sql":"SELECT 1","columns":["col1"],"rows":[[1]],"count":1}]}`+"\n\n")
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, slog.Default())
	ctx := context.Background()

	var progressStages []workflow.ProgressStage
	result, err := client.ChatStream(ctx, "test", nil, "session-123", func(p workflow.Progress) {
		progressStages = append(progressStages, p.Stage)
	})

	require.NoError(t, err)
	assert.Equal(t, "Final answer", result.Answer)
	assert.Equal(t, "session-123", result.SessionID)
	assert.Len(t, result.DataQuestions, 1)
	assert.Equal(t, "test question", result.DataQuestions[0].Question)
	assert.Len(t, result.ExecutedQueries, 1)
	assert.Equal(t, "SELECT 1", result.ExecutedQueries[0].GeneratedQuery.SQL)

	// Verify progress stages were reported
	assert.Contains(t, progressStages, workflow.StageClassifying)
	assert.Contains(t, progressStages, workflow.StageExecuting)
	assert.Contains(t, progressStages, workflow.StageComplete)
}

func TestAI_Slack_APIClient_ChatStream_HandlesAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send an error event
		fmt.Fprint(w, "event: error\n")
		fmt.Fprint(w, `data: {"error":"Something went wrong"}`+"\n\n")
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, slog.Default())
	ctx := context.Background()

	_, err := client.ChatStream(ctx, "test", nil, "", func(p workflow.Progress) {})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Something went wrong")
}

func TestAI_Slack_APIClient_ChatStream_GeneratesSessionID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: done\n")
		fmt.Fprint(w, `data: {"answer":"test","dataQuestions":[],"executedQueries":[]}`+"\n\n")
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, slog.Default())
	ctx := context.Background()

	result, err := client.ChatStream(ctx, "test", nil, "", func(p workflow.Progress) {})

	require.NoError(t, err)
	assert.NotEmpty(t, result.SessionID, "expected session ID to be generated")
}

func TestAI_Slack_APIClient_ChatStream_WithConversationHistory(t *testing.T) {
	t.Parallel()

	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = make([]byte, r.ContentLength)
		r.Body.Read(receivedBody)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: done\n")
		fmt.Fprint(w, `data: {"answer":"test","dataQuestions":[],"executedQueries":[]}`+"\n\n")
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, slog.Default())
	ctx := context.Background()

	history := []workflow.ConversationMessage{
		{Role: "user", Content: "previous question"},
		{Role: "assistant", Content: "previous answer", ExecutedQueries: []string{"SELECT 1"}},
	}

	_, err := client.ChatStream(ctx, "new question", history, "session-id", func(p workflow.Progress) {})

	require.NoError(t, err)
	assert.Contains(t, string(receivedBody), "previous question")
	assert.Contains(t, string(receivedBody), "previous answer")
	assert.Contains(t, string(receivedBody), "new question")
}
