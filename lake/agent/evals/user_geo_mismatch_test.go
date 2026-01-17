//go:build evals

package evals_test

import (
	"context"
	"net"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/geoip"
	maxmindgeoip "github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_UserGeoMismatch(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_UserGeoMismatch(t, newAnthropicLLMClient)
}


func runTest_UserGeoMismatch(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()
	clientInfo := testClientInfo(t)

	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	seedUserGeoMismatchData(t, ctx, conn)
	validateUserGeoMismatchQuery(t, ctx, conn)

	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	question := "find DZ users with client IPs in a different metro than the DZD they're connected to"
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	t.Logf("Workflow response:\n%s", response)

	expectations := []Expectation{
		{
			Description:   "Response identifies mismatched users",
			ExpectedValue: "user2 and user3 identified as having geo-mismatch (connected to NYC but client IP is in Tokyo/London)",
			Rationale:     "Test data has 2 users connected to NYC device but with Tokyo/London client IPs",
		},
		{
			Description:   "Response does NOT flag correctly matched users",
			ExpectedValue: "user1 should NOT be flagged as mismatched since they're in NYC connecting to NYC",
			Rationale:     "user1 is correctly matched - NYC client IP connecting to NYC device",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err)
	require.True(t, isCorrect, "Evaluation failed for user geo-mismatch")
}

// seedUserGeoMismatchData creates users with geo-mismatches
// - user1: NYC client IP -> NYC device (MATCH)
// - user2: Tokyo client IP -> NYC device (MISMATCH)
// - user3: London client IP -> NYC device (MISMATCH)
func seedUserGeoMismatchData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()
	opID := uuid.New()

	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	users := []serviceability.User{
		{PK: "user1", Status: "activated", DevicePK: "device1", DZIP: net.ParseIP("10.1.1.1"), ClientIP: net.ParseIP("203.0.113.1"), Kind: "ibrl"},
		{PK: "user2", Status: "activated", DevicePK: "device1", DZIP: net.ParseIP("10.1.1.2"), ClientIP: net.ParseIP("45.76.100.1"), Kind: "ibrl"},
		{PK: "user3", Status: "activated", DevicePK: "device1", DZIP: net.ParseIP("10.1.1.3"), ClientIP: net.ParseIP("51.15.0.1"), Kind: "ibrl"},
	}
	seedUsers(t, ctx, conn, users, now, now, opID)

	// Seed GeoIP records
	geoipDS, err := geoip.NewGeoIPRecordDataset(log)
	require.NoError(t, err)

	geoRecords := []*maxmindgeoip.Record{
		{IP: net.ParseIP("203.0.113.1"), City: "New York", MetroName: "New York", Country: "United States", CountryCode: "US"},
		{IP: net.ParseIP("45.76.100.1"), City: "Tokyo", MetroName: "Tokyo", Country: "Japan", CountryCode: "JP"},
		{IP: net.ParseIP("51.15.0.1"), City: "London", MetroName: "London", Country: "United Kingdom", CountryCode: "GB"},
	}

	var geoSchema geoip.GeoIPRecordSchema
	err = geoipDS.WriteBatch(ctx, conn, len(geoRecords), func(i int) ([]any, error) {
		return geoSchema.ToRow(geoRecords[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       opID,
	})
	require.NoError(t, err)
}

func validateUserGeoMismatchQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	query := `
SELECT u.pk, geo.metro_name AS client_metro, m.name AS connected_metro
FROM dz_users_current u
JOIN dz_devices_current d ON u.device_pk = d.pk
JOIN dz_metros_current m ON d.metro_pk = m.pk
LEFT JOIN geoip_records_current geo ON u.client_ip = geo.ip
WHERE u.status = 'activated'
ORDER BY u.pk
`
	result, err := dataset.Query(ctx, conn, query, nil)
	require.NoError(t, err)
	require.Equal(t, 3, result.Count, "Should have 3 users")
	t.Logf("Database validation passed: 3 users with geo data")
}
