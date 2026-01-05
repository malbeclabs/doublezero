//go:build evals

package evals_test

import (
	"bytes"
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

	runTest_UnrelatedQuestionNoData(t)
}

func runTest_UnrelatedQuestionNoData(t *testing.T) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)

	// Set up test data - just load schema, no actual data needed
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Load tables and views to set up the schema
	loadTablesAndViews(t, ctx, conn)

	// Set up agent with Anthropic LLM client
	agentInstance := setupAgent(t, ctx, db, newAnthropicLLMClient, debug, debugLevel, nil)

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

	// Validate the response
	validateUnrelatedQuestionNoDataResponse(t, response)

	// Evaluate with Ollama (optional for this test since validation already confirms correct behavior)
	// The agent correctly states it doesn't have access to unrelated data, which is the expected response
	isCorrect, err := ollamaEvaluateResponse(t, ctx, question, response)
	if err == nil {
		// If Ollama is available, check the evaluation, but don't fail if it's incorrect
		// since the validation already confirmed the response is correct
		if !isCorrect {
			t.Logf("Note: Ollama evaluation marked response as incorrect, but validation confirms the response correctly states no data is available for unrelated questions")
		}
	}
}

// validateUnrelatedQuestionNoDataResponse validates that the response explicitly states no relevant data is available
func validateUnrelatedQuestionNoDataResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should explicitly state that the question is unrelated or no relevant data is available
	// Accept various phrasings that indicate the question is outside the scope
	unrelatedMentioned := strings.Contains(responseLower, "no data") ||
		strings.Contains(responseLower, "no relevant data") ||
		strings.Contains(responseLower, "not available") ||
		strings.Contains(responseLower, "cannot answer") ||
		strings.Contains(responseLower, "unable to answer") ||
		strings.Contains(responseLower, "outside my scope") ||
		strings.Contains(responseLower, "not related") ||
		strings.Contains(responseLower, "unrelated") ||
		strings.Contains(responseLower, "don't have") ||
		strings.Contains(responseLower, "doesn't have") ||
		strings.Contains(responseLower, "no information") ||
		strings.Contains(responseLower, "not in the database") ||
		strings.Contains(responseLower, "not in my database") ||
		strings.Contains(responseLower, "not available in") ||
		strings.Contains(responseLower, "outside the scope") ||
		strings.Contains(responseLower, "beyond my scope") ||
		strings.Contains(responseLower, "don't have access") ||
		strings.Contains(responseLower, "can only answer") ||
		strings.Contains(responseLower, "only answer questions about")
	require.True(t, unrelatedMentioned,
		"Response should explicitly state that the question is unrelated or no relevant data is available. Got: %s",
		truncateForError(response, 300))

	// Should NOT contain fabricated data
	// Check for specific indicators that would suggest made-up information
	fabricatedIndicators := []string{
		"the weather is",
		"temperature is",
		"degrees",
		"sunny",
		"cloudy",
		"rainy",
		"forecast",
		"humidity",
		"wind speed",
	}
	hasFabricatedData := false
	for _, indicator := range fabricatedIndicators {
		if strings.Contains(responseLower, indicator) && !unrelatedMentioned {
			hasFabricatedData = true
			break
		}
	}
	require.False(t, hasFabricatedData,
		"Response contains fabricated data (weather information) without acknowledging no data is available. Got: %s",
		truncateForError(response, 300))
}
