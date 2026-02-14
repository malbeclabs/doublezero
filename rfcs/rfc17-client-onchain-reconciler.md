# RFC-17: Client Onchain Reconciler

## Summary

**Status: Implemented**

Move tunnel provisioning responsibility from the CLI to a reconciliation loop inside the client daemon (`doublezerod`). The daemon polls onchain User state and automatically provisions or removes tunnels when users are activated or deactivated, replacing the current flow where the CLI directly calls the daemon's `/provision` endpoint. This makes the daemon self-healing on restart, eliminates the JSON state file used for crash recovery, and decouples the daemon's lifecycle from the CLI. With onchain state as the sole source of truth, any tool or integration that creates a User onchain will trigger the daemon to provision the corresponding tunnel — the CLI is no longer the only path to a working connection.

## Motivation

The current provisioning flow has the CLI acting as an orchestrator: it creates a User onchain, waits for activation, fetches device/config/multicast data, computes tunnel parameters, and sends a `/provision` request to the daemon. This creates several problems:

- **Fragile recovery**: If the daemon restarts, it reads a JSON state file (`doublezerod.json`) to re-provision tunnels. If this file is corrupted, missing, or stale, tunnels are not restored until the user manually runs `doublezero connect` again.

- **CLI complexity**: The CLI must understand networking details (tunnel source/destination, ASN config, DZ prefixes, multicast group resolution) that are purely daemon concerns. This makes the CLI harder to maintain and test.

- **Split responsibility**: Provisioning logic is spread across the CLI (parameter computation) and daemon (tunnel setup), making it difficult to reason about the full flow.

- **No automatic deprovisioning**: If a user is deactivated onchain (e.g., rejected, suspended), the daemon has no way to know — tunnels remain up until the user manually disconnects.

- **Tight coupling to CLI**: The daemon cannot provision tunnels on its own — it requires the CLI to compute parameters and call `/provision`. This means the CLI is the only path to a working connection. External tools, automation systems, or alternative frontends that create Users onchain have no way to trigger tunnel setup without reimplementing the CLI's provisioning logic.

## New Terminology

- **Reconciler**: A background loop in the daemon that polls onchain state and converges local tunnel state to match it.
- **Fetcher**: An interface for retrieving onchain program data (`GetProgramData`), abstracting the Solana RPC client.

## Alternatives Considered

- **Keep the CLI-driven model and fix the state file**: We could make the JSON state file more robust (e.g., fsync, checksums). This doesn't solve the split-responsibility problem, doesn't enable automatic deprovisioning, and keeps the CLI unnecessarily complex.

- **Event-driven via WebSocket subscriptions**: The daemon could subscribe to onchain account changes instead of polling. This would reduce latency but adds complexity (reconnection handling, missed events, fallback polling). Polling is simpler, sufficient for the 10-second convergence target, and can be replaced with event-driven updates later if needed.

- **Controller-pushed provisioning**: The controller could push provisioning commands to daemons when user state changes. This would require the controller to track per-client state and maintain connections to all client daemons, adding significant operational complexity.

## Detailed Design

### Architecture

The reconciler runs as a goroutine inside the daemon, alongside the existing BGP, PIM, multicast, and API subsystems. It is enabled when the `--client-ip` flag is set.

```
                        ┌──────────────────────────────────────────┐
                        │              doublezerod                  │
                        │                                          │
                        │  ┌────────────┐     ┌─────────────────┐  │
  Solana RPC ◄──────────│──│ Reconciler │────►│ Network Manager │  │
  (poll every 10s)      │  └────────────┘     └────────┬────────┘  │
                        │                              │           │
                        │                     ┌────────┴────────┐  │
                        │                     │ BGP / PIM /     │  │
                        │                     │ Multicast /     │  │
                        │                     │ Tunnel setup    │  │
                        │                     └─────────────────┘  │
                        └──────────────────────────────────────────┘
```

### Reconciliation Loop

On each tick (default 10 seconds, configurable via `--reconciler-poll-interval`):

1. **Fetch** all program data from Solana (devices, users, multicast groups, config) via the `Fetcher` interface.
2. **Filter** users matching this daemon's client IP and `Activated` status.
3. **Classify** matching users as unicast (IBRL, IBRLWithAllocatedIP, EdgeFiltering) or multicast.
4. **Diff** desired state against current in-memory service state:
   - If unicast users exist but no unicast service is running → provision.
   - If no unicast users but a unicast service is running → remove.
   - Same logic independently for multicast.
5. **Build** a `ProvisionRequest` from onchain data when provisioning:
   - Resolve the device's public IP as tunnel destination.
   - Resolve the tunnel source IP via kernel route lookup (`ip route get`), falling back to the client IP.
   - Collect all DZ prefixes across all devices.
   - Resolve multicast publisher/subscriber groups.
   - Read ASN configuration from the onchain global config.

### Daemon State Management

The JSON state file (`doublezerod.json`) and the `Recover()` startup path are removed entirely. Services now store their `ProvisionRequest` in memory. On daemon restart, the reconciler runs immediately (first tick at t=0) and re-provisions tunnels from onchain state.

The `Provisioner` interface gains a `ProvisionRequest()` method, and the manager exposes `GetProvisionedServices()` for the routes API endpoint.

### CLI Changes

The CLI no longer sends `/provision` requests to the daemon. After creating/activating a user onchain, it polls the daemon's `/status` endpoint until the reconciler has provisioned the tunnel:

```
CLI                          Daemon                      Solana
 │                             │                            │
 │  1. CreateUser ─────────────│───────────────────────────>│
 │                             │                            │ User (Activated)
 │                             │  2. Poll (reconciler) ────>│
 │                             │<──── ProgramData ──────────│
 │                             │  3. Provision tunnel       │
 │  4. GET /status ───────────>│                            │
 │<──── tunnel details ────────│                            │
```

The CLI drops ~700 lines of networking logic (device fetching, global config fetching, tunnel parameter computation, multicast group resolution).

### New Daemon Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--client-ip` | (none) | Client's public IP. Enables the reconciler when set. |
| `--reconciler-poll-interval` | 10 | Seconds between reconciliation polls. |

### Interface Design

The reconciler depends on two interfaces, keeping it decoupled from Solana SDK details and the daemon's internal networking:

```go
// Fetcher abstracts onchain data retrieval.
type Fetcher interface {
    GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

// Manager abstracts daemon service provisioning.
type Manager interface {
    Provision(api.ProvisionRequest) error
    Remove(api.UserType) error
    HasUnicastService() bool
    HasMulticastService() bool
    ResolveTunnelSrc(dst net.IP) (net.IP, error)
}
```

## Impact

### Codebase

- **New code**: `client/doublezerod/internal/reconciler/` (~270 lines implementation, ~420 lines tests).
- **Removed code**: `client/doublezerod/internal/manager/db.go` (209 lines), DB test fixtures, `Recover()` method, CLI provisioning logic (~700 lines from `connect.rs`).
- **Net reduction**: ~470 lines deleted across the change.
- **Modified**: Manager, service implementations (IBRL, EdgeFiltering, Multicast), routes API handler, CLI connect/disconnect commands.

### Operational

- Daemon restart no longer depends on a state file — tunnels are re-provisioned from onchain state within one poll interval.
- Automatic deprovisioning when users are deactivated onchain.
- New `--client-ip` flag required to enable reconciliation.

### Performance

- One Solana RPC call per poll interval (fetches all program data). At 10-second intervals this is negligible load.
- Tunnel convergence time after user activation: up to ~10 seconds (one poll interval) plus tunnel setup time.

## Security Considerations

- The daemon reads onchain state via a public RPC endpoint. No private keys or signing authority is needed — the reconciler is read-only with respect to the ledger.
- The daemon only provisions tunnels for users matching its configured `--client-ip`, preventing a daemon from provisioning tunnels for other clients.
- The RPC endpoint URL and program ID are passed via existing daemon configuration (`--network-config`), not new trust boundaries.

## Backward Compatibility

- **CLI**: Old CLI versions that call `/provision` directly will continue to work — the endpoint is still present. The reconciler and CLI-driven provisioning can coexist; the reconciler will detect the already-provisioned service and skip it.
- **Daemon without `--client-ip`**: If the flag is not set, the reconciler is not started and the daemon behaves exactly as before (CLI-driven provisioning only).
- **Rollout**: The reconciler can be enabled per-client by setting the `--client-ip` flag. No coordinated rollout required.

## Open Questions

- Should the reconciler detect configuration drift (e.g., ASN changes, prefix changes) and re-provision running services, or only react to user activation/deactivation? Currently it only provisions/removes, not updates.
- Should we add a readiness probe that reports whether the reconciler has completed at least one successful poll? This would be useful for orchestration systems waiting for tunnel establishment.
