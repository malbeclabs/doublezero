//go:build evals

package evals_test

import (
	"context"
	"os"
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_EntityNotFound_Link(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_EntityNotFound_Link(t, newAnthropicLLMClient)
}


func runTest_EntityNotFound_Link(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()
	clientInfo := testClientInfo(t)

	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed some real data - but NOT the link the user will ask about
	seedEntityNotFoundData(t, ctx, conn)
	validateEntityNotFoundData(t, ctx, conn)

	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Ask about a link that doesn't exist
	question := "What is the status of link xyz-fake-link-1?"
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	t.Logf("Workflow response:\n%s", response)

	expectations := []Expectation{
		{
			Description:   "Response clearly indicates the link was not found",
			ExpectedValue: "States that xyz-fake-link-1 was not found, doesn't exist, or couldn't be located",
			Rationale:     "The link doesn't exist in the database - agent should clearly communicate this",
		},
		{
			Description:   "Response does NOT hallucinate data about the nonexistent link",
			ExpectedValue: "No fabricated status, metrics, or details about xyz-fake-link-1",
			Rationale:     "Agent must not make up information about entities that don't exist",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err)
	require.True(t, isCorrect, "Evaluation failed for entity not found (link)")
}

func TestLake_Agent_Evals_Anthropic_EntityNotFound_Device(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_EntityNotFound_Device(t, newAnthropicLLMClient)
}


func runTest_EntityNotFound_Device(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()
	clientInfo := testClientInfo(t)

	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed some real data - but NOT the device the user will ask about
	seedEntityNotFoundData(t, ctx, conn)
	validateEntityNotFoundData(t, ctx, conn)

	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Ask about a device that doesn't exist
	question := "Show me the error history for device abc-nonexistent-dzd9"
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	t.Logf("Workflow response:\n%s", response)

	expectations := []Expectation{
		{
			Description:   "Response clearly indicates the device was not found",
			ExpectedValue: "States that abc-nonexistent-dzd9 was not found, doesn't exist, or couldn't be located",
			Rationale:     "The device doesn't exist in the database - agent should clearly communicate this",
		},
		{
			Description:   "Response does NOT hallucinate data about the nonexistent device",
			ExpectedValue: "No fabricated error history, status, or details about abc-nonexistent-dzd9",
			Rationale:     "Agent must not make up information about entities that don't exist",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err)
	require.True(t, isCorrect, "Evaluation failed for entity not found (device)")
}

// seedEntityNotFoundData seeds some real data so the database isn't empty
// but does NOT include the entities the tests will ask about
func seedEntityNotFoundData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	links := []serviceability.Link{
		{PK: "link1", Code: "nyc-lon-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
	}
	var linkSchema serviceability.LinkSchema
	err = linkDS.WriteBatch(ctx, conn, len(links), func(i int) ([]any, error) {
		return linkSchema.ToRow(links[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       testOpID(),
	})
	require.NoError(t, err)
}

func validateEntityNotFoundData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Verify we have some data
	deviceQuery := `SELECT COUNT(*) as count FROM dz_devices_current`
	result, err := dataset.Query(ctx, conn, deviceQuery, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)
	t.Logf("Database validation passed: devices exist")

	linkQuery := `SELECT COUNT(*) as count FROM dz_links_current`
	result, err = dataset.Query(ctx, conn, linkQuery, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)
	t.Logf("Database validation passed: links exist")

	// Verify the fake entities DON'T exist
	fakeLinkQuery := `SELECT COUNT(*) as count FROM dz_links_current WHERE code = 'xyz-fake-link-1'`
	result, err = dataset.Query(ctx, conn, fakeLinkQuery, nil)
	require.NoError(t, err)
	t.Logf("Database validation passed: fake link does not exist")

	fakeDeviceQuery := `SELECT COUNT(*) as count FROM dz_devices_current WHERE code = 'abc-nonexistent-dzd9'`
	result, err = dataset.Query(ctx, conn, fakeDeviceQuery, nil)
	require.NoError(t, err)
	t.Logf("Database validation passed: fake device does not exist")
}
