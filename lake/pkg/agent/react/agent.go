package react

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

const (
	defaultMaxContextTokens = 20000
	defaultMaxRounds        = 10
)

// Config is the configuration for the Agent.
type Config struct {
	Logger             *slog.Logger
	LLM                LLMClient
	ToolClient         ToolClient
	MaxRounds          int
	MaxContextTokens   int
	FinalizationPrompt string // Optional prompt to inject when max rounds is reached
}

func (cfg *Config) Validate() error {
	if cfg.LLM == nil {
		return errors.New("LLM is required")
	}
	if cfg.ToolClient == nil {
		return errors.New("tool client is required")
	}
	if cfg.MaxRounds == 0 {
		cfg.MaxRounds = defaultMaxRounds
	}
	if cfg.MaxRounds <= 0 {
		return errors.New("max rounds must be greater than 0")
	}
	if cfg.MaxContextTokens == 0 {
		cfg.MaxContextTokens = defaultMaxContextTokens
	}
	if cfg.MaxContextTokens <= 0 {
		return errors.New("max context tokens must be greater than 0")
	}
	if cfg.FinalizationPrompt == "" {
		return errors.New("finalization prompt is required")
	}
	return nil
}

// Agent is a ReAct agent that can use tools to interact with an LLM.
type Agent struct {
	log *slog.Logger
	cfg *Config
}

// NewAgent creates a new ReAct agent.
func NewAgent(cfg *Config) (*Agent, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Agent{
		log: cfg.Logger,
		cfg: cfg,
	}, nil
}

// Run executes the ReAct tool-calling loop.
func (a *Agent) Run(ctx context.Context, initialMessages []Message, output io.Writer) (*RunResult, error) {
	msgs := make([]Message, len(initialMessages))
	copy(msgs, initialMessages)

	fullConversation := make([]Message, len(initialMessages))
	copy(fullConversation, initialMessages)

	// Track unique tools used during this run
	toolsUsedSet := make(map[string]struct{})

	// Get available tools
	tools, err := a.cfg.ToolClient.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	for round := 0; round < a.cfg.MaxRounds; round++ {
		roundNum := round + 1
		if a.log != nil {
			a.log.Info("react: starting round", "round", roundNum, "max_rounds", a.cfg.MaxRounds)
		}

		// Calculate and log context size before calling LLM
		contextChars, contextTokens := a.calculateContextSize(msgs, tools)
		if a.log != nil {
			a.log.Info("react: context size", "round", roundNum, "chars", contextChars, "tokens_est", contextTokens)
		}

		// Check if we need to compact the conversation history
		// Keep compacting until we're below the threshold
		originalTokens := contextTokens
		maxIterations := 5 // Prevent infinite loops
		for iteration := 0; iteration < maxIterations && contextTokens > a.cfg.MaxContextTokens; iteration++ {
			if a.log != nil && iteration == 0 {
				a.log.Info("react: context exceeds threshold, compacting conversation",
					"round", roundNum,
					"tokens", contextTokens,
					"threshold", a.cfg.MaxContextTokens)
			}

			// Start with keeping 10 recent messages, but reduce if we need more aggressive compaction
			keepRecent := 10 - (iteration * 2)
			if keepRecent < 2 {
				keepRecent = 2 // Always keep at least the first message and 2 recent messages
			}

			// If we can't summarize (not enough messages), we can't compact further
			if len(msgs) <= keepRecent+1 {
				if a.log != nil {
					a.log.Warn("react: cannot compact further, not enough messages",
						"round", roundNum,
						"messages", len(msgs),
						"keep_recent", keepRecent)
				}
				break
			}

			compacted, err := a.summarizeMessages(ctx, msgs, keepRecent)
			if err != nil {
				if a.log != nil {
					a.log.Warn("react: failed to summarize messages",
						"round", roundNum,
						"iteration", iteration+1,
						"error", err)
				}
				// If summarization fails, we can't continue compacting
				break
			}

			msgs = compacted
			// Recalculate context size after compaction
			_, contextTokens = a.calculateContextSize(msgs, tools)

			if a.log != nil {
				reduction := originalTokens - contextTokens
				reductionPercent := float64(reduction) / float64(originalTokens) * 100
				a.log.Info("react: conversation compacted",
					"round", roundNum,
					"iteration", iteration+1,
					"keep_recent", keepRecent,
					"original_tokens_est", originalTokens,
					"new_tokens_est", contextTokens,
					"reduction", reduction,
					"reduction_percent", fmt.Sprintf("%.1f%%", reductionPercent))
			}

			// If we're now below threshold, we're done
			if contextTokens <= a.cfg.MaxContextTokens {
				if a.log != nil {
					a.log.Info("react: compaction successful, below threshold",
						"round", roundNum,
						"final_tokens_est", contextTokens,
						"threshold", a.cfg.MaxContextTokens)
				}
				break
			}

			// If we're still above threshold but made progress, continue
			// If we didn't make progress (shouldn't happen, but safety check), break
			if iteration > 0 && contextTokens >= originalTokens {
				if a.log != nil {
					a.log.Warn("react: compaction not reducing tokens, stopping",
						"round", roundNum,
						"tokens", contextTokens)
				}
				break
			}
			originalTokens = contextTokens // Update for next iteration
		}

		// Final check: if we're still above threshold after all iterations, log a warning
		if contextTokens > a.cfg.MaxContextTokens {
			if a.log != nil {
				a.log.Warn("react: context still exceeds threshold after compaction",
					"round", roundNum,
					"tokens", contextTokens,
					"threshold", a.cfg.MaxContextTokens)
			}
		}

		// If this is the last round and we have a finalization prompt, inject it
		isLastRound := round == a.cfg.MaxRounds-1
		if isLastRound && a.cfg.FinalizationPrompt != "" {
			if a.log != nil {
				a.log.Info("react: injecting finalization prompt on last round", "round", roundNum)
			}
			finalizationMsg := anthropic.NewUserMessage(anthropic.NewTextBlock(a.cfg.FinalizationPrompt))
			msgs = append(msgs, AnthropicMessage{Msg: finalizationMsg})
			fullConversation = append(fullConversation, AnthropicMessage{Msg: finalizationMsg})
		}

		// Call LLM
		response, err := a.cfg.LLM.Call(ctx, msgs, tools)
		if err != nil {
			return nil, fmt.Errorf("failed to get response: %w", err)
		}

		if a.log != nil {
			a.log.Debug("react: received response", "round", roundNum, "contentBlocks", len(response.Content()))
		}

		assistantMsg := response.ToMessage()
		msgs = append(msgs, assistantMsg)
		fullConversation = append(fullConversation, assistantMsg)

		// Extract tool uses
		toolUses := a.extractToolUses(response.Content())
		if len(toolUses) == 0 {
			if a.log != nil {
				a.log.Info("react: no tool calls, returning final response", "round", roundNum)
			}

			var finalText string
			for _, blk := range response.Content() {
				text, ok := blk.AsText()
				if ok && text != "" {
					finalText += text
					if output != nil {
						fmt.Fprint(output, text)
					}
				}
			}
			if output != nil {
				fmt.Fprintln(output)
			}

			return &RunResult{
				FinalText:        strings.TrimSpace(finalText),
				FullConversation: fullConversation,
				ToolsUsed:        setToSlice(toolsUsedSet),
			}, nil
		}

		// Track tools used (before potentially returning early on last round)
		for _, tu := range toolUses {
			toolsUsedSet[tu.Name] = struct{}{}
		}

		// If this is the last round, return the response even if there are tool calls
		if isLastRound {
			if a.log != nil {
				a.log.Info("react: last round reached, returning response despite tool calls", "round", roundNum, "tool_calls", len(toolUses))
			}

			var finalText string
			for _, blk := range response.Content() {
				text, ok := blk.AsText()
				if ok && text != "" {
					finalText += text
					if output != nil {
						fmt.Fprint(output, text)
					}
				}
			}
			if output != nil {
				fmt.Fprintln(output)
			}

			return &RunResult{
				FinalText:        strings.TrimSpace(finalText),
				FullConversation: fullConversation,
				ToolsUsed:        setToSlice(toolsUsedSet),
			}, nil
		}

		if a.log != nil {
			if len(toolUses) > 1 {
				a.log.Info("react: found multiple tool calls, executing in parallel", "round", roundNum, "count", len(toolUses))
			} else {
				a.log.Info("react: found tool call to execute", "round", roundNum, "name", toolUses[0].Name)
			}
		}

		// Execute tools in parallel
		toolResults, err := a.executeTools(ctx, toolUses)
		if err != nil {
			return nil, fmt.Errorf("failed to execute tools: %w", err)
		}

		if a.log != nil {
			a.log.Debug("react: sending tool results back to model")
		}

		// Convert tool results to messages and add to conversation
		toolResultMsgs, err := a.cfg.LLM.ConvertToolResults(toolUses, toolResults)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool results: %w", err)
		}

		msgs = append(msgs, toolResultMsgs...)
		fullConversation = append(fullConversation, toolResultMsgs...)
	}

	return nil, fmt.Errorf("exceeded maximum rounds (%d)", a.cfg.MaxRounds)
}

// extractToolUses extracts tool use requests from response content blocks.
func (a *Agent) extractToolUses(content []ContentBlock) []ToolUse {
	var toolUses []ToolUse
	for _, blk := range content {
		id, name, inputBytes, ok := blk.AsToolUse()
		if !ok || id == "" || name == "" {
			continue
		}
		// Parse input JSON
		var input map[string]any
		if err := json.Unmarshal(inputBytes, &input); err != nil {
			continue
		}
		toolUses = append(toolUses, ToolUse{
			ID:    id,
			Name:  name,
			Input: input,
		})
	}
	return toolUses
}

// executeTools executes tools in parallel and returns tool results.
func (a *Agent) executeTools(ctx context.Context, toolUses []ToolUse) ([]ToolResult, error) {
	if len(toolUses) == 0 {
		return nil, nil
	}

	type toolResult struct {
		index int
		id    string
		out   string
		isErr bool
		err   error
	}

	results := make([]toolResult, len(toolUses))
	var wg sync.WaitGroup

	for i, tu := range toolUses {
		wg.Add(1)
		go func(idx int, toolUse ToolUse) {
			defer wg.Done()
			out, isErr, callErr := a.cfg.ToolClient.CallToolText(ctx, toolUse.Name, toolUse.Input)
			results[idx] = toolResult{
				index: idx,
				id:    toolUse.ID,
				out:   out,
				isErr: isErr,
				err:   callErr,
			}
		}(i, tu)
	}

	wg.Wait()

	// Process results in order
	toolResults := make([]ToolResult, 0, len(toolUses))
	for _, result := range results {
		if result.err != nil {
			if a.log != nil {
				a.log.Error("react: tool execution error", "error", result.err, "tool_id", result.id)
			}
			toolResults = append(toolResults, ToolResult{
				ID:      result.id,
				Content: fmt.Sprintf("Error: %v", result.err),
				IsError: true,
			})
			continue
		}

		var content string
		if result.isErr {
			content = fmt.Sprintf("Error: %s", result.out)
		} else {
			content = result.out
		}

		toolResults = append(toolResults, ToolResult{
			ID:      result.id,
			Content: content,
			IsError: result.isErr,
		})
	}

	return toolResults, nil
}

// calculateContextSize estimates the context size in characters and tokens.
// It extracts text content from messages and serializes tools to get accurate counts.
// Token estimation uses ~4 characters per token (Anthropic's approximate ratio for English text).
func (a *Agent) calculateContextSize(msgs []Message, tools []Tool) (chars int, tokens int) {
	// Extract text content from messages by serializing to JSON
	// This gives us the full message structure including metadata
	for _, msg := range msgs {
		param := msg.ToParam()
		if jsonData, err := json.Marshal(param); err == nil {
			chars += len(jsonData)
		}
	}

	// Serialize tools to JSON and count
	for _, tool := range tools {
		if jsonData, err := json.Marshal(tool); err == nil {
			chars += len(jsonData)
		}
	}

	// Estimate tokens: Anthropic uses ~4 characters per token on average for English text
	// This is a rough approximation; actual tokenization may vary
	// For JSON/structured data, tokens might be slightly higher, but 4 is a reasonable average
	tokens = chars / 4

	return chars, tokens
}

// extractMessageText extracts a text representation of a message for summarization.
// It serializes the message to JSON, which provides a complete representation including
// all content blocks, tool calls, and tool results.
func (a *Agent) extractMessageText(msg Message) string {
	param := msg.ToParam()
	// Serialize to JSON for a complete representation
	if jsonData, err := json.Marshal(param); err == nil {
		// Try to make it more readable by parsing and reformatting
		var msgData map[string]any
		if err := json.Unmarshal(jsonData, &msgData); err == nil {
			// Format as a more readable string
			if formatted, err := json.MarshalIndent(msgData, "", "  "); err == nil {
				return string(formatted)
			}
		}
		return string(jsonData)
	}
	return ""
}

// summarizeMessages compacts the conversation history by summarizing older messages.
// It keeps the first message (initial query) and the last keepRecent messages, and summarizes everything in between.
func (a *Agent) summarizeMessages(ctx context.Context, msgs []Message, keepRecent int) ([]Message, error) {
	if len(msgs) <= keepRecent+1 {
		// Not enough messages to summarize
		return msgs, nil
	}

	// Extract messages to summarize (everything except first and last keepRecent)
	messagesToSummarize := msgs[1 : len(msgs)-keepRecent]

	// Build summary prompt
	var conversationText strings.Builder
	for i, msg := range messagesToSummarize {
		text := a.extractMessageText(msg)
		conversationText.WriteString(fmt.Sprintf("Message %d: %s\n", i+1, text))
	}

	summaryPrompt := fmt.Sprintf(`Please provide a concise summary of the following conversation history, including:
- Key user queries and requests
- Important tool calls that were made
- Significant tool results and findings
- Any important context or decisions made

Conversation history to summarize:
%s

Provide a clear, concise summary that preserves the essential information needed for the conversation to continue effectively.`, conversationText.String())

	// Create a summary message using the LLM
	summaryMsg := anthropic.NewUserMessage(anthropic.NewTextBlock(summaryPrompt))
	summaryMessages := []Message{AnthropicMessage{Msg: summaryMsg}}

	// Call LLM to generate summary (without tools, just for summarization)
	response, err := a.cfg.LLM.Call(ctx, summaryMessages, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	// Extract summary text
	var summaryText strings.Builder
	for _, blk := range response.Content() {
		if text, ok := blk.AsText(); ok && text != "" {
			summaryText.WriteString(text)
		}
	}

	// Create a user message with the summary
	summaryUserMsg := anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf("[Previous conversation summary]: %s", summaryText.String())))
	summaryMessage := AnthropicMessage{Msg: summaryUserMsg}

	// Reconstruct messages: first message + summary + recent messages
	compacted := make([]Message, 0, 1+1+keepRecent)
	compacted = append(compacted, msgs[0])                        // Keep first message
	compacted = append(compacted, summaryMessage)                 // Add summary
	compacted = append(compacted, msgs[len(msgs)-keepRecent:]...) // Keep recent messages

	if a.log != nil {
		a.log.Info("react: compacted conversation",
			"original_messages", len(msgs),
			"compacted_messages", len(compacted),
			"summarized_messages", len(messagesToSummarize))
	}

	return compacted, nil
}

// setToSlice converts a set (map[string]struct{}) to a sorted slice of strings.
func setToSlice(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	result := make([]string, 0, len(set))
	for k := range set {
		result = append(result, k)
	}
	// Sort for deterministic output
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i] > result[j] {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}
