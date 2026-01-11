//go:build evals

package evals_test

import (
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
	clientInfo := testClientInfo(t)

	// Set up test data - just load schema, no actual data needed
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Set up pipeline with LLM client
	p := setupPipeline(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query - asking something completely unrelated to DZ or Solana
	question := "what's the weather today?"
	if debug {
		if debugLevel == 1 {
			t.Logf("=== Query: '%s' ===\n", question)
		} else {
			t.Logf("=== Starting pipeline query: '%s' ===\n", question)
		}
	}
	result, err := p.Run(ctx, question)
	// For unrelated questions, the pipeline might return an error from decomposition
	// which is the expected behavior
	if err != nil {
		response := err.Error()
		if debug {
			if debugLevel == 1 {
				t.Logf("=== Response (error) ===\n%s\n", response)
			} else {
				t.Logf("\n=== Final Pipeline Response (error) ===\n%s\n", response)
			}
		} else {
			t.Logf("Pipeline response (error):\n%s", response)
		}

		// Evaluate with Ollama - the error message should indicate it can't help
		expectations := []OllamaExpectation{
			{
				Description:   "Agent correctly declines unrelated question",
				ExpectedValue: "agent indicates its scope is limited to DoubleZero/Solana network data and cannot help with other topics",
				Rationale:     "Declining is correct - agent should NOT fabricate weather data or try to answer unrelated questions",
			},
		}
		isCorrect, evalErr := ollamaEvaluateResponse(t, ctx, question, response, expectations...)
		require.NoError(t, evalErr, "Ollama evaluation must be available")
		require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question")
		return
	}

	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	// Basic validation - the response should acknowledge no relevant data
	response := result.Answer
	if debug {
		if debugLevel == 1 {
			t.Logf("=== Response ===\n%s\n", response)
		} else {
			t.Logf("\n=== Final Pipeline Response ===\n%s\n", response)
		}
	} else {
		t.Logf("Pipeline response:\n%s", response)
	}

	// Evaluate with Ollama - include specific expectations for "no data" response
	expectations := []OllamaExpectation{
		{
			Description:   "Agent correctly declines unrelated question",
			ExpectedValue: "agent indicates its scope is limited to DoubleZero/Solana network data and cannot help with other topics",
			Rationale:     "Declining is correct - agent should NOT fabricate weather data or try to answer unrelated questions",
		},
	}
	isCorrect, err := ollamaEvaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question")
}
