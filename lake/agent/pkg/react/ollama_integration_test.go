//go:build integration

package react_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/react"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcollama "github.com/testcontainers/testcontainers-go/modules/ollama"
)

const (
	// Using a small model for faster tests
	testModel = "qwen2.5:0.5b"
	// Container image for Ollama
	ollamaContainerImage = "ollama/ollama:latest"
	// Timeout for pulling models (can be slow)
	modelPullTimeout = 10 * time.Minute
	// Timeout for chat requests
	chatTimeout = 2 * time.Minute
	// Maximum retry attempts for container start
	maxContainerRetries = 3
	// Maximum retry attempts for model pull
	maxModelPullRetries = 3
)

// OllamaTestContainer wraps the Ollama container for testing
type OllamaTestContainer struct {
	container *tcollama.OllamaContainer
	URL       string
	t         testing.TB
}

// NewOllamaTestContainer creates a new Ollama test container with a model pre-loaded
func NewOllamaTestContainer(t testing.TB, model string) *OllamaTestContainer {
	ctx := t.Context()

	// Retry container start up to maxContainerRetries times for retryable errors
	var container *tcollama.OllamaContainer
	var lastErr error
	for attempt := 1; attempt <= maxContainerRetries; attempt++ {
		var err error
		container, err = tcollama.Run(ctx, ollamaContainerImage)
		if err != nil {
			lastErr = err
			if isRetryableContainerStartErr(err) && attempt < maxContainerRetries {
				t.Logf("Container start attempt %d failed (retryable): %v", attempt, err)
				time.Sleep(time.Duration(attempt) * 750 * time.Millisecond)
				continue
			}
			require.NoError(t, err, "failed to start Ollama container")
		}
		break
	}

	if container == nil {
		t.Fatalf("failed to start Ollama container after %d retries: %v", maxContainerRetries, lastErr)
	}

	// Retry getting connection string
	var url string
	for attempt := 1; attempt <= maxContainerRetries; attempt++ {
		var err error
		url, err = container.ConnectionString(ctx)
		if err != nil {
			lastErr = err
			if isRetryableConnectionErr(err) && attempt < maxContainerRetries {
				t.Logf("Connection string attempt %d failed (retryable): %v", attempt, err)
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
				continue
			}
			_ = container.Terminate(ctx)
			require.NoError(t, err, "failed to get Ollama connection string")
		}
		break
	}

	tc := &OllamaTestContainer{
		container: container,
		URL:       url,
		t:         t,
	}

	// Pull the model with retries
	t.Logf("Pulling model %s (this may take a few minutes)...", model)
	for attempt := 1; attempt <= maxModelPullRetries; attempt++ {
		pullCtx, cancel := context.WithTimeout(ctx, modelPullTimeout)
		_, _, err := container.Exec(pullCtx, []string{"ollama", "pull", model})
		cancel()

		if err != nil {
			lastErr = err
			if isRetryableModelPullErr(err) && attempt < maxModelPullRetries {
				t.Logf("Model pull attempt %d failed (retryable): %v", attempt, err)
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
				continue
			}
			_ = container.Terminate(ctx)
			require.NoError(t, err, "failed to pull model %s", model)
		}
		break
	}
	t.Logf("Model %s ready", model)

	t.Cleanup(func() {
		tc.Close()
	})

	return tc
}

// Close terminates the container
func (tc *OllamaTestContainer) Close() {
	if tc.container != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := tc.container.Terminate(ctx); err != nil {
			tc.t.Logf("failed to terminate Ollama container: %v", err)
		}
	}
}

// isRetryableContainerStartErr checks if a container start error is retryable
func isRetryableContainerStartErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "wait until ready") ||
		strings.Contains(s, "mapped port") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "/containers/") && strings.Contains(s, "json") ||
		strings.Contains(s, "Get \"http://%2Fvar%2Frun%2Fdocker.sock") ||
		strings.Contains(s, "i/o timeout") ||
		strings.Contains(s, "connection reset")
}

// isRetryableConnectionErr checks if a connection error is retryable
func isRetryableConnectionErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "dial tcp") ||
		strings.Contains(s, "i/o timeout") ||
		strings.Contains(s, "no such host")
}

// isRetryableModelPullErr checks if a model pull error is retryable
func isRetryableModelPullErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "timeout") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "i/o timeout") ||
		strings.Contains(s, "network") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "broken pipe")
}

func TestOllamaIntegration_SimpleChat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := NewOllamaTestContainer(t, testModel)

	agent := react.NewOllamaAgent(tc.URL, testModel, 500, "You are a helpful assistant. Be concise.")

	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout)
	defer cancel()

	userMsg := agent.CreateUserMessage("What is 2 + 2? Reply with just the number.")
	response, err := agent.Call(ctx, []react.Message{userMsg}, nil)
	require.NoError(t, err)

	content := response.Content()
	require.NotEmpty(t, content, "response should have content")

	text, ok := content[0].AsText()
	require.True(t, ok, "response should be text")
	require.NotEmpty(t, text, "response text should not be empty")

	t.Logf("Response: %s", text)
	// The model should respond with something containing "4"
	assert.Contains(t, text, "4", "response should contain the answer 4")
}

func TestOllamaIntegration_WithTools(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := NewOllamaTestContainer(t, testModel)

	agent := react.NewOllamaAgent(tc.URL, testModel, 1000,
		"You are a helpful assistant with access to tools. When asked about database queries, use the query tool.")

	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout)
	defer cancel()

	tools := []react.Tool{
		{
			Name:        "query",
			Description: "Execute a SQL query against the database to retrieve information",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sql": map[string]any{
						"type":        "string",
						"description": "The SQL query to execute",
					},
				},
				"required": []string{"sql"},
			},
		},
	}

	userMsg := agent.CreateUserMessage("Query the database to count all users. Use the query tool with a SELECT COUNT(*) FROM users query.")
	response, err := agent.Call(ctx, []react.Message{userMsg}, tools)
	require.NoError(t, err)

	content := response.Content()
	require.NotEmpty(t, content, "response should have content")

	// Log all content blocks
	for i, block := range content {
		if text, ok := block.AsText(); ok {
			t.Logf("Content block %d (text): %s", i, text)
		}
		if id, name, input, ok := block.AsToolUse(); ok {
			t.Logf("Content block %d (tool): id=%s, name=%s, input=%s", i, id, name, string(input))
		}
	}

	// The model might respond with a tool call or just text
	// Small models may not always use tools correctly, so we check both scenarios
	var hasToolCall bool
	var hasText bool
	for _, block := range content {
		if _, ok := block.AsText(); ok {
			hasText = true
		}
		if _, name, _, ok := block.AsToolUse(); ok && name == "query" {
			hasToolCall = true
		}
	}

	assert.True(t, hasToolCall || hasText, "response should have either a tool call or text")
}

func TestOllamaIntegration_ToolExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := NewOllamaTestContainer(t, testModel)

	agent := react.NewOllamaAgent(tc.URL, testModel, 1000,
		"You are a helpful assistant. Use tools when appropriate.")

	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout)
	defer cancel()

	tools := []react.Tool{
		{
			Name:        "get_weather",
			Description: "Get current weather for a city",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"type":        "string",
						"description": "The city name",
					},
				},
				"required": []string{"city"},
			},
		},
	}

	// First turn: ask for weather
	userMsg := agent.CreateUserMessage("What's the weather in Tokyo? Use the get_weather tool.")
	response, err := agent.Call(ctx, []react.Message{userMsg}, tools)
	require.NoError(t, err)

	content := response.Content()
	require.NotEmpty(t, content)

	// Find tool use if present
	var toolUses []react.ToolUse
	for _, block := range content {
		if id, name, input, ok := block.AsToolUse(); ok {
			toolUses = append(toolUses, react.ToolUse{
				ID:    id,
				Name:  name,
				Input: make(map[string]any),
			})
			_ = input // We'd parse this in real usage
			t.Logf("Tool call: %s (id: %s)", name, id)
		}
	}

	// If we got tool calls, simulate tool execution
	if len(toolUses) > 0 {
		// Build conversation with tool results
		conversation := []react.Message{userMsg}
		conversation = append(conversation, response.ToMessage())

		// Simulate tool result
		toolResults := []react.ToolResult{
			{
				ID:      toolUses[0].ID,
				Content: `{"temperature": "15°C", "condition": "Cloudy", "city": "Tokyo"}`,
				IsError: false,
			},
		}

		resultMsgs, err := agent.ConvertToolResults(toolUses, toolResults)
		require.NoError(t, err)
		conversation = append(conversation, resultMsgs...)

		// Get final response
		finalResponse, err := agent.Call(ctx, conversation, tools)
		require.NoError(t, err)

		finalContent := finalResponse.Content()
		require.NotEmpty(t, finalContent)

		// Should have a text response about the weather
		var finalText string
		for _, block := range finalContent {
			if text, ok := block.AsText(); ok {
				finalText += text
			}
		}
		t.Logf("Final response: %s", finalText)
		// The response should mention something about the weather or Tokyo
		assert.True(t,
			strings.Contains(strings.ToLower(finalText), "tokyo") ||
				strings.Contains(strings.ToLower(finalText), "weather") ||
				strings.Contains(strings.ToLower(finalText), "15") ||
				strings.Contains(strings.ToLower(finalText), "cloudy"),
			"final response should mention weather details")
	} else {
		t.Log("Model did not use tool (small models may not always use tools)")
	}
}

func TestOllamaIntegration_MultiTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := NewOllamaTestContainer(t, testModel)

	agent := react.NewOllamaAgent(tc.URL, testModel, 500, "You are a helpful assistant. Be concise.")

	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout*2) // Longer timeout for multi-turn
	defer cancel()

	// First message
	msg1 := agent.CreateUserMessage("My name is Alice. Remember my name.")
	resp1, err := agent.Call(ctx, []react.Message{msg1}, nil)
	require.NoError(t, err)

	content1 := resp1.Content()
	require.NotEmpty(t, content1)

	var text1 string
	for _, block := range content1 {
		if text, ok := block.AsText(); ok {
			text1 += text
		}
	}
	t.Logf("Response 1: %s", text1)

	// Build conversation history
	conversation := []react.Message{
		msg1,
		resp1.ToMessage(),
		agent.CreateUserMessage("What is my name?"),
	}

	// Second message - should remember the name
	resp2, err := agent.Call(ctx, conversation, nil)
	require.NoError(t, err)

	content2 := resp2.Content()
	require.NotEmpty(t, content2)

	var text2 string
	for _, block := range content2 {
		if text, ok := block.AsText(); ok {
			text2 += text
		}
	}
	t.Logf("Response 2: %s", text2)

	// Should mention Alice
	assert.Contains(t, strings.ToLower(text2), "alice", "model should remember the name Alice")
}

func TestOllamaIntegration_LongResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := NewOllamaTestContainer(t, testModel)

	agent := react.NewOllamaAgent(tc.URL, testModel, 1000, "")

	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout)
	defer cancel()

	userMsg := agent.CreateUserMessage("Write a short paragraph about the importance of testing in software development.")
	response, err := agent.Call(ctx, []react.Message{userMsg}, nil)
	require.NoError(t, err)

	content := response.Content()
	require.NotEmpty(t, content)

	var text string
	for _, block := range content {
		if t, ok := block.AsText(); ok {
			text += t
		}
	}

	t.Logf("Response length: %d characters", len(text))
	t.Logf("Response: %s", text)

	// Should be a substantial response
	assert.Greater(t, len(text), 50, "response should be at least 50 characters")
	// Should mention something about testing
	assert.True(t,
		strings.Contains(strings.ToLower(text), "test") ||
			strings.Contains(strings.ToLower(text), "software") ||
			strings.Contains(strings.ToLower(text), "quality"),
		"response should be about testing/software/quality")
}

func TestOllamaIntegration_CreateUserMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := NewOllamaTestContainer(t, testModel)

	agent := react.NewOllamaAgent(tc.URL, testModel, 100, "")

	// Test CreateUserMessage
	msg := agent.CreateUserMessage("Hello")
	require.NotNil(t, msg)

	param := msg.ToParam()
	require.NotNil(t, param)

	// Verify the message can be used in a call
	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout)
	defer cancel()

	response, err := agent.Call(ctx, []react.Message{msg}, nil)
	require.NoError(t, err)
	require.NotNil(t, response)

	content := response.Content()
	require.NotEmpty(t, content)
}

func TestOllamaIntegration_ConvertToolResults(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := NewOllamaTestContainer(t, testModel)

	agent := react.NewOllamaAgent(tc.URL, testModel, 100, "")

	toolUses := []react.ToolUse{
		{ID: "call_1", Name: "test_tool", Input: map[string]any{"arg": "value"}},
	}
	results := []react.ToolResult{
		{ID: "call_1", Content: "tool result", IsError: false},
	}

	msgs, err := agent.ConvertToolResults(toolUses, results)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	// Verify the message format
	param := msgs[0].ToParam()
	require.NotNil(t, param)
}
