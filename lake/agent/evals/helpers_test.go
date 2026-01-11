//go:build evals

package evals_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/agent"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/prompts"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/react"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/tools"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/sol"
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

func testLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// testTime returns a test timestamp truncated to milliseconds
func testTime() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}

// testOpID returns a new UUID for testing
func testOpID() uuid.UUID {
	return uuid.New()
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
func executeSQLStatements(t *testing.T, ctx context.Context, conn clickhouse.Connection, sql string) {
	// Split by semicolon, but be careful with semicolons inside strings/comments
	statements := strings.Split(sql, ";")
	for i, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}
		err := conn.Exec(ctx, stmt)
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
func loadTablesAndViews(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Load and execute table creation migration
	tablesSQL, err := loadMigration("00000001-create-tables.sql")
	require.NoError(t, err, "Failed to load tables migration")
	executeSQLStatements(t, ctx, conn, tablesSQL)

	// Load and execute views creation migration
	viewsSQL, err := loadMigration("00000001-create-views.sql")
	require.NoError(t, err, "Failed to load views migration")
	executeSQLStatements(t, ctx, conn, viewsSQL)
}

// OllamaExpectation represents a specific expectation for the ollama validator to check
type OllamaExpectation struct {
	// Description describes what should be present (e.g., "the number of newly connected validators")
	Description string
	// ExpectedValue is the expected value (e.g., "3")
	ExpectedValue string
	// Rationale explains why this value is expected (optional, helps the validator understand the context)
	Rationale string
}

// ollamaEvaluateResponse uses a local Ollama instance to evaluate if the response correctly answers the question.
// Returns true if the response is evaluated as correct, false otherwise.
// If Ollama is not available, returns an error indicating the service is unavailable.
func ollamaEvaluateResponse(t *testing.T, ctx context.Context, question, response string, expectations ...OllamaExpectation) (bool, error) {
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		// Detect if running in a devcontainer and use DIND_LOCALHOST hostname
		if dindHost := os.Getenv("DIND_LOCALHOST"); dindHost != "" {
			ollamaURL = fmt.Sprintf("http://%s:11434", dindHost)
		} else {
			ollamaURL = "http://localhost:11434"
		}
	}

	model := os.Getenv("OLLAMA_EVAL_MODEL")
	if model == "" {
		// Use llama3.1:8b for evaluation - fast and good comprehension
		// Evaluation is simpler than agent work (no tool calling, just YES/NO judgment)
		model = "llama3.1:8b"
	}

	// Build expectations section if provided
	var expectationsSection string
	if len(expectations) > 0 {
		var expectationLines []string
		for i, exp := range expectations {
			line := fmt.Sprintf("%d. %s: %s", i+1, exp.Description, exp.ExpectedValue)
			if exp.Rationale != "" {
				line += fmt.Sprintf(" (%s)", exp.Rationale)
			}
			expectationLines = append(expectationLines, line)
		}
		expectationsSection = fmt.Sprintf(`
CRITICAL - Expectations to verify (ALL must be present):
%s

The response MUST contain information matching each expectation above.
If ALL expectations are met, respond with "YES" even if the response contains additional relevant information.
Only respond with "NO" if one or more expectations are NOT met.
`, strings.Join(expectationLines, "\n"))
	}

	// Create evaluation prompt
	evalPrompt := fmt.Sprintf(`You are evaluating whether an AI agent's response correctly handles a user's question.

Question: %s

Agent's Response:
%s
%s
Evaluation criteria:
1. Does the response address the question appropriately?
2. Does the response contain all required information from the expectations?
3. Is the information accurate and relevant?

IMPORTANT: Including additional relevant context or details beyond the expectations is ACCEPTABLE and should NOT cause a "NO" verdict. A comprehensive answer is better than a minimal one.

Respond with only "YES" or "NO" followed by a brief explanation.`, question, response, expectationsSection)

	// Prepare request
	reqBody := map[string]any{
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
func setupAgent(t *testing.T, ctx context.Context, db clickhouse.Client, llmFactory LLMClientFactory, debug bool, debugLevel int, cfg *react.Config) *agent.Agent {
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
	baseToolClient := tools.NewClickhouseQueryTool(db)

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
	cfg.SummaryPrompt = prompts.Summary
	reactAgent, err := react.NewAgent(cfg)
	require.NoError(t, err)

	// Create agent
	return agent.NewAgent(&agent.AgentConfig{
		ReactAgent: reactAgent,
		LLMClient:  llmClient,
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

// newOllamaLLMClient creates an Ollama LLM client for testing
func newOllamaLLMClient(t *testing.T, systemPrompt string) react.LLMClient {
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		// Detect if running in a devcontainer and use DIND_LOCALHOST hostname
		if dindHost := os.Getenv("DIND_LOCALHOST"); dindHost != "" {
			ollamaURL = fmt.Sprintf("http://%s:11434", dindHost)
		} else {
			ollamaURL = "http://localhost:11434"
		}
	}

	model := os.Getenv("OLLAMA_AGENT_MODEL")
	if model == "" {
		// Use mistral-nemo - specifically optimized for tool calling in Ollama
		// Other models (llama3.1, qwen2.5) have known issues with tool calling
		model = "mistral-nemo"
	}

	return react.NewOllamaAgent(
		ollamaURL,
		model,
		4000,
		systemPrompt,
	)
}

// isOllamaAvailable checks if the local Ollama server is available
func isOllamaAvailable() bool {
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		if dindHost := os.Getenv("DIND_LOCALHOST"); dindHost != "" {
			ollamaURL = fmt.Sprintf("http://%s:11434", dindHost)
		} else {
			ollamaURL = "http://localhost:11434"
		}
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ollamaURL + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
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
	var jsonData any
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

// Helper functions for pointer creation
func int64Ptr(i int64) *int64 {
	return &i
}

func strPtr(s string) *string {
	return &s
}

func float64Ptr(f float64) *float64 {
	return &f
}

// Seed functions for dimension tables
func seedMetros(t *testing.T, ctx context.Context, conn clickhouse.Connection, metros []serviceability.Metro, snapshotTS, ingestedAt time.Time) {
	log := testLogger(t)
	metroDS, err := serviceability.NewMetroDataset(log)
	require.NoError(t, err)
	// Create schema instance to access ToRow
	var metroSchema serviceability.MetroSchema
	err = metroDS.WriteBatch(ctx, conn, len(metros), func(i int) ([]any, error) {
		return metroSchema.ToRow(metros[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: snapshotTS,
		OpID:       testOpID(),
	})
	require.NoError(t, err)
}

func seedDevices(t *testing.T, ctx context.Context, conn clickhouse.Connection, devices []serviceability.Device, snapshotTS, ingestedAt time.Time) {
	log := testLogger(t)
	deviceDS, err := serviceability.NewDeviceDataset(log)
	require.NoError(t, err)
	// Create schema instance to access ToRow
	var deviceSchema serviceability.DeviceSchema
	err = deviceDS.WriteBatch(ctx, conn, len(devices), func(i int) ([]any, error) {
		return deviceSchema.ToRow(devices[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: snapshotTS,
		OpID:       testOpID(),
	})
	require.NoError(t, err)
}

func seedUsers(t *testing.T, ctx context.Context, conn clickhouse.Connection, users []serviceability.User, snapshotTS, ingestedAt time.Time, opID uuid.UUID) {
	log := testLogger(t)
	userDS, err := serviceability.NewUserDataset(log)
	require.NoError(t, err)
	// Create schema instance to access ToRow
	var userSchema serviceability.UserSchema
	err = userDS.WriteBatch(ctx, conn, len(users), func(i int) ([]any, error) {
		return userSchema.ToRow(users[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: snapshotTS,
		OpID:       opID,
	})
	require.NoError(t, err)
}

// Test helper types for Solana entities (used by test files)
type testGossipNode struct {
	Pubkey      string
	GossipIP    net.IP
	GossipPort  int32
	TPUQUICIP   net.IP
	TPUQUICPort int32
	Version     string
	Epoch       uint64
}

type testVoteAccount struct {
	VotePubkey       string
	NodePubkey       string
	EpochVoteAccount bool
	Epoch            uint64
	ActivatedStake   int64
	Commission       int64
}

func seedGossipNodes(t *testing.T, ctx context.Context, conn clickhouse.Connection, nodes []*testGossipNode, snapshotTS, ingestedAt time.Time, opID uuid.UUID) {
	log := testLogger(t)
	gossipDS, err := sol.NewGossipNodeDataset(log)
	require.NoError(t, err)
	err = gossipDS.WriteBatch(ctx, conn, len(nodes), func(i int) ([]any, error) {
		node := nodes[i]
		// PK: pubkey, Payload: epoch, gossip_ip, gossip_port, tpuquic_ip, tpuquic_port, version
		return []any{
			node.Pubkey,
			int64(node.Epoch),
			node.GossipIP.String(),
			node.GossipPort,
			node.TPUQUICIP.String(),
			node.TPUQUICPort,
			node.Version,
		}, nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: snapshotTS,
		OpID:       opID,
	})
	require.NoError(t, err)
}

func seedVoteAccounts(t *testing.T, ctx context.Context, conn clickhouse.Connection, accounts []testVoteAccount, snapshotTS, ingestedAt time.Time, opID uuid.UUID) {
	log := testLogger(t)
	voteDS, err := sol.NewVoteAccountDataset(log)
	require.NoError(t, err)
	err = voteDS.WriteBatch(ctx, conn, len(accounts), func(i int) ([]any, error) {
		account := accounts[i]
		epochVoteAccountStr := "false"
		if account.EpochVoteAccount {
			epochVoteAccountStr = "true"
		}
		// PK: vote_pubkey, Payload: epoch, node_pubkey, activated_stake_lamports, epoch_vote_account, commission_percentage
		return []any{
			account.VotePubkey,
			int64(account.Epoch),
			account.NodePubkey,
			account.ActivatedStake,
			epochVoteAccountStr,
			account.Commission,
		}, nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: snapshotTS,
		OpID:       opID,
	})
	require.NoError(t, err)
}

func seedLinks(t *testing.T, ctx context.Context, conn clickhouse.Connection, links []serviceability.Link, snapshotTS time.Time, opID uuid.UUID) {
	log := testLogger(t)
	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	// Create schema instance to access ToRow
	var linkSchema serviceability.LinkSchema
	err = linkDS.WriteBatch(ctx, conn, len(links), func(i int) ([]any, error) {
		return linkSchema.ToRow(links[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: snapshotTS,
		OpID:       opID,
	})
	require.NoError(t, err)
}
