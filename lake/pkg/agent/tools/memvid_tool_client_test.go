package tools

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockRunCall records a single call to the mock runner.
type MockRunCall struct {
	Name  string
	Args  []string
	Stdin string
}

// MockCommandRunner implements CommandRunner for testing.
type MockCommandRunner struct {
	RunFunc func(ctx context.Context, name string, args []string, stdin io.Reader) (string, string, error)
	Calls   []MockRunCall
}

// Run implements CommandRunner.
func (m *MockCommandRunner) Run(ctx context.Context, name string, args []string, stdin io.Reader) (string, string, error) {
	var stdinContent string
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		stdinContent = string(data)
	}
	m.Calls = append(m.Calls, MockRunCall{
		Name:  name,
		Args:  args,
		Stdin: stdinContent,
	})
	if m.RunFunc != nil {
		return m.RunFunc(ctx, name, args, nil)
	}
	return "", "", nil
}

func TestMemvidToolClient_ListTools(t *testing.T) {
	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath: "/usr/bin/memvid",
		BrainPath:  "/path/to/brain.mv2",
	})

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 4)

	toolNames := make([]string, len(tools))
	for i, tool := range tools {
		toolNames[i] = tool.Name
	}

	assert.Contains(t, toolNames, "memory_save")
	assert.Contains(t, toolNames, "memory_search")
	assert.Contains(t, toolNames, "memory_ask")
	assert.Contains(t, toolNames, "memory_stats")
}

func TestMemvidToolClient_MemorySave(t *testing.T) {
	mock := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return `{"id": "frame-123", "status": "ok"}`, "", nil
		},
	}

	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mock,
	})

	result, isError, err := client.CallToolText(context.Background(), "memory_save", map[string]any{
		"content": "Test content to save",
		"title":   "Test Title",
	})

	require.NoError(t, err)
	assert.False(t, isError)
	assert.Contains(t, result, "frame-123")

	require.Len(t, mock.Calls, 1)
	call := mock.Calls[0]
	assert.Equal(t, "/usr/bin/memvid", call.Name)
	assert.Contains(t, call.Args, "put")
	assert.Contains(t, call.Args, "/path/to/brain.mv2")
	assert.Contains(t, call.Args, "--json")
	assert.Contains(t, call.Args, "--title")
	assert.Contains(t, call.Args, "Test Title")
	assert.Equal(t, "Test content to save", call.Stdin)
}

func TestMemvidToolClient_MemorySave_WithOptionalParams(t *testing.T) {
	mock := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return `{"id": "frame-456"}`, "", nil
		},
	}

	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mock,
	})

	result, isError, err := client.CallToolText(context.Background(), "memory_save", map[string]any{
		"content": "Content with metadata",
		"title":   "Metadata Test",
		"uri":     "custom://resource",
		"tags":    []any{"env=prod", "type=config"},
	})

	require.NoError(t, err)
	assert.False(t, isError)
	assert.Contains(t, result, "frame-456")

	require.Len(t, mock.Calls, 1)
	call := mock.Calls[0]
	assert.Contains(t, call.Args, "--uri")
	assert.Contains(t, call.Args, "custom://resource")
	assert.Contains(t, call.Args, "--tag")
	assert.Contains(t, call.Args, "env=prod")
	assert.Contains(t, call.Args, "type=config")
}

func TestMemvidToolClient_MemorySave_MissingContent(t *testing.T) {
	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath: "/usr/bin/memvid",
		BrainPath:  "/path/to/brain.mv2",
	})

	result, isError, err := client.CallToolText(context.Background(), "memory_save", map[string]any{
		"title": "Test Title",
	})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "content parameter is required")
}

func TestMemvidToolClient_MemorySave_MissingTitle(t *testing.T) {
	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath: "/usr/bin/memvid",
		BrainPath:  "/path/to/brain.mv2",
	})

	result, isError, err := client.CallToolText(context.Background(), "memory_save", map[string]any{
		"content": "Test content",
	})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "title parameter is required")
}

func TestMemvidToolClient_MemorySearch(t *testing.T) {
	mock := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return `{"results": [{"id": "1", "score": 0.95}]}`, "", nil
		},
	}

	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mock,
	})

	result, isError, err := client.CallToolText(context.Background(), "memory_search", map[string]any{
		"query": "test query",
	})

	require.NoError(t, err)
	assert.False(t, isError)
	assert.Contains(t, result, "results")

	require.Len(t, mock.Calls, 1)
	call := mock.Calls[0]
	assert.Contains(t, call.Args, "find")
	assert.Contains(t, call.Args, "--query")
	assert.Contains(t, call.Args, "test query")
	assert.Contains(t, call.Args, "--json")
}

func TestMemvidToolClient_MemorySearch_WithModeAndTopK(t *testing.T) {
	mock := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return `{"results": []}`, "", nil
		},
	}

	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mock,
	})

	_, isError, err := client.CallToolText(context.Background(), "memory_search", map[string]any{
		"query": "semantic search",
		"mode":  "sem",
		"top_k": float64(15),
	})

	require.NoError(t, err)
	assert.False(t, isError)

	require.Len(t, mock.Calls, 1)
	call := mock.Calls[0]
	assert.Contains(t, call.Args, "--mode")
	assert.Contains(t, call.Args, "sem")
	assert.Contains(t, call.Args, "--top-k")
	assert.Contains(t, call.Args, "15")
}

func TestMemvidToolClient_MemorySearch_MissingQuery(t *testing.T) {
	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath: "/usr/bin/memvid",
		BrainPath:  "/path/to/brain.mv2",
	})

	result, isError, err := client.CallToolText(context.Background(), "memory_search", map[string]any{})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "query parameter is required")
}

func TestMemvidToolClient_MemoryAsk(t *testing.T) {
	mock := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return `{"answer": "The answer is 42", "sources": []}`, "", nil
		},
	}

	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mock,
	})

	result, isError, err := client.CallToolText(context.Background(), "memory_ask", map[string]any{
		"question": "What is the meaning of life?",
	})

	require.NoError(t, err)
	assert.False(t, isError)
	assert.Contains(t, result, "answer")

	require.Len(t, mock.Calls, 1)
	call := mock.Calls[0]
	assert.Contains(t, call.Args, "ask")
	assert.Contains(t, call.Args, "--question")
	assert.Contains(t, call.Args, "What is the meaning of life?")
	assert.Contains(t, call.Args, "--json")
}

func TestMemvidToolClient_MemoryAsk_WithFlags(t *testing.T) {
	mock := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return `{"context": "relevant context"}`, "", nil
		},
	}

	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mock,
	})

	_, isError, err := client.CallToolText(context.Background(), "memory_ask", map[string]any{
		"question":     "Test question",
		"context_only": true,
		"sources":      true,
	})

	require.NoError(t, err)
	assert.False(t, isError)

	require.Len(t, mock.Calls, 1)
	call := mock.Calls[0]
	assert.Contains(t, call.Args, "--context-only")
	assert.Contains(t, call.Args, "--sources")
}

func TestMemvidToolClient_MemoryAsk_MissingQuestion(t *testing.T) {
	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath: "/usr/bin/memvid",
		BrainPath:  "/path/to/brain.mv2",
	})

	result, isError, err := client.CallToolText(context.Background(), "memory_ask", map[string]any{})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "question parameter is required")
}

func TestMemvidToolClient_MemoryStats(t *testing.T) {
	mock := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return `{"frame_count": 100, "size_bytes": 1024000}`, "", nil
		},
	}

	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mock,
	})

	result, isError, err := client.CallToolText(context.Background(), "memory_stats", map[string]any{})

	require.NoError(t, err)
	assert.False(t, isError)
	assert.Contains(t, result, "frame_count")

	require.Len(t, mock.Calls, 1)
	call := mock.Calls[0]
	assert.Contains(t, call.Args, "stats")
	assert.Contains(t, call.Args, "/path/to/brain.mv2")
	assert.Contains(t, call.Args, "--json")
}

func TestMemvidToolClient_UnknownTool(t *testing.T) {
	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath: "/usr/bin/memvid",
		BrainPath:  "/path/to/brain.mv2",
	})

	_, _, err := client.CallToolText(context.Background(), "unknown_tool", map[string]any{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

func TestMemvidToolClient_CLIError_ReturnsStderr(t *testing.T) {
	mock := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return "", "Error: memory file not found", errors.New("exit status 1")
		},
	}

	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mock,
	})

	result, isError, err := client.CallToolText(context.Background(), "memory_stats", map[string]any{})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "memory file not found")
}

func TestMemvidToolClient_CLIError_NoStderr(t *testing.T) {
	mock := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return "", "", errors.New("signal: killed")
		},
	}

	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mock,
	})

	result, isError, err := client.CallToolText(context.Background(), "memory_stats", map[string]any{})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "command failed")
}

func TestMemvidToolClient_OutputTruncation(t *testing.T) {
	longOutput := make([]byte, 15000)
	for i := range longOutput {
		longOutput[i] = 'x'
	}

	mock := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return string(longOutput), "", nil
		},
	}

	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath:     "/usr/bin/memvid",
		BrainPath:      "/path/to/brain.mv2",
		MaxOutputChars: 10000,
		CommandRunner:  mock,
	})

	result, isError, err := client.CallToolText(context.Background(), "memory_stats", map[string]any{})

	require.NoError(t, err)
	assert.False(t, isError)
	assert.Contains(t, result, "truncated")
	assert.Less(t, len(result), 15000)
}

func TestMemvidToolClient_DefaultMaxOutput(t *testing.T) {
	client := NewMemvidToolClient(MemvidConfig{
		BinaryPath: "/usr/bin/memvid",
		BrainPath:  "/path/to/brain.mv2",
	})

	assert.Equal(t, defaultMemvidMaxOutputChars, client.config.MaxOutputChars)
}
