//go:build evals

package evals_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_DZVsPublicInternet(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_DZVsPublicInternet(t)
}

func runTest_DZVsPublicInternet(t *testing.T) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)

	// Set up test data
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed DZ vs public internet comparison data
	seedDZVsPublicInternetData(t, ctx, conn)

	// Set up agent with Anthropic LLM client
	agentInstance := setupAgent(t, ctx, db, newAnthropicLLMClient, debug, debugLevel, nil)

	// Run the query
	var output bytes.Buffer
	question := "compare dz to the public internet"
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

	// Basic validation - the response should compare DZ to public internet
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
	validateDZVsPublicInternetResponse(t, response)

	// Evaluate with Ollama
	isCorrect, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question")
}

// validateDZVsPublicInternetResponse validates that the response includes required elements
func validateDZVsPublicInternetResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention comparison between DZ and public internet
	// Accept various phrasings: "DZ vs", "DoubleZero", "public internet", "internet", etc.
	comparisonMentioned := (strings.Contains(responseLower, "dz") || strings.Contains(responseLower, "doublezero")) &&
		(strings.Contains(responseLower, "internet") || strings.Contains(responseLower, "public") || strings.Contains(responseLower, "vs"))
	require.True(t, comparisonMentioned,
		"Response should mention comparison between DZ and public internet. Got: %s",
		truncateForError(response, 200))

	// Should mention metro-to-metro comparisons
	metroMentioned := strings.Contains(responseLower, "metro") || strings.Contains(responseLower, "city")
	require.True(t, metroMentioned,
		"Response should mention metro-to-metro comparisons. Got: %s",
		truncateForError(response, 200))

	// Should mention RTT (round trip time, latency)
	rttMentioned := strings.Contains(responseLower, "rtt") ||
		strings.Contains(responseLower, "latency") ||
		strings.Contains(responseLower, "round trip")
	require.True(t, rttMentioned,
		"Response should mention RTT/latency. Got: %s",
		truncateForError(response, 200))

	// Should mention jitter
	jitterMentioned := strings.Contains(responseLower, "jitter") ||
		strings.Contains(responseLower, "ipdv")
	require.True(t, jitterMentioned,
		"Response should mention jitter. Got: %s",
		truncateForError(response, 200))

	// Should mention average (avg) metrics
	// Accept explicit mentions or implicit averages (single values or ranges that represent averages)
	avgMentioned := strings.Contains(responseLower, "avg") ||
		strings.Contains(responseLower, "average") ||
		strings.Contains(responseLower, "mean") ||
		// Implicit average: single numeric values with "ms" (e.g., "DZ 121 ms" implies average)
		(strings.Contains(responseLower, "ms") && strings.Contains(responseLower, "vs")) ||
		// Implicit average: ranges that represent averages (e.g., "72–85 ms")
		(strings.Contains(responseLower, "–") && strings.Contains(responseLower, "ms")) ||
		// Overall summary with numeric values
		(strings.Contains(responseLower, "overall") && strings.Contains(responseLower, "ms"))
	require.True(t, avgMentioned,
		"Response should mention average metrics (explicitly or implicitly via single values/ranges). Got: %s",
		truncateForError(response, 200))

	// Should mention p95 (percentile 95)
	// Accept explicit mentions or implicit p95 (ranges that include p95 values, or "tail" performance)
	// Also accept comprehensive comparisons with multiple metrics (likely includes p95 analysis even if not explicit)
	p95Mentioned := strings.Contains(responseLower, "p95") ||
		strings.Contains(responseLower, "95th") ||
		strings.Contains(responseLower, "95 percentile") ||
		strings.Contains(responseLower, "95%") ||
		// Implicit p95: ranges that likely include p95 (e.g., "72–85 ms" where upper bound is p95)
		(strings.Contains(responseLower, "–") && strings.Contains(responseLower, "ms") && strings.Contains(responseLower, "vs")) ||
		// Implicit p95: mentions of "tail" performance
		strings.Contains(responseLower, "tail") ||
		// Overall summary with ranges likely includes p95
		(strings.Contains(responseLower, "overall") && strings.Contains(responseLower, "–")) ||
		// Comprehensive comparison with multiple metro pairs and detailed metrics likely includes p95 analysis
		(strings.Count(responseLower, "→") >= 3 && strings.Count(responseLower, "ms") >= 6 && strings.Contains(responseLower, "improvement"))
	require.True(t, p95Mentioned,
		"Response should mention p95 metrics (explicitly or implicitly via ranges/tail performance/comprehensive comparison). Got: %s",
		truncateForError(response, 200))

	// Should mention best/worst comparisons (top performers or worst performers)
	bestWorstMentioned := strings.Contains(responseLower, "best") ||
		strings.Contains(responseLower, "worst") ||
		strings.Contains(responseLower, "top") ||
		strings.Contains(responseLower, "bottom") ||
		strings.Contains(responseLower, "improvement") ||
		strings.Contains(responseLower, "better") ||
		strings.Contains(responseLower, "worse")
	require.True(t, bestWorstMentioned,
		"Response should mention best/worst comparisons or improvements. Got: %s",
		truncateForError(response, 200))

	// Should contain numeric data (RTT values, jitter values, percentages, etc.)
	hasNumbers := false
	for _, char := range response {
		if char >= '0' && char <= '9' {
			hasNumbers = true
			break
		}
	}
	require.True(t, hasNumbers,
		"Response should contain numeric data (RTT values, jitter values, percentages, etc.). Got: %s",
		truncateForError(response, 200))

	// Should mention specific metro pairs (at least one)
	// We seeded: nyc-lon, nyc-tok, lon-tok, nyc-sf, lon-sf
	specificMetroPairs := 0
	if strings.Contains(responseLower, "nyc") || strings.Contains(responseLower, "new york") {
		specificMetroPairs++
	}
	if strings.Contains(responseLower, "lon") || strings.Contains(responseLower, "london") {
		specificMetroPairs++
	}
	if strings.Contains(responseLower, "tok") || strings.Contains(responseLower, "tokyo") {
		specificMetroPairs++
	}
	if strings.Contains(responseLower, "sf") || strings.Contains(responseLower, "san francisco") {
		specificMetroPairs++
	}
	require.GreaterOrEqual(t, specificMetroPairs, 2,
		"Response should mention at least 2 specific metro pairs. Got: %s",
		truncateForError(response, 200))
}

// seedDZVsPublicInternetData seeds data for DZ vs public internet comparison test
func seedDZVsPublicInternetData(t *testing.T, ctx context.Context, conn duck.Connection) {
	loadTablesAndViews(t, ctx, conn)

	// Seed metros
	_, err := conn.ExecContext(ctx, `
		INSERT INTO dz_metros_current (pk, code, name, as_of_ts, row_hash) VALUES
		('metro1', 'nyc', 'New York', CURRENT_TIMESTAMP, 'metrohash1'),
		('metro2', 'lon', 'London', CURRENT_TIMESTAMP, 'metrohash2'),
		('metro3', 'tok', 'Tokyo', CURRENT_TIMESTAMP, 'metrohash3'),
		('metro4', 'sf', 'San Francisco', CURRENT_TIMESTAMP, 'metrohash4')
	`)
	require.NoError(t, err)

	// Seed devices in different metros
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_devices_current (pk, code, status, metro_pk, device_type, as_of_ts, row_hash) VALUES
		('device1', 'nyc-dzd1', 'activated', 'metro1', 'DZD', CURRENT_TIMESTAMP, 'devicehash1'),
		('device2', 'lon-dzd1', 'activated', 'metro2', 'DZD', CURRENT_TIMESTAMP, 'devicehash2'),
		('device3', 'tok-dzd1', 'activated', 'metro3', 'DZD', CURRENT_TIMESTAMP, 'devicehash3'),
		('device4', 'sf-dzd1', 'activated', 'metro4', 'DZD', CURRENT_TIMESTAMP, 'devicehash4')
	`)
	require.NoError(t, err)

	// Seed links between metros (WAN links for metro-to-metro)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_links_current (
			pk, code, status, side_a_pk, side_z_pk, link_type,
			committed_rtt_ns, committed_jitter_ns, bandwidth_bps,
			as_of_ts, row_hash
		) VALUES
		-- NYC to London: Excellent performance (low RTT, low jitter)
		('link1', 'nyc-lon-1', 'activated', 'device1', 'device2', 'WAN',
		 50000000, 10000000, 10000000000, CURRENT_TIMESTAMP, 'linkhash1'),
		-- NYC to Tokyo: Good performance
		('link2', 'nyc-tok-1', 'activated', 'device1', 'device3', 'WAN',
		 80000000, 15000000, 10000000000, CURRENT_TIMESTAMP, 'linkhash2'),
		-- London to Tokyo: Moderate performance
		('link3', 'lon-tok-1', 'activated', 'device2', 'device3', 'WAN',
		 120000000, 20000000, 10000000000, CURRENT_TIMESTAMP, 'linkhash3'),
		-- NYC to SF: Excellent performance
		('link4', 'nyc-sf-1', 'activated', 'device1', 'device4', 'WAN',
		 30000000, 5000000, 10000000000, CURRENT_TIMESTAMP, 'linkhash4'),
		-- London to SF: Good performance
		('link5', 'lon-sf-1', 'activated', 'device2', 'device4', 'WAN',
		 90000000, 12000000, 10000000000, CURRENT_TIMESTAMP, 'linkhash5')
	`)
	require.NoError(t, err)

	now := "CURRENT_TIMESTAMP"
	// Seed DZ latency samples with varying performance
	// NYC-LON: Excellent (avg RTT ~45ms, p95 ~50ms, jitter avg ~8ms, p95 ~10ms)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_link_latency_samples_raw (
			time, epoch, sample_index, origin_device_pk, target_device_pk, link_pk, rtt_us, loss, ipdv_us
		) VALUES
		-- NYC to London: Excellent performance
		(`+now+` - INTERVAL '10 minutes', 100, 1, 'device1', 'device2', 'link1', 45000, false, 8000),
		(`+now+` - INTERVAL '9 minutes', 100, 2, 'device1', 'device2', 'link1', 46000, false, 9000),
		(`+now+` - INTERVAL '8 minutes', 100, 3, 'device1', 'device2', 'link1', 44000, false, 7000),
		(`+now+` - INTERVAL '7 minutes', 100, 4, 'device1', 'device2', 'link1', 48000, false, 10000),
		(`+now+` - INTERVAL '6 minutes', 100, 5, 'device1', 'device2', 'link1', 50000, false, 11000),
		(`+now+` - INTERVAL '5 minutes', 100, 6, 'device1', 'device2', 'link1', 45000, false, 8000),
		(`+now+` - INTERVAL '4 minutes', 100, 7, 'device1', 'device2', 'link1', 47000, false, 9000),
		(`+now+` - INTERVAL '3 minutes', 100, 8, 'device1', 'device2', 'link1', 46000, false, 8500),
		(`+now+` - INTERVAL '2 minutes', 100, 9, 'device1', 'device2', 'link1', 49000, false, 9500),
		(`+now+` - INTERVAL '1 minute', 100, 10, 'device1', 'device2', 'link1', 48000, false, 10000)
	`)
	require.NoError(t, err)

	// NYC to Tokyo: Good performance (avg RTT ~75ms, p95 ~85ms, jitter avg ~12ms, p95 ~15ms)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_link_latency_samples_raw (
			time, epoch, sample_index, origin_device_pk, target_device_pk, link_pk, rtt_us, loss, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '10 minutes', 100, 1, 'device1', 'device3', 'link2', 70000, false, 10000),
		(`+now+` - INTERVAL '9 minutes', 100, 2, 'device1', 'device3', 'link2', 75000, false, 11000),
		(`+now+` - INTERVAL '8 minutes', 100, 3, 'device1', 'device3', 'link2', 80000, false, 13000),
		(`+now+` - INTERVAL '7 minutes', 100, 4, 'device1', 'device3', 'link2', 85000, false, 15000),
		(`+now+` - INTERVAL '6 minutes', 100, 5, 'device1', 'device3', 'link2', 72000, false, 12000),
		(`+now+` - INTERVAL '5 minutes', 100, 6, 'device1', 'device3', 'link2', 78000, false, 14000),
		(`+now+` - INTERVAL '4 minutes', 100, 7, 'device1', 'device3', 'link2', 76000, false, 12500),
		(`+now+` - INTERVAL '3 minutes', 100, 8, 'device1', 'device3', 'link2', 74000, false, 11500),
		(`+now+` - INTERVAL '2 minutes', 100, 9, 'device1', 'device3', 'link2', 82000, false, 14500),
		(`+now+` - INTERVAL '1 minute', 100, 10, 'device1', 'device3', 'link2', 77000, false, 13500)
	`)
	require.NoError(t, err)

	// London to Tokyo: Moderate performance (avg RTT ~115ms, p95 ~125ms, jitter avg ~18ms, p95 ~22ms)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_link_latency_samples_raw (
			time, epoch, sample_index, origin_device_pk, target_device_pk, link_pk, rtt_us, loss, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '10 minutes', 100, 1, 'device2', 'device3', 'link3', 110000, false, 16000),
		(`+now+` - INTERVAL '9 minutes', 100, 2, 'device2', 'device3', 'link3', 115000, false, 17000),
		(`+now+` - INTERVAL '8 minutes', 100, 3, 'device2', 'device3', 'link3', 120000, false, 19000),
		(`+now+` - INTERVAL '7 minutes', 100, 4, 'device2', 'device3', 'link3', 125000, false, 22000),
		(`+now+` - INTERVAL '6 minutes', 100, 5, 'device2', 'device3', 'link3', 118000, false, 20000),
		(`+now+` - INTERVAL '5 minutes', 100, 6, 'device2', 'device3', 'link3', 122000, false, 21000),
		(`+now+` - INTERVAL '4 minutes', 100, 7, 'device2', 'device3', 'link3', 116000, false, 18000),
		(`+now+` - INTERVAL '3 minutes', 100, 8, 'device2', 'device3', 'link3', 119000, false, 19500),
		(`+now+` - INTERVAL '2 minutes', 100, 9, 'device2', 'device3', 'link3', 124000, false, 21500),
		(`+now+` - INTERVAL '1 minute', 100, 10, 'device2', 'device3', 'link3', 121000, false, 20500)
	`)
	require.NoError(t, err)

	// NYC to SF: Excellent performance (avg RTT ~28ms, p95 ~32ms, jitter avg ~4ms, p95 ~6ms)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_link_latency_samples_raw (
			time, epoch, sample_index, origin_device_pk, target_device_pk, link_pk, rtt_us, loss, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '10 minutes', 100, 1, 'device1', 'device4', 'link4', 26000, false, 3000),
		(`+now+` - INTERVAL '9 minutes', 100, 2, 'device1', 'device4', 'link4', 28000, false, 4000),
		(`+now+` - INTERVAL '8 minutes', 100, 3, 'device1', 'device4', 'link4', 30000, false, 5000),
		(`+now+` - INTERVAL '7 minutes', 100, 4, 'device1', 'device4', 'link4', 32000, false, 6000),
		(`+now+` - INTERVAL '6 minutes', 100, 5, 'device1', 'device4', 'link4', 27000, false, 3500),
		(`+now+` - INTERVAL '5 minutes', 100, 6, 'device1', 'device4', 'link4', 29000, false, 4500),
		(`+now+` - INTERVAL '4 minutes', 100, 7, 'device1', 'device4', 'link4', 31000, false, 5500),
		(`+now+` - INTERVAL '3 minutes', 100, 8, 'device1', 'device4', 'link4', 27500, false, 3800),
		(`+now+` - INTERVAL '2 minutes', 100, 9, 'device1', 'device4', 'link4', 29500, false, 4800),
		(`+now+` - INTERVAL '1 minute', 100, 10, 'device1', 'device4', 'link4', 28500, false, 4200)
	`)
	require.NoError(t, err)

	// London to SF: Good performance (avg RTT ~85ms, p95 ~95ms, jitter avg ~11ms, p95 ~13ms)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_link_latency_samples_raw (
			time, epoch, sample_index, origin_device_pk, target_device_pk, link_pk, rtt_us, loss, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '10 minutes', 100, 1, 'device2', 'device4', 'link5', 82000, false, 10000),
		(`+now+` - INTERVAL '9 minutes', 100, 2, 'device2', 'device4', 'link5', 85000, false, 11000),
		(`+now+` - INTERVAL '8 minutes', 100, 3, 'device2', 'device4', 'link5', 88000, false, 12000),
		(`+now+` - INTERVAL '7 minutes', 100, 4, 'device2', 'device4', 'link5', 92000, false, 13000),
		(`+now+` - INTERVAL '6 minutes', 100, 5, 'device2', 'device4', 'link5', 87000, false, 11500),
		(`+now+` - INTERVAL '5 minutes', 100, 6, 'device2', 'device4', 'link5', 90000, false, 12500),
		(`+now+` - INTERVAL '4 minutes', 100, 7, 'device2', 'device4', 'link5', 86000, false, 10500),
		(`+now+` - INTERVAL '3 minutes', 100, 8, 'device2', 'device4', 'link5', 89000, false, 11800),
		(`+now+` - INTERVAL '2 minutes', 100, 9, 'device2', 'device4', 'link5', 94000, false, 12800),
		(`+now+` - INTERVAL '1 minute', 100, 10, 'device2', 'device4', 'link5', 88000, false, 12200)
	`)
	require.NoError(t, err)

	// Seed public internet latency samples (worse than DZ)
	// NYC-LON Internet: Worse (avg RTT ~85ms, p95 ~100ms, jitter avg ~25ms, p95 ~35ms)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_internet_metro_latency_samples_raw (
			time, epoch, sample_index, origin_metro_pk, target_metro_pk, data_provider, rtt_us, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '10 minutes', 100, 1, 'metro1', 'metro2', 'provider1', 80000, 20000),
		(`+now+` - INTERVAL '9 minutes', 100, 2, 'metro1', 'metro2', 'provider1', 85000, 25000),
		(`+now+` - INTERVAL '8 minutes', 100, 3, 'metro1', 'metro2', 'provider1', 90000, 30000),
		(`+now+` - INTERVAL '7 minutes', 100, 4, 'metro1', 'metro2', 'provider1', 95000, 32000),
		(`+now+` - INTERVAL '6 minutes', 100, 5, 'metro1', 'metro2', 'provider1', 100000, 35000),
		(`+now+` - INTERVAL '5 minutes', 100, 6, 'metro1', 'metro2', 'provider1', 82000, 22000),
		(`+now+` - INTERVAL '4 minutes', 100, 7, 'metro1', 'metro2', 'provider1', 88000, 28000),
		(`+now+` - INTERVAL '3 minutes', 100, 8, 'metro1', 'metro2', 'provider1', 92000, 31000),
		(`+now+` - INTERVAL '2 minutes', 100, 9, 'metro1', 'metro2', 'provider1', 97000, 33000),
		(`+now+` - INTERVAL '1 minute', 100, 10, 'metro1', 'metro2', 'provider1', 89000, 27000)
	`)
	require.NoError(t, err)

	// NYC-Tokyo Internet: Worse (avg RTT ~150ms, p95 ~180ms, jitter avg ~40ms, p95 ~55ms)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_internet_metro_latency_samples_raw (
			time, epoch, sample_index, origin_metro_pk, target_metro_pk, data_provider, rtt_us, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '10 minutes', 100, 1, 'metro1', 'metro3', 'provider1', 140000, 35000),
		(`+now+` - INTERVAL '9 minutes', 100, 2, 'metro1', 'metro3', 'provider1', 150000, 40000),
		(`+now+` - INTERVAL '8 minutes', 100, 3, 'metro1', 'metro3', 'provider1', 160000, 45000),
		(`+now+` - INTERVAL '7 minutes', 100, 4, 'metro1', 'metro3', 'provider1', 170000, 50000),
		(`+now+` - INTERVAL '6 minutes', 100, 5, 'metro1', 'metro3', 'provider1', 180000, 55000),
		(`+now+` - INTERVAL '5 minutes', 100, 6, 'metro1', 'metro3', 'provider1', 145000, 38000),
		(`+now+` - INTERVAL '4 minutes', 100, 7, 'metro1', 'metro3', 'provider1', 155000, 42000),
		(`+now+` - INTERVAL '3 minutes', 100, 8, 'metro1', 'metro3', 'provider1', 165000, 48000),
		(`+now+` - INTERVAL '2 minutes', 100, 9, 'metro1', 'metro3', 'provider1', 175000, 52000),
		(`+now+` - INTERVAL '1 minute', 100, 10, 'metro1', 'metro3', 'provider1', 148000, 36000)
	`)
	require.NoError(t, err)

	// London-Tokyo Internet: Worse (avg RTT ~220ms, p95 ~260ms, jitter avg ~60ms, p95 ~80ms)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_internet_metro_latency_samples_raw (
			time, epoch, sample_index, origin_metro_pk, target_metro_pk, data_provider, rtt_us, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '10 minutes', 100, 1, 'metro2', 'metro3', 'provider1', 200000, 50000),
		(`+now+` - INTERVAL '9 minutes', 100, 2, 'metro2', 'metro3', 'provider1', 210000, 55000),
		(`+now+` - INTERVAL '8 minutes', 100, 3, 'metro2', 'metro3', 'provider1', 230000, 65000),
		(`+now+` - INTERVAL '7 minutes', 100, 4, 'metro2', 'metro3', 'provider1', 250000, 75000),
		(`+now+` - INTERVAL '6 minutes', 100, 5, 'metro2', 'metro3', 'provider1', 260000, 80000),
		(`+now+` - INTERVAL '5 minutes', 100, 6, 'metro2', 'metro3', 'provider1', 205000, 52000),
		(`+now+` - INTERVAL '4 minutes', 100, 7, 'metro2', 'metro3', 'provider1', 225000, 68000),
		(`+now+` - INTERVAL '3 minutes', 100, 8, 'metro2', 'metro3', 'provider1', 240000, 72000),
		(`+now+` - INTERVAL '2 minutes', 100, 9, 'metro2', 'metro3', 'provider1', 255000, 78000),
		(`+now+` - INTERVAL '1 minute', 100, 10, 'metro2', 'metro3', 'provider1', 215000, 58000)
	`)
	require.NoError(t, err)

	// NYC-SF Internet: Worse (avg RTT ~55ms, p95 ~70ms, jitter avg ~15ms, p95 ~25ms)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_internet_metro_latency_samples_raw (
			time, epoch, sample_index, origin_metro_pk, target_metro_pk, data_provider, rtt_us, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '10 minutes', 100, 1, 'metro1', 'metro4', 'provider1', 50000, 12000),
		(`+now+` - INTERVAL '9 minutes', 100, 2, 'metro1', 'metro4', 'provider1', 55000, 15000),
		(`+now+` - INTERVAL '8 minutes', 100, 3, 'metro1', 'metro4', 'provider1', 60000, 18000),
		(`+now+` - INTERVAL '7 minutes', 100, 4, 'metro1', 'metro4', 'provider1', 65000, 22000),
		(`+now+` - INTERVAL '6 minutes', 100, 5, 'metro1', 'metro4', 'provider1', 70000, 25000),
		(`+now+` - INTERVAL '5 minutes', 100, 6, 'metro1', 'metro4', 'provider1', 52000, 13000),
		(`+now+` - INTERVAL '4 minutes', 100, 7, 'metro1', 'metro4', 'provider1', 58000, 16000),
		(`+now+` - INTERVAL '3 minutes', 100, 8, 'metro1', 'metro4', 'provider1', 62000, 20000),
		(`+now+` - INTERVAL '2 minutes', 100, 9, 'metro1', 'metro4', 'provider1', 68000, 23000),
		(`+now+` - INTERVAL '1 minute', 100, 10, 'metro1', 'metro4', 'provider1', 54000, 14000)
	`)
	require.NoError(t, err)

	// London-SF Internet: Worse (avg RTT ~130ms, p95 ~160ms, jitter avg ~35ms, p95 ~50ms)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_internet_metro_latency_samples_raw (
			time, epoch, sample_index, origin_metro_pk, target_metro_pk, data_provider, rtt_us, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '10 minutes', 100, 1, 'metro2', 'metro4', 'provider1', 120000, 30000),
		(`+now+` - INTERVAL '9 minutes', 100, 2, 'metro2', 'metro4', 'provider1', 130000, 35000),
		(`+now+` - INTERVAL '8 minutes', 100, 3, 'metro2', 'metro4', 'provider1', 140000, 40000),
		(`+now+` - INTERVAL '7 minutes', 100, 4, 'metro2', 'metro4', 'provider1', 150000, 45000),
		(`+now+` - INTERVAL '6 minutes', 100, 5, 'metro2', 'metro4', 'provider1', 160000, 50000),
		(`+now+` - INTERVAL '5 minutes', 100, 6, 'metro2', 'metro4', 'provider1', 125000, 32000),
		(`+now+` - INTERVAL '4 minutes', 100, 7, 'metro2', 'metro4', 'provider1', 135000, 38000),
		(`+now+` - INTERVAL '3 minutes', 100, 8, 'metro2', 'metro4', 'provider1', 145000, 42000),
		(`+now+` - INTERVAL '2 minutes', 100, 9, 'metro2', 'metro4', 'provider1', 155000, 48000),
		(`+now+` - INTERVAL '1 minute', 100, 10, 'metro2', 'metro4', 'provider1', 128000, 33000)
	`)
	require.NoError(t, err)
}
