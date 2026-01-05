package agent

import (
	"context"
	"io"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
)

// AgentConfig configures an Agent.
type AgentConfig struct {
	ReactAgent *react.Agent
}

// Agent is an agent that uses react.Agent with a system prompt built from Role and Catalog.
type Agent struct {
	reactAgent *react.Agent
}

// NewAgent creates a new Agent.
func NewAgent(cfg *AgentConfig) *Agent {
	return &Agent{
		reactAgent: cfg.ReactAgent,
	}
}

// Run executes the agent workflow.
func (a *Agent) Run(ctx context.Context, userQuery string, output io.Writer) (*react.RunResult, error) {
	// Create initial user message
	anthropicMsg := anthropic.NewUserMessage(anthropic.NewTextBlock(userQuery))
	userMsg := react.AnthropicMessage{Msg: anthropicMsg}
	initialMessages := []react.Message{userMsg}

	// Run the react agent
	return a.reactAgent.Run(ctx, initialMessages, output)
}

// RunWithMessages executes the agent workflow with existing conversation history.
func (a *Agent) RunWithMessages(ctx context.Context, messages []react.Message, output io.Writer) (*react.RunResult, error) {
	// Run the react agent with the provided conversation history
	return a.reactAgent.Run(ctx, messages, output)
}
