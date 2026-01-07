package tools

import (
	"context"
	"fmt"

	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
)

// MultiToolClient aggregates multiple ToolClient implementations and routes
// tool calls to the appropriate client based on tool name.
type MultiToolClient struct {
	clients   []react.ToolClient
	tools     []react.Tool
	toolIndex map[string]react.ToolClient
}

// NewMultiToolClient creates a new MultiToolClient that aggregates the given clients.
// It builds an index of all tools from all clients and returns an error if any
// tool name appears in multiple clients.
func NewMultiToolClient(clients ...react.ToolClient) (*MultiToolClient, error) {
	m := &MultiToolClient{
		clients:   clients,
		tools:     make([]react.Tool, 0),
		toolIndex: make(map[string]react.ToolClient),
	}

	ctx := context.Background()

	for _, client := range clients {
		tools, err := client.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list tools from client: %w", err)
		}

		for _, tool := range tools {
			if existing, ok := m.toolIndex[tool.Name]; ok {
				_ = existing // Avoid unused variable warning
				return nil, fmt.Errorf("duplicate tool name %q: tool exists in multiple clients", tool.Name)
			}
			m.toolIndex[tool.Name] = client
			m.tools = append(m.tools, tool)
		}
	}

	return m, nil
}

// ListTools returns the combined list of tools from all clients.
func (m *MultiToolClient) ListTools(_ context.Context) ([]react.Tool, error) {
	return m.tools, nil
}

// CallToolText routes the tool call to the appropriate client based on tool name.
// Returns an error if the tool is not found in any client.
func (m *MultiToolClient) CallToolText(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	client, ok := m.toolIndex[name]
	if !ok {
		return "", true, fmt.Errorf("unknown tool: %s", name)
	}
	return client.CallToolText(ctx, name, args)
}
