# RFC-17: Client Onchain Reconciler

## Summary

**Status: Implemented**

Move tunnel provisioning responsibility from the CLI to a reconciliation loop inside the client daemon (`doublezerod`). The daemon polls onchain User state and automatically provisions or removes tunnels when users are activated or deactivated, replacing the current flow where the CLI directly calls the daemon's `/provision` endpoint. This makes the daemon self-healing on restart, eliminates the JSON state file used for crash recovery, and decouples the daemon's lifecycle from the CLI. With onchain state as the sole source of truth, any tool or integration that creates a User onchain will trigger the daemon to provision the corresponding tunnel — the CLI is no longer the only path to a working connection.

## Motivation

The current provisioning flow has the CLI acting as an orchestrator: it creates a User onchain, waits for activation, fetches device/config/multicast data, computes tunnel parameters, and sends a `/provision` request to the daemon. This creates several problems:

- **Fragile recovery**: If the daemon restarts, it reads a JSON state file (`doublezerod.json`) to re-provision tunnels. If this file is corrupted, missing, or stale, tunnels are not restored until the user manually runs `doublezero connect` again.

- **No automatic deprovisioning**: If a user is deactivated onchain (e.g., rejected, suspended), the daemon has no way to know — tunnels remain up until the user manually disconnects.

- **Tight coupling to CLI**: The daemon cannot provision tunnels on its own — it requires the CLI to compute parameters and call `/provision`. This means the CLI is the only path to a working connection. External tools, automation systems, or alternative frontends that create Users onchain have no way to trigger tunnel setup without reimplementing the CLI's provisioning logic.

## New Terminology

- **Reconciler**: A background loop in the daemon that polls onchain state and converges local tunnel state to match it.
- **Fetcher**: An interface for retrieving onchain program data (`GetProgramData`), abstracting the RPC client.

## Detailed Design

### Architecture

The reconciler runs as a goroutine inside the network manager (`NetlinkManager`), alongside the existing BGP, PIM, multicast, and API subsystems. It is enabled when the client IP is known — either via the `--client-ip` flag or auto-discovered via default route / external lookup.

```
                  ┌────────────────────────────────────────┐
                  │            doublezerod                 │
                  │                                        │
                  │  ┌──────────────────────────────────┐  │
DZ Ledger RPC ◄───│──│      Network Manager             │  │
(poll every 10s)  │  │ (reconciler loop + provisioning) │  │
                  │  └────────────────┬─────────────────┘  │
                  │                   │                    │
                  │         ┌─────────┴──────────┐         │
                  │         │ BGP / PIM /        │         │
                  │         │ Multicast /        │         │
                  │         │ Tunnel setup       │         │
                  │         └────────────────────┘         │
                  └────────────────────────────────────────┘
```

### Reconciliation Loop

On each tick (default 10 seconds, configurable via `--reconciler-poll-interval`):

1. **Fetch** all program data from the DZ Ledger (devices, users, multicast groups, config) via the `Fetcher` interface.
2. **Filter** users matching this daemon's client IP and `Activated` status.
3. **Classify** matching users as unicast (IBRL, IBRLWithAllocatedIP, EdgeFiltering) or multicast.
4. **Diff** desired state against current in-memory service state:
   - If unicast users exist but no unicast service is running → provision using the first matching user.
   - If no unicast users but a unicast service is running → remove.
   - Same logic independently for multicast.
   - Unicast and multicast are reconciled independently — a client can have one of each running simultaneously.

   In practice, the activator enforces one user per type per client IP, so multiple matching unicast users shouldn't occur. If they do, the reconciler uses the first one it encounters.
5. **Build** a `ProvisionRequest` from onchain data when provisioning:
   - Resolve the device's public IP as tunnel destination.
   - Resolve the tunnel source IP via kernel route lookup (`ip route get`), falling back to the client IP.
   - Collect all DZ prefixes across all devices.
   - Resolve multicast publisher/subscriber groups.
   - Read ASN configuration from the onchain global config.

If the RPC call fails or any step in the reconciliation encounters an error, the reconciler logs the error and retries on the next tick. Transient failures do not crash the daemon or affect already-provisioned tunnels.

### Daemon State Management

The JSON state file (`doublezerod.json`) and the `Recover()` startup path are removed entirely. Services now store their `ProvisionRequest` in memory. On daemon restart, the reconciler re-provisions tunnels from onchain state if enabled.

The `Provisioner` interface gains a `ProvisionRequest()` method, and the manager exposes `GetProvisionedServices()` for the routes API endpoint.

### Reconciler Enable/Disable

The reconciler starts in a disabled state on fresh installs and must be explicitly enabled. This prevents the daemon from polling and provisioning tunnels before the user has connected for the first time.

**Persistent state file** (`/var/lib/doublezerod/state.json`):

Stores `{"reconciler_enabled": true|false}`. On startup:

1. `state.json` exists → use its value.
2. `state.json` missing, old `doublezerod.json` exists → migration from pre-reconciler daemon: write `state.json` with `enabled: true`, delete old file, start reconciler.
3. Neither file exists → fresh install: write `state.json` with `enabled: false`, don't start reconciler.

**Runtime control** via `enableCh` channel:

- On enable: write state file, reconcile immediately, resume polling.
- On disable: write state file, tear down all active services (unicast and multicast), stop polling.

**Daemon HTTP endpoints:**

| Endpoint | Description |
|----------|-------------|
| `POST /enable` | Enable the reconciler, persist state |
| `POST /disable` | Disable the reconciler, tear down tunnels, persist state |
| `GET /v2/status` | Returns `{"reconciler_enabled": bool, "client_ip": "...", "services": [...]}` — includes the daemon's discovered client IP and the services array (same shape as the existing `GET /status` response) |

The existing `GET /status` endpoint is kept unchanged for external tooling compatibility.

### CLI Changes

**New commands:**

| Command | Description |
|---------|-------------|
| `doublezero enable` | Enable the reconciler (calls `POST /enable`) |
| `doublezero disable` | Disable the reconciler, tear down tunnels (calls `POST /disable`) |

**Connect implicit enable:** `doublezero connect` enables the reconciler before polling for daemon provisioning, so users don't need to run `enable` separately.

**Client IP from daemon:** `doublezero connect` reads the client IP from the daemon's `GET /v2/status` response (`client_ip` field) instead of discovering it independently. The CLI's `--client-ip` flag is deprecated — users should set `--client-ip` on the daemon (`doublezerod`) instead.

**Status display:** `doublezero status` uses `GET /v2/status` and shows reconciler enabled/disabled state alongside service details.

**Disconnect:** does not disable the reconciler — it only deletes the onchain user. The reconciler (if enabled) sees no matching user and tears down the tunnel.

The CLI no longer sends `/provision` requests to the daemon. After creating a user onchain, it polls until the user reaches `Activated` status (the activator handles this transition), then polls the daemon's `/status` endpoint until the reconciler has provisioned the tunnel:

```
CLI                          Activator       Daemon                      DZ Ledger
 │                             │               │                            │
 │  1. CreateUser ─────────────│───────────────│───────────────────────────>│
 │                             │               │                            │ User (Pending)
 │                             │  2. Activate ─│───────────────────────────>│
 │                             │               │                            │ User (Activated)
 │  3. Poll user status ───────│───────────────│───────────────────────────>│
 │<──── Activated ─────────────│───────────────│────────────────────────────│
 │                             │               │                            │
 │                             │               │  4. Poll (reconciler) ────>│
 │                             │               │<──── ProgramData ──────────│
 │                             │               │  5. Provision tunnel       │
 │  6. GET /status ───────────>│               │                            │
 │<──── tunnel details ────────│───────────────│                            │
```

The CLI polls the daemon's `/status` endpoint up to 12 times at 5-second intervals (60 seconds total) waiting for the tunnel to appear. If the reconciler hasn't provisioned the tunnel within that window, the CLI times out with an error — though the reconciler will still provision the tunnel on a subsequent poll.

The CLI drops ~400 lines of networking logic (device fetching, global config fetching, tunnel parameter computation, multicast group resolution).

### New Daemon Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--client-ip` | (auto-discovered) | Client's public IP. Optional — if not set, the daemon auto-discovers the IP via a kernel default-route source hint (UDP dial to a well-known public IP), falling back to an external lookup via `ifconfig.me`. The default-route approach avoids the ambiguity of interface scanning on multi-homed hosts. Explicit value takes precedence. |
| `--reconciler-poll-interval` | 10 | Seconds between reconciliation polls. |
| `--state-dir` | `/var/lib/doublezerod` | Directory for persistent state files (`state.json`). |

### Interface Design

The reconciler depends on one interface for onchain data retrieval:

```go
// Fetcher abstracts onchain data retrieval.
type Fetcher interface {
    GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}
```

## Impact

### Codebase

- **New code**: Reconciler logic on `NetlinkManager` in `client/doublezerod/internal/manager/manager.go`, state persistence in `manager/state.go`, metrics in `manager/metrics.go`, client IP auto-discovery in `runtime/clientip.go`, caching fetcher in `onchain/fetcher.go`.
- **Removed code**: `client/doublezerod/internal/manager/db.go` (on-disk JSON state database), DB test fixtures, `Recover()` method, CLI provisioning logic (~400 lines from `connect.rs`).
- **Modified**: Manager, service implementations (IBRL, EdgeFiltering, Multicast), routes API handler, CLI connect/disconnect commands.

### Operational

- Daemon restart no longer depends on a state file — tunnels are re-provisioned from onchain state within one poll interval.
- Automatic deprovisioning when users are deactivated onchain.
- Client IP is auto-discovered (or set explicitly via `--client-ip`) to enable reconciliation.

### Performance

- One RPC call per poll interval (fetches all program data). At 10-second intervals this is negligible load.
- Tunnel convergence time after user activation: up to ~10 seconds (one poll interval) plus tunnel setup time.

## Security Considerations

- The daemon reads onchain state via a public RPC endpoint. No private keys or signing authority is needed — the reconciler is read-only with respect to the ledger.
- The daemon filters onchain users by client IP — it only provisions tunnels for `Activated` users whose `ClientIp` field exactly matches the daemon's discovered or configured IP. Users belonging to other client IPs are ignored entirely.
- The RPC endpoint URL and program ID come from the daemon's environment configuration (`--env`), with optional overrides via `--program-id` and `--solana-rpc-endpoint`.

## Backward Compatibility

- **CLI**: The `/provision` and `/remove` HTTP endpoints are still present on the daemon for now but are unused by the CLI. They will be removed in a future cleanup since the daemon and CLI are always packaged and released together.
- **Daemon without `--client-ip`**: If the flag is not set, the daemon auto-discovers the client IP via default route or external lookup. If discovery fails (e.g., no network connectivity), the daemon exits with a fatal error — the reconciler requires a client IP to function and there is no fallback mode.
- **Rollout**: The daemon auto-discovers the client IP on startup (overridable with `--client-ip`). Fresh installs start with the reconciler disabled; operators run `doublezero enable` after the first `doublezero connect`. Existing installs that already have a `doublezerod.json` state file are migrated to enabled automatically.

