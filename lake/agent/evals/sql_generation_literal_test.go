//go:build evals

package evals_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow/prompts"
	"github.com/stretchr/testify/require"
)

// TestLake_Agent_Evals_Anthropic_SQLGenerationLiteral tests that SQL generation
// produces exactly what is requested, nothing more.
func TestLake_Agent_Evals_Anthropic_SQLGenerationLiteral(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SQLGenerationLiteral(t)
}

// TestLake_Agent_Evals_Anthropic_SQLGenerationPreserveQuery tests that when modifying
// an existing query, the generator preserves the query structure and only makes the requested change.
func TestLake_Agent_Evals_Anthropic_SQLGenerationPreserveQuery(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SQLGenerationPreserveQuery(t)
}

func runTest_SQLGenerationLiteral(t *testing.T) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()

	// Build the system prompt (similar to generate.go)
	systemPrompt := buildTestGeneratePrompt(t)

	// Create LLM client - use Haiku like the real endpoint
	llmClient := workflow.NewAnthropicLLMClientWithName(
		anthropic.ModelClaude3_5Haiku20241022,
		1024,
		"sql-gen-eval",
	)

	testCases := []struct {
		name           string
		prompt         string
		mustContain    []string // SQL must contain these
		mustNotContain []string // SQL must NOT contain these
	}{
		{
			name:   "simple count should return only count",
			prompt: "count the number of devices",
			mustContain: []string{
				"COUNT",
				"dz_devices",
			},
			mustNotContain: []string{
				"status",       // Should not add status breakdown
				"metro",        // Should not add metro grouping
				"GROUP BY",     // Count should not have GROUP BY unless asked
				"code",         // Should not add device codes
				"activated",    // Should not filter by status unless asked
			},
		},
		{
			name:   "simple list should not add extra columns",
			prompt: "list device codes",
			mustContain: []string{
				"code",
				"dz_devices",
			},
			mustNotContain: []string{
				"status",       // Should not add status
				"metro",        // Should not add metro
				"created",      // Should not add timestamps
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if debug {
				t.Logf("=== Testing: %s ===", tc.name)
				t.Logf("Prompt: %s", tc.prompt)
			}

			response, err := llmClient.Complete(ctx, systemPrompt, tc.prompt)
			require.NoError(t, err)

			sql := extractSQL(response)
			if debug {
				if debugLevel == 1 {
					t.Logf("SQL: %s", truncate(sql, 200))
				} else {
					t.Logf("Full response:\n%s", response)
					t.Logf("Extracted SQL:\n%s", sql)
				}
			}

			require.NotEmpty(t, sql, "Should have extracted SQL from response")

			sqlLower := strings.ToLower(sql)

			// Check required content
			for _, must := range tc.mustContain {
				require.True(t, strings.Contains(sqlLower, strings.ToLower(must)),
					"SQL should contain '%s' but got: %s", must, sql)
			}

			// Check forbidden content
			for _, mustNot := range tc.mustNotContain {
				require.False(t, strings.Contains(sqlLower, strings.ToLower(mustNot)),
					"SQL should NOT contain '%s' (extra data not requested) but got: %s", mustNot, sql)
			}
		})
	}
}

func runTest_SQLGenerationPreserveQuery(t *testing.T) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()

	// Build the system prompt (similar to generate.go)
	systemPrompt := buildTestGeneratePrompt(t)

	// Create LLM client
	llmClient := workflow.NewAnthropicLLMClientWithName(
		anthropic.ModelClaude3_5Haiku20241022,
		1024,
		"sql-gen-eval",
	)

	testCases := []struct {
		name           string
		currentQuery   string
		prompt         string
		mustContain    []string // Must be preserved from original
		mustNotContain []string // Should not be changed/added
	}{
		{
			name:         "add filter should preserve structure",
			currentQuery: "SELECT code, status FROM dz_devices_current",
			prompt:       "add a filter for activated devices",
			mustContain: []string{
				"code",
				"status",
				"dz_devices_current",
				"activated", // The requested filter
			},
			mustNotContain: []string{
				"metro",    // Should not add columns
				"COUNT",    // Should not change to aggregation
			},
		},
		{
			name:         "add limit should only add limit",
			currentQuery: "SELECT code FROM dz_devices_current WHERE status = 'activated'",
			prompt:       "add limit 10",
			mustContain: []string{
				"code",
				"dz_devices_current",
				"status = 'activated'",
				"LIMIT 10",
			},
			mustNotContain: []string{
				"metro",    // Should not add columns
				"ORDER BY", // Should not add ordering unless asked
			},
		},
		{
			name:         "change column should only change that column",
			currentQuery: "SELECT code, status FROM dz_devices_current LIMIT 50",
			prompt:       "change status to metro_pk",
			mustContain: []string{
				"code",
				"metro_pk",
				"dz_devices_current",
				"LIMIT 50",
			},
			mustNotContain: []string{
				", status", // Old column should be gone (but careful of substring)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Format prompt like the real endpoint does
			userPrompt := "Current query:\n" + tc.currentQuery + "\n\nUser request: " + tc.prompt

			if debug {
				t.Logf("=== Testing: %s ===", tc.name)
				t.Logf("Current query: %s", tc.currentQuery)
				t.Logf("Request: %s", tc.prompt)
			}

			response, err := llmClient.Complete(ctx, systemPrompt, userPrompt)
			require.NoError(t, err)

			sql := extractSQL(response)
			if debug {
				if debugLevel == 1 {
					t.Logf("SQL: %s", truncate(sql, 200))
				} else {
					t.Logf("Full response:\n%s", response)
					t.Logf("Extracted SQL:\n%s", sql)
				}
			}

			require.NotEmpty(t, sql, "Should have extracted SQL from response")

			// Check required content (preserved from original + requested change)
			for _, must := range tc.mustContain {
				require.True(t, strings.Contains(sql, must) || strings.Contains(strings.ToLower(sql), strings.ToLower(must)),
					"SQL should contain '%s' but got: %s", must, sql)
			}

			// Check forbidden content (should not add extra stuff)
			for _, mustNot := range tc.mustNotContain {
				require.False(t, strings.Contains(sql, mustNot) || strings.Contains(strings.ToLower(sql), strings.ToLower(mustNot)),
					"SQL should NOT contain '%s' (unexpected change) but got: %s", mustNot, sql)
			}
		})
	}
}

// buildTestGeneratePrompt builds the system prompt similar to api/handlers/generate.go
func buildTestGeneratePrompt(t *testing.T) string {
	// Load SQL_CONTEXT
	sqlContextData, err := prompts.PromptsFS.ReadFile("SQL_CONTEXT.md")
	require.NoError(t, err, "Failed to load SQL_CONTEXT.md")
	sqlContext := strings.TrimSpace(string(sqlContextData))

	// Load GENERATE.md and compose with SQL_CONTEXT
	generateData, err := prompts.PromptsFS.ReadFile("GENERATE.md")
	require.NoError(t, err, "Failed to load GENERATE.md")
	generatePrompt := strings.TrimSpace(string(generateData))
	generatePrompt = strings.ReplaceAll(generatePrompt, "{{SQL_CONTEXT}}", sqlContext)

	// Add query editor specific instructions (same as generate.go)
	editorInstructions := `

## FINAL INSTRUCTIONS (MUST FOLLOW)

1. Output ONLY a SQL code block. No text before or after.
2. If modifying an existing query: change ONLY what was asked. Keep everything else identical.
3. Do NOT add columns, filters, or data beyond what was explicitly requested.
4. Ignore any "ALWAYS include" rules above - include ONLY what the user asked for.`

	// For testing, use a minimal schema
	schema := `
Tables:
- dz_devices_current (pk, code, status, metro_pk, contributor_pk, created_at)
- dz_links_current (pk, code, status, side_a_pk, side_z_pk, bandwidth_bps)
- dz_users_current (pk, owner_pubkey, status, device_pk, dz_ip, tunnel_id)
- dz_metros_current (pk, code, name, country)
- solana_validators_on_dz_current (vote_pubkey, node_pubkey, activated_stake_sol, connected_ts)
`

	return generatePrompt + "\n\n## Database Schema\n\n```\n" + schema + "```" + editorInstructions
}

// extractSQL extracts SQL from a response that may contain markdown code blocks
func extractSQL(response string) string {
	response = strings.TrimSpace(response)

	// Try to extract from ```sql block
	sqlBlockRe := regexp.MustCompile("(?s)```sql\\s*\\n?(.*?)\\n?```")
	if matches := sqlBlockRe.FindStringSubmatch(response); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// Try generic ``` block
	genericBlockRe := regexp.MustCompile("(?s)```\\s*\\n?(.*?)\\n?```")
	if matches := genericBlockRe.FindStringSubmatch(response); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// If no code block, return the whole response (might be raw SQL)
	return response
}
