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

func TestLake_Agent_Evals_Anthropic_NetworkLinkIncidentTimeline(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_NetworkLinkIncidentTimeline(t)
}

func runTest_NetworkLinkIncidentTimeline(t *testing.T) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)

	// Set up test data
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed network link incident timeline data
	seedNetworkLinkIncidentTimelineData(t, ctx, conn)

	// Set up agent with Anthropic LLM client
	agentInstance := setupAgent(t, ctx, db, newAnthropicLLMClient, debug, debugLevel, nil)

	// Run the query
	var output bytes.Buffer
	question := "show timeline for drained link nyc-lon-1"
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

	// Basic validation - the response should show a timeline
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
	validateNetworkLinkIncidentTimelineResponse(t, response)

	// Evaluate with Ollama
	isCorrect, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question")
}

// validateNetworkLinkIncidentTimelineResponse validates that the response includes required timeline elements
func validateNetworkLinkIncidentTimelineResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention timeline or chronological progression
	timelineMentioned := strings.Contains(responseLower, "timeline") ||
		strings.Contains(responseLower, "chronological") ||
		strings.Contains(responseLower, "sequence") ||
		strings.Contains(responseLower, "progression") ||
		strings.Contains(responseLower, "events") ||
		strings.Contains(responseLower, "incident")
	require.True(t, timelineMentioned,
		"Response should mention timeline or chronological progression. Got: %s",
		truncateForError(response, 200))

	// Should mention drain/drained status
	drainMentioned := strings.Contains(responseLower, "drain") ||
		strings.Contains(responseLower, "drained")
	require.True(t, drainMentioned,
		"Response should mention drain/drained status. Got: %s",
		truncateForError(response, 200))

	// Should mention packet loss
	lossMentioned := strings.Contains(responseLower, "loss") ||
		strings.Contains(responseLower, "packet loss")
	require.True(t, lossMentioned,
		"Response should mention packet loss. Got: %s",
		truncateForError(response, 200))

	// Should mention errors or carrier transitions
	errorsMentioned := strings.Contains(responseLower, "error") ||
		strings.Contains(responseLower, "carrier") ||
		strings.Contains(responseLower, "transition") ||
		strings.Contains(responseLower, "discard")
	require.True(t, errorsMentioned,
		"Response should mention errors, discards, or carrier transitions. Got: %s",
		truncateForError(response, 200))

	// Should mention recovery or undrained
	recoveryMentioned := strings.Contains(responseLower, "recover") ||
		strings.Contains(responseLower, "undrain") ||
		strings.Contains(responseLower, "restore") ||
		strings.Contains(responseLower, "resolved") ||
		strings.Contains(responseLower, "activated")
	require.True(t, recoveryMentioned,
		"Response should mention recovery or undrained status. Got: %s",
		truncateForError(response, 200))

	// Should mention specific link code (nyc-lon-1)
	linkCodeMentioned := strings.Contains(responseLower, "nyc-lon-1") ||
		strings.Contains(responseLower, "nyc-lon")
	require.True(t, linkCodeMentioned,
		"Response should mention the specific link code. Got: %s",
		truncateForError(response, 200))

	// Should contain time references (dates, timestamps, or relative times)
	hasTimeReferences := false
	timeKeywords := []string{"2026", "2025", "january", "february", "march", "april", "may", "june",
		"july", "august", "september", "october", "november", "december",
		"hour", "minute", "day", "ago", "at", "on", ":", "am", "pm", "utc"}
	for _, keyword := range timeKeywords {
		if strings.Contains(responseLower, keyword) {
			hasTimeReferences = true
			break
		}
	}
	require.True(t, hasTimeReferences,
		"Response should contain time references (dates, timestamps, or relative times). Got: %s",
		truncateForError(response, 200))

	// Should show progression (multiple events/stages)
	// Check for multiple time references or event markers
	progressionIndicators := []string{"first", "then", "next", "after", "before", "during", "while",
		"initially", "subsequently", "finally", "eventually", "later", "earlier"}
	hasProgression := false
	for _, indicator := range progressionIndicators {
		if strings.Contains(responseLower, indicator) {
			hasProgression = true
			break
		}
	}
	// Also check for numbered lists or bullet points which indicate multiple events
	if strings.Contains(response, "1.") || strings.Contains(response, "- ") ||
		strings.Contains(response, "* ") || strings.Contains(response, "•") {
		hasProgression = true
	}
	require.True(t, hasProgression,
		"Response should show progression with multiple events/stages. Got: %s",
		truncateForError(response, 200))
}

// seedNetworkLinkIncidentTimelineData seeds data for a link incident timeline
// Timeline:
// - T-6h: Normal operation (activated, no issues)
// - T-5h: Issues start (packet loss begins, errors appear)
// - T-4h: Link drained (soft-drained, isis_delay_override_ns = 1000000000)
// - T-3h: Issues continue (packet loss, errors, carrier transitions)
// - T-2h: Recovery begins (errors decrease, packet loss intermittent)
// - T-1h: Link undrained (back to activated, isis_delay_override_ns = NULL)
// - T-0h: Full recovery (no issues, normal operation)
func seedNetworkLinkIncidentTimelineData(t *testing.T, ctx context.Context, conn duck.Connection) {
	loadTablesAndViews(t, ctx, conn)

	// Seed metros
	_, err := conn.ExecContext(ctx, `
		INSERT INTO dz_metros_current (pk, code, name, as_of_ts, row_hash) VALUES
		('metro1', 'nyc', 'New York', CURRENT_TIMESTAMP, 'metrohash1'),
		('metro2', 'lon', 'London', CURRENT_TIMESTAMP, 'metrohash2')
	`)
	require.NoError(t, err)

	// Seed devices
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_devices_current (pk, code, status, metro_pk, device_type, as_of_ts, row_hash) VALUES
		('device1', 'nyc-dzd1', 'activated', 'metro1', 'DZD', CURRENT_TIMESTAMP, 'devicehash1'),
		('device2', 'lon-dzd1', 'activated', 'metro2', 'DZD', CURRENT_TIMESTAMP, 'devicehash2')
	`)
	require.NoError(t, err)

	// Seed link history showing the timeline
	// T-6h: Normal operation (activated)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_links_history (
			pk, code, status, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name,
			link_type, isis_delay_override_ns, committed_rtt_ns, committed_jitter_ns, bandwidth_bps,
			valid_from, valid_to, op, row_hash
		) VALUES
		-- T-6h: Normal operation (activated)
		('link1', 'nyc-lon-1', 'activated', 'device1', 'device2', 'Ethernet1', 'Ethernet1',
		 'WAN', NULL, 50000000, 10000000, 10000000000,
		 CURRENT_TIMESTAMP - INTERVAL '6 hours', CURRENT_TIMESTAMP - INTERVAL '4 hours', 'I', 'linkhash1'),
		-- T-4h: Link drained (soft-drained)
		('link1', 'nyc-lon-1', 'soft-drained', 'device1', 'device2', 'Ethernet1', 'Ethernet1',
		 'WAN', 1000000000, 50000000, 10000000, 10000000000,
		 CURRENT_TIMESTAMP - INTERVAL '4 hours', CURRENT_TIMESTAMP - INTERVAL '1 hour', 'U', 'linkhash2'),
		-- T-1h: Link undrained (back to activated)
		('link1', 'nyc-lon-1', 'activated', 'device1', 'device2', 'Ethernet1', 'Ethernet1',
		 'WAN', NULL, 50000000, 10000000, 10000000000,
		 CURRENT_TIMESTAMP - INTERVAL '1 hour', NULL, 'U', 'linkhash3')
	`)
	require.NoError(t, err)

	// Seed current link state (activated, recovered)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_links_current (
			pk, code, status, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name,
			link_type, isis_delay_override_ns, committed_rtt_ns, committed_jitter_ns, bandwidth_bps,
			as_of_ts, row_hash
		) VALUES
		('link1', 'nyc-lon-1', 'activated', 'device1', 'device2', 'Ethernet1', 'Ethernet1',
		 'WAN', NULL, 50000000, 10000000, 10000000000,
		 CURRENT_TIMESTAMP, 'linkhash3')
	`)
	require.NoError(t, err)

	now := "CURRENT_TIMESTAMP"

	// T-6h to T-5h: Normal operation (no packet loss, no errors)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_link_latency_samples_raw (
			time, epoch, sample_index, origin_device_pk, target_device_pk, link_pk, rtt_us, loss, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '6 hours', 100, 1, 'device1', 'device2', 'link1', 45000, false, 5000),
		(`+now+` - INTERVAL '5 hours 50 minutes', 100, 2, 'device1', 'device2', 'link1', 46000, false, 5200),
		(`+now+` - INTERVAL '5 hours 40 minutes', 100, 3, 'device1', 'device2', 'link1', 47000, false, 4800)
	`)
	require.NoError(t, err)

	// T-5h: Issues start (packet loss begins, some errors)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_link_latency_samples_raw (
			time, epoch, sample_index, origin_device_pk, target_device_pk, link_pk, rtt_us, loss, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '5 hours 30 minutes', 100, 4, 'device1', 'device2', 'link1', 0, true, NULL),
		(`+now+` - INTERVAL '5 hours 20 minutes', 100, 5, 'device1', 'device2', 'link1', 50000, false, 8000),
		(`+now+` - INTERVAL '5 hours 10 minutes', 100, 6, 'device1', 'device2', 'link1', 0, true, NULL),
		(`+now+` - INTERVAL '5 hours', 100, 7, 'device1', 'device2', 'link1', 48000, false, 7500)
	`)
	require.NoError(t, err)

	// T-4h to T-3h: Link drained, issues continue (packet loss, errors, carrier transitions)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_link_latency_samples_raw (
			time, epoch, sample_index, origin_device_pk, target_device_pk, link_pk, rtt_us, loss, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '4 hours 50 minutes', 100, 8, 'device1', 'device2', 'link1', 0, true, NULL),
		(`+now+` - INTERVAL '4 hours 40 minutes', 100, 9, 'device1', 'device2', 'link1', 55000, false, 12000),
		(`+now+` - INTERVAL '4 hours 30 minutes', 100, 10, 'device1', 'device2', 'link1', 0, true, NULL),
		(`+now+` - INTERVAL '4 hours 20 minutes', 100, 11, 'device1', 'device2', 'link1', 60000, false, 15000),
		(`+now+` - INTERVAL '4 hours 10 minutes', 100, 12, 'device1', 'device2', 'link1', 0, true, NULL),
		(`+now+` - INTERVAL '4 hours', 100, 13, 'device1', 'device2', 'link1', 58000, false, 14000),
		(`+now+` - INTERVAL '3 hours 50 minutes', 100, 14, 'device1', 'device2', 'link1', 0, true, NULL),
		(`+now+` - INTERVAL '3 hours 40 minutes', 100, 15, 'device1', 'device2', 'link1', 62000, false, 16000),
		(`+now+` - INTERVAL '3 hours 30 minutes', 100, 16, 'device1', 'device2', 'link1', 0, true, NULL),
		(`+now+` - INTERVAL '3 hours 20 minutes', 100, 17, 'device1', 'device2', 'link1', 59000, false, 14500),
		(`+now+` - INTERVAL '3 hours 10 minutes', 100, 18, 'device1', 'device2', 'link1', 0, true, NULL),
		(`+now+` - INTERVAL '3 hours', 100, 19, 'device1', 'device2', 'link1', 61000, false, 15500)
	`)
	require.NoError(t, err)

	// T-2h: Recovery begins (errors decrease, packet loss intermittent)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_link_latency_samples_raw (
			time, epoch, sample_index, origin_device_pk, target_device_pk, link_pk, rtt_us, loss, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '2 hours 50 minutes', 100, 20, 'device1', 'device2', 'link1', 50000, false, 9000),
		(`+now+` - INTERVAL '2 hours 40 minutes', 100, 21, 'device1', 'device2', 'link1', 0, true, NULL),
		(`+now+` - INTERVAL '2 hours 30 minutes', 100, 22, 'device1', 'device2', 'link1', 51000, false, 8500),
		(`+now+` - INTERVAL '2 hours 20 minutes', 100, 23, 'device1', 'device2', 'link1', 49000, false, 8000),
		(`+now+` - INTERVAL '2 hours 10 minutes', 100, 24, 'device1', 'device2', 'link1', 52000, false, 9000),
		(`+now+` - INTERVAL '2 hours', 100, 25, 'device1', 'device2', 'link1', 48000, false, 7500)
	`)
	require.NoError(t, err)

	// T-1h to T-0h: Full recovery (no issues, normal operation)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_link_latency_samples_raw (
			time, epoch, sample_index, origin_device_pk, target_device_pk, link_pk, rtt_us, loss, ipdv_us
		) VALUES
		(`+now+` - INTERVAL '1 hour 50 minutes', 100, 26, 'device1', 'device2', 'link1', 46000, false, 5500),
		(`+now+` - INTERVAL '1 hour 40 minutes', 100, 27, 'device1', 'device2', 'link1', 45000, false, 5000),
		(`+now+` - INTERVAL '1 hour 30 minutes', 100, 28, 'device1', 'device2', 'link1', 47000, false, 5200),
		(`+now+` - INTERVAL '1 hour 20 minutes', 100, 29, 'device1', 'device2', 'link1', 46000, false, 5100),
		(`+now+` - INTERVAL '1 hour 10 minutes', 100, 30, 'device1', 'device2', 'link1', 45000, false, 4900),
		(`+now+` - INTERVAL '1 hour', 100, 31, 'device1', 'device2', 'link1', 47000, false, 5300),
		(`+now+` - INTERVAL '50 minutes', 100, 32, 'device1', 'device2', 'link1', 46000, false, 5000),
		(`+now+` - INTERVAL '40 minutes', 100, 33, 'device1', 'device2', 'link1', 45000, false, 4800),
		(`+now+` - INTERVAL '30 minutes', 100, 34, 'device1', 'device2', 'link1', 47000, false, 5200),
		(`+now+` - INTERVAL '20 minutes', 100, 35, 'device1', 'device2', 'link1', 46000, false, 5100),
		(`+now+` - INTERVAL '10 minutes', 100, 36, 'device1', 'device2', 'link1', 45000, false, 4900),
		(`+now+` - INTERVAL '0 minutes', 100, 37, 'device1', 'device2', 'link1', 47000, false, 5300)
	`)
	require.NoError(t, err)

	// Seed interface usage with errors and carrier transitions
	// T-5h: Errors start appearing
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_iface_usage_raw (
			time, device_pk, host, intf, link_pk, link_side,
			in_errors_delta, out_errors_delta, in_discards_delta, out_discards_delta, carrier_transitions_delta,
			in_octets_delta, out_octets_delta, in_pkts_delta, out_pkts_delta, delta_duration
		) VALUES
		-- T-5h: Errors start
		(`+now+` - INTERVAL '5 hours', 'device1', 'nyc-dzd1', 'Ethernet1', 'link1', 'A', 5, 3, 2, 1, 0, 1000000, 1000000, 1000, 1000, 3600.0),
		(`+now+` - INTERVAL '5 hours', 'device2', 'lon-dzd1', 'Ethernet1', 'link1', 'Z', 4, 2, 1, 1, 0, 1000000, 1000000, 1000, 1000, 3600.0),
		-- T-4h: Link drained, errors and carrier transitions increase
		(`+now+` - INTERVAL '4 hours', 'device1', 'nyc-dzd1', 'Ethernet1', 'link1', 'A', 15, 10, 8, 5, 2, 500000, 500000, 500, 500, 3600.0),
		(`+now+` - INTERVAL '4 hours', 'device2', 'lon-dzd1', 'Ethernet1', 'link1', 'Z', 12, 8, 6, 4, 1, 500000, 500000, 500, 500, 3600.0),
		-- T-3h: Issues continue
		(`+now+` - INTERVAL '3 hours', 'device1', 'nyc-dzd1', 'Ethernet1', 'link1', 'A', 18, 12, 10, 6, 3, 400000, 400000, 400, 400, 3600.0),
		(`+now+` - INTERVAL '3 hours', 'device2', 'lon-dzd1', 'Ethernet1', 'link1', 'Z', 14, 9, 7, 5, 2, 400000, 400000, 400, 400, 3600.0),
		-- T-2h: Recovery begins, errors decrease
		(`+now+` - INTERVAL '2 hours', 'device1', 'nyc-dzd1', 'Ethernet1', 'link1', 'A', 8, 5, 4, 2, 1, 800000, 800000, 800, 800, 3600.0),
		(`+now+` - INTERVAL '2 hours', 'device2', 'lon-dzd1', 'Ethernet1', 'link1', 'Z', 6, 4, 3, 2, 0, 800000, 800000, 800, 800, 3600.0),
		-- T-1h: Link undrained, errors minimal
		(`+now+` - INTERVAL '1 hour', 'device1', 'nyc-dzd1', 'Ethernet1', 'link1', 'A', 2, 1, 1, 0, 0, 1200000, 1200000, 1200, 1200, 3600.0),
		(`+now+` - INTERVAL '1 hour', 'device2', 'lon-dzd1', 'Ethernet1', 'link1', 'Z', 1, 1, 0, 0, 0, 1200000, 1200000, 1200, 1200, 3600.0),
		-- T-0h: Full recovery, no errors
		(`+now+` - INTERVAL '0 hours', 'device1', 'nyc-dzd1', 'Ethernet1', 'link1', 'A', 0, 0, 0, 0, 0, 1500000, 1500000, 1500, 1500, 3600.0),
		(`+now+` - INTERVAL '0 hours', 'device2', 'lon-dzd1', 'Ethernet1', 'link1', 'Z', 0, 0, 0, 0, 0, 1500000, 1500000, 1500, 1500, 3600.0)
	`)
	require.NoError(t, err)
}
