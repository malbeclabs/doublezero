package slack

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
)

// APIClient is an HTTP client for the Lake API.
type APIClient struct {
	baseURL    string
	httpClient *http.Client
	log        *slog.Logger
}

// NewAPIClient creates a new API client.
func NewAPIClient(baseURL string, log *slog.Logger) *APIClient {
	return &APIClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // Long timeout for streaming responses
		},
		log: log,
	}
}

// ChatStreamResult holds the result from the chat stream API.
type ChatStreamResult struct {
	Answer          string
	Classification  workflow.Classification
	DataQuestions   []workflow.DataQuestion
	ExecutedQueries []workflow.ExecutedQuery
}

// chatRequest is the request body for POST /api/chat/stream.
type chatRequest struct {
	Message   string        `json:"message"`
	History   []chatMessage `json:"history"`
	SessionID string        `json:"session_id"`
	Format    string        `json:"format,omitempty"`
}

// chatMessage represents a message in the conversation history.
type chatMessage struct {
	Role            string   `json:"role"`
	Content         string   `json:"content"`
	ExecutedQueries []string `json:"executedQueries,omitempty"`
}

// doneEventData represents the data in a "done" SSE event.
type doneEventData struct {
	Answer          string `json:"answer"`
	DataQuestions   []struct {
		Question  string `json:"question"`
		Rationale string `json:"rationale"`
	} `json:"dataQuestions"`
	ExecutedQueries []struct {
		Question string   `json:"question"`
		SQL      string   `json:"sql"`
		Columns  []string `json:"columns"`
		Rows     [][]any  `json:"rows"`
		Count    int      `json:"count"`
		Error    string   `json:"error,omitempty"`
	} `json:"executedQueries"`
}

// errorEventData represents the data in an "error" SSE event.
type errorEventData struct {
	Error string `json:"error"`
}

// queryStartedEventData represents the data in a "query_started" SSE event.
type queryStartedEventData struct {
	Question string `json:"question"`
	SQL      string `json:"sql"`
}

// queryDoneEventData represents the data in a "query_done" SSE event.
type queryDoneEventData struct {
	Question string `json:"question"`
	SQL      string `json:"sql"`
	Rows     int    `json:"rows"`
	Error    string `json:"error"`
}

// ChatStream sends a message to the API and streams the response.
// It calls onProgress for each progress update and returns the final result.
func (c *APIClient) ChatStream(
	ctx context.Context,
	message string,
	history []workflow.ConversationMessage,
	onProgress func(workflow.Progress),
) (ChatStreamResult, error) {
	// Convert history to API format
	apiHistory := make([]chatMessage, 0, len(history))
	for _, msg := range history {
		apiHistory = append(apiHistory, chatMessage{
			Role:            msg.Role,
			Content:         msg.Content,
			ExecutedQueries: msg.ExecutedQueries,
		})
	}

	// Build request body
	reqBody := chatRequest{
		Message:   message,
		History:   apiHistory,
		SessionID: uuid.New().String(),
		Format:    "slack",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return ChatStreamResult{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat/stream", bytes.NewReader(bodyBytes))
	if err != nil {
		return ChatStreamResult{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ChatStreamResult{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return ChatStreamResult{}, fmt.Errorf("API error: %s (status %d)", string(body), resp.StatusCode)
	}

	// Parse SSE stream
	return c.parseSSEStream(ctx, resp.Body, onProgress)
}

// parseSSEStream reads the SSE stream and returns the result.
func (c *APIClient) parseSSEStream(
	ctx context.Context,
	body io.Reader,
	onProgress func(workflow.Progress),
) (ChatStreamResult, error) {
	reader := bufio.NewReader(body)
	var result ChatStreamResult
	var queriesTotal, queriesDone int
	var dataQuestions []workflow.DataQuestion

	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return result, fmt.Errorf("unexpected end of stream")
			}
			return result, fmt.Errorf("error reading stream: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse SSE event
		if eventType, ok := strings.CutPrefix(line, "event: "); ok {

			// Read the data line
			dataLine, err := reader.ReadString('\n')
			if err != nil {
				return result, fmt.Errorf("error reading data line: %w", err)
			}
			dataLine = strings.TrimSpace(dataLine)

			if !strings.HasPrefix(dataLine, "data: ") {
				c.log.Debug("unexpected line after event", "line", dataLine)
				continue
			}

			data := strings.TrimPrefix(dataLine, "data: ")

			switch eventType {
			case "workflow_started":
				// Just log, we don't need to track the workflow_id for reconnection
				c.log.Debug("workflow started")

			case "thinking":
				// Map thinking to classifying or synthesizing based on query count
				stage := workflow.StageClassifying
				if queriesTotal > 0 {
					stage = workflow.StageSynthesizing
				}
				onProgress(workflow.Progress{
					Stage:         stage,
					DataQuestions: dataQuestions,
					QueriesTotal:  queriesTotal,
					QueriesDone:   queriesDone,
				})

			case "query_started":
				var qsData queryStartedEventData
				if err := json.Unmarshal([]byte(data), &qsData); err != nil {
					c.log.Warn("failed to parse query_started", "error", err)
					continue
				}
				queriesTotal++
				// Add to data questions for display
				dataQuestions = append(dataQuestions, workflow.DataQuestion{
					Question: qsData.Question,
				})
				onProgress(workflow.Progress{
					Stage:         workflow.StageExecuting,
					DataQuestions: dataQuestions,
					QueriesTotal:  queriesTotal,
					QueriesDone:   queriesDone,
				})

			case "query_done":
				var qdData queryDoneEventData
				if err := json.Unmarshal([]byte(data), &qdData); err != nil {
					c.log.Warn("failed to parse query_done", "error", err)
					continue
				}
				queriesDone++
				onProgress(workflow.Progress{
					Stage:         workflow.StageExecuting,
					DataQuestions: dataQuestions,
					QueriesTotal:  queriesTotal,
					QueriesDone:   queriesDone,
				})

			case "done":
				var doneData doneEventData
				if err := json.Unmarshal([]byte(data), &doneData); err != nil {
					return result, fmt.Errorf("failed to parse done event: %w", err)
				}

				// Convert to result
				result.Answer = doneData.Answer
				if queriesTotal > 0 {
					result.Classification = workflow.ClassificationDataAnalysis
				} else {
					result.Classification = workflow.ClassificationConversational
				}

				// Convert data questions
				for _, dq := range doneData.DataQuestions {
					result.DataQuestions = append(result.DataQuestions, workflow.DataQuestion{
						Question:  dq.Question,
						Rationale: dq.Rationale,
					})
				}

				// Convert executed queries
				for _, eq := range doneData.ExecutedQueries {
					// Convert rows from array format back to map format
					rows := make([]map[string]any, 0, len(eq.Rows))
					for _, row := range eq.Rows {
						rowMap := make(map[string]any)
						for i, col := range eq.Columns {
							if i < len(row) {
								rowMap[col] = row[i]
							}
						}
						rows = append(rows, rowMap)
					}

					result.ExecutedQueries = append(result.ExecutedQueries, workflow.ExecutedQuery{
						GeneratedQuery: workflow.GeneratedQuery{
							DataQuestion: workflow.DataQuestion{Question: eq.Question},
							SQL:          eq.SQL,
						},
						Result: workflow.QueryResult{
							SQL:     eq.SQL,
							Columns: eq.Columns,
							Rows:    rows,
							Count:   eq.Count,
							Error:   eq.Error,
						},
					})
				}

				onProgress(workflow.Progress{
					Stage:          workflow.StageComplete,
					Classification: result.Classification,
					DataQuestions:  result.DataQuestions,
					QueriesTotal:   queriesTotal,
					QueriesDone:    queriesDone,
				})

				return result, nil

			case "error":
				var errData errorEventData
				if err := json.Unmarshal([]byte(data), &errData); err != nil {
					return result, fmt.Errorf("failed to parse error event: %w", err)
				}
				return result, fmt.Errorf("API error: %s", errData.Error)

			case "heartbeat":
				// Ignore heartbeat events
				continue

			default:
				c.log.Debug("unknown event type", "type", eventType)
			}
		}
	}
}
