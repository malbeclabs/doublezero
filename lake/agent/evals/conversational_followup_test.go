//go:build evals

package evals_test

import (
	"context"
	"os"
	"testing"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_ConversationalFollowup(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_ConversationalFollowup(t, newAnthropicLLMClient)
}

func TestLake_Agent_Evals_OllamaLocal_ConversationalFollowup(t *testing.T) {
	t.Parallel()
	if !isOllamaAvailable() {
		t.Skip("Ollama not available, skipping eval test")
	}

	runTest_ConversationalFollowup(t, newOllamaLLMClient)
}

func runTest_ConversationalFollowup(t *testing.T, llmFactory LLMClientFactory) {
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

	// First, run a data query to establish conversation history
	firstQuestion := "How many devices are there?"
	if debug {
		t.Logf("=== First query (data analysis): '%s' ===\n", firstQuestion)
	}
	firstResult, err := p.Run(ctx, firstQuestion)
	require.NoError(t, err)
	require.NotNil(t, firstResult)
	require.Equal(t, pipeline.ClassificationDataAnalysis, firstResult.Classification)
	if debug {
		t.Logf("=== First response ===\n%s\n", firstResult.Answer)
	}

	// Build conversation history
	history := []pipeline.ConversationMessage{
		{Role: "user", Content: firstQuestion},
		{Role: "assistant", Content: firstResult.Answer},
	}

	// Now ask a conversational follow-up question
	followupQuestion := "What do you mean by that? Can you explain in simpler terms?"
	if debug {
		t.Logf("=== Follow-up query (conversational): '%s' ===\n", followupQuestion)
	}
	followupResult, err := p.RunWithHistory(ctx, followupQuestion, history)
	require.NoError(t, err)
	require.NotNil(t, followupResult)
	require.NotEmpty(t, followupResult.Answer)

	// Verify it was classified as conversational (not data_analysis)
	require.Equal(t, pipeline.ClassificationConversational, followupResult.Classification,
		"Follow-up question should be classified as conversational, not data_analysis")

	// Verify no data questions were generated (conversational path doesn't query data)
	require.Empty(t, followupResult.DataQuestions,
		"Conversational questions should not generate data questions")

	if debug {
		t.Logf("=== Follow-up response (classification: %s) ===\n%s\n",
			followupResult.Classification, followupResult.Answer)
	} else {
		t.Logf("Follow-up response (classification: %s):\n%s",
			followupResult.Classification, followupResult.Answer)
	}

	// Evaluate with Ollama - the response should be a helpful clarification
	expectations := []OllamaExpectation{
		{
			Description:   "Agent provides a helpful clarification or rephrasing",
			ExpectedValue: "a response that attempts to explain or clarify the previous answer in different terms",
			Rationale:     "The agent should recognize this as a request for clarification and respond conversationally",
		},
	}
	isCorrect, evalErr := ollamaEvaluateResponse(t, ctx, followupQuestion, followupResult.Answer, expectations...)
	require.NoError(t, evalErr, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not appropriately handle the conversational follow-up")
}

func TestLake_Agent_Evals_Anthropic_CapabilitiesQuestion(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_CapabilitiesQuestion(t, newAnthropicLLMClient)
}

func TestLake_Agent_Evals_OllamaLocal_CapabilitiesQuestion(t *testing.T) {
	t.Parallel()
	if !isOllamaAvailable() {
		t.Skip("Ollama not available, skipping eval test")
	}

	runTest_CapabilitiesQuestion(t, newOllamaLLMClient)
}

func runTest_CapabilitiesQuestion(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data - just load schema
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Set up pipeline with LLM client
	p := setupPipeline(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Ask about capabilities - this should be conversational, not require data
	question := "What kind of questions can you help me with?"
	if debug {
		t.Logf("=== Query: '%s' ===\n", question)
	}
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	// Verify it was classified as conversational
	require.Equal(t, pipeline.ClassificationConversational, result.Classification,
		"Capabilities question should be classified as conversational")

	// Verify no data questions were generated
	require.Empty(t, result.DataQuestions,
		"Capabilities questions should not generate data questions")

	if debug {
		t.Logf("=== Response (classification: %s) ===\n%s\n",
			result.Classification, result.Answer)
	} else {
		t.Logf("Response (classification: %s):\n%s",
			result.Classification, result.Answer)
	}

	// Evaluate with Ollama
	expectations := []OllamaExpectation{
		{
			Description:   "Agent explains its capabilities",
			ExpectedValue: "mentions being able to help with DoubleZero network data, devices, links, validators, or similar topics",
			Rationale:     "The agent should describe what kinds of data questions it can answer",
		},
	}
	isCorrect, evalErr := ollamaEvaluateResponse(t, ctx, question, result.Answer, expectations...)
	require.NoError(t, evalErr, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not appropriately explain capabilities")
}

func TestLake_Agent_Evals_Anthropic_ThankYouResponse(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_ThankYouResponse(t, newAnthropicLLMClient)
}

func TestLake_Agent_Evals_OllamaLocal_ThankYouResponse(t *testing.T) {
	t.Parallel()
	if !isOllamaAvailable() {
		t.Skip("Ollama not available, skipping eval test")
	}

	runTest_ThankYouResponse(t, newOllamaLLMClient)
}

func runTest_ThankYouResponse(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data - just load schema
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Set up pipeline with LLM client
	p := setupPipeline(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Build conversation history as if we just answered a question
	history := []pipeline.ConversationMessage{
		{Role: "user", Content: "How many validators are connected?"},
		{Role: "assistant", Content: "There are currently 50 validators connected to the DoubleZero network."},
	}

	// Simple acknowledgment - should be conversational
	question := "Thanks, that helps!"
	if debug {
		t.Logf("=== Query: '%s' ===\n", question)
	}
	result, err := p.RunWithHistory(ctx, question, history)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	// Verify it was classified as conversational
	require.Equal(t, pipeline.ClassificationConversational, result.Classification,
		"Thank you message should be classified as conversational")

	// Verify no data questions were generated
	require.Empty(t, result.DataQuestions,
		"Thank you messages should not generate data questions")

	if debug {
		t.Logf("=== Response (classification: %s) ===\n%s\n",
			result.Classification, result.Answer)
	} else {
		t.Logf("Response (classification: %s):\n%s",
			result.Classification, result.Answer)
	}

	// Evaluate with Ollama - response should be friendly acknowledgment
	expectations := []OllamaExpectation{
		{
			Description:   "Agent responds appropriately to thanks",
			ExpectedValue: "a friendly acknowledgment, offer to help with more questions, or similar polite response",
			Rationale:     "The agent should recognize this as a simple acknowledgment and respond naturally",
		},
	}
	isCorrect, evalErr := ollamaEvaluateResponse(t, ctx, question, result.Answer, expectations...)
	require.NoError(t, evalErr, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not appropriately handle the thank you message")
}
