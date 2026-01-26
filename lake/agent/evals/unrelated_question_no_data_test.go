//go:build evals

package evals_test

import (
	"context"
	"os"
	"strings"
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

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query - asking something completely unrelated to DZ or Solana
	question := "what's the weather today?"
	if debug {
		if debugLevel == 1 {
			t.Logf("=== Query: '%s' ===\n", question)
		} else {
			t.Logf("=== Starting workflow query: '%s' ===\n", question)
		}
	}
	result, err := p.Run(ctx, question)

	var response string
	if err != nil {
		// For unrelated questions, the workflow might return an error from decomposition
		// which is the expected behavior
		response = err.Error()
	} else {
		require.NotNil(t, result)
		require.NotEmpty(t, result.Answer)
		response = result.Answer
	}

	if debug {
		if debugLevel == 1 {
			t.Logf("=== Response ===\n%s\n", response)
		} else {
			t.Logf("\n=== Final Workflow Response ===\n%s\n", response)
		}
	} else {
		t.Logf("Workflow response:\n%s", response)
	}

	// Deterministic validation: agent should redirect to relevant topics, not answer weather
	responseLower := strings.ToLower(response)

	// Should mention relevant topics (DZ, network, validator, etc.)
	hasRelevantTopic := strings.Contains(responseLower, "doublezero") ||
		strings.Contains(responseLower, "dz") ||
		strings.Contains(responseLower, "network") ||
		strings.Contains(responseLower, "validator") ||
		strings.Contains(responseLower, "solana") ||
		strings.Contains(responseLower, "device") ||
		strings.Contains(responseLower, "link")
	require.True(t, hasRelevantTopic, "Response should mention relevant topics (DZ, network, validator, etc.) when declining unrelated question")

	// Should NOT attempt to actually answer weather (no specific weather answers)
	weatherAnswers := []string{"sunny", "cloudy", "rain", "snow", "degrees", "celsius", "fahrenheit", "°f", "°c", "forecast"}
	for _, weather := range weatherAnswers {
		require.False(t, strings.Contains(responseLower, weather),
			"Response should NOT contain weather answer '%s' - agent should decline, not fabricate", weather)
	}
}
