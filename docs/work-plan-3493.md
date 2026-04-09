# Work Plan: Issue #3493 - device-health-oracle: Add activation criterion for controller calls

## Summary

Add a device activation criterion to the device-health-oracle: do not advance a device's health to `ReadyForLinks` or `ReadyForUsers` unless the device has consistently called the controller (at least once per minute) over the burn-in period. This is verified by querying the ClickHouse `controller_grpc_getconfig_success` table. In addition, this PR (1) introduces a criteria-based evaluation pattern to support the many future criteria described in RFC-12, (2) makes device health progression stage-aware (devices must pass through `ReadyForLinks` before `ReadyForUsers`), and (3) optimizes the oracle to skip health updates when the value is already at the desired state.

RFC: [rfcs/rfc12-network-provisioning.md](../rfcs/rfc12-network-provisioning.md)

## Approach

### 1. Design Pattern: Interface-based Criteria

Each criterion is a Go interface implementation. Criteria are grouped per health stage and composed in a slice.

```go
type DeviceCriterion interface {
    Name() string
    Check(ctx context.Context, device serviceability.Device, burnInStart time.Time) (bool, string)
}
```

A `DeviceHealthEvaluator` holds two slices of criteria — one for `ReadyForLinks`, one for `ReadyForUsers` — and evaluates the appropriate set based on the device's current health state.

**Why this pattern:** Named criteria (useful for logging which criterion failed), clean dependency injection (ClickHouse client, etc. are struct fields), and independent testability. It requires trivially more code than plain functions but is significantly more maintainable as the number of criteria grows per RFC-12.

### 2. Stage-Aware Device Health Evaluation

Per RFC-12, device health progresses through stages:

```
Unknown/Pending → ReadyForLinks → ReadyForUsers
```

The evaluator must:
- Only advance to `ReadyForLinks` if the device's current health is `Pending` (or `Unknown`) and all ReadyForLinks criteria pass.
- Only advance to `ReadyForUsers` if the device's current health is `ReadyForLinks` and all ReadyForUsers criteria pass.
- Never skip stages (e.g., jump from `Pending` to `ReadyForUsers`).

The `ControllerSuccessCriterion` applies to **both** the `ReadyForLinks` and `ReadyForUsers` stages. Since a device must pass through `ReadyForLinks` first, re-checking at `ReadyForUsers` verifies the device *continued* calling the controller during the link provisioning phase.

### 3. Burn-in Time Window: Slot-based RPC Lookup (NOT wall-clock estimation)

Instead of a `--slot-duration-ms` flag with a static 400ms estimate, we use the Solana `GetBlockTime` RPC call to get accurate wall-clock timestamps for burn-in slot boundaries.

The `LedgerRPCClient` interface already defines `GetBlockTime(ctx, slot) (*solana.UnixTimeSeconds, error)`, and `tick()` already computes the burn-in slot boundaries (`provisioningSlot`, `drainedSlot`). The approach:

1. In `tick()`, after computing `provisioningSlot` and `drainedSlot`, call `GetBlockTime` for each to get wall-clock timestamps.
2. Pass these timestamps to `updatePendingDeviceHealth` so criteria can use them.
3. `ControllerSuccessCriterion.Check()` receives the burn-in start time and current time, then queries ClickHouse for coverage in that window.

This avoids any slot-duration approximation. If `GetBlockTime` fails (e.g., the slot has been pruned from the ledger), log an error and skip the tick (fail-closed).

### 4. ClickHouse Query Design

**Table:** `{env}.controller_grpc_getconfig_success`
- `timestamp DateTime64(3)` — when the GetConfig call was recorded
- `device_pubkey LowCardinality(String)` — the calling device's public key

**Query logic:** Check that the device has called the controller at least once per minute over the burn-in interval:

```sql
SELECT count(DISTINCT toStartOfMinute(timestamp)) AS minutes_with_calls
FROM "{db}".controller_grpc_getconfig_success
WHERE device_pubkey = {pubkey:String}
  AND timestamp >= {start:DateTime64(3)}
  AND timestamp <= {end:DateTime64(3)}
```

Compare `minutes_with_calls` against `expectedMinutes = duration / 60s`. The criterion passes if `minutes_with_calls >= expectedMinutes`.

Devices call the controller every 5 seconds, so "at least once per minute" is already generous. No tolerance percentage is needed — strict 100%.

Edge cases:
- If ClickHouse is unreachable, the criterion fails (fail-closed — we don't advance health). The device will be re-evaluated on the next tick.
- If the burn-in window maps to 0 minutes (e.g., ledger just started), the criterion passes.

### 5. ClickHouse Connection Configuration

Use environment variables (consistent with the controller, keeps secrets out of the Linux process list):
- `CLICKHOUSE_ADDR` — server address (host:port)
- `CLICKHOUSE_DB` — database name (defaults to `"default"`)
- `CLICKHOUSE_USER` — username (defaults to `"default"`)
- `CLICKHOUSE_PASS` — password
- `CLICKHOUSE_TLS_DISABLED` — set to `"true"` for plain HTTP (local dev only)

**If `CLICKHOUSE_ADDR` is not set, the oracle logs an error on every tick.** This is intentional — ClickHouse is now a required dependency for correct operation. The oracle does not crash (so it can recover when the env var is set without a restart), but it will not advance any device health and will log a clear error message on every tick.

### 6. Skip-Update Optimization

Currently `updatePendingDeviceHealth` and `updatePendingLinkHealth` queue updates for every device/link unconditionally with `ReadyForUsers`/`ReadyForService`. The fix:
- For devices: evaluate the target health via the evaluator, and only queue an update if the target health differs from `device.DeviceHealth`.
- For links: evaluate the target health via the evaluator, and only queue an update if the target health differs from `link.LinkHealth`.

### 7. E2E ClickHouse Integration

The e2e devnet must include a ClickHouse container so that both the controller (which writes `controller_grpc_getconfig_success` events) and the device-health-oracle (which reads them) can operate correctly. This requires:

1. A new `Clickhouse` component in the devnet package (following the InfluxDB/Prometheus pattern).
2. Passing `CLICKHOUSE_*` env vars to both the controller and device-health-oracle containers.
3. The controller creates the table on startup; the oracle reads from it.

The ClickHouse container must start **before** the controller in the `Start()` sequence, so the controller can connect to it on startup. When `DeviceHealthOracle.Enabled` is true, ClickHouse is automatically enabled.

## Files to Change

### New Files

1. **`controlplane/device-health-oracle/internal/worker/criteria.go`** (~100 lines)
   - `DeviceCriterion` interface: `Name() string`, `Check(ctx, device, burnInStart time.Time) (bool, string)`
   - `LinkCriterion` interface: `Name() string`, `Check(ctx, link) (bool, string)`
   - `DeviceHealthEvaluator` struct with `readyForLinksCriteria` and `readyForUsersCriteria` slices
   - `Evaluate(ctx, device, burnInStart time.Time) DeviceHealth` method that respects stage ordering
   - `LinkHealthEvaluator` struct with `readyForServiceCriteria` slice
   - `Evaluate(ctx, link) LinkHealth` method

2. **`controlplane/device-health-oracle/internal/worker/criteria_test.go`** (~150 lines)
   - Test evaluator stage progression (Pending → ReadyForLinks → ReadyForUsers)
   - Test that stages are not skipped
   - Test that a failing criterion blocks advancement
   - Test evaluator with no criteria (defaults to advancing)

3. **`controlplane/device-health-oracle/internal/worker/clickhouse.go`** (~100 lines)
   - `ClickHouseClient` struct wrapping `clickhouse.Conn`
   - `NewClickHouseClient(addr, db, user, pass string, disableTLS bool) (*ClickHouseClient, error)` — mirrors controller's `buildClickhouseOptions` pattern (HTTP protocol, optional TLS)
   - `ControllerCallCoverage(ctx, devicePubkey string, start, end time.Time) (minutesWithCalls int64, err error)` — the query above
   - `Close() error`

4. **`controlplane/device-health-oracle/internal/worker/clickhouse_test.go`** (~80 lines)
   - Test `ControllerCallCoverage` with a testcontainers-go ClickHouse instance (follow `geoprobe_test.go:startClickhouseContainer` pattern)
   - Create the `controller_grpc_getconfig_success` table
   - Insert test data with known coverage patterns
   - Verify correct minute counts, gaps, and empty table behavior

5. **`controlplane/device-health-oracle/internal/worker/controller_success.go`** (~80 lines)
   - `ControllerSuccessCriterion` struct: holds a `ControllerCallCoverageQuerier` interface (for testing)
   - Implements `DeviceCriterion`
   - `Check()`: receives burn-in start time, calls `ControllerCallCoverage` with `[burnInStart, now]`, computes expected minutes from the time window duration, compares result

6. **`controlplane/device-health-oracle/internal/worker/controller_success_test.go`** (~120 lines)
   - Test with mock ClickHouse client
   - Test time window computation and expected minutes calculation
   - Test pass/fail thresholds
   - Test zero-duration edge case

7. **`e2e/internal/devnet/clickhouse.go`** (~180 lines)
   - `ClickhouseSpec` struct with `Enabled`, `ContainerImage`, `User`, `Password`, `Database` fields
   - `Clickhouse` component struct following the InfluxDB/Prometheus pattern
   - `Start()` method: creates container with `clickhouse/clickhouse-server:24.12` image, connects to default network with alias `"clickhouse"`, exposes port 8123 (HTTP)
   - `StartIfNotRunning()`, `Exists()`, `setState()` methods (standard devnet component pattern)
   - `InternalAddr() string` method returning `"clickhouse:8123"` for inter-container use
   - Health check: wait for `clickhouse-client --query "SELECT 1"` to succeed (following `geoprobe_test.go` pattern)

### Modified Files

8. **`controlplane/device-health-oracle/internal/worker/worker.go`** (~80 lines changed)
   - In `tick()`: after computing `provisioningSlot` and `drainedSlot`, call `GetBlockTime` for each to get wall-clock timestamps. If `GetBlockTime` fails, log error and return early (fail-closed).
   - If ClickHouse is not configured (`cfg.ClickHouseConfigured == false`), log an error on every tick and return early (no health updates).
   - Update `updatePendingDeviceHealth` signature to accept burn-in timestamps (`provisioningBurnInStart`, `drainedBurnInStart time.Time`).
   - Refactor `updatePendingDeviceHealth`:
     - Select burn-in start time based on device status (DeviceProvisioning/LinkProvisioning → `provisioningBurnInStart`, Drained → `drainedBurnInStart`)
     - Call evaluator to determine target health per device, passing burn-in start time
     - Skip update if `device.DeviceHealth == targetHealth`
     - Log criterion results at debug level
   - Refactor `updatePendingLinkHealth`:
     - Call evaluator to determine target health per link
     - Skip update if `link.LinkHealth == targetHealth`

9. **`controlplane/device-health-oracle/internal/worker/config.go`** (~20 lines added)
   - Add `DeviceHealthEvaluator *DeviceHealthEvaluator` and `LinkHealthEvaluator *LinkHealthEvaluator` to `Config`
   - Add `ClickHouseConfigured bool` field (set when `CLICKHOUSE_ADDR` is present, used for per-tick error logging)

10. **`controlplane/device-health-oracle/cmd/device-health-oracle/main.go`** (~40 lines added)
    - Parse ClickHouse env vars (`CLICKHOUSE_ADDR`, `CLICKHOUSE_DB`, `CLICKHOUSE_USER`, `CLICKHOUSE_PASS`, `CLICKHOUSE_TLS_DISABLED`)
    - If `CLICKHOUSE_ADDR` is set: create `ClickHouseClient`, create `ControllerSuccessCriterion`
    - Default `CLICKHOUSE_DB` to `"default"` when not explicitly set (following controller pattern)
    - Build `DeviceHealthEvaluator` with `readyForLinksCriteria: [controllerSuccess]` and `readyForUsersCriteria: [controllerSuccess]`
    - Build `LinkHealthEvaluator` with empty criteria (links not affected by this criterion)
    - Pass evaluators and `ClickHouseConfigured` flag to `worker.Config`
    - If `CLICKHOUSE_ADDR` is not set: log an error at startup, build evaluators with empty criteria, set `ClickHouseConfigured = false`

11. **`controlplane/device-health-oracle/internal/worker/metrics.go`** (~15 lines added)
    - Add `device_health_oracle_criterion_results` counter with labels `criterion`, `stage`, `result` (pass/fail/error)
    - Add `device_health_oracle_updates_skipped_total` counter

12. **`e2e/internal/devnet/devnet.go`** (~25 lines added)
    - Add `Clickhouse ClickhouseSpec` to `DevnetSpec` struct
    - Add `Clickhouse *Clickhouse` to `Devnet` struct
    - Add validation call in `DevnetSpec.Validate()`
    - Initialize `Clickhouse` component in `New()` — auto-enable if `DeviceHealthOracle.Enabled`
    - Add `Clickhouse.StartIfNotRunning()` call in `Start()`, **before** the controller start (so the controller can connect on startup)

13. **`e2e/internal/devnet/controller.go`** (~10 lines added)
    - In `Start()`, if `c.dn.Clickhouse != nil`, add `CLICKHOUSE_ADDR`, `CLICKHOUSE_DB`, `CLICKHOUSE_USER`, `CLICKHOUSE_PASS`, `CLICKHOUSE_TLS_DISABLED` env vars to the controller container environment

14. **`e2e/internal/devnet/device_health_oracle.go`** (~10 lines added)
    - In `Start()`, if `d.dn.Clickhouse != nil`, add `CLICKHOUSE_ADDR`, `CLICKHOUSE_DB`, `CLICKHOUSE_USER`, `CLICKHOUSE_PASS`, `CLICKHOUSE_TLS_DISABLED` env vars to the device-health-oracle container environment

### Files NOT Changed

- `e2e/docker/device-health-oracle/entrypoint.sh` — No changes needed. ClickHouse config uses env vars which the binary reads directly via `os.Getenv()`. The entrypoint only passes flag-based config.
- `e2e/qa_provisioning_test.go` — Runs against real infrastructure where ClickHouse is already configured.
- `CLAUDE.md` — No new conventions introduced.
- `CHANGELOG.md` — Will be updated at PR time using `/changelog` skill.
- `go.mod` — `clickhouse-go/v2` is already a dependency.
- Serviceability SDK — No schema changes needed.
- Controller code — Read-only interaction with its table (the controller already writes the data, no changes needed).

## Risks & Considerations

### Risk 1: ClickHouse Availability

If ClickHouse is down, no devices can advance health. This is intentional (fail-closed): we don't want to mark a device as ready if we can't verify it's been calling the controller. The oracle retries every tick (default 1 minute), so recovery is automatic once ClickHouse returns.

**Mitigation:** Clear error logging with ClickHouse connection status. Metrics for criterion failures. Operators can monitor `device_health_oracle_criterion_results{result="error"}`.

### Risk 2: GetBlockTime RPC Reliability

The burn-in period is defined in slots. We use `GetBlockTime` RPC to convert slot numbers to wall-clock timestamps. `GetBlockTime` can fail if the slot has been purged from the validator's block store.

**Mitigation:** The provisioning burn-in (200K slots ≈ 20 hours) is well within typical validator retention windows (days to weeks). If `GetBlockTime` fails, the tick logs an error and skips — the device will be re-evaluated on the next tick. The drained burn-in is only 5K slots (≈ 30 minutes), even less likely to be pruned.

### Risk 3: ClickHouse Not Configured → Error on Every Tick

If `CLICKHOUSE_ADDR` is not set, the oracle logs an error on every tick. This is a deliberate behavioral change from the previous silent operation.

**Mitigation:** The error message will be clear and actionable: "CLICKHOUSE_ADDR not configured — device health criteria cannot be evaluated." The oracle does not crash, so it can recover once the env var is set.

### Risk 4: Database Name Quoting

The `mainnet-beta` database name contains a hyphen, requiring quoting in ClickHouse SQL (`"mainnet-beta".controller_grpc_getconfig_success`). The controller already uses `fmt.Sprintf('"%s".table_name', db)`.

**Mitigation:** Follow the controller's quoting pattern exactly.

### Risk 5: E2E Test Impact

Tests that enable `DeviceHealthOracle` must now also have ClickHouse available so that the controller writes GetConfig events and the oracle can read them. Without this, devices would never reach `ReadyForLinks` or `ReadyForUsers`.

**Mitigation:** Add a `Clickhouse` devnet component that is auto-enabled when `DeviceHealthOracle.Enabled` is true. The controller and device-health-oracle containers both receive ClickHouse env vars. This keeps the e2e environment self-contained and requires no manual test changes.

### Risk 6: E2E Timing

In e2e tests, the device-health-oracle interval is 10 seconds, and burn-in slot counts can be overridden. The controller must have time to write enough `controller_grpc_getconfig_success` records to cover the burn-in period.

**Mitigation:** E2e tests use short burn-in slot counts. With the controller writing events every few seconds, coverage should accumulate quickly. If timing proves tight, an `Eventually` poll in the test will handle the wait.

## Testing Strategy

### Unit Tests (criteria_test.go, controller_success_test.go)
- **Evaluator stage progression:** Verify `Pending` → `ReadyForLinks` → `ReadyForUsers` with passing criteria
- **Stage gating:** Verify device at `Pending` cannot jump to `ReadyForUsers`
- **Criterion failure:** Verify failing criterion blocks advancement (health stays at current value)
- **No-criteria default:** Verify evaluator with empty criteria advances health (used for links)
- **Skip-update:** Verify no update is queued when health already matches target
- **Time window math:** Verify expected minutes calculation from timestamps
- **ControllerSuccess criterion:** Test with mock ClickHouse client, verify pass/fail thresholds, zero-duration edge case

### Integration Tests (clickhouse_test.go)
- **ClickHouse query:** Use testcontainers-go ClickHouse module (already in go.mod) to:
  - Create the `controller_grpc_getconfig_success` table
  - Insert test data with known coverage patterns
  - Verify `ControllerCallCoverage` returns correct minute counts
  - Test with gaps in coverage (missing minutes)
  - Test with empty table

### E2E Verification
- Tests that enable `DeviceHealthOracle` automatically get ClickHouse
- Controller writes GetConfig events to ClickHouse
- Oracle reads events and evaluates the `ControllerSuccessCriterion`
- Devices advance through `ReadyForLinks` → `ReadyForUsers` after burn-in

### Build Verification
- `make go-build` — binary compiles
- `make go-lint` — no lint errors
- `make go-fmt` — formatting correct

## Estimated Scope

**Size: Medium-Large (~500 lines of new/changed code, ~350 lines of tests)**

New code: ~460 lines across 7 new files
Modified code: ~200 lines across 7 existing files
Test code: ~350 lines

This is at the upper end of the 500-line guideline. The bulk of the new code is the e2e ClickHouse component (`clickhouse.go`, ~180 lines) which is infrastructure boilerplate following established patterns. If the reviewer prefers, the e2e ClickHouse component could be split into a separate prerequisite PR.

## Decisions (from operator feedback)

| Question | Decision |
|---|---|
| Slot-to-time conversion | Use `GetBlockTime` RPC — no `--slot-duration-ms` flag |
| ClickHouse not configured | Log an error on every tick (not a warning, not silent) |
| ClickHouse connection config | Env vars (`CLICKHOUSE_ADDR`, etc.) — keeps secrets out of process list |
| ControllerSuccessCriterion stages | Apply to both `ReadyForLinks` and `ReadyForUsers` |
| Coverage threshold | Strict 100% — at least once per minute, no tolerance |
| E2E test impact | Update e2e to include ClickHouse; tests must pass with the new criterion |
