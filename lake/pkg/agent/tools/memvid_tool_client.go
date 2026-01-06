package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
)

const (
	defaultMemvidTimeout        = 30 * time.Second
	defaultMemvidMaxOutputChars = 10000
)

// MemvidConfig holds configuration for the MemvidToolClient.
type MemvidConfig struct {
	// BinaryPath is the path to the memvid CLI binary.
	BinaryPath string
	// BrainPath is the path to the .mv2 memory file.
	BrainPath string
	// Timeout is the command timeout (default 30s).
	Timeout time.Duration
	// MaxOutputChars is the truncation limit (default 10000).
	MaxOutputChars int
	// CommandRunner is optional, for testing.
	CommandRunner CommandRunner
}

// MemvidToolClient implements react.ToolClient using the memvid CLI.
type MemvidToolClient struct {
	config MemvidConfig
	runner CommandRunner
}

// NewMemvidToolClient creates a new MemvidToolClient with the given config.
func NewMemvidToolClient(config MemvidConfig) *MemvidToolClient {
	runner := config.CommandRunner
	if runner == nil {
		timeout := config.Timeout
		if timeout == 0 {
			timeout = defaultMemvidTimeout
		}
		runner = &ExecCommandRunner{Timeout: timeout}
	}

	maxOutput := config.MaxOutputChars
	if maxOutput == 0 {
		maxOutput = defaultMemvidMaxOutputChars
	}

	return &MemvidToolClient{
		config: MemvidConfig{
			BinaryPath:     config.BinaryPath,
			BrainPath:      config.BrainPath,
			Timeout:        config.Timeout,
			MaxOutputChars: maxOutput,
			CommandRunner:  config.CommandRunner,
		},
		runner: runner,
	}
}

// ListTools returns the available memory tools.
func (m *MemvidToolClient) ListTools(_ context.Context) ([]react.Tool, error) {
	return []react.Tool{
		{
			Name:        "memory_save",
			Description: "Store content in persistent memory. Use to remember facts, context, or information for later retrieval.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "The content to store in memory",
					},
					"title": map[string]any{
						"type":        "string",
						"description": "A descriptive title for the memory entry",
					},
					"uri": map[string]any{
						"type":        "string",
						"description": "Optional URI to associate with this content",
					},
					"tags": map[string]any{
						"type":        "array",
						"description": "Optional tags in key=value format",
						"items": map[string]any{
							"type": "string",
						},
					},
				},
				"required": []string{"content", "title"},
			},
		},
		{
			Name:        "memory_search",
			Description: "Search memory for relevant content. Returns matching entries based on the query.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The search query",
					},
					"mode": map[string]any{
						"type":        "string",
						"description": "Search mode: 'auto' (default), 'lex' (lexical), or 'sem' (semantic)",
						"enum":        []string{"auto", "lex", "sem"},
					},
					"top_k": map[string]any{
						"type":        "integer",
						"description": "Number of results to return (default: 8)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "memory_ask",
			Description: "Ask a question using RAG (Retrieval Augmented Generation). Retrieves relevant context and synthesizes an answer.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question": map[string]any{
						"type":        "string",
						"description": "The question to ask",
					},
					"context_only": map[string]any{
						"type":        "boolean",
						"description": "If true, return only the retrieved context without synthesis",
					},
					"sources": map[string]any{
						"type":        "boolean",
						"description": "If true, include detailed source information",
					},
				},
				"required": []string{"question"},
			},
		},
		{
			Name:        "memory_stats",
			Description: "Get statistics about the memory store (number of entries, size, etc.).",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}, nil
}

// CallToolText calls a tool and returns the result as text.
func (m *MemvidToolClient) CallToolText(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	switch name {
	case "memory_save":
		return m.callMemorySave(ctx, args)
	case "memory_search":
		return m.callMemorySearch(ctx, args)
	case "memory_ask":
		return m.callMemoryAsk(ctx, args)
	case "memory_stats":
		return m.callMemoryStats(ctx)
	default:
		return "", true, fmt.Errorf("unknown tool: %s", name)
	}
}

func (m *MemvidToolClient) callMemorySave(ctx context.Context, args map[string]any) (string, bool, error) {
	content, ok := args["content"].(string)
	if !ok {
		return "content parameter is required and must be a string", true, nil
	}

	title, ok := args["title"].(string)
	if !ok {
		return "title parameter is required and must be a string", true, nil
	}

	cmdArgs := []string{"put", m.config.BrainPath, "--json", "--title", title}

	if uri, ok := args["uri"].(string); ok && uri != "" {
		cmdArgs = append(cmdArgs, "--uri", uri)
	}

	if tags, ok := args["tags"].([]any); ok {
		for _, tag := range tags {
			if tagStr, ok := tag.(string); ok {
				cmdArgs = append(cmdArgs, "--tag", tagStr)
			}
		}
	}

	stdout, stderr, err := m.runner.Run(ctx, m.config.BinaryPath, cmdArgs, strings.NewReader(content))
	if err != nil {
		// Return stderr as result with isError=true
		if stderr != "" {
			return m.truncate(stderr), true, nil
		}
		return fmt.Sprintf("command failed: %v", err), true, nil
	}

	return m.truncate(stdout), false, nil
}

func (m *MemvidToolClient) callMemorySearch(ctx context.Context, args map[string]any) (string, bool, error) {
	query, ok := args["query"].(string)
	if !ok {
		return "query parameter is required and must be a string", true, nil
	}

	cmdArgs := []string{"find", m.config.BrainPath, "--query", query, "--json"}

	if mode, ok := args["mode"].(string); ok && mode != "" {
		cmdArgs = append(cmdArgs, "--mode", mode)
	}

	if topK, ok := args["top_k"].(float64); ok {
		cmdArgs = append(cmdArgs, "--top-k", fmt.Sprintf("%d", int(topK)))
	}

	stdout, stderr, err := m.runner.Run(ctx, m.config.BinaryPath, cmdArgs, nil)
	if err != nil {
		if stderr != "" {
			return m.truncate(stderr), true, nil
		}
		return fmt.Sprintf("command failed: %v", err), true, nil
	}

	return m.truncate(stdout), false, nil
}

func (m *MemvidToolClient) callMemoryAsk(ctx context.Context, args map[string]any) (string, bool, error) {
	question, ok := args["question"].(string)
	if !ok {
		return "question parameter is required and must be a string", true, nil
	}

	cmdArgs := []string{"ask", m.config.BrainPath, "--question", question, "--json"}

	if contextOnly, ok := args["context_only"].(bool); ok && contextOnly {
		cmdArgs = append(cmdArgs, "--context-only")
	}

	if sources, ok := args["sources"].(bool); ok && sources {
		cmdArgs = append(cmdArgs, "--sources")
	}

	stdout, stderr, err := m.runner.Run(ctx, m.config.BinaryPath, cmdArgs, nil)
	if err != nil {
		if stderr != "" {
			return m.truncate(stderr), true, nil
		}
		return fmt.Sprintf("command failed: %v", err), true, nil
	}

	return m.truncate(stdout), false, nil
}

func (m *MemvidToolClient) callMemoryStats(ctx context.Context) (string, bool, error) {
	cmdArgs := []string{"stats", m.config.BrainPath, "--json"}

	stdout, stderr, err := m.runner.Run(ctx, m.config.BinaryPath, cmdArgs, nil)
	if err != nil {
		if stderr != "" {
			return m.truncate(stderr), true, nil
		}
		return fmt.Sprintf("command failed: %v", err), true, nil
	}

	return m.truncate(stdout), false, nil
}

func (m *MemvidToolClient) truncate(s string) string {
	if len(s) > m.config.MaxOutputChars {
		return s[:m.config.MaxOutputChars] + "\n... (truncated)"
	}
	return s
}
