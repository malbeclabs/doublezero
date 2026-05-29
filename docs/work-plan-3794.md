# Work plan: #3794 — agent Prometheus scrape

Parent: #3747. Stacks on top of #3793 (`nikw9944/doublezero-3793` →
`nikw9944/doublezero-3794`).

## Summary

The `device-observer` already has flag parsing, signal handling, and the eAPI
sampler from PR #3793; the `promscrape` package exists only as a `Noop` stub.
This PR replaces that stub with a real scraper that, on every
`--sample-interval` tick, fetches the doublezero-agent Prometheus endpoint at
`--agent-metrics-url`, parses the exposition-format response with
`expfmt.NewTextParser`, and appends one row per metric sample to
`observer.agent_metrics.json` in the working directory. It also exposes a
thread-safe `Snapshot()` accessor returning the latest counter values so the
abort decider that lands in PR #3796 can detect "counter incremented within a
sample" without needing its own scraper.

## Approach

### Package layout

`internal/promscrape/scrape.go` (single file, package `promscrape`):

```go
type Scraper struct { /* unexported fields */ }

func New(metricsURL, workingDir string, interval time.Duration, logger *slog.Logger) *Scraper
func (s *Scraper) Run(ctx context.Context) error          // satisfies collector.Collector
func (s *Scraper) Snapshot() map[string]float64           // counter name → latest value
```

`Scraper` will hold:
- `metricsURL`, `outPath` (`<workingDir>/observer.agent_metrics.json`),
  `interval`, `logger`
- An `*http.Client` with a per-request timeout (5 s, matching the e2e prom
  client at `e2e/internal/prometheus/metrics.go:46`)
- A `sync.RWMutex` + `map[string]float64` snapshot

### Scrape loop

Mirror the structure of `sample/eos.go`:
- Immediate first tick, then `time.Ticker(interval)`.
- On `ctx.Done()`, return `nil`.
- On a per-tick error (HTTP, parse, or write), log at WARN and continue —
  the abort decider owns "repeated failure" policy and counter values are
  intentionally allowed to go stale across a missed scrape.

### Output format: NDJSON

The parent issue prescribes appending rows `{t_ns, metric_name, value,
labels_json}` to `observer.agent_metrics.json`. The only sane append-safe
shape is newline-delimited JSON (one row per line); the filename is kept as
`.json` to match the issue text. Per tick:

1. Parse the exposition body into `map[string]*prom.MetricFamily`.
2. For each family/metric, emit a row. Type handling:
   - **Counter / Gauge / Untyped:** one row, `value` is the float.
   - **Summary / Histogram:** emit one row per `_sum` / `_count` / quantile
     / bucket, with the appropriate suffix appended to `metric_name`. This
     mirrors how `prometheus/client_model` text-encodes these types and is
     forward-compatible with future agent metrics; the current
     doublezero-agent emits only counters and gauges (see
     `controlplane/agent/internal/agent/metrics.go`), so this path is
     exercised only by tests.
3. `labels_json` is a JSON string field whose value is a compact JSON object
   of `name → value` pairs (empty object `{}` for unlabeled metrics). The
   field name in the issue (`labels_json` rather than `labels`) signals the
   "serialized JSON in a string" intent.
4. Open the file in `O_APPEND|O_CREATE|O_WRONLY` (0640), write all rows
   buffered in one `Write` so a single tick is atomic on POSIX append.

A single `t_ns` captured once at the top of the tick is used for every row
in that tick so multiple metrics within one scrape share a timestamp.

### Snapshot

Under the lock, on each successful scrape, replace the snapshot map with
counter totals: for each metric family of type `COUNTER`, key by family
name; if a family has multiple labeled series, sum the values (the
agent's counters are not labeled, so this is a defensive choice). Gauges
and other types are intentionally excluded — the abort triggers in #3747
that consume this Snapshot are counter deltas only
(`doublezero_agent_apply_config_errors_total`,
`doublezero_agent_get_config_errors_total`,
`doublezero_agent_bgp_neighbors_errors_total`).

`Snapshot()` returns a fresh copy of the map so callers can't mutate
internal state and don't need to hold the lock.

If a tick fails (HTTP error, parse error), the snapshot is **not**
updated — the decider sees the last successful values, which is correct
for "did the counter increment" semantics.

### Wiring in `main.go`

Replace the `promscrape.New(*agentMetricsURL, *workingDir)` line in
`tools/stress/device-observer/cmd/device-observer/main.go:115` with the
new four-argument constructor passing `*sampleInterval` and `logger`. No
other main.go changes are needed — the errgroup already runs the
collector.

## Files to Change

| File                                                                    | Change                                                                                |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `tools/stress/device-observer/internal/promscrape/scrape.go`            | Replace stub with `Scraper` struct, `New`, `Run`, `Snapshot`, and NDJSON writer.       |
| `tools/stress/device-observer/internal/promscrape/scrape_test.go`       | **New.** Unit tests using `httptest.Server` for happy path, fetch failure, parse failure, label serialization, snapshot semantics, context cancellation. |
| `tools/stress/device-observer/cmd/device-observer/main.go`              | Update one call site (`promscrape.New(...)`) to pass `*sampleInterval` and `logger`.   |
| `tools/stress/device-observer/README.md`                                | Add a "Metrics output" section documenting `observer.agent_metrics.json` (row schema + example), update the alpha banner, and remove the "PR #3794 stub" reference from the Layout block. |
| `docs/work-plan-3794.md`                                                | **New.** This file.                                                                    |

No changes to `go.mod` / `go.sum`: `prometheus/common`, `prometheus/client_model`,
and `prometheus/common/model` are already pulled in (see `go.mod:50-51` and
their use in `e2e/internal/prometheus/metrics.go`).

`CHANGELOG.md` / `CLAUDE.md` are not updated — `device-observer` is an
internal stress-test tool that has no changelog entry from PR #3793, and the
project-level CLAUDE.md has no device-observer section yet. Following the
convention set by #3793.

## Risks & Considerations

- **File grows unbounded.** A 10 s sample interval × ~6 metric families = ~52k
  rows / 24 h. Each row is small (~150 B), so ~8 MB/day. Acceptable for the
  capacity-study sweep window (single-digit hours); the existing README
  "Disk usage" section already declares pruning is the orchestrator's job, so
  no new policy is needed.
- **NDJSON vs. JSON-array.** Strict JSON-array would require seeking and
  rewriting the closing bracket per append, which is incompatible with
  "append rows" semantics and corruption-prone on crash. NDJSON is the
  industry-standard append-safe shape; the issue's `{t_ns, ...}` row schema
  implies this. The README will be explicit that the file is NDJSON.
- **HTTP timeout.** Using `context.WithTimeout(parent, 5 * time.Second)` on
  the per-request context, identical to the e2e prom client. If
  `--sample-interval` is set below 5 s a slow scrape could overlap the next
  tick; the ticker drops missed ticks (Go stdlib behavior) so this is safe
  but worth a code comment.
- **Counter-only Snapshot.** Limiting to counters keeps the API tight for
  the one consumer (the PR #3796 decider). If gauges become trigger inputs
  later, extend the snapshot type then; YAGNI for now.
- **Backwards compatibility.** Stub callers in `main.go` use the old
  two-argument `New(metricsURL, workingDir) collector.Collector` signature.
  Changing the signature is fine because the only caller is in this PR's
  diff and there are no external consumers.
- **Re-marshaling vs. preserving exposition text.** We intentionally drop
  the original text payload — the project already standardized on
  `expfmt.NewTextParser` in `e2e/internal/prometheus/metrics.go` and the
  abort decider needs structured values anyway. The trade-off (no
  byte-for-byte preservation) is identical to the eAPI sampler's
  re-marshal trade-off documented in the README's "Known limitations".

## Testing Strategy

### Unit tests (`scrape_test.go`)

All using `httptest.NewServer` so no network is involved:

1. **Happy path.** Server returns a small fixture exposition body covering
   counter (with and without labels), gauge, and a histogram. After one
   tick: file exists, contains the expected number of rows, each row
   parses as JSON with the expected fields, `t_ns` is monotonic.
2. **Label serialization.** A counter with two label pairs produces a
   `labels_json` field whose value, when re-parsed, equals the source
   labels map.
3. **Snapshot returns counter totals.** Two counters and a gauge; after
   tick, `Snapshot()` contains both counter names with correct values and
   excludes the gauge. A second tick with incremented counters reflects
   the new values.
4. **Snapshot is stable across failed tick.** First tick succeeds; server
   then returns HTTP 500; Snapshot still returns the first-tick values.
5. **Parse error.** Server returns malformed exposition body; `Run` logs
   WARN and continues (verified by inspecting the file is empty and Run
   does not return).
6. **Context cancellation.** Pattern matching `TestRunCancelsCleanly` in
   `sample/eos_test.go`: cancel after the first tick, assert `Run` returns
   `nil` within 2 s.
7. **`Scraper` satisfies `collector.Collector`.** Compile-time `var _
   collector.Collector = (*Scraper)(nil)` assertion.

### Manual / smoke

Per README's existing "Local devnet smoke test" block — point the rebuilt
binary at `dz-local-device-dz1` and confirm `observer.agent_metrics.json`
appears in the working directory with non-empty rows that match the
agent's exposed metrics. The plan adds an example block under the new
README section.

### CI

`make go-test` runs the new unit tests. `make go-build` / `make go-lint`
verify the wiring change in `main.go`.

## Estimated Scope

| Component                                  | Lines added (excl. tests/docs) |
| ------------------------------------------ | ------------------------------ |
| `internal/promscrape/scrape.go`            | ~140                           |
| `cmd/device-observer/main.go` (1-line edit)| ~1                             |
| **Total code**                             | **~141**                       |

Tests (`scrape_test.go`): ~150 lines.
Docs (README addition + work plan): ~80 lines.

**Within the 250-LOC threshold.** No sub-issue split needed.
