package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/agent"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/logger"
	mcpclient "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/client"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

type anthropicMessageAdapter struct {
	msg anthropic.MessageParam
}

func (a anthropicMessageAdapter) ToParam() any {
	return a.msg
}

func run() error {
	verboseFlag := flag.Bool("verbose", false, "enable verbose (debug) logging")
	flag.Parse()

	log := logger.New(*verboseFlag)

	// Get question from positional args, or use default
	question := "Come up with an interesting question to ask about DoubleZero, and answer it using the tools."
	if len(flag.Args()) > 0 {
		question = flag.Arg(0)
	}

	mcpURL := os.Getenv("MCP_URL")
	if mcpURL == "" {
		return fmt.Errorf("MCP_URL is required")
	}

	anthropicAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicAPIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	anthropicClient := anthropic.NewClient(option.WithAPIKey(anthropicAPIKey))
	model := anthropic.ModelClaudeSonnet4_5_20250929

	mcpToken := os.Getenv("MCP_TOKEN")
	mcpClient, err := mcpclient.New(ctx, mcpclient.Config{
		Endpoint: mcpURL,
		Logger:   log,
		Token:    mcpToken,
	})
	if err != nil {
		return fmt.Errorf("failed to create MCP client: %w", err)
	}
	defer mcpClient.Close()

	anthropicAgent := agent.NewAnthropicAgent(&agent.AnthropicAgentConfig{
		Client:           anthropicClient,
		Model:            model,
		MaxTokens:        int64(2000),
		MaxRounds:        16,
		MaxToolResultLen: 20000,
		Logger:           log,
		System:           agent.SystemPrompt,
	})

	msgs := []agent.Message{
		anthropicMessageAdapter{
			msg: anthropic.NewUserMessage(anthropic.NewTextBlock(question)),
		},
	}

	result, err := agent.RunAgent(ctx, anthropicAgent, mcpClient, msgs, os.Stdout)
	if err != nil {
		return fmt.Errorf("failed to run agent: %w", err)
	}
	_ = result

	return nil
}
