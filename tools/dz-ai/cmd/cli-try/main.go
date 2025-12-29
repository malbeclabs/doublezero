package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/malbeclabs/doublezero/lake/pkg/logger"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/agent"
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
	maxRoundsFlag := flag.Int("max-rounds", 16, "Maximum number of rounds for the AI agent in normal mode")
	flag.Parse()

	log := logger.New(*verboseFlag)

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
	// model := anthropic.ModelClaudeSonnet4_5_20250929
	model := anthropic.ModelClaudeHaiku4_5_20251001

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
		Client:                anthropicClient,
		Model:                 model,
		MaxTokens:             int64(2000),
		MaxRounds:             *maxRoundsFlag,
		MaxToolResultLen:      20000,
		Logger:                log,
		System:                agent.SystemPrompt,
		KeepToolResultsRounds: 3,
	})

	// Initialize conversation history
	conversationHistory := []agent.Message{}

	// If a question is provided as a positional arg, use it as the first message
	if len(flag.Args()) > 0 {
		question := flag.Arg(0)
		conversationHistory = append(conversationHistory, anthropicMessageAdapter{
			msg: anthropic.NewUserMessage(anthropic.NewTextBlock(question)),
		})
		result, err := agent.RunAgent(ctx, anthropicAgent, mcpClient, conversationHistory, os.Stdout)
		if err != nil {
			return fmt.Errorf("failed to run agent: %w", err)
		}
		conversationHistory = result.FullConversation
		fmt.Println() // Add blank line after response
	}

	// Interactive loop using channel to handle Ctrl-C
	inputChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			inputChan <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			errChan <- err
		}
		close(inputChan)
	}()

	fmt.Print("> ")
	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nGoodbye!")
			return nil
		case err := <-errChan:
			return fmt.Errorf("error reading input: %w", err)
		case input, ok := <-inputChan:
			if !ok {
				// Channel closed, stdin EOF
				return nil
			}

			input = strings.TrimSpace(input)
			if input == "" {
				fmt.Print("> ")
				continue
			}

			// Check for exit commands
			if input == "exit" || input == "quit" || input == "q" {
				fmt.Println("Goodbye!")
				return nil
			}

			// Add user message to conversation history
			userMsg := anthropicMessageAdapter{
				msg: anthropic.NewUserMessage(anthropic.NewTextBlock(input)),
			}
			conversationHistory = append(conversationHistory, userMsg)

			// Run agent with full conversation history
			result, err := agent.RunAgent(ctx, anthropicAgent, mcpClient, conversationHistory, os.Stdout)
			if err != nil {
				// Check if error is due to context cancellation
				if ctx.Err() != nil {
					fmt.Println("\nGoodbye!")
					return nil
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				// Remove the failed message from history
				conversationHistory = conversationHistory[:len(conversationHistory)-1]
				fmt.Print("> ")
				continue
			}

			// Update conversation history with full result
			conversationHistory = result.FullConversation
			fmt.Println() // Add blank line after response
			fmt.Print("> ")
		}
	}
}
