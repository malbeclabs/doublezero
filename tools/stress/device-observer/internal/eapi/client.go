// Package eapi is a thin wrapper around the Arista goeapi client used by
// the device-observer sampler.
package eapi

import (
	"encoding/json"
	"fmt"

	"github.com/aristanetworks/goeapi"
)

type Client struct {
	node *goeapi.Node
}

// NewClient dials the device's eAPI endpoint over HTTP. HTTPS support is
// deferred; see docs/work-plan-3793.md.
func NewClient(host, user, pass string, port int) (*Client, error) {
	node, err := goeapi.Connect("http", host, user, pass, port)
	if err != nil {
		return nil, fmt.Errorf("eapi connect %s:%d: %w", host, port, err)
	}
	return &Client{node: node}, nil
}

func (c *Client) RunShowJSON(cmd string) (json.RawMessage, error) {
	resp, err := c.node.RunCommands([]string{cmd}, "json")
	if err != nil {
		return nil, fmt.Errorf("run %q (json): %w", cmd, err)
	}
	if resp == nil || len(resp.Result) != 1 {
		return nil, fmt.Errorf("run %q (json): unexpected result length", cmd)
	}
	raw, err := json.Marshal(resp.Result[0])
	if err != nil {
		return nil, fmt.Errorf("re-marshal %q result: %w", cmd, err)
	}
	return raw, nil
}

func (c *Client) RunShowText(cmd string) (string, error) {
	resp, err := c.node.RunCommands([]string{cmd}, "text")
	if err != nil {
		return "", fmt.Errorf("run %q (text): %w", cmd, err)
	}
	if resp == nil || len(resp.Result) != 1 {
		return "", fmt.Errorf("run %q (text): unexpected result length", cmd)
	}
	out, ok := resp.Result[0]["output"].(string)
	if !ok {
		return "", fmt.Errorf("run %q (text): missing output field", cmd)
	}
	return out, nil
}
