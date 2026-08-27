# Latency Command UX Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the daemon probe race condition, expose readiness state, and add spinner + better error messages to the CLI latency command.

**Architecture:** The daemon's `LatencyManager` gets a `devicesFetched` channel so the probe goroutine waits for device data before its first run, and an `atomic.Bool` to track probe readiness. `ServeLatency` wraps the response in `{"ready": bool, "results": [...]}`. The CLI deserializes this new format, adds a spinner, and uses the `ready` flag to show accurate progress messages.

**Tech Stack:** Go (daemon), Rust (CLI), `indicatif` (spinner), `sync/atomic` (readiness flag)

**Spec:** `docs/superpowers/specs/2026-03-26-latency-command-ux-design.md`

---

### Task 1: Daemon — Add `devicesFetched` channel and `probeReady` flag to LatencyManager

**Files:**
- Modify: `client/doublezerod/internal/latency/manager.go:192-220` (struct + constructor)
- Test: `client/doublezerod/internal/latency/manager_test.go`

- [ ] **Step 1: Write the failing test — probe waits for device fetch**

Add a new test to `manager_test.go` that verifies the probe goroutine does not run before the device cache is populated. Use a slow smart contract func that takes 500ms, and verify that the first probe sees the devices (not an empty cache).

```go
func TestLatencyManager_ProbeWaitsForDeviceFetch(t *testing.T) {
	probeTargets := make(chan []latency.ProbeTarget, 1)

	slowSmartContractFunc := func(ctx context.Context) (*latency.ContractData, error) {
		time.Sleep(500 * time.Millisecond)
		return &latency.ContractData{
			Devices: []serviceability.Device{
				{
					AccountType: serviceability.DeviceType,
					PublicIp:    [4]uint8{127, 0, 0, 1},
					PubKey:      [32]byte{1},
					Code:        "dev01",
				},
			},
		}, nil
	}

	mockProber := func(ctx context.Context, target latency.ProbeTarget) latency.LatencyResult {
		probeTargets <- []latency.ProbeTarget{target}
		return latency.LatencyResult{
			Min:       1,
			Max:       10,
			Avg:       5,
			Loss:      0,
			Device:    target.Device,
			IP:        target.IP,
			Reachable: true,
		}
	}

	manager := latency.NewLatencyManager(
		latency.WithSmartContractFunc(slowSmartContractFunc),
		latency.WithProberFunc(mockProber),
		latency.WithProbeInterval(30*time.Second),
		latency.WithCacheUpdateInterval(30*time.Second),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = manager.Start(ctx)
	}()

	// The probe should have received actual targets (not empty), meaning it waited for fetch
	select {
	case targets := <-probeTargets:
		if len(targets) == 0 {
			t.Fatal("probe ran with empty targets — did not wait for device fetch")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for probe to run")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run TestLatencyManager_ProbeWaitsForDeviceFetch -v -count=1 ./client/doublezerod/internal/latency/...`

Expected: The test may pass or fail depending on timing. If it passes, the race is hard to reproduce deterministically — that's OK, the structural fix is still needed. Proceed to the implementation.

- [ ] **Step 3: Add `devicesFetched` channel and `probeReady` flag to LatencyManager**

In `manager.go`, add two fields to the `LatencyManager` struct:

```go
type LatencyManager struct {
	SmartContractFunc    SmartContractorFunc
	fetcher              Fetcher
	proberFunc           ProberFunc
	DeviceCache          *DeviceCache
	ResultsCache         *LatencyResults
	probeInterval        time.Duration
	cacheUpdateInterval  time.Duration
	metricsEnabled       bool
	probeTunnelEndpoints bool
	devicesFetched       chan struct{} // closed after first successful fetch
	probeReady           atomic.Bool   // true after first probe completes
}
```

Add `"sync/atomic"` to imports.

Update `NewLatencyManager` to initialize the channel:

```go
func NewLatencyManager(options ...Option) *LatencyManager {
	lm := &LatencyManager{
		DeviceCache:    &DeviceCache{Devices: []serviceability.Device{}, Lock: sync.Mutex{}},
		ResultsCache:   &LatencyResults{Results: []LatencyResult{}, Lock: sync.RWMutex{}},
		proberFunc:     UdpPing,
		probeInterval:  10 * time.Second,
		cacheUpdateInterval: 300 * time.Second,
		metricsEnabled: false,
		devicesFetched: make(chan struct{}),
	}
	for _, o := range options {
		o(lm)
	}
	return lm
}
```

Add a public getter for readiness:

```go
func (l *LatencyManager) IsProbeReady() bool {
	return l.probeReady.Load()
}
```

- [ ] **Step 4: Update `Start()` — fetch goroutine closes channel, probe goroutine waits**

In the fetch goroutine, close `devicesFetched` after the first successful fetch:

```go
go func() {
	fetch := func() {
		// ... existing fetch logic unchanged ...
	}
	// don't wait for first tick and populate cache
	fetch()
	// Signal that initial device data is available for probing
	select {
	case <-l.devicesFetched:
		// already closed
	default:
		close(l.devicesFetched)
	}

	ticker := time.NewTicker(l.cacheUpdateInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetch()
		}
	}
}()
```

In the probe goroutine, wait for `devicesFetched` before the first probe:

```go
go func() {
	probe := func() {
		// ... existing probe logic unchanged ...
	}

	// Wait for initial device fetch before first probe
	select {
	case <-l.devicesFetched:
	case <-ctx.Done():
		return
	}

	// don't wait for first tick to ping stuff
	probe()
	l.probeReady.Store(true)

	ticker := time.NewTicker(l.probeInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probe()
		}
	}
}()
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test -run TestLatencyManager_ProbeWaitsForDeviceFetch -v -count=1 ./client/doublezerod/internal/latency/...`

Expected: PASS

- [ ] **Step 6: Run all existing latency tests to verify no regressions**

Run: `go test -v -count=1 ./client/doublezerod/internal/latency/...`

Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add client/doublezerod/internal/latency/manager.go client/doublezerod/internal/latency/manager_test.go
git commit -m "client: add devicesFetched channel and probeReady flag to LatencyManager"
```

---

### Task 2: Daemon — Update `ServeLatency` response to include readiness

**Files:**
- Modify: `client/doublezerod/internal/latency/manager.go:386-394` (ServeLatency handler)
- Test: `client/doublezerod/internal/latency/manager_test.go`

- [ ] **Step 1: Write the failing test — HTTP response includes ready field**

Add a test that verifies the `/latency` HTTP response includes the `ready` and `results` fields. Add this after the existing `check_results_via_http_are_correct` test in `TestLatencyManager`:

```go
func TestServeLatency_ResponseFormat(t *testing.T) {
	manager := latency.NewLatencyManager(
		latency.WithSmartContractFunc(func(context.Context) (*latency.ContractData, error) {
			return &latency.ContractData{
				Devices: []serviceability.Device{
					{
						AccountType: serviceability.DeviceType,
						PublicIp:    [4]uint8{127, 0, 0, 1},
						PubKey:      [32]byte{1},
						Code:        "dev01",
					},
				},
			}, nil
		}),
		latency.WithProberFunc(func(ctx context.Context, target latency.ProbeTarget) latency.LatencyResult {
			return latency.LatencyResult{
				Min: 1, Max: 10, Avg: 5, Loss: 0,
				Device: target.Device, IP: target.IP, Reachable: true,
			}
		}),
		latency.WithProbeInterval(30*time.Second),
		latency.WithCacheUpdateInterval(30*time.Second),
	)

	// Before Start — probe not ready, no results
	f, err := os.CreateTemp("/tmp", "doublezero-test.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	_ = unix.Unlink(f.Name())

	lis, err := net.Listen("unix", f.Name())
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /latency", manager.ServeLatency)
	server := http.Server{Handler: mux}
	defer server.Close()
	go func() { _ = server.Serve(lis) }()

	client := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", f.Name())
			},
		},
	}

	// Test: before probing, ready should be false
	resp, err := client.Get("http://localhost/latency")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(resp.Body)

	var parsed struct {
		Ready   bool              `json:"ready"`
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(buf, &parsed); err != nil {
		t.Fatalf("failed to parse response as {ready, results}: %v\nbody: %s", err, buf)
	}
	if parsed.Ready {
		t.Error("expected ready=false before probe has run")
	}

	// Start the manager and wait for probe to complete
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = manager.Start(ctx) }()

	// Poll until ready
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if manager.IsProbeReady() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !manager.IsProbeReady() {
		t.Fatal("manager never became ready")
	}

	// Test: after probing, ready should be true with results
	resp2, err := client.Get("http://localhost/latency")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	buf2, _ := io.ReadAll(resp2.Body)

	var parsed2 struct {
		Ready   bool              `json:"ready"`
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(buf2, &parsed2); err != nil {
		t.Fatalf("failed to parse response: %v\nbody: %s", err, buf2)
	}
	if !parsed2.Ready {
		t.Error("expected ready=true after probe completed")
	}
	if len(parsed2.Results) == 0 {
		t.Error("expected non-empty results after probe completed")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run TestServeLatency_ResponseFormat -v -count=1 ./client/doublezerod/internal/latency/...`

Expected: FAIL — the response is currently a bare JSON array, not `{"ready":..., "results":...}`.

- [ ] **Step 3: Update `ServeLatency` to wrap response**

In `manager.go`, replace the `ServeLatency` method:

```go
// latencyResponse is the wire format for the /latency endpoint.
// This is internal to the daemon-CLI communication — not the user-facing output.
type latencyResponse struct {
	Ready   bool            `json:"ready"`
	Results *LatencyResults `json:"results"`
}

func (l *LatencyManager) ServeLatency(w http.ResponseWriter, r *http.Request) {
	resp := latencyResponse{
		Ready:   l.probeReady.Load(),
		Results: l.ResultsCache,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "error generating latency: %v", err)
		return
	}
	_, _ = w.Write(data)
}
```

- [ ] **Step 4: Update the existing `check_results_via_http_are_correct` test**

The existing test in `TestLatencyManager` parses the response as `[]map[string]any`. Update it to expect the new wrapped format:

```go
t.Run("check_results_via_http_are_correct", func(t *testing.T) {
	req, err := http.NewRequest("GET", "http://localhost/latency", nil)
	if err != nil {
		t.Fatalf("error generating http request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("error while making http request: %v", err)
	}
	defer resp.Body.Close()

	buf, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Ready   bool             `json:"ready"`
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(buf, &parsed); err != nil {
		t.Fatalf("error unmarshaling latency data: %v\nbody: %s", err, buf)
	}

	if !parsed.Ready {
		t.Error("expected ready=true")
	}

	want := []map[string]any{
		{
			"device_pk":       base58.Encode(tests[0].DeviceCache[0].PubKey[:]),
			"device_code":     tests[0].DeviceCache[0].Code,
			"device_ip":       "127.0.0.1",
			"min_latency_ns":  float64(1),
			"max_latency_ns":  float64(10),
			"avg_latency_ns":  float64(5),
			"loss_percentage": float64(0),
			"reachable":       true,
		},
	}

	if diff := cmp.Diff(want, parsed.Results); diff != "" {
		t.Errorf("LatencyResults mismatch (-want +got): %s\n", diff)
	}
})
```

- [ ] **Step 5: Run all latency tests to verify**

Run: `go test -v -count=1 ./client/doublezerod/internal/latency/...`

Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add client/doublezerod/internal/latency/manager.go client/doublezerod/internal/latency/manager_test.go
git commit -m "client: update ServeLatency to include readiness in response"
```

---

### Task 3: CLI — Update `ServiceController::latency()` to return `LatencyResponse`

**Files:**
- Modify: `client/doublezero/src/servicecontroller.rs:15-30,167-179,233-250`

- [ ] **Step 1: Add `LatencyResponse` struct**

In `servicecontroller.rs`, add the new struct after the `LatencyRecord` definition (after line 50):

```rust
#[derive(Deserialize, Debug)]
pub struct LatencyResponse {
    pub ready: bool,
    pub results: Vec<LatencyRecord>,
}
```

- [ ] **Step 2: Update `ServiceController` trait and impl**

Change the trait method signature at line 173:

```rust
async fn latency(&self) -> eyre::Result<LatencyResponse>;
```

Change the implementation at line 233-250:

```rust
async fn latency(&self) -> eyre::Result<LatencyResponse> {
    let uri = Uri::new(&self.socket_path, "/latency").into();
    let client: Client<UnixConnector, Full<Bytes>> =
        Client::builder(TokioExecutor::new()).build(UnixConnector);
    let res = client
        .get(uri)
        .await
        .map_err(|e| eyre!("Unable to connect to doublezero daemon: {e}"))?;

    let data = res
        .into_body()
        .collect()
        .await
        .map_err(|e| eyre!("Unable to read response body: {e}"))?
        .to_bytes();

    parse_daemon_response::<LatencyResponse>(&data, "/latency")
}
```

- [ ] **Step 3: Update the mock expectation return type**

The `#[automock]` macro will auto-generate the mock. But all existing test code that calls `expect_latency()` returns `Ok(vec![...])`. These need to return `Ok(LatencyResponse { ready: true, results: vec![...] })`.

This is handled in Task 5 (updating `dzd_latency.rs`).

- [ ] **Step 4: Verify it compiles (expect test failures from mock changes)**

Run: `cargo check -p doublezero`

Expected: Compilation errors in `dzd_latency.rs` tests where `expect_latency().returning(...)` returns the old type. That's expected — Task 5 fixes those.

- [ ] **Step 5: Commit**

```bash
git add client/doublezero/src/servicecontroller.rs
git commit -m "client: update ServiceController::latency() to return LatencyResponse"
```

---

### Task 4: CLI — Add spinner to latency command

**Files:**
- Modify: `client/doublezero/src/command/latency.rs`

- [ ] **Step 1: Add spinner to latency command**

Replace the entire `latency.rs` file content:

```rust
use crate::command::util;
use clap::Args;
use doublezero_cli::doublezerocommand::CliCommand;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use indicatif::{ProgressBar, ProgressStyle};
use std::time::Duration;

use crate::{
    dzd_latency::retrieve_latencies, requirements::check_doublezero,
    servicecontroller::ServiceControllerImpl,
};

#[derive(Args, Debug)]
pub struct LatencyCliCommand {
    /// Output as json
    #[arg(long, default_value = "false")]
    json: bool,
}

impl LatencyCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);

        let spinner = ProgressBar::new_spinner();
        spinner.set_style(
            ProgressStyle::default_spinner()
                .template("{spinner:.green} [{elapsed_precise}] {msg}")
                .expect("Failed to set template")
                .tick_strings(&["-", "\\", "|", "/"]),
        );
        spinner.enable_steady_tick(Duration::from_millis(100));
        spinner.set_message("Checking daemon...");

        check_doublezero(&controller, client, Some(&spinner)).await?;

        spinner.set_message("Fetching devices...");
        let devices = client.list_device(ListDeviceCommand)?;

        let latencies =
            retrieve_latencies(&controller, &devices, false, Some(&spinner)).await?;

        spinner.finish_and_clear();
        util::show_output(latencies, self.json)?;

        Ok(())
    }
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cargo check -p doublezero`

Expected: May still have errors from Task 3 mock changes — that's OK if Task 3 is committed but Task 5 isn't yet.

- [ ] **Step 3: Commit**

```bash
git add client/doublezero/src/command/latency.rs
git commit -m "client: add spinner to latency command"
```

---

### Task 5: CLI — Update `retrieve_latencies` to use readiness flag

**Files:**
- Modify: `client/doublezero/src/dzd_latency.rs:68-124`
- Test: `client/doublezero/src/dzd_latency.rs` (test module)

- [ ] **Step 1: Update `retrieve_latencies` to use `LatencyResponse`**

Replace the `retrieve_latencies` function (lines 68-124):

```rust
pub async fn retrieve_latencies<T: ServiceController>(
    controller: &T,
    devices: &HashMap<Pubkey, Device>,
    reachable_only: bool,
    spinner: Option<&indicatif::ProgressBar>,
) -> eyre::Result<Vec<LatencyRecord>> {
    if let Some(spinner) = spinner {
        spinner.set_message("Retrieving latency stats...");
    }

    let max_wait = Duration::from_secs(60);
    let poll_interval = Duration::from_secs(1);
    let start = std::time::Instant::now();

    let mut latencies = loop {
        let response = controller.latency().await.map_err(|e| eyre::eyre!(e))?;

        let mut results = response.results;
        results.retain(|l| {
            Pubkey::from_str(&l.device_pk)
                .ok()
                .and_then(|pubkey| devices.get(&pubkey))
                .map(|device| device.status == DeviceStatus::Activated)
                .unwrap_or(false)
        });

        if reachable_only {
            results.retain(|l| l.reachable);
        }

        if !results.is_empty() {
            break results;
        }

        // Daemon is still warming up — poll with feedback
        if !response.ready {
            if start.elapsed() >= max_wait {
                eyre::bail!(
                    "Timed out waiting for daemon to finish probing devices. \
                     The daemon may still be starting up — try again in a few seconds."
                );
            }
            if let Some(spinner) = spinner {
                spinner.set_message("Waiting for daemon to finish probing devices...");
            }
            tokio::time::sleep(poll_interval).await;
            continue;
        }

        // Daemon is ready but no results — this is a real "no devices" situation
        eyre::bail!("No activated devices found");
    };

    latencies.sort_by(|a, b| {
        let reachable_cmp = b.reachable.cmp(&a.reachable);
        if reachable_cmp != std::cmp::Ordering::Equal {
            return reachable_cmp;
        }
        a.avg_latency_ns
            .partial_cmp(&b.avg_latency_ns)
            .unwrap_or(std::cmp::Ordering::Equal)
    });

    Ok(latencies)
}
```

- [ ] **Step 2: Update all test mock expectations to return `LatencyResponse`**

In the test module, add the import:

```rust
use crate::servicecontroller::LatencyResponse;
```

Then update every `expect_latency().returning(...)` call. Each test that does:

```rust
controller
    .expect_latency()
    .returning(move || Ok(latencies.clone()));
```

Must become:

```rust
controller
    .expect_latency()
    .returning(move || Ok(LatencyResponse { ready: true, results: latencies.clone() }));
```

Apply this to all tests:
- `test_retrieve_latencies_filters_and_sorts`
- `test_best_latency_prefers_current_within_tolerance`
- `test_best_latency_selects_lowest`
- `test_best_latency_ignores_unreachable_devices`
- `test_best_latency_ignores_faster_devices_at_max_users`
- `test_best_latency_current_faster_but_at_max_users`
- `test_best_latency_excludes_ips`
- `test_best_latency_excludes_specific_ip`
- `test_best_latency_device_with_multiple_endpoints_not_excluded`
- `test_best_latency_device_all_endpoints_excluded`
- `test_best_latency_prefers_same_device_with_available_endpoint`

- [ ] **Step 3: Add a new test for the "not ready" polling behavior**

```rust
#[tokio::test]
async fn test_retrieve_latencies_waits_for_daemon_ready() {
    let (pk1, dev1) = make_device(DeviceStatus::Activated, 0);
    let mut devices = HashMap::new();
    devices.insert(pk1, dev1);

    let latencies = vec![make_latency(&pk1.to_string(), 10000000, true)];
    let call_count = std::sync::Arc::new(std::sync::atomic::AtomicU32::new(0));
    let call_count_clone = call_count.clone();
    let latencies_clone = latencies.clone();

    let mut controller = MockServiceController::new();
    controller.expect_latency().returning(move || {
        let count = call_count_clone.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        if count < 2 {
            // First two calls: not ready yet
            Ok(LatencyResponse {
                ready: false,
                results: vec![],
            })
        } else {
            // Third call: ready with results
            Ok(LatencyResponse {
                ready: true,
                results: latencies_clone.clone(),
            })
        }
    });

    let result = retrieve_latencies(&controller, &devices, false, None)
        .await
        .unwrap();
    assert_eq!(result.len(), 1);
    assert_eq!(result[0].device_pk, pk1.to_string());
    assert!(call_count.load(std::sync::atomic::Ordering::SeqCst) >= 3);
}
```

- [ ] **Step 4: Add a test for "ready but no devices" error**

```rust
#[tokio::test]
async fn test_retrieve_latencies_ready_but_empty_returns_error() {
    let devices = HashMap::new();

    let mut controller = MockServiceController::new();
    controller.expect_latency().returning(move || {
        Ok(LatencyResponse {
            ready: true,
            results: vec![],
        })
    });

    let result = retrieve_latencies(&controller, &devices, false, None).await;
    assert!(result.is_err());
    assert_eq!(
        result.unwrap_err().to_string(),
        "No activated devices found"
    );
}
```

- [ ] **Step 5: Run all Rust tests**

Run: `cargo test -p doublezero`

Expected: All tests pass.

- [ ] **Step 6: Remove unused `backon` import**

The `retrieve_latencies` function no longer uses `backon`'s retry logic. Remove the import from the top of `dzd_latency.rs`:

```rust
// Remove this line:
use backon::{ExponentialBuilder, Retryable};
```

Also add the `std::time::Instant` import (used by the new polling loop, `Instant` was not previously imported):

```rust
use std::{collections::HashMap, net::Ipv4Addr, str::FromStr, time::Duration};
```

Note: `std::time::Instant` is used as `std::time::Instant::now()` in the function body, so the fully qualified path is fine without a dedicated import. Alternatively add it to the existing `use std::` line.

- [ ] **Step 7: Run formatting**

Run: `make rust-fmt`

- [ ] **Step 8: Run all Rust tests**

Run: `cargo test -p doublezero`

Expected: All tests pass.

- [ ] **Step 9: Commit**

```bash
git add client/doublezero/src/dzd_latency.rs
git commit -m "client: update retrieve_latencies to use daemon readiness flag"
```

---

### Task 6: Final verification

**Files:** None (verification only)

- [ ] **Step 1: Run full Go test suite for the daemon**

Run: `go test -v -count=1 ./client/doublezerod/internal/latency/...`

Expected: All tests pass.

- [ ] **Step 2: Run full Rust test suite for the CLI**

Run: `cargo test -p doublezero`

Expected: All tests pass.

- [ ] **Step 3: Run lint for both**

Run: `make rust-lint` and `make go-lint`

Expected: No lint errors.

- [ ] **Step 4: Run formatting for both**

Run: `make rust-fmt` and `make go-fmt`

Expected: No formatting changes needed.
