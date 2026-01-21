//go:build evals

package evals_test

import (
	"context"
	"os"
	"testing"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
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

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// First, run a data query to establish conversation history
	firstQuestion := "How many devices are there?"
	if debug {
		t.Logf("=== First query (data analysis): '%s' ===\n", firstQuestion)
	}
	firstResult, err := p.Run(ctx, firstQuestion)
	require.NoError(t, err)
	require.NotNil(t, firstResult)
	require.Equal(t, workflow.ClassificationDataAnalysis, firstResult.Classification)
	if debug {
		t.Logf("=== First response ===\n%s\n", firstResult.Answer)
	}

	// Build conversation history
	history := []workflow.ConversationMessage{
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

	// V3 classifies based on tool usage (whether queries were executed), not question semantics.
	// The model may choose to re-run a query to provide a better answer, which is acceptable.

	if debug {
		t.Logf("=== Follow-up response (classification: %s) ===\n%s\n",
			followupResult.Classification, followupResult.Answer)
	} else {
		t.Logf("Follow-up response (classification: %s):\n%s",
			followupResult.Classification, followupResult.Answer)
	}

	// Evaluate with Ollama - the response should be a helpful clarification
	expectations := []Expectation{
		{
			Description:   "Agent provides a helpful clarification or rephrasing",
			ExpectedValue: "a response that attempts to explain or clarify the previous answer in different terms",
			Rationale:     "The agent should recognize this as a request for clarification and respond conversationally",
		},
	}
	isCorrect, evalErr := evaluateResponse(t, ctx, followupQuestion, followupResult.Answer, expectations...)
	require.NoError(t, evalErr, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not appropriately handle the conversational follow-up")
}

func TestLake_Agent_Evals_Anthropic_CapabilitiesQuestion(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_CapabilitiesQuestion(t, newAnthropicLLMClient)
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

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

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
	require.Equal(t, workflow.ClassificationConversational, result.Classification,
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
	expectations := []Expectation{
		{
			Description:   "Agent explains its capabilities",
			ExpectedValue: "mentions being able to help with DoubleZero network data, devices, links, validators, or similar topics",
			Rationale:     "The agent should describe what kinds of data questions it can answer",
		},
	}
	isCorrect, evalErr := evaluateResponse(t, ctx, question, result.Answer, expectations...)
	require.NoError(t, evalErr, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not appropriately explain capabilities")
}

func TestLake_Agent_Evals_Anthropic_AffirmativeQueryConfirmation(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_AffirmativeQueryConfirmation(t, newAnthropicLLMClient)
}


// runTest_AffirmativeQueryConfirmation tests that when the assistant offers to run a query
// and the user says "yes", it should be classified as data_analysis and actually execute the query.
func runTest_AffirmativeQueryConfirmation(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data - just load schema
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Build conversation history where assistant offered to run a query
	history := []workflow.ConversationMessage{
		{Role: "user", Content: "What's the WAN link utilization?"},
		{Role: "assistant", Content: "I noticed there may have been an issue with the query. Would you like me to query the WAN link utilization with the corrected filter (link_type = 'WAN')?"},
	}

	// User confirms with "yes" - this should trigger data_analysis, NOT conversational
	question := "Yes"
	if debug {
		t.Logf("=== Query: '%s' (expecting data_analysis, not conversational) ===\n", question)
	}
	result, err := p.RunWithHistory(ctx, question, history)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	// KEY ASSERTION: "Yes" to a query offer should be data_analysis, NOT conversational
	require.Equal(t, workflow.ClassificationDataAnalysis, result.Classification,
		"Affirmative response to query offer should be classified as data_analysis, not conversational")

	// Verify data questions WERE generated (the query should actually run)
	require.NotEmpty(t, result.DataQuestions,
		"Affirmative response to query offer should generate data questions")

	if debug {
		t.Logf("=== Response (classification: %s) ===\n%s\n",
			result.Classification, result.Answer)
		t.Logf("=== Data questions generated: %d ===\n", len(result.DataQuestions))
		for i, dq := range result.DataQuestions {
			t.Logf("  Q%d: %s\n", i+1, dq.Question)
		}
	} else {
		t.Logf("Response (classification: %s):\n%s",
			result.Classification, result.Answer)
	}
}

func TestLake_Agent_Evals_Anthropic_ThankYouResponse(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_ThankYouResponse(t, newAnthropicLLMClient)
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

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Build conversation history as if we just answered a question
	history := []workflow.ConversationMessage{
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
	require.Equal(t, workflow.ClassificationConversational, result.Classification,
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
	expectations := []Expectation{
		{
			Description:   "Agent responds appropriately to thanks",
			ExpectedValue: "a friendly acknowledgment, offer to help with more questions, or similar polite response",
			Rationale:     "The agent should recognize this as a simple acknowledgment and respond naturally",
		},
	}
	isCorrect, evalErr := evaluateResponse(t, ctx, question, result.Answer, expectations...)
	require.NoError(t, evalErr, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not appropriately handle the thank you message")
}
