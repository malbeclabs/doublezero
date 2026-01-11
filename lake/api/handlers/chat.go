package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

type ChatMessage struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

type ChatRequest struct {
	Message string        `json:"message"`
	History []ChatMessage `json:"history"`
}

type QueryResult struct {
	SQL     string   `json:"sql"`
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	Error   string   `json:"error,omitempty"`
}

type ChatResponse struct {
	Response string        `json:"response"`
	Queries  []QueryResult `json:"queries,omitempty"`
	Error    string        `json:"error,omitempty"`
}

const maxToolIterations = 10

func Chat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Fetch schema for context
	schema, err := fetchSchema()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: "Failed to fetch schema: " + err.Error()})
		return
	}

	// Check if we should use Anthropic
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: "Chat requires ANTHROPIC_API_KEY to be set"})
		return
	}

	// Run the agentic chat loop with tool use
	response, queries, err := runChatAgentWithTools(schema, req.Message, req.History)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChatResponse{
		Response: response,
		Queries:  queries,
	})
}

func runChatAgentWithTools(schema, message string, history []ChatMessage) (string, []QueryResult, error) {
	client := anthropic.NewClient()

	systemPrompt := buildChatSystemPrompt(schema)

	// Define the run_query tool
	tools := []anthropic.ToolUnionParam{
		{
			OfTool: &anthropic.ToolParam{
				Name:        "run_query",
				Description: anthropic.String("Execute a SQL query against the ClickHouse database and return the results. Use this to fetch data needed to answer the user's question."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"sql": map[string]any{
							"type":        "string",
							"description": "The SQL query to execute. Must be valid ClickHouse SQL.",
						},
						"reason": map[string]any{
							"type":        "string",
							"description": "Brief explanation of why you're running this query.",
						},
					},
					Required: []string{"sql"},
					Type:     "object",
				},
			},
		},
	}

	// Build initial messages from history
	var messages []anthropic.MessageParam
	for _, msg := range history {
		if msg.Role == "user" {
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
		} else {
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)))
		}
	}
	messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(message)))

	var allQueries []QueryResult

	// Agentic loop - keep calling until no more tool use
	for i := 0; i < maxToolIterations; i++ {
		resp, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
			Model:     anthropic.ModelClaude3_5Haiku20241022,
			MaxTokens: 4096,
			System: []anthropic.TextBlockParam{
				{Type: "text", Text: systemPrompt},
			},
			Messages: messages,
			Tools:    tools,
		})

		if err != nil {
			return "", allQueries, fmt.Errorf("anthropic error: %w", err)
		}

		// Check if we're done (no tool use, just text response)
		if resp.StopReason == "end_turn" {
			// Extract final text response
			var finalResponse string
			for _, block := range resp.Content {
				if block.Type == "text" {
					finalResponse += block.Text
				}
			}
			return finalResponse, allQueries, nil
		}

		// Process tool calls
		var toolResults []anthropic.ContentBlockParamUnion
		var assistantContent []anthropic.ContentBlockParamUnion

		for _, block := range resp.Content {
			if block.Type == "text" {
				assistantContent = append(assistantContent, anthropic.NewTextBlock(block.Text))
			} else if block.Type == "tool_use" {
				assistantContent = append(assistantContent, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    block.ID,
						Name:  block.Name,
						Input: block.Input,
						Type:  "tool_use",
					},
				})

				if block.Name == "run_query" {
					// Parse the tool input
					var input struct {
						SQL    string `json:"sql"`
						Reason string `json:"reason"`
					}

					inputBytes, _ := json.Marshal(block.Input)
					json.Unmarshal(inputBytes, &input)

					// Execute the query
					result := executeQueryForChat(input.SQL)
					result.SQL = input.SQL
					allQueries = append(allQueries, result)

					// Format result for tool response
					var toolResponse string
					if result.Error != "" {
						toolResponse = fmt.Sprintf("Error: %s", result.Error)
					} else {
						toolResponse = formatQueryResult(result)
					}

					toolResults = append(toolResults, anthropic.NewToolResultBlock(block.ID, toolResponse, false))
				}
			}
		}

		// Add assistant message with tool use
		messages = append(messages, anthropic.MessageParam{
			Role:    "assistant",
			Content: assistantContent,
		})

		// Add tool results as user message
		if len(toolResults) > 0 {
			messages = append(messages, anthropic.MessageParam{
				Role:    "user",
				Content: toolResults,
			})
		}

		// If no tool calls were made, we're done
		if len(toolResults) == 0 {
			var finalResponse string
			for _, block := range resp.Content {
				if block.Type == "text" {
					finalResponse += block.Text
				}
			}
			return finalResponse, allQueries, nil
		}
	}

	return "I ran out of iterations while trying to answer your question. Here's what I found so far.", allQueries, nil
}

func formatQueryResult(result QueryResult) string {
	if len(result.Rows) == 0 {
		return "Query returned no results."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Results (%d rows):\n", len(result.Rows)))
	sb.WriteString("Columns: " + strings.Join(result.Columns, " | ") + "\n")
	sb.WriteString(strings.Repeat("-", 40) + "\n")

	// Limit output to first 50 rows for context
	maxRows := 50
	if len(result.Rows) < maxRows {
		maxRows = len(result.Rows)
	}

	for i := 0; i < maxRows; i++ {
		row := result.Rows[i]
		var values []string
		for _, v := range row {
			values = append(values, fmt.Sprintf("%v", v))
		}
		sb.WriteString(strings.Join(values, " | ") + "\n")
	}

	if len(result.Rows) > 50 {
		sb.WriteString(fmt.Sprintf("... and %d more rows\n", len(result.Rows)-50))
	}

	return sb.String()
}

func executeQueryForChat(sql string) QueryResult {
	sql = strings.TrimSuffix(strings.TrimSpace(sql), ";")
	query := sql + " FORMAT JSON"

	resp, err := http.Post(clickhouseURL+"/", "text/plain", strings.NewReader(query))
	if err != nil {
		return QueryResult{Error: "Failed to connect to database: " + err.Error()}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return QueryResult{Error: "Failed to read response: " + err.Error()}
	}

	if resp.StatusCode != http.StatusOK {
		return QueryResult{Error: strings.TrimSpace(string(body))}
	}

	var chResp struct {
		Meta []struct {
			Name string `json:"name"`
		} `json:"meta"`
		Data []map[string]any `json:"data"`
	}

	if err := json.Unmarshal(body, &chResp); err != nil {
		return QueryResult{Error: "Failed to parse response: " + err.Error()}
	}

	columns := make([]string, 0, len(chResp.Meta))
	for _, m := range chResp.Meta {
		columns = append(columns, m.Name)
	}

	rows := make([][]any, 0, len(chResp.Data))
	for _, row := range chResp.Data {
		rowData := make([]any, 0, len(columns))
		for _, col := range columns {
			rowData = append(rowData, row[col])
		}
		rows = append(rows, rowData)
	}

	return QueryResult{Columns: columns, Rows: rows}
}

func buildChatSystemPrompt(schema string) string {
	return `You are a data analyst assistant helping users explore and understand their ClickHouse database.

Available tables and their columns:
` + schema + `

Your job is to answer the user's questions about their data. You have access to a tool called "run_query" that lets you execute SQL queries against the database.

How to work:
1. When the user asks a question, think about what data you need
2. Use the run_query tool to fetch data - you can run multiple queries if needed
3. Analyze the results and provide a clear, helpful answer
4. Include relevant numbers, insights, and context in your response

Guidelines:
- Always use LIMIT clauses to avoid fetching too much data (start with LIMIT 100 or less)
- If a query fails, try to fix it and run again
- Format your final answer in markdown for readability
- Be concise but thorough in your explanations
- If you can't answer the question with the available data, explain why`
}
