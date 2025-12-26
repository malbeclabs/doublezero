package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/client"
)

const (
	// Default values for tool execution
	defaultMaxToolResultLen = 20000
	defaultListToolsRetries = 3
	defaultRetryDelay       = 500 * time.Millisecond

	// Truncation constants
	truncationNoticeEstimate = 120
	truncationSearchWindow   = 500
	minAvailableLen          = 100

	// Schema tool names that get 2x truncation limit
	schemaToolDoublezero          = "doublezero-schema"
	schemaToolDoublezeroTelemetry = "doublezero-telemetry-schema"
	schemaToolSolana              = "solana-schema"
)

type AnthropicAgentConfig struct {
	Logger           *slog.Logger
	Client           anthropic.Client
	Model            anthropic.Model
	MaxTokens        int64
	MaxRounds        int
	MaxToolResultLen int
	System           string
	// KeepToolResultsRounds controls how many rounds of tool results to keep in conversation history.
	// If 0, all tool results are kept. If > 0, only the last N rounds of tool results are kept.
	// This helps manage context window size and costs for long conversations.
	KeepToolResultsRounds int
}

// AnthropicAgent is an Agent implementation for Anthropic's Claude models.
type AnthropicAgent struct {
	cfg *AnthropicAgentConfig
}

func NewAnthropicAgent(cfg *AnthropicAgentConfig) *AnthropicAgent {
	return &AnthropicAgent{cfg: cfg}
}

// anthropicMessage wraps Anthropic's MessageParam to implement agent.Message.
type anthropicMessage struct {
	msg anthropic.MessageParam
}

func (m anthropicMessage) ToParam() any {
	return m.msg
}

// anthropicResponse wraps Anthropic's response to implement agent.Response.
type anthropicResponse struct {
	resp *anthropic.Message
}

func (r anthropicResponse) Content() []ContentBlock {
	blocks := make([]ContentBlock, len(r.resp.Content))
	for i, blk := range r.resp.Content {
		blocks[i] = anthropicContentBlock{blk}
	}
	return blocks
}

func (r anthropicResponse) ToMessage() Message {
	return anthropicMessage{msg: r.resp.ToParam()}
}

// anthropicContentBlock wraps Anthropic's ContentBlockUnion to implement agent.ContentBlock.
type anthropicContentBlock struct {
	blk anthropic.ContentBlockUnion
}

func (b anthropicContentBlock) AsText() (string, bool) {
	text := b.blk.AsText()
	if text.Text == "" {
		return "", false
	}
	return text.Text, true
}

func (b anthropicContentBlock) AsToolUse() (string, string, []byte, bool) {
	tu := b.blk.AsToolUse()
	if tu.ID == "" || tu.Name == "" {
		return "", "", nil, false
	}
	return tu.ID, tu.Name, tu.Input, true
}

// Run executes the tool calling loop with Anthropic.
func (a *AnthropicAgent) Run(ctx context.Context, mcpClient *client.Client, initialMessages []Message, output io.Writer) (*RunResult, error) {
	msgs := make([]anthropic.MessageParam, len(initialMessages))
	for i, msg := range initialMessages {
		msgs[i] = msg.ToParam().(anthropic.MessageParam)
	}

	fullConversation := make([]Message, len(initialMessages))
	copy(fullConversation, initialMessages)

	mcpTools, err := a.listToolsWithRetry(ctx, mcpClient)
	if err != nil {
		return nil, err
	}

	tools := toAnthropicTools(mcpTools)

	// Track tool result message indices for trimming
	toolResultIndices := make([]int, 0)

	for round := 0; round < a.cfg.MaxRounds; round++ {
		roundNum := round + 1
		if a.cfg.Logger != nil {
			a.cfg.Logger.Info("agent: starting round", "round", roundNum, "max_rounds", a.cfg.MaxRounds)
		}

		params := anthropic.MessageNewParams{
			Model:     a.cfg.Model,
			MaxTokens: a.cfg.MaxTokens,
			Messages:  msgs,
			Tools:     tools,
		}
		if a.cfg.System != "" {
			params.System = []anthropic.TextBlockParam{
				{Text: a.cfg.System},
			}
		}
		resp, err := a.cfg.Client.Messages.New(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to get response: %w", err)
		}

		if a.cfg.Logger != nil {
			a.cfg.Logger.Debug("agent: received response", "round", roundNum, "contentBlocks", len(resp.Content))
		}

		response := anthropicResponse{resp: resp}
		assistantMsg := response.ToMessage()
		msgs = append(msgs, assistantMsg.ToParam().(anthropic.MessageParam))
		fullConversation = append(fullConversation, assistantMsg)

		toolUses := extractToolUses(response.Content())
		if len(toolUses) == 0 {
			if a.cfg.Logger != nil {
				a.cfg.Logger.Info("agent: no tool calls, returning final response", "round", roundNum)
			}

			if resp.StopReason == "max_tokens" {
				if a.cfg.Logger != nil {
					a.cfg.Logger.Warn("response truncated due to max_tokens limit, requesting complete summary", "max_tokens", a.cfg.MaxTokens)
				}

				summaryPrompt := fmt.Sprintf(`Your previous response was cut off due to length limits. Please provide a complete, concise summary of what you were explaining, staying within %d tokens. Focus on the key points and main conclusions.`, a.cfg.MaxTokens)

				summaryMsgs := append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(summaryPrompt)))
				summaryParams := anthropic.MessageNewParams{
					Model:     a.cfg.Model,
					MaxTokens: a.cfg.MaxTokens,
					Messages:  summaryMsgs,
					Tools:     tools,
				}
				if a.cfg.System != "" {
					summaryParams.System = []anthropic.TextBlockParam{
						{Text: a.cfg.System},
					}
				}
				summaryResp, err := a.cfg.Client.Messages.New(ctx, summaryParams)
				if err != nil {
					if a.cfg.Logger != nil {
						a.cfg.Logger.Warn("failed to get summary after truncation", "error", err)
					}
					return nil, fmt.Errorf("response was truncated and summary request failed: %w", err)
				}

				var finalText string
				for _, blk := range summaryResp.Content {
					text := blk.AsText()
					if text.Text != "" {
						finalText += text.Text
						if output != nil {
							fmt.Fprint(output, text.Text)
						}
					}
				}
				if output != nil {
					fmt.Fprintln(output)
				}

				summaryResponse := anthropicResponse{resp: summaryResp}
				summaryMsg := summaryResponse.ToMessage()
				fullConversation = append(fullConversation, summaryMsg)

				return &RunResult{
					FinalText:        strings.TrimSpace(finalText),
					FullConversation: fullConversation,
				}, nil
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
				FinalText:        finalText,
				FullConversation: fullConversation,
			}, nil
		}

		if a.cfg.Logger != nil {
			if len(toolUses) > 1 {
				a.cfg.Logger.Info("agent: found multiple tool calls, executing in parallel", "round", roundNum, "count", len(toolUses))
			} else {
				a.cfg.Logger.Info("agent: found tool call to execute", "round", roundNum, "count", len(toolUses))
			}
			for i, tu := range toolUses {
				a.cfg.Logger.Debug("agent: executing tool", "round", roundNum, "index", i+1, "total", len(toolUses), "name", tu.Name)
			}
		}

		maxToolResultLen := a.cfg.MaxToolResultLen
		if maxToolResultLen == 0 {
			maxToolResultLen = defaultMaxToolResultLen
		}
		toolResults, err := executeAnthropicTools(ctx, mcpClient, toolUses, maxToolResultLen, a.cfg.Logger)
		if err != nil {
			return nil, fmt.Errorf("failed to execute tools: %w", err)
		}

		if a.cfg.Logger != nil {
			a.cfg.Logger.Debug("agent: sending tool results back to anthropic")
		}

		toolResultMsg := anthropic.NewUserMessage(toolResults...)
		msgs = append(msgs, toolResultMsg)
		fullConversation = append(fullConversation, anthropicMessage{msg: toolResultMsg})
		toolResultIndices = append(toolResultIndices, len(msgs)-1)

		// Optionally trim old tool results to manage context window size
		if a.cfg.KeepToolResultsRounds > 0 && len(toolResultIndices) > a.cfg.KeepToolResultsRounds {
			msgs, toolResultIndices = trimOldToolResults(msgs, toolResultIndices, a.cfg.KeepToolResultsRounds)
		}

		// If this was the last round and we executed tools, add a finalization prompt
		if round == a.cfg.MaxRounds-1 {
			if a.cfg.Logger != nil {
				a.cfg.Logger.Info("agent: last round completed with tool calls, adding finalization prompt")
			}

			finalizationPrompt := `This is your final response in this turn. You can't run additional data queries right now, so base your answer on what's already known. If any checks couldn't be refreshed, state that clearly and invite a follow-up for the latest data. Keep the response concise, factual, and decision-oriented.`

			finalMsgs := append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(finalizationPrompt)))
			fullConversation = append(fullConversation, anthropicMessage{msg: anthropic.NewUserMessage(anthropic.NewTextBlock(finalizationPrompt))})

			finalParams := anthropic.MessageNewParams{
				Model:     a.cfg.Model,
				MaxTokens: a.cfg.MaxTokens,
				Messages:  finalMsgs,
				Tools:     tools,
			}
			if a.cfg.System != "" {
				finalParams.System = []anthropic.TextBlockParam{
					{Text: a.cfg.System},
				}
			}

			finalResp, err := a.cfg.Client.Messages.New(ctx, finalParams)
			if err != nil {
				if a.cfg.Logger != nil {
					a.cfg.Logger.Warn("failed to get final response after finalization prompt", "error", err)
				}
				return nil, fmt.Errorf("exceeded maximum rounds (%d) and final response request failed: %w", a.cfg.MaxRounds, err)
			}

			finalResponse := anthropicResponse{resp: finalResp}
			finalMsg := finalResponse.ToMessage()
			fullConversation = append(fullConversation, finalMsg)

			var finalText string
			for _, blk := range finalResponse.Content() {
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
			}, nil
		}
	}

	return nil, fmt.Errorf("exceeded maximum rounds (%d)", a.cfg.MaxRounds)
}

// toAnthropicTools converts MCP tools to Anthropic tool parameters.
func toAnthropicTools(tools []client.Tool) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		props, _ := t.InputSchema["properties"].(map[string]any)
		required, _ := t.InputSchema["required"].([]string)
		toolParam := anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.Opt(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Type:       "object",
				Properties: props,
				Required:   required,
			},
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &toolParam})
	}
	return out
}

// executeAnthropicTools executes MCP tools in parallel and returns Anthropic tool result blocks.
func executeAnthropicTools(ctx context.Context, mcpClient *client.Client, toolUses []ToolUse, maxLen int, logger *slog.Logger) ([]anthropic.ContentBlockParamUnion, error) {
	if len(toolUses) == 0 {
		return nil, nil
	}

	// Execute tools in parallel for better performance
	type toolResult struct {
		index   int
		id      string
		out     string
		isErr   bool
		callErr error
	}

	results := make([]toolResult, len(toolUses))
	var wg sync.WaitGroup

	for i, tu := range toolUses {
		wg.Add(1)
		go func(idx int, toolUse ToolUse) {
			defer wg.Done()
			out, isErr, callErr := mcpClient.CallToolText(ctx, toolUse.Name, toolUse.Input)
			results[idx] = toolResult{
				index:   idx,
				id:      toolUse.ID,
				out:     out,
				isErr:   isErr,
				callErr: callErr,
			}
		}(i, tu)
	}

	wg.Wait()

	// Process results in order and apply truncation
	toolResults := make([]anthropic.ContentBlockParamUnion, 0, len(toolUses))
	for i, result := range results {
		out := result.out
		isErr := result.isErr

		if result.callErr != nil {
			out = fmt.Sprintf("%s\n(error: %v)", out, result.callErr)
			isErr = true
		}

		// Use index to get tool name directly (results[i] corresponds to toolUses[i])
		var toolName string
		if i < len(toolUses) && toolUses[i].ID == result.id {
			toolName = toolUses[i].Name
		} else {
			// Fallback: search by ID if index doesn't match (shouldn't happen, but be safe)
			for _, tu := range toolUses {
				if tu.ID == result.id {
					toolName = tu.Name
					break
				}
			}
		}

		effectiveMaxLen := maxLen
		if isSchemaTool(toolName) && maxLen > 0 {
			effectiveMaxLen = maxLen * 2
		}

		originalLen := len(out)
		if effectiveMaxLen > 0 && originalLen > effectiveMaxLen {
			truncated, err := truncateToolResult(out, toolName, effectiveMaxLen)
			if err != nil {
				if logger != nil {
					logger.Debug("smart truncation failed, using simple truncation", "tool", toolName, "error", err)
				}
				truncated = out[:effectiveMaxLen]
				out = fmt.Sprintf("%s\n\n[Result truncated from %d to %d characters to avoid token limits]", truncated, originalLen, effectiveMaxLen)
			} else {
				out = truncated
			}
			if logger != nil {
				logger.Warn("truncated large tool result", "tool", toolName, "original_len", originalLen, "truncated_len", len(out))
			}
		}

		toolResults = append(toolResults, anthropic.NewToolResultBlock(result.id, out, isErr))
	}
	return toolResults, nil
}

func truncateToolResult(result string, toolName string, maxLen int) (string, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		return truncateAtBoundary(result, maxLen), nil
	}

	switch {
	case isSchemaTool(toolName):
		return truncateListTables(data, maxLen)
	case toolName == "query":
		return truncateQueryResult(data, maxLen)
	default:
		return truncateGenericJSON(data, maxLen)
	}
}

func truncateListTables(data map[string]any, maxLen int) (string, error) {
	tables, ok := data["tables"].([]any)
	if !ok {
		return truncateGenericJSON(data, maxLen)
	}

	baseData := map[string]any{"tables": []any{}}
	baseJSON, _ := json.Marshal(baseData)
	baseSize := len(baseJSON) - 2

	availableLen := maxLen - baseSize - truncationNoticeEstimate
	if availableLen < minAvailableLen {
		return truncateGenericJSON(data, maxLen)
	}

	truncatedTables := make([]any, 0)
	currentLen := 0
	truncatedCount := 0

	for i, table := range tables {
		tableJSON, err := json.Marshal(table)
		if err != nil {
			continue
		}
		estimatedSize := len(tableJSON) + 2

		if currentLen+estimatedSize > availableLen && len(truncatedTables) > 0 {
			truncatedCount = len(tables) - i
			break
		}

		truncatedTables = append(truncatedTables, table)
		currentLen += estimatedSize
	}

	data["tables"] = truncatedTables
	resultJSON, err := json.Marshal(data)
	if err != nil {
		return truncateGenericJSON(data, maxLen)
	}

	result := string(resultJSON)
	if truncatedCount > 0 {
		notice := formatTruncationNotice("tables", len(truncatedTables), len(truncatedTables)+truncatedCount)
		// If notice would push us over, truncate more aggressively
		if len(result)+len(notice) > maxLen {
			// Need to remove some tables to fit the notice
			for len(truncatedTables) > 0 && len(result)+len(notice) > maxLen {
				truncatedTables = truncatedTables[:len(truncatedTables)-1]
				truncatedCount++
				data["tables"] = truncatedTables
				resultJSON, _ = json.Marshal(data)
				result = string(resultJSON)
			}
		}
		result += notice
	}

	// Final safety check - if still too long, fall back to generic truncation
	if len(result) > maxLen {
		return truncateGenericJSON(data, maxLen)
	}

	return result, nil
}

// truncateQueryResult truncates query results after complete rows.
func truncateQueryResult(data map[string]any, maxLen int) (string, error) {
	rows, ok := data["rows"].([]any)
	if !ok {
		return truncateGenericJSON(data, maxLen)
	}

	// Calculate base JSON structure size (wrapper around rows array + other fields)
	baseData := map[string]any{"rows": []any{}}
	// Preserve other fields like columns, count
	for k, v := range data {
		if k != "rows" {
			baseData[k] = v
		}
	}
	baseJSON, _ := json.Marshal(baseData)
	baseSize := len(baseJSON) - 2 // Subtract "[]" to get just the wrapper

	availableLen := maxLen - baseSize - truncationNoticeEstimate
	if availableLen < minAvailableLen {
		return truncateGenericJSON(data, maxLen)
	}

	truncatedRows := make([]any, 0)
	currentLen := 0
	truncatedCount := 0

	for i, row := range rows {
		rowJSON, err := json.Marshal(row)
		if err != nil {
			continue
		}
		estimatedSize := len(rowJSON) + 2

		if currentLen+estimatedSize > availableLen && len(truncatedRows) > 0 {
			truncatedCount = len(rows) - i
			break
		}

		truncatedRows = append(truncatedRows, row)
		currentLen += estimatedSize
	}

	data["rows"] = truncatedRows
	if count, ok := data["count"].(float64); ok {
		data["count"] = int(count)
	}

	resultJSON, err := json.Marshal(data)
	if err != nil {
		return truncateGenericJSON(data, maxLen)
	}

	result := string(resultJSON)
	if truncatedCount > 0 {
		notice := formatTruncationNotice("rows", len(truncatedRows), len(truncatedRows)+truncatedCount)
		if len(result)+len(notice) > maxLen {
			for len(truncatedRows) > 0 && len(result)+len(notice) > maxLen {
				truncatedRows = truncatedRows[:len(truncatedRows)-1]
				truncatedCount++
				data["rows"] = truncatedRows
				resultJSON, _ = json.Marshal(data)
				result = string(resultJSON)
			}
		}
		result += notice
	}

	if len(result) > maxLen {
		return truncateGenericJSON(data, maxLen)
	}

	return result, nil
}

func truncateGenericJSON(data map[string]any, maxLen int) (string, error) {
	resultJSON, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	if len(resultJSON) <= maxLen {
		return string(resultJSON), nil
	}

	return truncateAtBoundary(string(resultJSON), maxLen), nil
}

func truncateAtBoundary(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}

	cutoff := maxLen - truncationNoticeEstimate
	if cutoff < 0 {
		cutoff = maxLen / 2
	}

	bestBoundary := cutoff
	searchWindow := truncationSearchWindow
	if cutoff < searchWindow {
		searchWindow = cutoff
	}

	for i := cutoff; i > cutoff-searchWindow && i > 0; i-- {
		if text[i] == '\n' || text[i] == '}' || text[i] == ']' {
			bestBoundary = i + 1
			break
		}
		if bestBoundary == cutoff && (text[i] == ',' || text[i] == ' ') {
			bestBoundary = i + 1
		}
	}

	truncated := text[:bestBoundary]
	originalLen := len(text)
	truncationNotice := fmt.Sprintf("\n\n[Result truncated from %d to %d characters to avoid token limits]", originalLen, len(truncated))

	if len(truncated)+len(truncationNotice) > maxLen {
		cutoff = maxLen - len(truncationNotice)
		if cutoff > 0 {
			truncated = text[:cutoff]
		}
	}

	return truncated + truncationNotice
}

// listToolsWithRetry attempts to list tools with exponential backoff retry for connection errors.
func (a *AnthropicAgent) listToolsWithRetry(ctx context.Context, mcpClient *client.Client) ([]client.Tool, error) {
	var mcpTools []client.Tool
	var err error
	retryDelay := defaultRetryDelay

	for attempt := 0; attempt < defaultListToolsRetries; attempt++ {
		if attempt > 0 {
			if a.cfg.Logger != nil {
				a.cfg.Logger.Debug("retrying tool list after connection error", "attempt", attempt+1, "max_retries", defaultListToolsRetries)
			}
			time.Sleep(retryDelay)
			retryDelay *= 2
		}

		mcpTools, err = mcpClient.ListTools(ctx)
		if err == nil {
			return mcpTools, nil
		}

		errStr := err.Error()
		isConnectionError := strings.Contains(errStr, "connection closed") ||
			strings.Contains(errStr, "EOF") ||
			strings.Contains(errStr, "client is closing") ||
			strings.Contains(errStr, "broken pipe") ||
			strings.Contains(errStr, "connection reset")

		if !isConnectionError || attempt == defaultListToolsRetries-1 {
			return nil, fmt.Errorf("failed to get tools: %w", err)
		}
	}

	return nil, fmt.Errorf("failed to get tools after %d attempts: %w", defaultListToolsRetries, err)
}

// isSchemaTool returns true if the tool name is a schema tool that gets 2x truncation limit.
func isSchemaTool(toolName string) bool {
	return toolName == schemaToolDoublezero ||
		toolName == schemaToolDoublezeroTelemetry ||
		toolName == schemaToolSolana
}

// formatTruncationNotice creates a truncation notice message.
func formatTruncationNotice(itemType string, shown, total int) string {
	return fmt.Sprintf("\n\n[Result truncated: showing %d of %d %s to avoid token limits]", shown, total, itemType)
}

// trimOldToolResults trims old tool result rounds from the message history.
// It keeps the initial messages and the last keepRounds of tool results.
func trimOldToolResults(msgs []anthropic.MessageParam, toolResultIndices []int, keepRounds int) ([]anthropic.MessageParam, []int) {
	if len(toolResultIndices) <= keepRounds {
		return msgs, toolResultIndices
	}

	// Find the cutoff point: keep the last N tool result rounds
	// Each tool result is preceded by an assistant message, so we need to keep that too
	cutoffToolResultIndex := toolResultIndices[len(toolResultIndices)-keepRounds]
	// The assistant message with tool_use comes before the tool_result user message
	// So we keep from (cutoffToolResultIndex - 1) to preserve the assistant-tool_result pair
	cutoffIndex := cutoffToolResultIndex - 1
	if cutoffIndex < 0 {
		cutoffIndex = 0
	}

	// Keep initial messages (before first tool result) + recent messages
	firstToolResultIndex := toolResultIndices[0]
	firstAssistantIndex := firstToolResultIndex - 1
	if firstAssistantIndex < 0 {
		firstAssistantIndex = 0
	}

	trimmed := append(msgs[:firstAssistantIndex], msgs[cutoffIndex:]...)

	// Update indices for remaining tool results
	removedCount := cutoffIndex - firstAssistantIndex
	for i := range toolResultIndices {
		toolResultIndices[i] -= removedCount
	}
	toolResultIndices = toolResultIndices[len(toolResultIndices)-keepRounds:]

	return trimmed, toolResultIndices
}
