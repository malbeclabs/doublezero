package tools

import (
	"context"
	"testing"

	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockToolClient implements react.ToolClient for testing.
type MockToolClient struct {
	tools      []react.Tool
	listErr    error
	callFunc   func(ctx context.Context, name string, args map[string]any) (string, bool, error)
	callCounts map[string]int
}

func NewMockToolClient(tools []react.Tool) *MockToolClient {
	return &MockToolClient{
		tools:      tools,
		callCounts: make(map[string]int),
	}
}

func (m *MockToolClient) ListTools(_ context.Context) ([]react.Tool, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.tools, nil
}

func (m *MockToolClient) CallToolText(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	m.callCounts[name]++
	if m.callFunc != nil {
		return m.callFunc(ctx, name, args)
	}
	return "mock result for " + name, false, nil
}

func TestNewMultiToolClient_CombinesTwoClients(t *testing.T) {
	client1 := NewMockToolClient([]react.Tool{
		{Name: "tool_a", Description: "Tool A"},
		{Name: "tool_b", Description: "Tool B"},
	})
	client2 := NewMockToolClient([]react.Tool{
		{Name: "tool_c", Description: "Tool C"},
	})

	multi, err := NewMultiToolClient(client1, client2)
	require.NoError(t, err)
	require.NotNil(t, multi)

	tools, err := multi.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 3)

	toolNames := make([]string, len(tools))
	for i, tool := range tools {
		toolNames[i] = tool.Name
	}
	assert.Contains(t, toolNames, "tool_a")
	assert.Contains(t, toolNames, "tool_b")
	assert.Contains(t, toolNames, "tool_c")
}

func TestNewMultiToolClient_DetectsDuplicateToolNames(t *testing.T) {
	client1 := NewMockToolClient([]react.Tool{
		{Name: "shared_tool", Description: "Tool from client 1"},
	})
	client2 := NewMockToolClient([]react.Tool{
		{Name: "shared_tool", Description: "Tool from client 2"},
	})

	multi, err := NewMultiToolClient(client1, client2)
	require.Error(t, err)
	assert.Nil(t, multi)
	assert.Contains(t, err.Error(), "duplicate tool")
	assert.Contains(t, err.Error(), "shared_tool")
}

func TestMultiToolClient_RoutesToCorrectClient(t *testing.T) {
	client1 := NewMockToolClient([]react.Tool{
		{Name: "query", Description: "Query tool"},
	})
	client1.callFunc = func(_ context.Context, name string, _ map[string]any) (string, bool, error) {
		return "result from client1: " + name, false, nil
	}

	client2 := NewMockToolClient([]react.Tool{
		{Name: "memory_save", Description: "Memory save tool"},
	})
	client2.callFunc = func(_ context.Context, name string, _ map[string]any) (string, bool, error) {
		return "result from client2: " + name, false, nil
	}

	multi, err := NewMultiToolClient(client1, client2)
	require.NoError(t, err)

	// Call tool from client1
	result, isError, err := multi.CallToolText(context.Background(), "query", map[string]any{"sql": "SELECT 1"})
	require.NoError(t, err)
	assert.False(t, isError)
	assert.Equal(t, "result from client1: query", result)
	assert.Equal(t, 1, client1.callCounts["query"])
	assert.Equal(t, 0, client2.callCounts["query"])

	// Call tool from client2
	result, isError, err = multi.CallToolText(context.Background(), "memory_save", map[string]any{"content": "test"})
	require.NoError(t, err)
	assert.False(t, isError)
	assert.Equal(t, "result from client2: memory_save", result)
	assert.Equal(t, 0, client1.callCounts["memory_save"])
	assert.Equal(t, 1, client2.callCounts["memory_save"])
}

func TestMultiToolClient_UnknownToolReturnsError(t *testing.T) {
	client1 := NewMockToolClient([]react.Tool{
		{Name: "known_tool", Description: "A known tool"},
	})

	multi, err := NewMultiToolClient(client1)
	require.NoError(t, err)

	_, _, err = multi.CallToolText(context.Background(), "unknown_tool", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
	assert.Contains(t, err.Error(), "unknown_tool")
}

func TestNewMultiToolClient_EmptyClients(t *testing.T) {
	multi, err := NewMultiToolClient()
	require.NoError(t, err)
	require.NotNil(t, multi)

	tools, err := multi.ListTools(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tools)
}

func TestNewMultiToolClient_ClientWithNoTools(t *testing.T) {
	client1 := NewMockToolClient([]react.Tool{})
	client2 := NewMockToolClient([]react.Tool{
		{Name: "tool_a", Description: "Tool A"},
	})

	multi, err := NewMultiToolClient(client1, client2)
	require.NoError(t, err)

	tools, err := multi.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)
	assert.Equal(t, "tool_a", tools[0].Name)
}

func TestNewMultiToolClient_ListToolsError(t *testing.T) {
	client1 := NewMockToolClient([]react.Tool{})
	client1.listErr = assert.AnError

	multi, err := NewMultiToolClient(client1)
	require.Error(t, err)
	assert.Nil(t, multi)
	assert.Contains(t, err.Error(), "failed to list tools")
}

func TestMultiToolClient_PreservesToolMetadata(t *testing.T) {
	client1 := NewMockToolClient([]react.Tool{
		{
			Name:        "complex_tool",
			Description: "A tool with complex schema",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"param1": map[string]any{"type": "string"},
				},
				"required": []string{"param1"},
			},
		},
	})

	multi, err := NewMultiToolClient(client1)
	require.NoError(t, err)

	tools, err := multi.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1)

	tool := tools[0]
	assert.Equal(t, "complex_tool", tool.Name)
	assert.Equal(t, "A tool with complex schema", tool.Description)
	assert.NotNil(t, tool.InputSchema)
	assert.Equal(t, "object", tool.InputSchema["type"])
}

func TestMultiToolClient_PassesArgsToClient(t *testing.T) {
	var capturedArgs map[string]any

	client1 := NewMockToolClient([]react.Tool{
		{Name: "arg_tool", Description: "Tool that captures args"},
	})
	client1.callFunc = func(_ context.Context, _ string, args map[string]any) (string, bool, error) {
		capturedArgs = args
		return "ok", false, nil
	}

	multi, err := NewMultiToolClient(client1)
	require.NoError(t, err)

	expectedArgs := map[string]any{
		"key1": "value1",
		"key2": float64(42),
		"key3": true,
	}

	_, _, err = multi.CallToolText(context.Background(), "arg_tool", expectedArgs)
	require.NoError(t, err)
	assert.Equal(t, expectedArgs, capturedArgs)
}

func TestMultiToolClient_PropagatesIsError(t *testing.T) {
	client1 := NewMockToolClient([]react.Tool{
		{Name: "error_tool", Description: "Tool that returns isError"},
	})
	client1.callFunc = func(_ context.Context, _ string, _ map[string]any) (string, bool, error) {
		return "validation failed", true, nil
	}

	multi, err := NewMultiToolClient(client1)
	require.NoError(t, err)

	result, isError, err := multi.CallToolText(context.Background(), "error_tool", map[string]any{})
	require.NoError(t, err)
	assert.True(t, isError)
	assert.Equal(t, "validation failed", result)
}

func TestMultiToolClient_PropagatesError(t *testing.T) {
	client1 := NewMockToolClient([]react.Tool{
		{Name: "failing_tool", Description: "Tool that returns error"},
	})
	client1.callFunc = func(_ context.Context, _ string, _ map[string]any) (string, bool, error) {
		return "", false, assert.AnError
	}

	multi, err := NewMultiToolClient(client1)
	require.NoError(t, err)

	_, _, err = multi.CallToolText(context.Background(), "failing_tool", map[string]any{})
	require.Error(t, err)
	assert.Equal(t, assert.AnError, err)
}
