# Latency Command UX Improvements

## Problem

The `doublezero latency` CLI command has three issues:

1. **Race condition in daemon:** The device fetch goroutine and probe goroutine start concurrently on daemon startup. The first `probe()` often reads an empty `DeviceCache`, probes nothing, and caches empty results. Users must wait for the next probe tick (default 30s) before data appears.

2. **No progress feedback:** The latency command passes `None` for the spinner, so the user sees a blank terminal during what can be a 30+ second wait.

3. **Misleading error:** When retries exhaust, the user sees "No devices found" — which conflates "daemon hasn't finished probing yet" (transient) with "no activated devices exist" (permanent).

## Changes

### Daemon: Fix probe sequencing and expose readiness

**File:** `client/doublezerod/internal/latency/manager.go`

Add to `LatencyManager`:
- `devicesFetched chan struct{}` — created in `NewLatencyManager`, closed after first successful `fetch()` populates `DeviceCache`
- `probeReady atomic.Bool` — set to `true` after first `probe()` completes and writes to `ResultsCache`

In `Start()`:
- The fetch goroutine closes `devicesFetched` after the first successful `fetch()` call
- The probe goroutine waits on `<-l.devicesFetched` before running the first `probe()`
- After the first `probe()` writes results, set `l.probeReady.Store(true)`

Update `ServeLatency` response format from a bare `[]LatencyResult` to:

```json
{
  "ready": false,
  "results": []
}
```

Where `ready` reflects `probeReady.Load()`.

### CLI: Spinner and improved feedback

**File:** `client/doublezero/src/servicecontroller.rs`

Add a new response struct for the latency endpoint:

```rust
struct LatencyResponse {
    ready: bool,
    results: Vec<LatencyRecord>,
}
```

Update `ServiceController::latency()` to return `LatencyResponse` instead of `Vec<LatencyRecord>`.

**File:** `client/doublezero/src/command/latency.rs`

Create a spinner in `execute()` and pass it to `check_doublezero()` and `retrieve_latencies()`.

**File:** `client/doublezero/src/dzd_latency.rs`

Update `retrieve_latencies()` retry logic:
- When `ready: false`: retry with 1s interval, spinner shows "Waiting for daemon to finish probing devices..."
- When `ready: true` but results empty: stop retrying immediately, return clear error "No activated devices found"
- When `ready: true` with results: return normally

Remove exponential backoff for the "not ready" case — a simple 1s poll is appropriate since we're waiting for a one-time daemon initialization, not recovering from an error.

Keep the existing retry with backoff for transient errors from the daemon endpoint itself (connection refused, etc.).

## Files Modified

| File | Change |
|------|--------|
| `client/doublezerod/internal/latency/manager.go` | Add `devicesFetched` channel, `probeReady` atomic, sequencing, new response format |
| `client/doublezero/src/servicecontroller.rs` | New `LatencyResponse` struct, update `latency()` return type |
| `client/doublezero/src/command/latency.rs` | Add spinner creation and passing |
| `client/doublezero/src/dzd_latency.rs` | Update retry logic to use `ready` field, improve error messages |

## User-facing output unchanged

The `{ready, results}` wrapper is only the internal daemon-to-CLI wire format over the Unix socket. The user-facing output from `doublezero latency` (table or `--json`) remains `Vec<LatencyRecord>` unchanged.

## Not in scope

- Changing probe intervals or timeouts
- Concurrent device fetch + latency fetch in the CLI (they're already fast once the daemon is ready)
- Streaming/incremental display of results
