package agent

import (
	"context"
	"io"

	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
)

// AgentConfig configures an Agent.
type AgentConfig struct {
	ReactAgent *react.Agent
	LLMClient  react.LLMClient
}

// Agent is an agent that uses react.Agent with a system prompt built from Role and Catalog.
type Agent struct {
	reactAgent *react.Agent
	llmClient  react.LLMClient
}

// NewAgent creates a new Agent.
func NewAgent(cfg *AgentConfig) *Agent {
	return &Agent{
		reactAgent: cfg.ReactAgent,
		llmClient:  cfg.LLMClient,
	}
}

// Run executes the agent workflow.
func (a *Agent) Run(ctx context.Context, userQuery string, output io.Writer) (*react.RunResult, error) {
	// Create initial user message using the LLM client
	userMsg := a.llmClient.CreateUserMessage(userQuery)
	initialMessages := []react.Message{userMsg}

	// Run the react agent
	return a.reactAgent.Run(ctx, initialMessages, output)
}

// RunWithMessages executes the agent workflow with existing conversation history.
func (a *Agent) RunWithMessages(ctx context.Context, messages []react.Message, output io.Writer) (*react.RunResult, error) {
	// Run the react agent with the provided conversation history
	return a.reactAgent.Run(ctx, messages, output)
}
