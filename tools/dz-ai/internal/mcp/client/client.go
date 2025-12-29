package client

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/lake/pkg/retry"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultRequestTimeout = 120 * time.Second
)

var (
	mcpClientImplementation = &mcp.Implementation{
		Name:    "mcp-client",
		Version: "1.0.0",
	}
)

type Config struct {
	Logger *slog.Logger

	Endpoint       string
	RequestTimeout time.Duration
	Token          string // Optional Bearer token for authentication
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	if c.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}

	if c.RequestTimeout == 0 {
		c.RequestTimeout = defaultRequestTimeout
	}

	return nil
}

type Client struct {
	log       *slog.Logger
	cfg       *Config
	session   *mcp.ClientSession
	sessionMu sync.RWMutex // protects session and mcpClient
	mcpClient *mcp.Client
}

func New(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	client := &Client{
		log:       cfg.Logger,
		cfg:       &cfg,
		mcpClient: mcp.NewClient(mcpClientImplementation, nil),
	}

	if err := client.connect(ctx); err != nil {
		return nil, err
	}

	return client, nil
}

// connect establishes a new connection to the MCP server
func (c *Client) connect(ctx context.Context) error {
	// Create HTTP client with optional token authentication
	var httpClient *http.Client
	if c.cfg.Token != "" {
		// Wrap the default transport to add Authorization header
		baseTransport := http.DefaultTransport
		transport := &tokenTransport{
			base:  baseTransport,
			token: c.cfg.Token,
		}
		httpClient = &http.Client{
			Timeout:   c.cfg.RequestTimeout,
			Transport: transport,
		}
	} else {
		httpClient = &http.Client{Timeout: c.cfg.RequestTimeout}
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:   c.cfg.Endpoint,
		HTTPClient: httpClient,
	}

	session, err := c.mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to MCP server: %w", err)
	}

	c.sessionMu.Lock()
	if c.session != nil {
		c.session.Close()
	}
	c.session = session
	c.sessionMu.Unlock()

	c.log.Info("mcp/client: connected to server", "endpoint", c.cfg.Endpoint)
	return nil
}

// reconnect attempts to reconnect to the MCP server
func (c *Client) reconnect(ctx context.Context) error {
	c.log.Warn("mcp/client: attempting to reconnect")
	c.sessionMu.Lock()
	if c.session != nil {
		c.session.Close()
		c.session = nil
	}
	c.sessionMu.Unlock()

	return c.connect(ctx)
}

// isConnectionError checks if an error is a connection error that warrants reconnection
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection closed") ||
		strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "client is closing") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset")
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	c.log.Debug("mcp/client: listing available tools")

	c.sessionMu.RLock()
	session := c.session
	c.sessionMu.RUnlock()

	if session == nil {
		return nil, fmt.Errorf("session not connected")
	}

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		if isConnectionError(err) {
			c.log.Warn("mcp/client: connection error, attempting reconnect", "error", err)
			if reconnectErr := c.reconnect(ctx); reconnectErr != nil {
				return nil, fmt.Errorf("failed to reconnect: %w (original error: %w)", reconnectErr, err)
			}
			c.sessionMu.RLock()
			session = c.session
			c.sessionMu.RUnlock()
			if session == nil {
				return nil, fmt.Errorf("session still not connected after reconnect")
			}
			result, err = session.ListTools(ctx, &mcp.ListToolsParams{})
			if err != nil {
				return nil, fmt.Errorf("failed after reconnect: %w", err)
			}
		} else {
			return nil, err
		}
	}

	tools := make([]Tool, 0, len(result.Tools))
	for _, t := range result.Tools {
		inputSchema, _ := t.InputSchema.(map[string]any)
		tools = append(tools, Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: inputSchema,
		})
	}

	c.log.Debug("mcp/client: found tools", "count", len(tools))
	return tools, nil
}

func (c *Client) CallToolText(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	c.log.Debug("mcp/client: calling tool", "name", name)

	var result *mcp.CallToolResult
	var err error

	retryCfg := retry.DefaultConfig()
	err = retry.Do(ctx, retryCfg, func() error {
		c.sessionMu.RLock()
		session := c.session
		c.sessionMu.RUnlock()

		if session == nil {
			// Try to reconnect if session is nil
			if reconnectErr := c.reconnect(ctx); reconnectErr != nil {
				return fmt.Errorf("session not connected and reconnect failed: %w", reconnectErr)
			}
			c.sessionMu.RLock()
			session = c.session
			c.sessionMu.RUnlock()
			if session == nil {
				return fmt.Errorf("session still not connected after reconnect")
			}
		}

		result, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name:      name,
			Arguments: args,
		})
		if err != nil {
			// If it's a connection error, try to reconnect before retrying
			if isConnectionError(err) {
				c.log.Warn("mcp/client: connection error, attempting reconnect", "error", err)
				if reconnectErr := c.reconnect(ctx); reconnectErr != nil {
					return fmt.Errorf("failed to reconnect: %w (original error: %w)", reconnectErr, err)
				}
				// After reconnecting, get the new session and retry the call
				c.sessionMu.RLock()
				session = c.session
				c.sessionMu.RUnlock()
				if session == nil {
					return fmt.Errorf("session still not connected after reconnect")
				}
				result, err = session.CallTool(ctx, &mcp.CallToolParams{
					Name:      name,
					Arguments: args,
				})
			}
			return err
		}
		return nil
	})

	if err != nil {
		return "", true, fmt.Errorf("failed to call tool after retries: %w", err)
	}

	var textParts []string
	for _, content := range result.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			textParts = append(textParts, textContent.Text)
		}
	}

	str := ""
	if len(textParts) > 0 {
		for i, part := range textParts {
			if i > 0 {
				str += "\n"
			}
			str += part
		}
	}
	isError := result.IsError

	if isError {
		c.log.Warn("mcp/client: tool returned error result", "error", str)
	} else {
		c.log.Debug("mcp/client: called tool", "chars", len(str))
	}
	return str, isError, nil
}

func (c *Client) Close() error {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.session != nil {
		return c.session.Close()
	}
	return nil
}

// tokenTransport wraps an http.RoundTripper to add Authorization header
type tokenTransport struct {
	base  http.RoundTripper
	token string
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t.token))
	return t.base.RoundTrip(req)
}
