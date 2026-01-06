//go:build evals

package evals_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/joho/godotenv"
	"github.com/malbeclabs/doublezero/lake/pkg/agent"
	"github.com/malbeclabs/doublezero/lake/pkg/agent/prompts"
	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
	"github.com/malbeclabs/doublezero/lake/pkg/agent/tools"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	"github.com/malbeclabs/doublezero/lake/pkg/querier"
	"github.com/stretchr/testify/require"
)

func init() {
	possiblePaths := []string{".env"}

	for _, path := range possiblePaths {
		if err := godotenv.Load(path); err == nil {
			break
		}
	}
}

// isDevContainer detects if the code is running inside a devcontainer
// by checking for the DIND_LOCALHOST environment variable set in the devcontainer Dockerfile
func isDevContainer() bool {
	return os.Getenv("DIND_LOCALHOST") != ""
}

func testLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func testDB(t *testing.T) duck.DB {
	db, err := duck.NewDB(context.Background(), "", testLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
	})
	return db
}

func testQuerier(t *testing.T, db duck.DB) *querier.Querier {
	q, err := querier.New(querier.Config{
		Logger: testLogger(t),
		DB:     db,
	})
	require.NoError(t, err)
	return q
}

// loadMigration loads a migration file from the filesystem
// It looks for migrations relative to the workspace root (where lake/migrations exists)
func loadMigration(filename string) (string, error) {
	// Try multiple possible paths
	possiblePaths := []string{
		filepath.Join("lake", "migrations", filename),                         // From workspace root
		filepath.Join("..", "..", "..", "..", "lake", "migrations", filename), // From lake/pkg/agent/evals
		filepath.Join("..", "..", "..", "migrations", filename),               // From lake/pkg
		filepath.Join("..", "..", "migrations", filename),                     // From lake/pkg/agent
	}

	for _, path := range possiblePaths {
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data), nil
		}
	}

	// If none worked, return the last error
	return "", os.ErrNotExist
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// executeSQLStatements executes SQL statements split by semicolons
func executeSQLStatements(t *testing.T, ctx context.Context, conn duck.Connection, sql string) {
	// Split by semicolon, but be careful with semicolons inside strings/comments
	statements := strings.Split(sql, ";")
	for i, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}
		_, err := conn.ExecContext(ctx, stmt)
		if err != nil {
			// Log more context about which statement failed
			stmtPreview := stmt
			if len(stmtPreview) > 200 {
				stmtPreview = stmtPreview[:200] + "..."
			}
			t.Logf("Failed statement %d: %s", i+1, stmtPreview)
		}
		require.NoError(t, err, "Failed to execute SQL statement %d: %s", i+1, stmt[:min(200, len(stmt))])
	}
}

// loadTablesAndViews loads and executes the table and view creation migrations
func loadTablesAndViews(t *testing.T, ctx context.Context, conn duck.Connection) {
	// Load and execute table creation migration
	tablesSQL, err := loadMigration("00000001-create-tables.sql")
	require.NoError(t, err, "Failed to load tables migration")
	executeSQLStatements(t, ctx, conn, tablesSQL)

	// Load and execute views creation migration
	viewsSQL, err := loadMigration("00000001-create-views.sql")
	require.NoError(t, err, "Failed to load views migration")
	executeSQLStatements(t, ctx, conn, viewsSQL)
}

// ollamaEvaluateResponse uses a local Ollama instance to evaluate if the response correctly answers the question.
// Returns true if the response is evaluated as correct, false otherwise.
// If Ollama is not available, returns an error indicating the service is unavailable.
func ollamaEvaluateResponse(t *testing.T, ctx context.Context, question, response string) (bool, error) {
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		// Detect if running in a devcontainer and use DIND_LOCALHOST hostname
		if dindHost := os.Getenv("DIND_LOCALHOST"); dindHost != "" {
			ollamaURL = fmt.Sprintf("http://%s:11434", dindHost)
		} else {
			ollamaURL = "http://localhost:11434"
		}
	}

	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		// model = "llama3.1:8b"
		model = "qwen2.5:7b-instruct"
	}

	// Create evaluation prompt
	evalPrompt := fmt.Sprintf(`You are evaluating whether an AI agent's response correctly answers a user's question.

Question: %s

Agent's Response:
%s

Does the agent's response correctly answer the question? Consider:
- Does it directly address what was asked?
- Is the information relevant and accurate?
- Is it complete enough to be useful?

Respond with only "YES" or "NO" followed by a brief explanation.`, question, response)

	// Prepare request
	reqBody := map[string]interface{}{
		"model":  model,
		"prompt": evalPrompt,
		"stream": false,
	}

	jsonData, err := json.Marshal(reqBody)
	require.NoError(t, err)

	// Make request with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "POST", ollamaURL+"/api/generate", bytes.NewBuffer(jsonData))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to connect to Ollama at %s: %w", ollamaURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("Ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response - Ollama may return streaming or non-streaming
	var ollamaResp struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
	}

	// Try to decode as single response first
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read Ollama response: %w", err)
	}

	// Try parsing as single JSON object
	if err := json.Unmarshal(bodyBytes, &ollamaResp); err == nil && ollamaResp.Done {
		// Successfully parsed as single response
	} else {
		// Try parsing as streaming response (newline-delimited JSON)
		var fullResponse strings.Builder
		lines := strings.Split(string(bodyBytes), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var streamChunk struct {
				Response string `json:"response"`
				Done     bool   `json:"done"`
			}
			if err := json.Unmarshal([]byte(line), &streamChunk); err != nil {
				continue
			}
			fullResponse.WriteString(streamChunk.Response)
			if streamChunk.Done {
				ollamaResp.Done = true
				break
			}
		}
		ollamaResp.Response = fullResponse.String()
	}

	// Parse evaluation result
	evalText := strings.ToUpper(strings.TrimSpace(ollamaResp.Response))
	originalResponse := strings.TrimSpace(ollamaResp.Response)

	// Helper function to extract and log explanation
	extractAndLogExplanation := func(prefix string, verdict string) string {
		explanation := originalResponse
		prefixUpper := strings.ToUpper(prefix)
		if strings.HasPrefix(strings.ToUpper(explanation), prefixUpper) {
			prefixIdx := len(prefix)
			if len(explanation) > prefixIdx {
				explanation = strings.TrimSpace(explanation[prefixIdx:])
				// Remove common separators like ":" or "-" after prefix
				explanation = strings.TrimLeft(explanation, ":-\t ")
			}
		}
		if explanation != "" {
			t.Logf("Ollama evaluation explanation (%s): %s", verdict, explanation)
		} else {
			// If no explanation found, log the full response for debugging
			t.Logf("Ollama evaluation said %s but no explanation found. Full response: %s", verdict, originalResponse)
		}
		return explanation
	}

	if strings.HasPrefix(evalText, "YES") {
		extractAndLogExplanation("YES", "YES")
		return true, nil
	} else if strings.HasPrefix(evalText, "NO") {
		extractAndLogExplanation("NO", "NO")
		return false, nil
	}

	// If we can't parse clearly, check if response contains positive indicators
	if strings.Contains(evalText, "CORRECT") || strings.Contains(evalText, "YES") || strings.Contains(evalText, "ACCURATE") {
		return true, nil
	}

	// Default to false if unclear
	t.Logf("Ollama evaluation response was unclear: %s", ollamaResp.Response)
	return false, nil
}

// LLMClientFactory creates an LLM client for testing
type LLMClientFactory func(t *testing.T, systemPrompt string) react.LLMClient

// setupAgent creates an agent instance with the given LLM client factory
func setupAgent(t *testing.T, ctx context.Context, db duck.DB, llmFactory LLMClientFactory, debug bool, debugLevel int, cfg *react.Config) *agent.Agent {
	if cfg == nil {
		cfg = &react.Config{}
	}

	// Create logger with appropriate level
	var logger *slog.Logger
	if debug {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	} else {
		logger = testLogger(t)
	}

	// Set up querier with the same database
	q := testQuerier(t, db)

	// Load prompts
	prompts, err := prompts.Load()
	require.NoError(t, err)

	// Build system prompt
	systemPrompt := prompts.BuildSystemPrompt()

	// Create LLM client using factory
	baseLLMClient := llmFactory(t, systemPrompt)

	// Wrap LLM client with debug logging if DEBUG is set
	var llmClient react.LLMClient = baseLLMClient
	if debug {
		llmClient = &debugLLMClient{
			LLMClient:  baseLLMClient,
			t:          t,
			debugLevel: debugLevel,
		}
	}

	// Create tool client
	baseToolClient := tools.NewQuerierToolClient(&querierAdapter{querier: q})

	// Wrap tool client with debug logging if DEBUG is set
	var toolClient react.ToolClient = baseToolClient
	if debug {
		toolClient = &debugToolClient{
			ToolClient: baseToolClient,
			t:          t,
			debugLevel: debugLevel,
		}
	}

	// Create react agent
	cfg.Logger = logger
	cfg.LLM = llmClient
	cfg.ToolClient = toolClient
	cfg.FinalizationPrompt = prompts.Finalization
	reactAgent, err := react.NewAgent(cfg)
	require.NoError(t, err)

	// Create agent
	return agent.NewAgent(&agent.AgentConfig{
		ReactAgent: reactAgent,
	})
}

// getDebugLevel parses the DEBUG environment variable
func getDebugLevel() (int, bool) {
	debugLevel := 0
	debugEnv := os.Getenv("DEBUG")
	switch debugEnv {
	case "1", "true", "TRUE":
		debugLevel = 1
	case "2":
		debugLevel = 2
	}
	return debugLevel, debugLevel > 0
}

// newAnthropicLLMClient creates an Anthropic LLM client for testing
func newAnthropicLLMClient(t *testing.T, systemPrompt string) react.LLMClient {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	require.NotEmpty(t, apiKey, "ANTHROPIC_API_KEY must be set for Anthropic tests")

	anthropicClient := anthropic.NewClient(option.WithAPIKey(apiKey))
	return react.NewAnthropicAgent(
		anthropicClient,
		anthropic.ModelClaudeHaiku4_5_20251001, // Use Haiku for faster/cheaper eval tests
		4000,
		systemPrompt,
	)
}

// querierAdapter adapts querier.Querier to tools.Querier interface
type querierAdapter struct {
	querier *querier.Querier
}

func (q *querierAdapter) Query(ctx context.Context, sql string) (querier.QueryResponse, error) {
	return q.querier.Query(ctx, sql)
}

// debugToolClient wraps a ToolClient to log all tool calls and results when DEBUG is enabled
type debugToolClient struct {
	react.ToolClient
	t          *testing.T
	debugLevel int
}

func (d *debugToolClient) CallToolText(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	// Log tool call
	var argsJSON []byte
	var argsStr string
	if d.debugLevel == 1 {
		// Compact: non-pretty JSON, truncated
		argsJSON, _ = json.Marshal(args)
		argsStr = truncate(string(argsJSON), 150)
	} else {
		// Full: pretty JSON
		argsJSON, _ = json.MarshalIndent(args, "  ", "  ")
		argsStr = string(argsJSON)
	}

	if d.debugLevel == 1 {
		d.t.Logf("🔧 %s %s", name, argsStr)
	} else {
		d.t.Logf("\n🔧 [TOOL CALL] %s\n  Args:\n%s\n", name, argsStr)
	}

	// Call the actual tool
	result, isErr, err := d.ToolClient.CallToolText(ctx, name, args)

	// Log tool result
	resultTruncLen := 100
	if d.debugLevel == 2 {
		resultTruncLen = 500
	}

	// Format result for logging
	resultToLog := result
	if d.debugLevel == 1 && !isErr && err == nil {
		// In compact mode, try to compact JSON results
		resultToLog = compactJSON(result)
	}

	if err != nil {
		if d.debugLevel == 1 {
			d.t.Logf("❌ %s: %v", name, err)
		} else {
			d.t.Logf("❌ [TOOL ERROR] %s: %v\n", name, err)
		}
	} else if isErr {
		if d.debugLevel == 1 {
			d.t.Logf("⚠️  %s: %s", name, truncate(resultToLog, resultTruncLen))
		} else {
			d.t.Logf("⚠️  [TOOL RESULT] %s (error): %s\n", name, truncate(resultToLog, resultTruncLen))
		}
	} else {
		if d.debugLevel == 1 {
			d.t.Logf("✅ %s: %s", name, truncate(resultToLog, resultTruncLen))
		} else {
			d.t.Logf("✅ [TOOL RESULT] %s: %s\n", name, truncate(resultToLog, resultTruncLen))
		}
	}

	return result, isErr, err
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}

// compactJSON attempts to compact pretty-printed JSON by parsing and re-marshaling without indentation.
// If parsing fails, returns the original string.
func compactJSON(s string) string {
	// Try to parse as JSON
	var jsonData interface{}
	if err := json.Unmarshal([]byte(s), &jsonData); err != nil {
		// Not valid JSON, return as-is
		return s
	}
	// Re-marshal without indentation
	compact, err := json.Marshal(jsonData)
	if err != nil {
		return s
	}
	return string(compact)
}

// debugLLMClient wraps an LLMClient to log all responses when DEBUG is enabled
type debugLLMClient struct {
	react.LLMClient
	t          *testing.T
	debugLevel int
}

func (d *debugLLMClient) Call(ctx context.Context, messages []react.Message, tools []react.Tool) (react.Response, error) {
	// Log that we're calling the LLM
	if d.debugLevel == 1 {
		d.t.Logf("🤖 LLM call (%d msgs)", len(messages))
	} else {
		d.t.Logf("\n🤖 [CALLING LLM] (message count: %d)\n", len(messages))
	}

	// Call the actual LLM
	response, err := d.LLMClient.Call(ctx, messages, tools)
	if err != nil {
		if d.debugLevel == 1 {
			d.t.Logf("❌ LLM error: %v", err)
		} else {
			d.t.Logf("❌ [LLM ERROR]: %v\n", err)
		}
		return nil, err
	}

	// Log the response
	content := response.Content()
	var textParts []string
	var toolCalls []string
	for _, blk := range content {
		if text, ok := blk.AsText(); ok && text != "" {
			textParts = append(textParts, text)
		}
		if id, name, inputBytes, ok := blk.AsToolUse(); ok {
			var input map[string]any
			json.Unmarshal(inputBytes, &input)
			var inputStr string
			if d.debugLevel == 1 {
				// Compact: non-pretty JSON, truncated
				inputJSON, _ := json.Marshal(input)
				inputStr = truncate(string(inputJSON), 100)
			} else {
				// Full: pretty JSON
				inputJSON, _ := json.MarshalIndent(input, "    ", "  ")
				inputStr = string(inputJSON)
			}
			if d.debugLevel == 1 {
				toolCalls = append(toolCalls, fmt.Sprintf("  %s(%s)", name, inputStr))
			} else {
				toolCalls = append(toolCalls, fmt.Sprintf("  - %s (id: %s)\n    Args:\n%s", name, id, inputStr))
			}
		}
	}

	textTruncLen := 300
	if d.debugLevel == 2 {
		textTruncLen = 2000
	}

	if len(textParts) > 0 {
		combinedText := ""
		for _, text := range textParts {
			combinedText += text
		}
		if d.debugLevel == 1 {
			d.t.Logf("🤖 Response: %s", truncate(combinedText, textTruncLen))
		} else {
			d.t.Logf("\n🤖 [ASSISTANT RESPONSE]\n%s\n", truncate(combinedText, textTruncLen))
		}
	}
	if len(toolCalls) > 0 {
		if d.debugLevel == 1 {
			d.t.Logf("🔧 Tools: %s", strings.Join(toolCalls, ", "))
		} else {
			d.t.Logf("🔧 [TOOL CALLS IN RESPONSE]\n%s\n", strings.Join(toolCalls, "\n"))
		}
	}

	return response, nil
}

func (d *debugLLMClient) ConvertToMessage(msg any) react.Message {
	return d.LLMClient.ConvertToMessage(msg)
}

func (d *debugLLMClient) ConvertToolResults(toolUses []react.ToolUse, results []react.ToolResult) ([]react.Message, error) {
	return d.LLMClient.ConvertToolResults(toolUses, results)
}

// truncateForError truncates a string for use in error messages
func truncateForError(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
