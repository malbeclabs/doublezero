package react

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLMClient is a mock LLM client for testing.
type mockLLMClient struct {
	responses []mockResponse
	callIndex int
}

type mockResponse struct {
	text      string
	toolCalls []mockToolCall
}

type mockToolCall struct {
	id    string
	name  string
	input map[string]any
}

func (m *mockLLMClient) Call(ctx context.Context, messages []Message, tools []Tool) (Response, error) {
	if m.callIndex >= len(m.responses) {
		// Return empty response if we've exhausted responses
		return &mockLLMResponse{}, nil
	}
	resp := m.responses[m.callIndex]
	m.callIndex++
	return &mockLLMResponse{text: resp.text, toolCalls: resp.toolCalls}, nil
}

func (m *mockLLMClient) ConvertToMessage(msg any) Message {
	return GenericMessage{Role: "user", Content: ""}
}

func (m *mockLLMClient) ConvertToolResults(toolUses []ToolUse, results []ToolResult) ([]Message, error) {
	var msgs []Message
	for i, tu := range toolUses {
		content := results[i].Content
		msgs = append(msgs, GenericMessage{Role: "tool", Content: "Tool " + tu.Name + ": " + content})
	}
	return msgs, nil
}

func (m *mockLLMClient) CreateUserMessage(content string) Message {
	return GenericMessage{Role: "user", Content: content}
}

// mockLLMResponse is a mock LLM response.
type mockLLMResponse struct {
	text      string
	toolCalls []mockToolCall
}

func (r *mockLLMResponse) Content() []ContentBlock {
	var blocks []ContentBlock
	for _, tc := range r.toolCalls {
		blocks = append(blocks, &mockToolUseBlock{id: tc.id, name: tc.name, input: tc.input})
	}
	if r.text != "" {
		blocks = append(blocks, &mockTextBlock{text: r.text})
	}
	return blocks
}

func (r *mockLLMResponse) ToMessage() Message {
	return GenericMessage{Role: "assistant", Content: r.text}
}

// mockTextBlock is a mock text content block.
type mockTextBlock struct {
	text string
}

func (b *mockTextBlock) AsText() (string, bool) {
	return b.text, true
}

func (b *mockTextBlock) AsToolUse() (string, string, []byte, bool) {
	return "", "", nil, false
}

// mockToolUseBlock is a mock tool use content block.
type mockToolUseBlock struct {
	id    string
	name  string
	input map[string]any
}

func (b *mockToolUseBlock) AsText() (string, bool) {
	return "", false
}

func (b *mockToolUseBlock) AsToolUse() (string, string, []byte, bool) {
	inputBytes, _ := json.Marshal(b.input)
	return b.id, b.name, inputBytes, true
}

// mockToolClient is a mock tool client for testing.
type mockToolClient struct {
	tools   []Tool
	results map[string]mockToolResult
}

type mockToolResult struct {
	content string
	isError bool
}

func (m *mockToolClient) ListTools(ctx context.Context) ([]Tool, error) {
	return m.tools, nil
}

func (m *mockToolClient) CallToolText(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	if result, ok := m.results[name]; ok {
		return result.content, result.isError, nil
	}
	return "no result", false, nil
}

func TestAgent_Run_NoRetryWhenToolContentContainsErrorWord(t *testing.T) {
	// This test verifies that the retry logic does NOT trigger when a tool result
	// contains the word "error" in its content but IsError is false.
	// This was a bug where interface stats like "8 input errors" would trigger retry.

	llm := &mockLLMClient{
		responses: []mockResponse{
			// Round 1: Model calls query tool
			{
				text:      "I'll check the interface stats.",
				toolCalls: []mockToolCall{{id: "1", name: "query", input: map[string]any{"sql": "SELECT * FROM interfaces"}}},
			},
			// Round 2: Model provides final response (no tool calls)
			{
				text: "The interface has 8 input errors and 4 output errors.",
			},
		},
	}

	toolClient := &mockToolClient{
		tools: []Tool{{Name: "query", Description: "Execute SQL", InputSchema: map[string]any{}}},
		results: map[string]mockToolResult{
			// Tool returns content with "error" in it, but IsError is false
			"query": {content: "interface_name,input_errors,output_errors\neth0,8,4", isError: false},
		},
	}

	agent, err := NewAgent(&Config{
		LLM:                llm,
		ToolClient:         toolClient,
		MaxRounds:          5,
		MaxContextTokens:   50000,
		FinalizationPrompt: "Please provide a final answer.",
	})
	require.NoError(t, err)

	result, err := agent.Run(context.Background(), []Message{GenericMessage{Role: "user", Content: "Show interface errors"}}, nil)
	require.NoError(t, err)

	// Should get the final response without retry injection
	assert.Contains(t, result.FinalText, "8 input errors")

	// Verify only 2 LLM calls were made (no retry)
	assert.Equal(t, 2, llm.callIndex, "Expected 2 LLM calls, but got %d - retry may have been incorrectly triggered", llm.callIndex)
}

func TestAgent_Run_RetryWhenToolReturnsExplicitError(t *testing.T) {
	// This test verifies that the retry logic DOES trigger when IsError is true.

	llm := &mockLLMClient{
		responses: []mockResponse{
			// Round 1: Model calls query tool
			{
				text:      "I'll query the data.",
				toolCalls: []mockToolCall{{id: "1", name: "query", input: map[string]any{"sql": "SELECT * FROM nonexistent"}}},
			},
			// Round 2: Model gives up without retrying (no tool calls)
			{
				text: "I couldn't get the data.",
			},
			// Round 3: After retry prompt, model tries again
			{
				text:      "Let me try a different query.",
				toolCalls: []mockToolCall{{id: "2", name: "query", input: map[string]any{"sql": "SELECT * FROM users"}}},
			},
			// Round 4: Success, final response
			{
				text: "Here are the results from the users table.",
			},
		},
	}

	callCount := 0
	toolClient := &mockToolClientWithDynamicResults{
		tools: []Tool{{Name: "query", Description: "Execute SQL", InputSchema: map[string]any{}}},
		callFunc: func(ctx context.Context, name string, args map[string]any) (string, bool, error) {
			callCount++
			if callCount == 1 {
				return "table 'nonexistent' does not exist", true, nil // IsError: true
			}
			return "id,name\n1,Alice\n2,Bob", false, nil
		},
	}

	agent, err := NewAgent(&Config{
		LLM:                llm,
		ToolClient:         toolClient,
		MaxRounds:          10,
		MaxContextTokens:   50000,
		FinalizationPrompt: "Please provide a final answer.",
	})
	require.NoError(t, err)

	result, err := agent.Run(context.Background(), []Message{GenericMessage{Role: "user", Content: "Get user data"}}, nil)
	require.NoError(t, err)

	// Should get the successful final response after retry
	assert.Contains(t, result.FinalText, "results from the users table")

	// Verify retry occurred: 4 LLM calls (initial, give up, retry after prompt, final)
	assert.Equal(t, 4, llm.callIndex, "Expected 4 LLM calls with retry")
}

func TestAgent_Run_NoRetryOnSuccessfulCompletion(t *testing.T) {
	// This test verifies normal completion without any retry.

	llm := &mockLLMClient{
		responses: []mockResponse{
			// Round 1: Model calls tool
			{
				text:      "Checking data.",
				toolCalls: []mockToolCall{{id: "1", name: "query", input: map[string]any{}}},
			},
			// Round 2: Model provides final response
			{
				text: "The data shows 100 records.",
			},
		},
	}

	toolClient := &mockToolClient{
		tools:   []Tool{{Name: "query", Description: "Execute SQL", InputSchema: map[string]any{}}},
		results: map[string]mockToolResult{"query": {content: "count\n100", isError: false}},
	}

	agent, err := NewAgent(&Config{
		LLM:                llm,
		ToolClient:         toolClient,
		MaxRounds:          5,
		MaxContextTokens:   50000,
		FinalizationPrompt: "Please provide a final answer.",
	})
	require.NoError(t, err)

	result, err := agent.Run(context.Background(), []Message{GenericMessage{Role: "user", Content: "Count records"}}, nil)
	require.NoError(t, err)

	assert.Contains(t, result.FinalText, "100 records")
	assert.Equal(t, 2, llm.callIndex, "Expected exactly 2 LLM calls for normal completion")
}

// mockToolClientWithDynamicResults allows per-call control of results.
type mockToolClientWithDynamicResults struct {
	tools    []Tool
	callFunc func(ctx context.Context, name string, args map[string]any) (string, bool, error)
}

func (m *mockToolClientWithDynamicResults) ListTools(ctx context.Context) ([]Tool, error) {
	return m.tools, nil
}

func (m *mockToolClientWithDynamicResults) CallToolText(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	return m.callFunc(ctx, name, args)
}
