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

func TestLake_Agent_Evals_Anthropic_DZVsPublicInternet(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_DZVsPublicInternet(t, newAnthropicLLMClient)
}


func runTest_DZVsPublicInternet(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed DZ vs public internet comparison data
	seedDZVsPublicInternetData(t, ctx, conn)

	// Validate database query results before testing agent
	validateDZVsPublicInternetQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query
	question := "compare dz to the public internet"
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

	// Basic validation - the response should compare DZ to public internet
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

	// Evaluate with Ollama - include specific expectations
	expectations := []Expectation{
		{
			Description:   "Side-by-side latency comparison with numbers",
			ExpectedValue: "shows DZ latency vs public internet latency in format like 'X ms vs Y ms' for each metro pair",
			Rationale:     "Response should show actual numeric comparisons between DZ and public internet latency",
		},
		{
			Description:   "Jitter/IPDV comparison",
			ExpectedValue: "shows jitter or IPDV metrics comparing DZ to public internet",
			Rationale:     "Jitter is a key performance metric for network quality",
		},
		{
			Description:   "Multiple metro-to-metro paths",
			ExpectedValue: "mentions specific metro pairs like nyc-lon, nyc-tok, lon-sf, etc.",
			Rationale:     "Performance varies by route so multiple paths should be shown",
		},
		{
			Description:   "DZ performs better than public internet",
			ExpectedValue: "DZ shows lower latency and jitter than public internet (faster/better performance)",
			Rationale:     "Based on test data, DZ outperforms public internet on all routes",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly answer the question")
}

// seedDZVsPublicInternetData seeds data for DZ vs public internet comparison test
func seedDZVsPublicInternetData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "tok", Name: "Tokyo"},
		{PK: "metro4", Code: "sf", Name: "San Francisco"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices in different metros
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "tok-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
		{PK: "device4", Code: "sf-dzd1", Status: "activated", MetroPK: "metro4", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed links between metros (WAN links for metro-to-metro)
	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	links := []serviceability.Link{
		{PK: "link1", Code: "nyc-lon-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000, CommittedRTTNs: 50000000, CommittedJitterNs: 10000000},
		{PK: "link2", Code: "nyc-tok-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device3", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000, CommittedRTTNs: 80000000, CommittedJitterNs: 15000000},
		{PK: "link3", Code: "lon-tok-1", Status: "activated", LinkType: "WAN", SideAPK: "device2", SideZPK: "device3", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000, CommittedRTTNs: 120000000, CommittedJitterNs: 20000000},
		{PK: "link4", Code: "nyc-sf-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device4", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000, CommittedRTTNs: 30000000, CommittedJitterNs: 5000000},
		{PK: "link5", Code: "lon-sf-1", Status: "activated", LinkType: "WAN", SideAPK: "device2", SideZPK: "device4", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000, CommittedRTTNs: 90000000, CommittedJitterNs: 12000000},
	}
	var linkSchema serviceability.LinkSchema
	err = linkDS.WriteBatch(ctx, conn, len(links), func(i int) ([]any, error) {
		return linkSchema.ToRow(links[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       testOpID(),
	})
	require.NoError(t, err)
	// Seed DZ latency samples with varying performance
	latencyDS, err := dztelemlatency.NewDeviceLinkLatencyDataset(log)
	require.NoError(t, err)
	ingestedAt := now
	latencySamples := []struct {
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
		// NYC to London: Excellent performance (avg RTT ~45ms, p95 ~50ms, jitter avg ~8ms, p95 ~10ms)
		{now.Add(-10 * time.Minute), 100, 1, "device1", "device2", "link1", 45000, false, int64Ptr(8000)},
		{now.Add(-9 * time.Minute), 100, 2, "device1", "device2", "link1", 46000, false, int64Ptr(9000)},
		{now.Add(-8 * time.Minute), 100, 3, "device1", "device2", "link1", 44000, false, int64Ptr(7000)},
		{now.Add(-7 * time.Minute), 100, 4, "device1", "device2", "link1", 48000, false, int64Ptr(10000)},
		{now.Add(-6 * time.Minute), 100, 5, "device1", "device2", "link1", 50000, false, int64Ptr(11000)},
		{now.Add(-5 * time.Minute), 100, 6, "device1", "device2", "link1", 45000, false, int64Ptr(8000)},
		{now.Add(-4 * time.Minute), 100, 7, "device1", "device2", "link1", 47000, false, int64Ptr(9000)},
		{now.Add(-3 * time.Minute), 100, 8, "device1", "device2", "link1", 46000, false, int64Ptr(8500)},
		{now.Add(-2 * time.Minute), 100, 9, "device1", "device2", "link1", 49000, false, int64Ptr(9500)},
		{now.Add(-1 * time.Minute), 100, 10, "device1", "device2", "link1", 48000, false, int64Ptr(10000)},
		// NYC to Tokyo: Good performance (avg RTT ~75ms, p95 ~85ms, jitter avg ~12ms, p95 ~15ms)
		{now.Add(-10 * time.Minute), 100, 1, "device1", "device3", "link2", 70000, false, int64Ptr(10000)},
		{now.Add(-9 * time.Minute), 100, 2, "device1", "device3", "link2", 75000, false, int64Ptr(11000)},
		{now.Add(-8 * time.Minute), 100, 3, "device1", "device3", "link2", 80000, false, int64Ptr(13000)},
		{now.Add(-7 * time.Minute), 100, 4, "device1", "device3", "link2", 85000, false, int64Ptr(15000)},
		{now.Add(-6 * time.Minute), 100, 5, "device1", "device3", "link2", 72000, false, int64Ptr(12000)},
		{now.Add(-5 * time.Minute), 100, 6, "device1", "device3", "link2", 78000, false, int64Ptr(14000)},
		{now.Add(-4 * time.Minute), 100, 7, "device1", "device3", "link2", 76000, false, int64Ptr(12500)},
		{now.Add(-3 * time.Minute), 100, 8, "device1", "device3", "link2", 74000, false, int64Ptr(11500)},
		{now.Add(-2 * time.Minute), 100, 9, "device1", "device3", "link2", 82000, false, int64Ptr(14500)},
		{now.Add(-1 * time.Minute), 100, 10, "device1", "device3", "link2", 77000, false, int64Ptr(13500)},
		// London to Tokyo: Moderate performance (avg RTT ~115ms, p95 ~125ms, jitter avg ~18ms, p95 ~22ms)
		{now.Add(-10 * time.Minute), 100, 1, "device2", "device3", "link3", 110000, false, int64Ptr(16000)},
		{now.Add(-9 * time.Minute), 100, 2, "device2", "device3", "link3", 115000, false, int64Ptr(17000)},
		{now.Add(-8 * time.Minute), 100, 3, "device2", "device3", "link3", 120000, false, int64Ptr(19000)},
		{now.Add(-7 * time.Minute), 100, 4, "device2", "device3", "link3", 125000, false, int64Ptr(22000)},
		{now.Add(-6 * time.Minute), 100, 5, "device2", "device3", "link3", 118000, false, int64Ptr(20000)},
		{now.Add(-5 * time.Minute), 100, 6, "device2", "device3", "link3", 122000, false, int64Ptr(21000)},
		{now.Add(-4 * time.Minute), 100, 7, "device2", "device3", "link3", 116000, false, int64Ptr(18000)},
		{now.Add(-3 * time.Minute), 100, 8, "device2", "device3", "link3", 119000, false, int64Ptr(19500)},
		{now.Add(-2 * time.Minute), 100, 9, "device2", "device3", "link3", 124000, false, int64Ptr(21500)},
		{now.Add(-1 * time.Minute), 100, 10, "device2", "device3", "link3", 121000, false, int64Ptr(20500)},
		// NYC to SF: Excellent performance (avg RTT ~28ms, p95 ~32ms, jitter avg ~4ms, p95 ~6ms)
		{now.Add(-10 * time.Minute), 100, 1, "device1", "device4", "link4", 26000, false, int64Ptr(3000)},
		{now.Add(-9 * time.Minute), 100, 2, "device1", "device4", "link4", 28000, false, int64Ptr(4000)},
		{now.Add(-8 * time.Minute), 100, 3, "device1", "device4", "link4", 30000, false, int64Ptr(5000)},
		{now.Add(-7 * time.Minute), 100, 4, "device1", "device4", "link4", 32000, false, int64Ptr(6000)},
		{now.Add(-6 * time.Minute), 100, 5, "device1", "device4", "link4", 27000, false, int64Ptr(3500)},
		{now.Add(-5 * time.Minute), 100, 6, "device1", "device4", "link4", 29000, false, int64Ptr(4500)},
		{now.Add(-4 * time.Minute), 100, 7, "device1", "device4", "link4", 31000, false, int64Ptr(5500)},
		{now.Add(-3 * time.Minute), 100, 8, "device1", "device4", "link4", 27500, false, int64Ptr(3800)},
		{now.Add(-2 * time.Minute), 100, 9, "device1", "device4", "link4", 29500, false, int64Ptr(4800)},
		{now.Add(-1 * time.Minute), 100, 10, "device1", "device4", "link4", 28500, false, int64Ptr(4200)},
		// London to SF: Good performance (avg RTT ~85ms, p95 ~95ms, jitter avg ~11ms, p95 ~13ms)
		{now.Add(-10 * time.Minute), 100, 1, "device2", "device4", "link5", 82000, false, int64Ptr(10000)},
		{now.Add(-9 * time.Minute), 100, 2, "device2", "device4", "link5", 85000, false, int64Ptr(11000)},
		{now.Add(-8 * time.Minute), 100, 3, "device2", "device4", "link5", 88000, false, int64Ptr(12000)},
		{now.Add(-7 * time.Minute), 100, 4, "device2", "device4", "link5", 92000, false, int64Ptr(13000)},
		{now.Add(-6 * time.Minute), 100, 5, "device2", "device4", "link5", 87000, false, int64Ptr(11500)},
		{now.Add(-5 * time.Minute), 100, 6, "device2", "device4", "link5", 90000, false, int64Ptr(12500)},
		{now.Add(-4 * time.Minute), 100, 7, "device2", "device4", "link5", 86000, false, int64Ptr(10500)},
		{now.Add(-3 * time.Minute), 100, 8, "device2", "device4", "link5", 89000, false, int64Ptr(11800)},
		{now.Add(-2 * time.Minute), 100, 9, "device2", "device4", "link5", 94000, false, int64Ptr(12800)},
		{now.Add(-1 * time.Minute), 100, 10, "device2", "device4", "link5", 88000, false, int64Ptr(12200)},
	}
	err = latencyDS.WriteBatch(ctx, conn, len(latencySamples), func(i int) ([]any, error) {
		s := latencySamples[i]
		return []any{
			s.time.UTC(),     // event_ts
			ingestedAt,       // ingested_at
			s.epoch,          // epoch
			s.sampleIndex,    // sample_index
			s.originDevicePK, // origin_device_pk
			s.targetDevicePK, // target_device_pk
			s.linkPK,         // link_pk
			int64(s.rttUs),   // rtt_us
			s.loss,           // loss
			s.ipdvUs,         // ipdv_us
		}, nil
	})
	require.NoError(t, err)

	// Seed public internet latency samples (worse than DZ)
	internetLatencyDS, err := dztelemlatency.NewInternetMetroLatencyDataset(log)
	require.NoError(t, err)
	internetLatencySamples := []struct {
		time          time.Time
		epoch         int64
		sampleIndex   int32
		originMetroPK string
		targetMetroPK string
		dataProvider  string
		rttUs         uint32
		ipdvUs        *int64
	}{
		// NYC-LON Internet: Worse (avg RTT ~85ms, p95 ~100ms, jitter avg ~25ms, p95 ~35ms)
		{now.Add(-10 * time.Minute), 100, 1, "metro1", "metro2", "provider1", 80000, int64Ptr(20000)},
		{now.Add(-9 * time.Minute), 100, 2, "metro1", "metro2", "provider1", 85000, int64Ptr(25000)},
		{now.Add(-8 * time.Minute), 100, 3, "metro1", "metro2", "provider1", 90000, int64Ptr(30000)},
		{now.Add(-7 * time.Minute), 100, 4, "metro1", "metro2", "provider1", 95000, int64Ptr(32000)},
		{now.Add(-6 * time.Minute), 100, 5, "metro1", "metro2", "provider1", 100000, int64Ptr(35000)},
		{now.Add(-5 * time.Minute), 100, 6, "metro1", "metro2", "provider1", 82000, int64Ptr(22000)},
		{now.Add(-4 * time.Minute), 100, 7, "metro1", "metro2", "provider1", 88000, int64Ptr(28000)},
		{now.Add(-3 * time.Minute), 100, 8, "metro1", "metro2", "provider1", 92000, int64Ptr(31000)},
		{now.Add(-2 * time.Minute), 100, 9, "metro1", "metro2", "provider1", 97000, int64Ptr(33000)},
		{now.Add(-1 * time.Minute), 100, 10, "metro1", "metro2", "provider1", 89000, int64Ptr(27000)},
		// NYC-Tokyo Internet: Worse (avg RTT ~150ms, p95 ~180ms, jitter avg ~40ms, p95 ~55ms)
		{now.Add(-10 * time.Minute), 100, 1, "metro1", "metro3", "provider1", 140000, int64Ptr(35000)},
		{now.Add(-9 * time.Minute), 100, 2, "metro1", "metro3", "provider1", 150000, int64Ptr(40000)},
		{now.Add(-8 * time.Minute), 100, 3, "metro1", "metro3", "provider1", 160000, int64Ptr(45000)},
		{now.Add(-7 * time.Minute), 100, 4, "metro1", "metro3", "provider1", 170000, int64Ptr(50000)},
		{now.Add(-6 * time.Minute), 100, 5, "metro1", "metro3", "provider1", 180000, int64Ptr(55000)},
		{now.Add(-5 * time.Minute), 100, 6, "metro1", "metro3", "provider1", 145000, int64Ptr(38000)},
		{now.Add(-4 * time.Minute), 100, 7, "metro1", "metro3", "provider1", 155000, int64Ptr(42000)},
		{now.Add(-3 * time.Minute), 100, 8, "metro1", "metro3", "provider1", 165000, int64Ptr(48000)},
		{now.Add(-2 * time.Minute), 100, 9, "metro1", "metro3", "provider1", 175000, int64Ptr(52000)},
		{now.Add(-1 * time.Minute), 100, 10, "metro1", "metro3", "provider1", 148000, int64Ptr(36000)},
		// London-Tokyo Internet: Worse (avg RTT ~220ms, p95 ~260ms, jitter avg ~60ms, p95 ~80ms)
		{now.Add(-10 * time.Minute), 100, 1, "metro2", "metro3", "provider1", 200000, int64Ptr(50000)},
		{now.Add(-9 * time.Minute), 100, 2, "metro2", "metro3", "provider1", 210000, int64Ptr(55000)},
		{now.Add(-8 * time.Minute), 100, 3, "metro2", "metro3", "provider1", 230000, int64Ptr(65000)},
		{now.Add(-7 * time.Minute), 100, 4, "metro2", "metro3", "provider1", 250000, int64Ptr(75000)},
		{now.Add(-6 * time.Minute), 100, 5, "metro2", "metro3", "provider1", 260000, int64Ptr(80000)},
		{now.Add(-5 * time.Minute), 100, 6, "metro2", "metro3", "provider1", 205000, int64Ptr(52000)},
		{now.Add(-4 * time.Minute), 100, 7, "metro2", "metro3", "provider1", 225000, int64Ptr(68000)},
		{now.Add(-3 * time.Minute), 100, 8, "metro2", "metro3", "provider1", 240000, int64Ptr(72000)},
		{now.Add(-2 * time.Minute), 100, 9, "metro2", "metro3", "provider1", 255000, int64Ptr(78000)},
		{now.Add(-1 * time.Minute), 100, 10, "metro2", "metro3", "provider1", 215000, int64Ptr(58000)},
		// NYC-SF Internet: Worse (avg RTT ~55ms, p95 ~70ms, jitter avg ~15ms, p95 ~25ms)
		{now.Add(-10 * time.Minute), 100, 1, "metro1", "metro4", "provider1", 50000, int64Ptr(12000)},
		{now.Add(-9 * time.Minute), 100, 2, "metro1", "metro4", "provider1", 55000, int64Ptr(15000)},
		{now.Add(-8 * time.Minute), 100, 3, "metro1", "metro4", "provider1", 60000, int64Ptr(18000)},
		{now.Add(-7 * time.Minute), 100, 4, "metro1", "metro4", "provider1", 65000, int64Ptr(22000)},
		{now.Add(-6 * time.Minute), 100, 5, "metro1", "metro4", "provider1", 70000, int64Ptr(25000)},
		{now.Add(-5 * time.Minute), 100, 6, "metro1", "metro4", "provider1", 52000, int64Ptr(13000)},
		{now.Add(-4 * time.Minute), 100, 7, "metro1", "metro4", "provider1", 58000, int64Ptr(16000)},
		{now.Add(-3 * time.Minute), 100, 8, "metro1", "metro4", "provider1", 62000, int64Ptr(20000)},
		{now.Add(-2 * time.Minute), 100, 9, "metro1", "metro4", "provider1", 68000, int64Ptr(23000)},
		{now.Add(-1 * time.Minute), 100, 10, "metro1", "metro4", "provider1", 54000, int64Ptr(14000)},
		// London-SF Internet: Worse (avg RTT ~130ms, p95 ~160ms, jitter avg ~35ms, p95 ~50ms)
		{now.Add(-10 * time.Minute), 100, 1, "metro2", "metro4", "provider1", 120000, int64Ptr(30000)},
		{now.Add(-9 * time.Minute), 100, 2, "metro2", "metro4", "provider1", 130000, int64Ptr(35000)},
		{now.Add(-8 * time.Minute), 100, 3, "metro2", "metro4", "provider1", 140000, int64Ptr(40000)},
		{now.Add(-7 * time.Minute), 100, 4, "metro2", "metro4", "provider1", 150000, int64Ptr(45000)},
		{now.Add(-6 * time.Minute), 100, 5, "metro2", "metro4", "provider1", 160000, int64Ptr(50000)},
		{now.Add(-5 * time.Minute), 100, 6, "metro2", "metro4", "provider1", 125000, int64Ptr(32000)},
		{now.Add(-4 * time.Minute), 100, 7, "metro2", "metro4", "provider1", 135000, int64Ptr(38000)},
		{now.Add(-3 * time.Minute), 100, 8, "metro2", "metro4", "provider1", 145000, int64Ptr(42000)},
		{now.Add(-2 * time.Minute), 100, 9, "metro2", "metro4", "provider1", 155000, int64Ptr(48000)},
		{now.Add(-1 * time.Minute), 100, 10, "metro2", "metro4", "provider1", 128000, int64Ptr(33000)},
	}
	err = internetLatencyDS.WriteBatch(ctx, conn, len(internetLatencySamples), func(i int) ([]any, error) {
		s := internetLatencySamples[i]
		return []any{
			s.time.UTC(),    // event_ts
			ingestedAt,      // ingested_at
			s.epoch,         // epoch
			s.sampleIndex,   // sample_index
			s.originMetroPK, // origin_metro_pk
			s.targetMetroPK, // target_metro_pk
			s.dataProvider,  // data_provider
			int64(s.rttUs),  // rtt_us
			s.ipdvUs,        // ipdv_us
		}, nil
	})
	require.NoError(t, err)
}

// validateDZVsPublicInternetQuery validates that key data exists in the database
func validateDZVsPublicInternetQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Verify DZ latency data exists
	dzQuery := `
SELECT COUNT(*) AS sample_count
FROM fact_dz_device_link_latency
WHERE event_ts >= now() - INTERVAL 24 HOUR
`

	dzResult, err := dataset.Query(ctx, conn, dzQuery, nil)
	require.NoError(t, err, "Failed to execute DZ latency query")
	require.Equal(t, 1, dzResult.Count, "Query should return exactly one row")

	dzCount, ok := dzResult.Rows[0]["sample_count"].(uint64)
	if !ok {
		switch v := dzResult.Rows[0]["sample_count"].(type) {
		case int64:
			dzCount = uint64(v)
		case int:
			dzCount = uint64(v)
		default:
			t.Fatalf("Unexpected type for sample_count: %T", v)
		}
	}

	require.Greater(t, dzCount, uint64(0), "Should have DZ latency samples")

	// Verify public internet latency data exists
	internetQuery := `
SELECT COUNT(*) AS sample_count
FROM fact_dz_internet_metro_latency
WHERE event_ts >= now() - INTERVAL 24 HOUR
`

	internetResult, err := dataset.Query(ctx, conn, internetQuery, nil)
	require.NoError(t, err, "Failed to execute internet latency query")
	require.Equal(t, 1, internetResult.Count, "Query should return exactly one row")

	internetCount, ok := internetResult.Rows[0]["sample_count"].(uint64)
	if !ok {
		switch v := internetResult.Rows[0]["sample_count"].(type) {
		case int64:
			internetCount = uint64(v)
		case int:
			internetCount = uint64(v)
		default:
			t.Fatalf("Unexpected type for sample_count: %T", v)
		}
	}

	require.Greater(t, internetCount, uint64(0), "Should have public internet latency samples")

	t.Logf("Database validation passed: Found %d DZ latency samples and %d internet latency samples", dzCount, internetCount)
}
