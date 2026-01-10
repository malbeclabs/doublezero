package react

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOllamaAgent(t *testing.T) {
	agent := NewOllamaAgent("http://localhost:11434", "llama3", 4000, "You are a helpful assistant.")
	require.NotNil(t, agent)

	ollamaAgent, ok := agent.(*OllamaAgent)
	require.True(t, ok)
	assert.Equal(t, "http://localhost:11434", ollamaAgent.baseURL)
	assert.Equal(t, "llama3", ollamaAgent.model)
	assert.Equal(t, int64(4000), ollamaAgent.maxOutputTokens)
	assert.Equal(t, "You are a helpful assistant.", ollamaAgent.system)
}

func TestNewOllamaAgentWithHTTPClient(t *testing.T) {
	customClient := &http.Client{Timeout: 30}
	agent := NewOllamaAgentWithHTTPClient("http://localhost:11434", customClient, "llama3", 4000, "test")
	require.NotNil(t, agent)

	ollamaAgent, ok := agent.(*OllamaAgent)
	require.True(t, ok)
	assert.Equal(t, customClient, ollamaAgent.httpClient)
}

func TestOllamaAgent_CreateUserMessage(t *testing.T) {
	agent := NewOllamaAgent("http://localhost:11434", "llama3", 4000, "")
	msg := agent.CreateUserMessage("Hello, world!")

	ollamaMsg, ok := msg.(OllamaMessage)
	require.True(t, ok)
	assert.Equal(t, "user", ollamaMsg.Msg.Role)
	assert.Equal(t, "Hello, world!", ollamaMsg.Msg.Content)
}

func TestOllamaAgent_ConvertToMessage(t *testing.T) {
	agent := NewOllamaAgent("http://localhost:11434", "llama3", 4000, "")

	tests := []struct {
		name     string
		input    any
		wantRole string
		wantText string
	}{
		{
			name: "ollamaMessage",
			input: ollamaMessage{
				Role:    "assistant",
				Content: "test response",
			},
			wantRole: "assistant",
			wantText: "test response",
		},
		{
			name: "map",
			input: map[string]any{
				"role":    "user",
				"content": "test query",
			},
			wantRole: "user",
			wantText: "test query",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := agent.ConvertToMessage(tt.input)
			ollamaMsg, ok := msg.(OllamaMessage)
			require.True(t, ok)
			assert.Equal(t, tt.wantRole, ollamaMsg.Msg.Role)
			assert.Equal(t, tt.wantText, ollamaMsg.Msg.Content)
		})
	}
}

func TestOllamaAgent_ConvertToolResults(t *testing.T) {
	agent := NewOllamaAgent("http://localhost:11434", "llama3", 4000, "")

	toolUses := []ToolUse{
		{ID: "1", Name: "test_tool", Input: map[string]any{"arg": "value"}},
	}
	results := []ToolResult{
		{ID: "1", Content: "tool result content", IsError: false},
	}

	msgs, err := agent.ConvertToolResults(toolUses, results)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	ollamaMsg, ok := msgs[0].(OllamaMessage)
	require.True(t, ok)
	assert.Equal(t, "tool", ollamaMsg.Msg.Role)
	assert.Equal(t, "tool result content", ollamaMsg.Msg.Content)
}

func TestOllamaAgent_Call_SimpleResponse(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/chat", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		// Parse request
		var req ollamaChatRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		assert.Equal(t, "llama3", req.Model)
		// System message should be first
		require.GreaterOrEqual(t, len(req.Messages), 1)
		assert.Equal(t, "system", req.Messages[0].Role)

		// Return a simple response
		resp := ollamaChatResponse{
			Model: "llama3",
			Message: ollamaMessage{
				Role:    "assistant",
				Content: "Hello! How can I help you?",
			},
			Done: true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	agent := NewOllamaAgent(server.URL, "llama3", 4000, "You are a helpful assistant.")
	ctx := context.Background()

	userMsg := agent.CreateUserMessage("Hi!")
	response, err := agent.Call(ctx, []Message{userMsg}, nil)
	require.NoError(t, err)
	require.NotNil(t, response)

	content := response.Content()
	require.Len(t, content, 1)

	text, ok := content[0].AsText()
	assert.True(t, ok)
	assert.Equal(t, "Hello! How can I help you?", text)
}

func TestOllamaAgent_Call_WithToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaChatResponse{
			Model: "llama3",
			Message: ollamaMessage{
				Role:    "assistant",
				Content: "I'll query the database for you.",
				ToolCalls: []ollamaToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: ollamaToolCallFnPart{
							Name:      "query",
							Arguments: ollamaJSONArgs(`{"sql": "SELECT * FROM users"}`),
						},
					},
				},
			},
			Done: true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	agent := NewOllamaAgent(server.URL, "llama3", 4000, "")
	ctx := context.Background()

	userMsg := agent.CreateUserMessage("Query users")
	response, err := agent.Call(ctx, []Message{userMsg}, []Tool{
		{
			Name:        "query",
			Description: "Execute SQL",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sql": map[string]any{"type": "string"},
				},
			},
		},
	})
	require.NoError(t, err)

	content := response.Content()
	require.Len(t, content, 2) // text + tool call

	// Check text
	text, ok := content[0].AsText()
	assert.True(t, ok)
	assert.Equal(t, "I'll query the database for you.", text)

	// Check tool call
	id, name, input, ok := content[1].AsToolUse()
	assert.True(t, ok)
	assert.Equal(t, "call_1", id)
	assert.Equal(t, "query", name)

	var args map[string]any
	err = json.Unmarshal(input, &args)
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM users", args["sql"])
}

func TestOllamaAgent_Call_StreamingResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate streaming response (newline-delimited JSON)
		w.Header().Set("Content-Type", "application/x-ndjson")

		chunks := []ollamaChatResponse{
			{Message: ollamaMessage{Role: "assistant", Content: "Hello"}, Done: false},
			{Message: ollamaMessage{Content: " "}, Done: false},
			{Message: ollamaMessage{Content: "world!"}, Done: false},
			{Message: ollamaMessage{}, Done: true},
		}

		for _, chunk := range chunks {
			json.NewEncoder(w).Encode(chunk)
		}
	}))
	defer server.Close()

	agent := NewOllamaAgent(server.URL, "llama3", 4000, "")
	ctx := context.Background()

	userMsg := agent.CreateUserMessage("Say hello")
	response, err := agent.Call(ctx, []Message{userMsg}, nil)
	require.NoError(t, err)

	content := response.Content()
	require.Len(t, content, 1)

	text, ok := content[0].AsText()
	assert.True(t, ok)
	assert.Equal(t, "Hello world!", text)
}

func TestOllamaAgent_Call_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	agent := NewOllamaAgent(server.URL, "llama3", 4000, "")
	ctx := context.Background()

	userMsg := agent.CreateUserMessage("test")
	_, err := agent.Call(ctx, []Message{userMsg}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestOllamaAgent_Call_OllamaError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaChatResponse{
			Error: "model not found",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	agent := NewOllamaAgent(server.URL, "llama3", 4000, "")
	ctx := context.Background()

	userMsg := agent.CreateUserMessage("test")
	_, err := agent.Call(ctx, []Message{userMsg}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model not found")
}

func TestOllamaJSONArgs_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "null",
			input:    `null`,
			expected: `{}`,
		},
		{
			name:     "object",
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "quoted string JSON",
			input:    `"{\"key\": \"value\"}"`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "double quoted string JSON",
			input:    `"\"{\\\"key\\\": \\\"value\\\"}\""`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "empty string",
			input:    `""`,
			expected: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args ollamaJSONArgs
			err := json.Unmarshal([]byte(tt.input), &args)
			require.NoError(t, err)

			// Normalize for comparison
			var expected, actual any
			json.Unmarshal([]byte(tt.expected), &expected)
			json.Unmarshal(args.Raw(), &actual)
			assert.Equal(t, expected, actual)
		})
	}
}

func TestOllamaMessage_ToParam(t *testing.T) {
	msg := OllamaMessage{Msg: ollamaMessage{
		Role:    "user",
		Content: "test",
	}}

	param := msg.ToParam()
	ollamaMsg, ok := param.(ollamaMessage)
	require.True(t, ok)
	assert.Equal(t, "user", ollamaMsg.Role)
	assert.Equal(t, "test", ollamaMsg.Content)
}

func TestOllamaResponse_ToMessage(t *testing.T) {
	resp := ollamaResponse{msg: ollamaMessage{
		Role:    "assistant",
		Content: "response text",
	}}

	msg := resp.ToMessage()
	ollamaMsg, ok := msg.(OllamaMessage)
	require.True(t, ok)
	assert.Equal(t, "assistant", ollamaMsg.Msg.Role)
	assert.Equal(t, "response text", ollamaMsg.Msg.Content)
}

func TestOllamaContentBlock_AsText(t *testing.T) {
	block := ollamaContentBlock{
		blockType: "text",
		text:      "hello",
	}

	text, ok := block.AsText()
	assert.True(t, ok)
	assert.Equal(t, "hello", text)

	// Non-text block
	toolBlock := ollamaContentBlock{blockType: "tool_use"}
	_, ok = toolBlock.AsText()
	assert.False(t, ok)
}

func TestOllamaContentBlock_AsToolUse(t *testing.T) {
	block := ollamaContentBlock{
		blockType: "tool_use",
		toolCall: ollamaToolCall{
			ID: "call_1",
			Function: ollamaToolCallFnPart{
				Name:      "test_tool",
				Arguments: ollamaJSONArgs(`{"arg": "value"}`),
			},
		},
	}

	id, name, input, ok := block.AsToolUse()
	assert.True(t, ok)
	assert.Equal(t, "call_1", id)
	assert.Equal(t, "test_tool", name)

	var args map[string]any
	err := json.Unmarshal(input, &args)
	require.NoError(t, err)
	assert.Equal(t, "value", args["arg"])
}

func TestOllamaContentBlock_AsToolUse_NoID(t *testing.T) {
	// Ollama may not provide an ID, test that we generate one
	block := ollamaContentBlock{
		blockType: "tool_use",
		toolCall: ollamaToolCall{
			Function: ollamaToolCallFnPart{
				Name:      "my_tool",
				Arguments: ollamaJSONArgs(`{}`),
			},
		},
	}

	id, name, _, ok := block.AsToolUse()
	assert.True(t, ok)
	assert.Equal(t, "tool_my_tool", id) // Generated ID
	assert.Equal(t, "my_tool", name)
}

func TestToOllamaTools(t *testing.T) {
	tools := []Tool{
		{
			Name:        "query",
			Description: "Execute a SQL query",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sql": map[string]any{
						"type":        "string",
						"description": "The SQL query",
					},
				},
				"required": []string{"sql"},
			},
		},
	}

	ollamaTools := toOllamaTools(tools)
	require.Len(t, ollamaTools, 1)

	assert.Equal(t, "function", ollamaTools[0].Type)
	assert.Equal(t, "query", ollamaTools[0].Function.Name)
	assert.Equal(t, "Execute a SQL query", ollamaTools[0].Function.Description)

	var params map[string]any
	err := json.Unmarshal(ollamaTools[0].Function.Parameters, &params)
	require.NoError(t, err)
	assert.Equal(t, "object", params["type"])
}

func TestNewOllamaUserMessage(t *testing.T) {
	msg := NewOllamaUserMessage("test content")
	assert.Equal(t, "user", msg.Msg.Role)
	assert.Equal(t, "test content", msg.Msg.Content)
}

func TestNewOllamaAssistantMessage(t *testing.T) {
	msg := NewOllamaAssistantMessage("assistant response")
	assert.Equal(t, "assistant", msg.Msg.Role)
	assert.Equal(t, "assistant response", msg.Msg.Content)
}

func TestNewOllamaToolResultMessage(t *testing.T) {
	msg := NewOllamaToolResultMessage("my_tool", "result content")
	assert.Equal(t, "tool", msg.Msg.Role)
	assert.Equal(t, "my_tool", msg.Msg.Name)
	assert.Equal(t, "result content", msg.Msg.Content)
}

func TestGenericMessage(t *testing.T) {
	msg := NewUserMessage("test")
	genericMsg, ok := msg.(GenericMessage)
	require.True(t, ok)
	assert.Equal(t, "user", genericMsg.Role)
	assert.Equal(t, "test", genericMsg.Content)

	param := msg.ToParam()
	m, ok := param.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "user", m["role"])
	assert.Equal(t, "test", m["content"])
}

func TestNewAssistantMessage(t *testing.T) {
	msg := NewAssistantMessage("response")
	genericMsg, ok := msg.(GenericMessage)
	require.True(t, ok)
	assert.Equal(t, "assistant", genericMsg.Role)
	assert.Equal(t, "response", genericMsg.Content)
}

