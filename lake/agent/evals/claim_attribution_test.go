//go:build evals

package evals_test

import (
	"context"
	"os"
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_ClaimAttribution(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_ClaimAttribution(t, newAnthropicLLMClient)
}

func runTest_ClaimAttribution(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()
	clientInfo := testClientInfo(t)

	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	seedClaimAttributionData(t, ctx, conn)

	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	question := "How many devices are activated, and how many metros do we have?"
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	t.Logf("Workflow response:\n%s", response)

	expectations := []Expectation{
		{
			Description:   "Response includes device count",
			ExpectedValue: "5 devices or 5 activated devices mentioned",
			Rationale:     "Test data has 5 activated devices",
		},
		{
			Description:   "Response includes metro count",
			ExpectedValue: "3 metros mentioned",
			Rationale:     "Test data has 3 metros",
		},
		{
			Description:   "Response includes claim attribution references",
			ExpectedValue: "At least one claim reference in the format [Q1], [Q2], etc. Must appear as bracketed references like [Q1] or [Q2] attached to factual claims",
			Rationale:     "The system prompt requires every factual claim to reference its source query (Q1, Q2, etc.) so users can trace claims back to data",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err)
	require.True(t, isCorrect, "Evaluation failed - response must include claim attribution references like [Q1]")
}

// seedClaimAttributionData creates simple test data for verifying claim attribution.
// - 3 metros
// - 5 activated devices
func seedClaimAttributionData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	now := testTime()

	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "tyo", Name: "Tokyo"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "nyc-dzd2", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device3", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device4", Code: "tyo-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
		{PK: "device5", Code: "tyo-dzd2", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)
}
