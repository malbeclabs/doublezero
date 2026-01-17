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
	dztelemlatency "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/latency"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_LinkOutagesByMetro(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_LinkOutagesByMetro(t, newAnthropicLLMClient)
}


func runTest_LinkOutagesByMetro(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed link outage data by metro
	seedLinkOutagesByMetroData(t, ctx, conn)

	// Validate database query results before testing agent
	validateLinkOutagesByMetroQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query - ask about outages for links going into Sao Paulo
	question := "identify the timestamps of outages on links going into Sao Paulo in the last 30 days"
	if debug {
		if debugLevel == 1 {
			t.Logf("=== Query: '%s' ===\n", question)
		} else {
			t.Logf("=== Starting workflow query: '%s' ===\n", question)
		}
	}
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	if debug {
		if debugLevel == 1 {
			t.Logf("=== Response ===\n%s\n", response)
		} else {
			t.Logf("\n=== Final Workflow Response ===\n%s\n", response)
		}
	} else {
		t.Logf("Workflow response:\n%s", response)
	}

	// Evaluate with expectations
	expectations := []Expectation{
		{
			Description:   "Response mentions nyc-sao-1 status outage",
			ExpectedValue: "nyc-sao-1 identified as having a status-based outage (soft-drained) with start/stop timestamps",
			Rationale:     "nyc-sao-1 connects NYC to SAO and had a resolved status-based outage",
		},
		{
			Description:   "Response mentions sao-lon-1 ongoing outage",
			ExpectedValue: "sao-lon-1 identified as having an ongoing outage (currently soft-drained)",
			Rationale:     "sao-lon-1 connects SAO to LON and is currently down",
		},
		{
			Description:   "Response mentions nyc-sao-2 packet loss with percentage as plain number",
			ExpectedValue: "nyc-sao-2 identified as having packet loss with an actual percentage value (any numeric %, not just 'packet loss detected'). Must NOT mention 'hex values', 'encoded', 'require decoding', or claim values need conversion.",
			Rationale:     "nyc-sao-2 had packet loss - the actual percentage must be included as a plain number, not described as encoded or requiring conversion",
		},
		{
			Description:   "Response does NOT mention nyc-lon-1",
			ExpectedValue: "nyc-lon-1 should NOT be mentioned as it doesn't connect to Sao Paulo",
			Rationale:     "nyc-lon-1 connects NYC-LON, not SAO",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly identify SAO link outages with timestamps")
}

// seedLinkOutagesByMetroData seeds data for link outages filtered by metro
// Scenario:
// - nyc-sao-1: NYC to SAO, status-based outage (soft-drained 10 days ago, recovered 8 days ago)
// - sao-lon-1: SAO to LON, status-based ongoing outage (soft-drained 2 days ago)
// - nyc-sao-2: NYC to SAO, packet loss outage (5% loss starting 5 days ago, recovered 4 days ago)
// - nyc-lon-1: NYC to LON, always healthy (should not appear - doesn't connect to SAO)
func seedLinkOutagesByMetroData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "sao", Name: "Sao Paulo"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "sao-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	var linkSchema serviceability.LinkSchema

	// Link 1: nyc-sao-1 - resolved outage (NYC to SAO)
	// T-35d: activated (before our 30-day window)
	linkNycSaoActivated := serviceability.Link{
		PK: "link1", Code: "nyc-sao-1", Status: "activated", LinkType: "WAN",
		SideAPK: "device1", SideZPK: "device3", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1",
		Bandwidth: 10000000000, CommittedRTTNs: 100000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNycSaoActivated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-35 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// T-10d: soft-drained (outage start)
	linkNycSaoDrained := serviceability.Link{
		PK: "link1", Code: "nyc-sao-1", Status: "soft-drained", LinkType: "WAN",
		SideAPK: "device1", SideZPK: "device3", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1",
		Bandwidth: 10000000000, CommittedRTTNs: 100000000, ISISDelayOverrideNs: 1000000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNycSaoDrained), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-10 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// T-8d: activated again (outage end)
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNycSaoActivated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-8 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Link 2: sao-lon-1 - ongoing outage (SAO to LON)
	// T-35d: activated
	linkSaoLonActivated := serviceability.Link{
		PK: "link2", Code: "sao-lon-1", Status: "activated", LinkType: "WAN",
		SideAPK: "device3", SideZPK: "device2", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet2",
		Bandwidth: 10000000000, CommittedRTTNs: 150000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkSaoLonActivated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-35 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// T-2d: soft-drained (ongoing outage)
	linkSaoLonDrained := serviceability.Link{
		PK: "link2", Code: "sao-lon-1", Status: "soft-drained", LinkType: "WAN",
		SideAPK: "device3", SideZPK: "device2", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet2",
		Bandwidth: 10000000000, CommittedRTTNs: 150000000, ISISDelayOverrideNs: 1000000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkSaoLonDrained), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-2 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Link 3: nyc-sao-2 - packet loss outage (status stays activated, but has telemetry-based outage)
	// T-35d: activated (status never changes)
	linkNycSao2Activated := serviceability.Link{
		PK: "link4", Code: "nyc-sao-2", Status: "activated", LinkType: "WAN",
		SideAPK: "device1", SideZPK: "device3", SideAIfaceName: "Ethernet4", SideZIfaceName: "Ethernet4",
		Bandwidth: 10000000000, CommittedRTTNs: 100000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNycSao2Activated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-35 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Link 4: nyc-lon-1 - always healthy, does NOT connect to SAO
	// T-35d: activated (never changed)
	linkNycLonActivated := serviceability.Link{
		PK: "link3", Code: "nyc-lon-1", Status: "activated", LinkType: "WAN",
		SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet3", SideZIfaceName: "Ethernet3",
		Bandwidth: 10000000000, CommittedRTTNs: 50000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNycLonActivated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-35 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Seed latency telemetry data for nyc-sao-2 with packet loss period
	latencyDS, err := dztelemlatency.NewDeviceLinkLatencyDataset(log)
	require.NoError(t, err)
	ingestedAt := now

	// Generate latency samples for nyc-sao-2:
	// - T-6d to T-5d: healthy (no loss)
	// - T-5d to T-4d: packet loss period (~5% loss)
	// - T-4d to now: healthy again (recovered)
	var latencySamples []struct {
		time           time.Time
		epoch          int64
		sampleIndex    int32
		originDevicePK string
		targetDevicePK string
		linkPK         string
		rttUs          uint32
		loss           bool
		ipdvUs         *int64
	}

	// Healthy period: T-6d to T-5d (hourly samples, no loss)
	for h := 0; h < 24; h++ {
		sampleTime := now.Add(-6*24*time.Hour + time.Duration(h)*time.Hour)
		latencySamples = append(latencySamples, struct {
			time           time.Time
			epoch          int64
			sampleIndex    int32
			originDevicePK string
			targetDevicePK string
			linkPK         string
			rttUs          uint32
			loss           bool
			ipdvUs         *int64
		}{
			time:           sampleTime,
			epoch:          100,
			sampleIndex:    int32(h),
			originDevicePK: "device1",
			targetDevicePK: "device3",
			linkPK:         "link4",
			rttUs:          50000, // 50ms
			loss:           false,
			ipdvUs:         int64Ptr(5000),
		})
	}

	// Packet loss period: T-5d to T-4d (hourly samples, ~3.33% loss = 1 loss sample per hour out of 30)
	// Using 30 samples produces a repeating decimal (1/30 = 0.0333... = 3.33...%) to test
	// that the synthesizer handles these values correctly without hallucinating hex encoding
	for h := 0; h < 24; h++ {
		sampleTime := now.Add(-5*24*time.Hour + time.Duration(h)*time.Hour)
		// Generate 30 samples per hour, with 1 being a loss sample (~3.33%)
		for s := 0; s < 30; s++ {
			isLoss := s == 0 // First sample of each hour is loss
			latencySamples = append(latencySamples, struct {
				time           time.Time
				epoch          int64
				sampleIndex    int32
				originDevicePK string
				targetDevicePK string
				linkPK         string
				rttUs          uint32
				loss           bool
				ipdvUs         *int64
			}{
				time:           sampleTime.Add(time.Duration(s) * time.Minute),
				epoch:          100,
				sampleIndex:    int32(h*30 + s),
				originDevicePK: "device1",
				targetDevicePK: "device3",
				linkPK:         "link4",
				rttUs:          50000,
				loss:           isLoss,
				ipdvUs:         int64Ptr(5000),
			})
		}
	}

	// Recovered period: T-4d to now (hourly samples, no loss)
	for h := 0; h < 96; h++ { // 4 days * 24 hours
		sampleTime := now.Add(-4*24*time.Hour + time.Duration(h)*time.Hour)
		latencySamples = append(latencySamples, struct {
			time           time.Time
			epoch          int64
			sampleIndex    int32
			originDevicePK string
			targetDevicePK string
			linkPK         string
			rttUs          uint32
			loss           bool
			ipdvUs         *int64
		}{
			time:           sampleTime,
			epoch:          100,
			sampleIndex:    int32(h),
			originDevicePK: "device1",
			targetDevicePK: "device3",
			linkPK:         "link4",
			rttUs:          50000,
			loss:           false,
			ipdvUs:         int64Ptr(5000),
		})
	}

	err = latencyDS.WriteBatch(ctx, conn, len(latencySamples), func(i int) ([]any, error) {
		s := latencySamples[i]
		return []any{
			s.time.UTC(),
			ingestedAt,
			s.epoch,
			s.sampleIndex,
			s.originDevicePK,
			s.targetDevicePK,
			s.linkPK,
			int64(s.rttUs),
			s.loss,
			s.ipdvUs,
		}, nil
	})
	require.NoError(t, err)
}

// validateLinkOutagesByMetroQuery validates that key data exists in the database
func validateLinkOutagesByMetroQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Verify SAO metro exists
	metroQuery := `
SELECT code, name
FROM dz_metros_current
WHERE code = 'sao'
`
	metroResult, err := dataset.Query(ctx, conn, metroQuery, nil)
	require.NoError(t, err, "Failed to execute metro query")
	require.Equal(t, 1, metroResult.Count, "Should have SAO metro")

	// Verify links connected to SAO
	linkQuery := `
SELECT l.code, ma.code AS side_a_metro, mz.code AS side_z_metro
FROM dz_links_current l
JOIN dz_devices_current da ON l.side_a_pk = da.pk
JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
JOIN dz_metros_current ma ON da.metro_pk = ma.pk
JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
WHERE ma.code = 'sao' OR mz.code = 'sao'
ORDER BY l.code
`
	linkResult, err := dataset.Query(ctx, conn, linkQuery, nil)
	require.NoError(t, err, "Failed to execute link query")
	require.Equal(t, 3, linkResult.Count, "Should have exactly 3 links connected to SAO")

	// Verify link history has status changes for SAO links
	historyQuery := `
SELECT lh.code, lh.status, lh.snapshot_ts
FROM dim_dz_links_history lh
WHERE lh.pk IN (
    SELECT l.pk FROM dz_links_current l
    JOIN dz_devices_current da ON l.side_a_pk = da.pk
    JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
    JOIN dz_metros_current ma ON da.metro_pk = ma.pk
    JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
    WHERE ma.code = 'sao' OR mz.code = 'sao'
)
AND lh.snapshot_ts >= now() - INTERVAL 30 DAY
ORDER BY lh.code, lh.snapshot_ts
`
	historyResult, err := dataset.Query(ctx, conn, historyQuery, nil)
	require.NoError(t, err, "Failed to execute history query")
	require.GreaterOrEqual(t, historyResult.Count, 2, "Should have status changes for SAO links in last 30 days")

	// Verify packet loss telemetry exists for nyc-sao-2
	latencyQuery := `
SELECT
    COUNT(*) AS total_samples,
    countIf(loss = true) AS loss_samples
FROM fact_dz_device_link_latency
WHERE link_pk = 'link4'
  AND event_ts >= now() - INTERVAL 30 DAY
`
	latencyResult, err := dataset.Query(ctx, conn, latencyQuery, nil)
	require.NoError(t, err, "Failed to execute latency query")
	require.Equal(t, 1, latencyResult.Count, "Should have latency data")

	totalSamples := latencyResult.Rows[0]["total_samples"].(uint64)
	lossSamples := latencyResult.Rows[0]["loss_samples"].(uint64)
	require.Greater(t, totalSamples, uint64(0), "Should have latency samples")
	require.Greater(t, lossSamples, uint64(0), "Should have loss samples")

	t.Logf("Database validation passed: Found SAO metro, %d SAO-connected links, %d history entries, %d latency samples (%d with loss)",
		linkResult.Count, historyResult.Count, totalSamples, lossSamples)
}
