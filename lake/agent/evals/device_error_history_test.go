//go:build evals

package evals_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	dztelemusage "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/usage"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_DeviceErrorHistory(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_DeviceErrorHistory(t, newAnthropicLLMClient)
}


func runTest_DeviceErrorHistory(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()
	clientInfo := testClientInfo(t)

	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	seedDeviceErrorHistoryData(t, ctx, conn)
	validateDeviceErrorHistoryQuery(t, ctx, conn)

	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	question := "when is the last time there have been errors on the Montreal DZD?"
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	t.Logf("Workflow response:\n%s", response)

	expectations := []Expectation{
		{
			Description:   "Response provides a timestamp or time reference for errors",
			ExpectedValue: "A specific time, date, or relative time reference (e.g., 'January 24, 2026', 'yesterday', specific timestamp)",
			Rationale:     "User asked when errors last occurred, so response must include when",
		},
		{
			Description:   "Response identifies the Montreal device",
			ExpectedValue: "yul-dzd1 or Montreal mentioned in the response",
			Rationale:     "User asked specifically about Montreal DZD",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err)
	require.True(t, isCorrect, "Evaluation failed for device error history")
}

// seedDeviceErrorHistoryData creates device with recent error history
// - Montreal (yul-dzd1): Had errors 2 hours ago
// - NYC (nyc-dzd1): No errors
func seedDeviceErrorHistoryData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	metros := []serviceability.Metro{
		{PK: "metro1", Code: "yul", Name: "Montreal"},
		{PK: "metro2", Code: "nyc", Name: "New York"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	devices := []serviceability.Device{
		{PK: "device1", Code: "yul-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed interface counters with errors for Montreal device
	countersDS, err := dztelemusage.NewDeviceInterfaceCountersDataset(log)
	require.NoError(t, err)

	counters := []struct {
		eventTS        time.Time
		devicePK       string
		host           string
		ifaceName      string
		inErrorsDelta  *int64
		outErrorsDelta *int64
	}{
		// Montreal device - errors 2 hours ago
		{now.Add(-2 * time.Hour), "device1", "yul-dzd1", "Ethernet1", int64Ptr(5), int64Ptr(3)},
		// Montreal device - no errors 1 hour ago
		{now.Add(-1 * time.Hour), "device1", "yul-dzd1", "Ethernet1", nil, nil},
		// NYC device - no errors
		{now.Add(-2 * time.Hour), "device2", "nyc-dzd1", "Ethernet1", nil, nil},
		{now.Add(-1 * time.Hour), "device2", "nyc-dzd1", "Ethernet1", nil, nil},
	}

	deltaDuration := float64(60.0)
	err = countersDS.WriteBatch(ctx, conn, len(counters), func(i int) ([]any, error) {
		c := counters[i]
		return []any{
			c.eventTS.UTC(),   // event_ts
			now,               // ingested_at
			c.devicePK,        // device_pk
			c.host,            // host
			c.ifaceName,       // intf
			nil,               // user_tunnel_id
			nil,               // link_pk
			nil,               // link_side
			nil,               // model_name
			nil,               // serial_number
			nil,               // carrier_transitions
			nil,               // in_broadcast_pkts
			nil,               // in_discards
			nil,               // in_errors
			nil,               // in_fcs_errors
			nil,               // in_multicast_pkts
			nil,               // in_octets
			nil,               // in_pkts
			nil,               // in_unicast_pkts
			nil,               // out_broadcast_pkts
			nil,               // out_discards
			nil,               // out_errors
			nil,               // out_multicast_pkts
			nil,               // out_octets
			nil,               // out_pkts
			nil,               // out_unicast_pkts
			nil,               // carrier_transitions_delta
			nil,               // in_broadcast_pkts_delta
			nil,               // in_discards_delta
			c.inErrorsDelta,   // in_errors_delta
			nil,               // in_fcs_errors_delta
			nil,               // in_multicast_pkts_delta
			nil,               // in_octets_delta
			nil,               // in_pkts_delta
			nil,               // in_unicast_pkts_delta
			nil,               // out_broadcast_pkts_delta
			nil,               // out_discards_delta
			c.outErrorsDelta,  // out_errors_delta
			nil,               // out_multicast_pkts_delta
			nil,               // out_octets_delta
			nil,               // out_pkts_delta
			nil,               // out_unicast_pkts_delta
			&deltaDuration,    // delta_duration
		}, nil
	})
	require.NoError(t, err)
}

func validateDeviceErrorHistoryQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	query := `
SELECT d.code, MAX(f.event_ts) AS last_error
FROM fact_dz_device_interface_counters f
JOIN dz_devices_current d ON f.device_pk = d.pk
JOIN dz_metros_current m ON d.metro_pk = m.pk
WHERE m.code = 'yul'
  AND (f.in_errors_delta > 0 OR f.out_errors_delta > 0)
GROUP BY d.code
`
	result, err := dataset.Query(ctx, conn, query, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Count, "Should have 1 device with errors")
	t.Logf("Database validation passed: Montreal device has error history")
}
