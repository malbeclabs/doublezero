package analyze

import (
	"regexp"
	"sort"

	"github.com/malbeclabs/doublezero/tools/stress/device-reporter/pkg/parser"
)

// tunnelIDRE matches `TunnelN` (any tunnel-ID number) so we can normalize
// command text like `interface Tunnel500` → `interface Tunnel<N>`. This
// collapses per-tunnel error spam ("invalid command" on Tunnel500,
// Tunnel501, ...) into one bucket.
var tunnelIDRE = regexp.MustCompile(`Tunnel\d+`)

// largeIntRE matches embedded integers ≥ 1000 (config session IDs, byte
// counts, etc.) so unrelated errors don't fragment into one-bucket-per-run.
var largeIntRE = regexp.MustCompile(`\b\d{4,}\b`)

// normalizeCommand collapses run-variable bits of a CLI command so the
// top-K aggregation buckets cleanly.
func normalizeCommand(cmd string) string {
	out := tunnelIDRE.ReplaceAllString(cmd, "Tunnel<N>")
	out = largeIntRE.ReplaceAllString(out, "<N>")
	return out
}

// topAgentErrors groups CLI failures by normalized command text and
// returns the top k by count, ties broken alphabetically.
func topAgentErrors(errs []parser.AgentCLIError, k int) []AgentErrorBucket {
	counts := map[string]int{}
	for _, e := range errs {
		key := normalizeCommand(e.Command) + " :: " + e.Reason
		counts[key]++
	}
	out := make([]AgentErrorBucket, 0, len(counts))
	for k, v := range counts {
		out = append(out, AgentErrorBucket{NormalizedCommand: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].NormalizedCommand < out[j].NormalizedCommand
	})
	if len(out) > k {
		out = out[:k]
	}
	return out
}
