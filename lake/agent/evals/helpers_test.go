//go:build evals

package evals_test

import (
	"bufio"
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
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
	v1 "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/v1"
	v2 "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/v2"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/sol"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
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

// Expectation represents a specific expectation for the evaluator to check
type Expectation struct {
	// Description describes what should be present (e.g., "the number of newly connected validators")
	Description string
	// ExpectedValue is the expected value (e.g., "3")
	ExpectedValue string
	// Rationale explains why this value is expected (optional, helps the validator understand the context)
	Rationale string
}

// evaluateResponse uses Anthropic Haiku to evaluate if the response correctly answers the question.
// Returns true if the response is evaluated as correct, false otherwise.
func evaluateResponse(t *testing.T, ctx context.Context, question, response string, expectations ...Expectation) (bool, error) {
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
	// Include current date so the evaluator doesn't think recent dates are "future dates"
	currentDate := time.Now().UTC().Format("January 2, 2006")
	evalPrompt := fmt.Sprintf(`You are evaluating whether an AI agent's response correctly handles a user's question.

Current date: %s

Question: %s

Agent's Response:
%s
%s
Evaluation criteria:
1. Does the response address the question appropriately?
2. Does the response contain all required information from the expectations?
3. Is the information accurate and relevant?

IMPORTANT: Including additional relevant context or details beyond the expectations is ACCEPTABLE and should NOT cause a "NO" verdict. A comprehensive answer is better than a minimal one.

Respond with only "YES" or "NO" followed by a brief explanation.`, currentDate, question, response, expectationsSection)

	// Use Anthropic Haiku for evaluation - fast and reliable
	llmClient := pipeline.NewAnthropicLLMClientWithName(
		anthropic.ModelClaudeHaiku4_5_20251001,
		1024, // Short response needed for YES/NO + explanation
		"eval",
	)

	evalResponse, err := llmClient.Complete(ctx, "You are a test evaluator. Respond with YES or NO followed by a brief explanation.", evalPrompt)
	if err != nil {
		return false, fmt.Errorf("evaluation API call failed: %w", err)
	}

	// Parse evaluation result
	evalText := strings.ToUpper(strings.TrimSpace(evalResponse))
	originalResponse := strings.TrimSpace(evalResponse)

	// Helper function to extract and log explanation
	extractAndLogExplanation := func(prefix string, verdict string) {
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
			t.Logf("Evaluation (%s): %s", verdict, explanation)
		}
	}

	if strings.HasPrefix(evalText, "YES") {
		extractAndLogExplanation("YES", "PASS")
		return true, nil
	} else if strings.HasPrefix(evalText, "NO") {
		extractAndLogExplanation("NO", "FAIL")
		return false, nil
	}

	// If we can't parse clearly, check if response contains positive indicators
	if strings.Contains(evalText, "CORRECT") || strings.Contains(evalText, "YES") || strings.Contains(evalText, "ACCURATE") {
		return true, nil
	}

	// Default to false if unclear
	t.Logf("Evaluation response was unclear: %s", evalResponse)
	return false, nil
}

// LLMClientFactory creates an LLM client for testing
type LLMClientFactory func(t *testing.T) pipeline.LLMClient

// setupPipeline creates a pipeline instance with the given LLM client factory.
// It uses the PIPELINE_VERSION environment variable to select v1 or v2.
func setupPipeline(t *testing.T, ctx context.Context, clientInfo *laketesting.ClientInfo, llmFactory LLMClientFactory, debug bool, debugLevel int) pipeline.Runner {
	// Create logger with appropriate level
	var logger *slog.Logger
	if debug {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	} else {
		logger = testLogger(t)
	}

	// Create LLM client using factory
	baseLLMClient := llmFactory(t)

	// Wrap LLM client with debug logging if DEBUG is set
	var llmClient pipeline.LLMClient = baseLLMClient
	if debug {
		llmClient = &debugPipelineLLMClient{
			LLMClient:  baseLLMClient,
			t:          t,
			debugLevel: debugLevel,
		}
	}

	// Create querier using the clickhouse client
	baseQuerier := NewClickhouseQuerier(clientInfo.Client)

	// Wrap querier with debug logging if DEBUG is set
	var querier pipeline.Querier = baseQuerier
	if debug {
		querier = &debugQuerier{
			Querier:    baseQuerier,
			t:          t,
			debugLevel: debugLevel,
		}
	}

	// Create schema fetcher using HTTP with auth (same code path as API)
	schemaFetcher := pipeline.NewHTTPSchemaFetcherWithAuth(
		sharedDB.HTTPAddr(),
		clientInfo.Database,
		sharedDB.Username(),
		sharedDB.Password(),
	)

	// Select pipeline version
	version := pipeline.DefaultVersion()
	t.Logf("Using pipeline version: %s", version)

	cfg := &pipeline.Config{
		Logger:        logger,
		LLM:           llmClient,
		Querier:       querier,
		SchemaFetcher: schemaFetcher,
		MaxTokens:     4096,
		MaxRetries:    4,
	}

	var runner pipeline.Runner
	var err error

	switch version {
	case pipeline.VersionV2:
		prompts, promptErr := v2.LoadPrompts()
		require.NoError(t, promptErr)
		cfg.Prompts = prompts
		runner, err = v2.New(cfg)
	default: // v1
		prompts, promptErr := v1.LoadPrompts()
		require.NoError(t, promptErr)
		cfg.Prompts = prompts
		runner, err = v1.New(cfg)
	}
	require.NoError(t, err)

	return runner
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

// newAnthropicLLMClient creates an Anthropic LLM client for the pipeline
func newAnthropicLLMClient(t *testing.T) pipeline.LLMClient {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	require.NotEmpty(t, apiKey, "ANTHROPIC_API_KEY must be set for Anthropic tests")

	return pipeline.NewAnthropicLLMClient(
		anthropic.ModelClaudeHaiku4_5_20251001, // Use Haiku for faster/cheaper eval tests
		4096,
	)
}

// newOllamaLLMClient creates an Ollama LLM client for the pipeline
func newOllamaLLMClient(t *testing.T) pipeline.LLMClient {
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
		// Use llama3.1:8b - good for text completion without tool calling
		model = "llama3.1:8b"
	}

	return NewOllamaLLMClient(ollamaURL, model, 4096)
}

// OllamaLLMClient implements pipeline.LLMClient for Ollama
type OllamaLLMClient struct {
	baseURL   string
	model     string
	maxTokens int64
}

// NewOllamaLLMClient creates a new Ollama LLM client for the pipeline
func NewOllamaLLMClient(baseURL, model string, maxTokens int64) *OllamaLLMClient {
	return &OllamaLLMClient{
		baseURL:   baseURL,
		model:     model,
		maxTokens: maxTokens,
	}
}

// Complete sends a prompt to Ollama and returns the response text
func (c *OllamaLLMClient) Complete(ctx context.Context, systemPrompt, userPrompt string, opts ...pipeline.CompleteOption) (string, error) {
	// Note: Ollama doesn't support prompt caching, so opts are ignored
	reqBody := map[string]any{
		"model":  c.model,
		"stream": false,
		"options": map[string]any{
			"num_predict": c.maxTokens,
		},
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	// Handle streaming response (newline-delimited JSON)
	var fullContent strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done  bool   `json:"done"`
			Error string `json:"error,omitempty"`
		}

		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}

		if chunk.Error != "" {
			return "", fmt.Errorf("ollama error: %s", chunk.Error)
		}

		fullContent.WriteString(chunk.Message.Content)

		if chunk.Done {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return fullContent.String(), nil
}

// ClickhouseQuerier implements pipeline.Querier using the clickhouse client
type ClickhouseQuerier struct {
	db clickhouse.Client
}

// NewClickhouseQuerier creates a new ClickhouseQuerier
func NewClickhouseQuerier(db clickhouse.Client) *ClickhouseQuerier {
	return &ClickhouseQuerier{db: db}
}

// Query executes a SQL query and returns the result
func (q *ClickhouseQuerier) Query(ctx context.Context, sql string) (pipeline.QueryResult, error) {
	sql = strings.TrimSuffix(strings.TrimSpace(sql), ";")

	conn, err := q.db.Conn(ctx)
	if err != nil {
		return pipeline.QueryResult{SQL: sql, Error: fmt.Sprintf("connection error: %v", err)}, nil
	}
	defer conn.Close()

	result, err := dataset.Query(ctx, conn, sql, nil)
	if err != nil {
		return pipeline.QueryResult{SQL: sql, Error: err.Error()}, nil
	}

	qr := pipeline.QueryResult{
		SQL:     sql,
		Columns: result.Columns,
		Rows:    result.Rows,
		Count:   result.Count,
	}

	// Generate formatted output
	qr.Formatted = formatQueryResult(qr)

	return qr, nil
}

// formatQueryResult creates a human-readable format of the query result
func formatQueryResult(result pipeline.QueryResult) string {
	if result.Error != "" {
		return fmt.Sprintf("Error: %s", result.Error)
	}

	if len(result.Rows) == 0 {
		return "Query returned no results."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Results (%d rows):\n", len(result.Rows)))
	sb.WriteString("Columns: " + strings.Join(result.Columns, " | ") + "\n")
	sb.WriteString(strings.Repeat("-", 40) + "\n")

	// Limit output to first 50 rows
	maxRows := 50
	if len(result.Rows) < maxRows {
		maxRows = len(result.Rows)
	}

	for i := 0; i < maxRows; i++ {
		row := result.Rows[i]
		var values []string
		for _, col := range result.Columns {
			values = append(values, fmt.Sprintf("%v", row[col]))
		}
		sb.WriteString(strings.Join(values, " | ") + "\n")
	}

	if len(result.Rows) > 50 {
		sb.WriteString(fmt.Sprintf("... and %d more rows\n", len(result.Rows)-50))
	}

	return sb.String()
}

// isOllamaAvailable checks if the local Ollama server is available for agent evals.
// Currently disabled by default: local Ollama models generate unreliable SQL queries.
// Set OLLAMA_AGENT_EVALS=1 to enable these tests for local development.
func isOllamaAvailable() bool {
	// Ollama agent tests are disabled by default due to unreliable SQL generation.
	if os.Getenv("OLLAMA_AGENT_EVALS") != "1" {
		return false
	}

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

// debugQuerier wraps a Querier to log all queries and results when DEBUG is enabled
type debugQuerier struct {
	pipeline.Querier
	t          *testing.T
	debugLevel int
}

func (d *debugQuerier) Query(ctx context.Context, sql string) (pipeline.QueryResult, error) {
	// Log query
	sqlStr := sql
	if d.debugLevel == 1 {
		sqlStr = truncate(sql, 150)
	}

	if d.debugLevel == 1 {
		d.t.Logf("🔧 query: %s", sqlStr)
	} else {
		d.t.Logf("\n🔧 [QUERY]\n%s\n", sql)
	}

	// Execute the query
	result, err := d.Querier.Query(ctx, sql)

	// Log result
	resultTruncLen := 100
	if d.debugLevel == 2 {
		resultTruncLen = 500
	}

	if err != nil {
		if d.debugLevel == 1 {
			d.t.Logf("❌ query error: %v", err)
		} else {
			d.t.Logf("❌ [QUERY ERROR]: %v\n", err)
		}
	} else if result.Error != "" {
		if d.debugLevel == 1 {
			d.t.Logf("⚠️  query error: %s", truncate(result.Error, resultTruncLen))
		} else {
			d.t.Logf("⚠️  [QUERY RESULT] (error): %s\n", truncate(result.Error, resultTruncLen))
		}
	} else {
		if d.debugLevel == 1 {
			d.t.Logf("✅ query: %d rows", result.Count)
		} else {
			d.t.Logf("✅ [QUERY RESULT]: %d rows\n", result.Count)
		}
	}

	return result, err
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}

// debugPipelineLLMClient wraps an LLMClient to log all responses when DEBUG is enabled
type debugPipelineLLMClient struct {
	pipeline.LLMClient
	t          *testing.T
	debugLevel int
}

func (d *debugPipelineLLMClient) Complete(ctx context.Context, systemPrompt, userPrompt string, opts ...pipeline.CompleteOption) (string, error) {
	// Log that we're calling the LLM
	if d.debugLevel == 1 {
		d.t.Logf("🤖 LLM call (system: %d chars, user: %d chars)", len(systemPrompt), len(userPrompt))
	} else {
		d.t.Logf("\n🤖 [CALLING LLM]\n  System: %s\n  User: %s\n",
			truncate(systemPrompt, 200),
			truncate(userPrompt, 500))
	}

	// Call the actual LLM, passing through options
	response, err := d.LLMClient.Complete(ctx, systemPrompt, userPrompt, opts...)
	if err != nil {
		if d.debugLevel == 1 {
			d.t.Logf("❌ LLM error: %v", err)
		} else {
			d.t.Logf("❌ [LLM ERROR]: %v\n", err)
		}
		return "", err
	}

	// Log the response
	textTruncLen := 300
	if d.debugLevel == 2 {
		textTruncLen = 2000
	}

	if d.debugLevel == 1 {
		d.t.Logf("🤖 Response: %s", truncate(response, textTruncLen))
	} else {
		d.t.Logf("\n🤖 [LLM RESPONSE]\n%s\n", truncate(response, textTruncLen))
	}

	return response, nil
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
