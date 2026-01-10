//go:build evals

package evals_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_UnrelatedQuestionNoData(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_UnrelatedQuestionNoData(t, newAnthropicLLMClient)
}

func TestLake_Agent_Evals_OllamaLocal_UnrelatedQuestionNoData(t *testing.T) {
	t.Parallel()
	if !isOllamaAvailable() {
		t.Skip("Ollama not available, skipping eval test")
	}

	runTest_UnrelatedQuestionNoData(t, newOllamaLLMClient)
}

func runTest_UnrelatedQuestionNoData(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)

	// Set up test data - just load schema, no actual data needed
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Set up agent with LLM client
	agentInstance := setupAgent(t, ctx, db, llmFactory, debug, debugLevel, nil)

	// Run the query - asking something completely unrelated to DZ or Solana
	var output bytes.Buffer
	question := "what's the weather today?"
	if debug {
		if debugLevel == 1 {
			t.Logf("=== Query: '%s' ===\n", question)
		} else {
			t.Logf("=== Starting agent query: '%s' ===\n", question)
		}
	}
	result, err := agentInstance.Run(ctx, question, &output)
	require.NoError(t, err)
	require.NotEmpty(t, result.FinalText)

	// Basic validation - the response should acknowledge no relevant data
	response := result.FinalText
	if debug {
		if debugLevel == 1 {
			t.Logf("=== Response ===\n%s\n", response)
		} else {
			t.Logf("\n=== Final Agent Response ===\n%s\n", response)
		}
	} else {
		t.Logf("Agent response:\n%s", response)
	}

	// Evaluate with Ollama - include specific expectations for "no data" response
	expectations := []OllamaExpectation{
		{
			Description:   "Agent correctly declines unrelated question",
			ExpectedValue: "agent explains it cannot help with weather because its scope is DoubleZero/Solana network data only",
			Rationale:     "Declining is correct - agent should NOT fabricate weather data",
		},
	}
	isCorrect, err := ollamaEvaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question")
}
