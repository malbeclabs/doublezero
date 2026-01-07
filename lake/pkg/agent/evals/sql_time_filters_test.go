//go:build evals

package evals_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// SQL Time Filter Evals: Verify queries don't use Grafana macros
// =============================================================================

func TestLake_Agent_Evals_Anthropic_NoGrafanaMacros(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}
	runTest_NoGrafanaMacros(t)
}

func TestLake_Agent_Evals_Anthropic_FactTableTimeFilters(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}
	runTest_FactTableTimeFilters(t)
}

// =============================================================================
// Test Implementations
// =============================================================================

func runTest_NoGrafanaMacros(t *testing.T) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed latency data
	seedLatencyData(t, ctx, conn)

	// Set up agent with SQL-capturing querier
	baseQuerier := testQuerier(t, db)
	capturingQuerier := newSQLCapturingQuerier(baseQuerier)

	agentInstance := setupAgentWithCapturingQuerier(t, ctx, db, capturingQuerier,
		newAnthropicLLMClient, debug, debugLevel, nil)

	// Ask a time-based question that requires querying fact tables
	var output bytes.Buffer
	question := "What was the average latency on the NYC to London link in the last 24 hours?"

	if debug {
		t.Logf("=== Query: '%s' ===\n", question)
	}

	result, err := agentInstance.Run(ctx, question, &output)
	require.NoError(t, err)
	require.NotEmpty(t, result.FinalText)

	if debug {
		t.Logf("=== Response ===\n%s\n", result.FinalText)
	}

	// Validate SQL doesn't contain Grafana macros
	queries := capturingQuerier.GetQueries()
	require.NotEmpty(t, queries, "Agent should have executed at least one query")

	if debug {
		t.Logf("=== Executed %d queries ===", len(queries))
		for i, q := range queries {
			t.Logf("Query %d: %s", i+1, truncateForError(q, 200))
		}
	}

	validateNoGrafanaMacros(t, queries)

	t.Logf("PASS: Executed %d queries, none contain Grafana macros", len(queries))
}

func runTest_FactTableTimeFilters(t *testing.T) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed latency data
	seedLatencyData(t, ctx, conn)

	// Set up agent with SQL-capturing querier
	baseQuerier := testQuerier(t, db)
	capturingQuerier := newSQLCapturingQuerier(baseQuerier)

	agentInstance := setupAgentWithCapturingQuerier(t, ctx, db, capturingQuerier,
		newAnthropicLLMClient, debug, debugLevel, nil)

	// Ask questions that should trigger fact table queries
	questions := []string{
		"Show me latency samples from the past hour",
		"What's the packet loss on link nyc-lon-1 today?",
	}

	for _, question := range questions {
		var output bytes.Buffer

		if debug {
			t.Logf("=== Query: '%s' ===\n", question)
		}

		result, err := agentInstance.Run(ctx, question, &output)
		require.NoError(t, err)
		require.NotEmpty(t, result.FinalText)

		if debug {
			t.Logf("=== Response ===\n%s\n", result.FinalText)
		}
	}

	// Validate all fact table queries have time filters
	queries := capturingQuerier.GetQueries()

	if debug {
		t.Logf("=== Executed %d queries total ===", len(queries))
		for i, q := range queries {
			t.Logf("Query %d: %s", i+1, truncateForError(q, 200))
		}
	}

	validateFactTableTimeFilters(t, queries)

	factTableCount := 0
	for _, q := range queries {
		if isFactTableQuery(q) {
			factTableCount++
		}
	}
	t.Logf("PASS: Validated %d fact table queries all have time filters", factTableCount)
}

// =============================================================================
// Test Data Seeding
// =============================================================================

// seedLatencyData seeds test data for latency queries
func seedLatencyData(t *testing.T, ctx context.Context, conn duck.Connection) {
	// Load tables and views
	loadTablesAndViews(t, ctx, conn)

	// Insert test devices, links, and latency samples
	sql := `
		-- Insert contributors
		INSERT INTO dz_contributors_history (pk, code, valid_from, valid_to)
		VALUES (1, 'test-contrib', now(), NULL);

		-- Insert metros
		INSERT INTO dz_metros_history (pk, code, name, longitude, latitude, valid_from, valid_to)
		VALUES
			(1, 'nyc', 'New York', -74.0, 40.7, now(), NULL),
			(2, 'lon', 'London', -0.1, 51.5, now(), NULL);

		-- Insert devices
		INSERT INTO dz_devices_history (pk, code, host, contributor_pk, metro_pk, status, device_type, valid_from, valid_to)
		VALUES
			(1, 'nyc-dzd1', 'nyc-dzd1.internal', 1, 1, 'activated', 'switch', now(), NULL),
			(2, 'lon-dzd1', 'lon-dzd1.internal', 1, 2, 'activated', 'switch', now(), NULL);

		-- Insert link
		INSERT INTO dz_links_history (pk, code, device_a_pk, device_z_pk, metro_a_pk, metro_z_pk, link_type, status, committed_rtt_ns, committed_jitter_ns, bandwidth_bps, isis_delay_override_ns, valid_from, valid_to)
		VALUES (1, 'nyc-lon-1', 1, 2, 1, 2, 'WAN', 'activated', 50000000, 5000000, 10000000000, NULL, now(), NULL);

		-- Insert latency samples (last 2 hours)
		INSERT INTO dz_device_link_latency_samples_raw (time, link_pk, src_device_pk, dest_device_pk, rtt_us, jitter_us)
		VALUES
			(now() - INTERVAL '1 hour', 1, 1, 2, 50000, 1000),
			(now() - INTERVAL '1 hour', 1, 2, 1, 51000, 1200),
			(now() - INTERVAL '30 minutes', 1, 1, 2, 49000, 900),
			(now() - INTERVAL '30 minutes', 1, 2, 1, 50500, 1100);
	`
	executeSQLStatements(t, ctx, conn, sql)
}
