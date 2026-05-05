# E2E Test Suite

The `e2e/` directory contains end-to-end tests that spin up a full devnet environment using Docker containers (Arista cEOS devices, clients, Solana ledger, controller, etc.). All tests use the `e2e` build tag unless otherwise noted.

## Running Tests

```bash
# Run a specific test
go test -tags e2e -run TestE2E_IBRL -v -count=1 ./e2e/...

# Keep containers after failure for debugging
TESTCONTAINERS_RYUK_DISABLED=true go test -tags e2e -run TestE2E_IBRL -v -count=1 ./e2e/...

# Skip image rebuilds (use existing images)
DZ_E2E_NO_BUILD=1 go test -tags e2e -run TestE2E_IBRL -v -count=1 ./e2e/...

# Enable debug logging
DZ_E2E_DEBUG=1 go test -tags e2e -run TestE2E_IBRL -v -count=1 ./e2e/...
```

Each test spins up multiple Docker containers (cEOS devices require 4 cores and 4.5 GB RAM each). Run specific tests rather than the full suite to avoid exhausting memory.

## File Overview

| File | Build Tag | Description |
|------|-----------|-------------|
| `main_test.go` | `e2e` | `TestMain`, shared helpers, `TestDevnet` struct |
| `fs.go` | (none) | Embedded filesystem (`//go:embed fixtures`) for test fixtures |
| `ibrl_test.go` | `e2e` | Single-device, single-client IBRL connectivity |
| `ibrl_with_allocated_ip_test.go` | `e2e` | IBRL with onchain-allocated IP addresses |
| `multi_client_ibrl_test.go` | `e2e` | Multiple clients on one device (IBRL) |
| `multi_client_ibrl_allocated_ip_test.go` | `e2e` | Multiple clients with allocated IPs |
| `multi_client_ibrl_liveness_test.go` | `e2e` | Route liveness (active/passive probing) |
| `multicast_test.go` | `e2e` | Multicast publisher/subscriber workflow |
| `ibrl_multicast_coexistence_test.go` | `e2e` | IBRL and multicast on the same device/client |
| `allocation_lifecycle_test.go` | `e2e` | Resource allocation/deallocation (tunnel IDs, DZ IPs, etc.) |
| `link_onchain_allocation_test.go` | `e2e` | Link-level onchain resource allocation |
| `device_maxusers_rollover_test.go` | `e2e` | Tunnel ID exhaustion and rollover |
| `dz_prefix_rollover_test.go` | `e2e` | DZ IP prefix exhaustion and rollover |
| `device_telemetry_test.go` | `e2e` | TWAMP probes, InfluxDB/Prometheus metrics |
| `interface_validation_test.go` | `e2e` | Device interface configuration validation rules |
| `activator_interface_delete_test.go` | `e2e` | Interface deletion with out-of-pool IPs |
| `multi_tenant_vrf_test.go` | `e2e` | Multi-tenant VRF isolation |
| `user_ban_test.go` | `e2e` | User ban enforcement |
| `funder_test.go` | `e2e` | SOL account funder service |
| `sdk_serviceability_test.go` | `e2e` | Serviceability program SDK operations |
| `sdk_device_telemetry_test.go` | `e2e` | Device latency samples SDK |
| `sdk_internet_telemetry_test.go` | `e2e` | Internet latency samples SDK |
| `compatibility_test.go` | `e2e` | CLI backward compatibility across versions |
| `device_stress_test.go` | `stress` | High-client-count stress testing |
| `qa_test.go` | `qa` | QA framework for external environments |
| `qa_unicast_test.go` | `qa` | QA unicast connectivity |
| `qa_multicast_test.go` | `qa` | QA multicast connectivity |
| `qa_alldevices_unicast_test.go` | `qa` | QA unicast across all devices |

## Shared Infrastructure

### `main_test.go`

`TestMain` initializes global state before any test runs:
- Parses flags (`-v`, `DZ_E2E_DEBUG`)
- Creates a Docker client and `SubnetAllocator` (allocates non-overlapping `/24` subnets for parallel tests)
- Builds all container images (unless `DZ_E2E_NO_BUILD` is set)

Global variables shared across tests:

```go
var (
    verbose         bool
    debug           bool
    logger          *slog.Logger
    subnetAllocator *docker.SubnetAllocator
    dockerClient    *client.Client
)
```

### `TestDevnet` struct

Wraps `devnet.Devnet` with convenience methods used by most tests:

- **`NewSingleDeviceSingleClientTestDevnet(t)`** -- Creates a standard devnet with one device and one client. Used by most single-device tests.
- **`Start()`** -- Starts the devnet, creates a device, registers a link, adds a client, and sets up latency routes.
- **`ConnectIBRLUserTunnel()`** / **`ConnectUserTunnelWithAllocatedIP()`** -- Connects a client via IBRL (with or without allocated IP).
- **`ConnectMulticastPublisher()`** / **`ConnectMulticastSubscriber()`** -- Connects multicast users.
- **`DisconnectUserTunnel()`** / **`DisconnectMulticastPublisher()`** / **`DisconnectMulticastSubscriber()`** -- Disconnects users.
- **`CreateMulticastGroupOnchain()`** -- Creates a multicast group on the ledger.
- **`WaitForAgentConfigMatchViaController()`** -- Polls the controller until the device's agent config matches an expected fixture.
- **`WaitForUserActivation()`** -- Polls until a user is activated onchain.
- **`GetDeviceLinkInterfaceIPs()`** -- Gets dynamically allocated interface IPs from the ledger.
- **`GetDevicePubkeyOnchain()`** -- Retrieves a device's pubkey via the manager.

### `fs.go`

Embeds the `fixtures/` directory as an `embed.FS` so fixture templates are available at test time without filesystem access.

## Test Categories

### IBRL (Internet Border Routing Layer) Connectivity

#### `TestE2E_IBRL` -- `ibrl_test.go`

Core single-device, single-client test. Subtests:

- **`connect`** -- Connects the client, waits for tunnel up
- **`check_post_connect`** -- Verifies agent config from controller, doublezero address, client routes, device BGP session, tunnel interface on both client and device
- **`disconnect`** -- Disconnects the client
- **`check_post_disconnect`** -- Verifies tunnel removed, agent config updated, BGP peer removed
- **`remove_ibgp_msdp_peer`** -- Removes iBGP/MSDP peer, verifies config update
- **`drain_device` / `undrain_device`** -- Sets device to drained status, verifies config reflects drain, then reactivates

#### `TestE2E_IBRL_WithAllocatedIP` -- `ibrl_with_allocated_ip_test.go`

Same flow as `TestE2E_IBRL` but the client receives an onchain-allocated IP address instead of a link-local address. Also includes a `ban_user` subtest that verifies a banned user's session is invalidated.

#### `TestE2E_MultiClientIBRL` -- `multi_client_ibrl_test.go`

Four clients connect to the same device with various route liveness modes (active, passive, both, neither). Validates device selection via latency and multi-client coexistence.

#### `TestE2E_MultiClientIBRLAllocatedIP` -- `multi_client_ibrl_allocated_ip_test.go`

Multiple clients with allocated IPs on the same device.

#### `TestE2E_MultiClientIBRL_RouteLiveness` -- `multi_client_ibrl_liveness_test.go`

Focused testing of route liveness probing (active and passive modes) with multiple clients.

### Multicast

#### `TestE2E_Multicast` -- `multicast_test.go`

Publisher/subscriber lifecycle:

- **`connect`** -- Creates a multicast group onchain, connects one publisher and one subscriber on the same device
- **`check_post_connect_{publisher,subscriber}`** -- Verifies CLI status, tunnel interface, routes, device tunnel, multicast routes (mroutes), and PIM neighbors
- **`disconnect`** -- Disconnects both users
- **`check_post_disconnect_{publisher,subscriber}`** -- Verifies tunnels removed, routes cleared, BGP peers removed

#### IBRL + Multicast Coexistence -- `ibrl_multicast_coexistence_test.go`

Eight tests covering all combinations:

| Test | Scenario |
|------|----------|
| `TestE2E_IBRL_Multicast_Coexistence` | IBRL user + multicast subscriber on same device |
| `TestE2E_IBRL_Multicast_Publisher_Coexistence` | IBRL user + multicast publisher on same device |
| `TestE2E_IBRL_AllocatedAddr_Multicast_Coexistence` | IBRL (allocated IP) + multicast subscriber |
| `TestE2E_IBRL_AllocatedAddr_Multicast_Publisher_Coexistence` | IBRL (allocated IP) + multicast publisher |
| `TestE2E_SingleClient_IBRL_Then_Multicast` | Single client: IBRL first, then add multicast subscriber |
| `TestE2E_SingleClient_IBRL_AllocatedAddr_Then_Multicast` | Single client: IBRL (allocated IP) first, then multicast subscriber |
| `TestE2E_SingleClient_IBRL_Then_Multicast_Publisher` | Single client: IBRL first, then multicast publisher |
| `TestE2E_Multicast_PublisherXorSubscriber` | Verifies a user cannot be both publisher and subscriber |

### Resource Allocation

#### `TestE2E_User_AllocationLifecycle` -- `allocation_lifecycle_test.go`

Connects and disconnects a user, capturing `ResourceExtension` snapshots before and after. Asserts that `tunnel_net`, `tunnel_id`, and `dz_ip` are properly allocated and then returned.

#### `TestE2E_MulticastGroup_AllocationLifecycle` -- `allocation_lifecycle_test.go`

Similar lifecycle test for multicast group `multicast_ip` allocation.

#### `TestE2E_MultipleLinks_AllocationLifecycle` -- `allocation_lifecycle_test.go`

Tests allocation with multiple links on a single device.

#### `TestE2E_Multicast_ReactivationPreservesAllocations` -- `allocation_lifecycle_test.go`

Regression test for bug #2798. Verifies that re-activating a device preserves existing allocations.

#### `TestE2E_LoopbackInterface_AllocationLifecycle` -- `allocation_lifecycle_test.go`

Tests loopback interface `ip_net` and `node_segment_idx` allocation.

#### `TestE2E_Link_OnchainAllocation` -- `link_onchain_allocation_test.go`

Tests link-level resource allocation on the ledger.

#### `TestE2E_DeviceMaxusersRollover` -- `device_maxusers_rollover_test.go`

Exhausts the tunnel ID space on a device and verifies rollover behavior.

#### `TestE2E_DzPrefix_RolloverAllocation` -- `dz_prefix_rollover_test.go`

Exhausts the DZ IP prefix pool and verifies rollover allocation.

### Device Management

#### `TestE2E_DeviceTelemetry` -- `device_telemetry_test.go`

Spins up devices with TWAMP telemetry enabled plus InfluxDB and Prometheus. Verifies that latency probes are collected and queryable from both backends. Dumps agent logs on failure for debugging.

#### `TestE2E_InterfaceValidation` -- `interface_validation_test.go`

Validates device interface configuration rules:

- `cyoa_on_loopback_rejected` -- CYOA interface type not allowed on loopback
- `cyoa_on_physical_allowed` -- CYOA on physical interface is fine
- `public_ip_on_loopback_with_ute_allowed` -- Public IP on loopback requires UTE flag
- `public_ip_on_loopback_without_ute_rejected` -- Rejected without UTE
- `update_loopback_add_cyoa_rejected` -- Can't add CYOA via update
- `full_lifecycle` -- Create, update, delete full lifecycle

#### `TestE2E_ActivatorInterfaceDeleteOutOfPoolIP` -- `activator_interface_delete_test.go`

Creates a loopback with an out-of-pool IP, waits for activation, deletes the interface, and verifies the activator stays healthy and the interface is removed.

### User Management

#### `TestE2E_UserBan` -- `user_ban_test.go`

Bans a user and verifies they cannot connect. Tests the ban enforcement path through the controller and device.

#### `TestE2E_MultiTenantVRF` -- `multi_tenant_vrf_test.go`

Tests VRF isolation between multiple tenants on the same device.

### SDK Tests

#### `TestE2E_SDK_Serviceability` -- `sdk_serviceability_test.go`

Tests the Go SDK's interaction with the serviceability program (global config updates).

#### `TestE2E_SDK_Telemetry_DeviceLatencySamples` -- `sdk_device_telemetry_test.go`

Step-by-step test of device latency sample operations via the SDK:

1. Attempt read/write before initialization (expect failure)
2. Initialize device latency samples
3. Write first batch of samples
4. Read back and verify
5. Write second batch, verify append
6. Write largest possible batch per transaction

#### `TestE2E_SDK_Telemetry_InternetLatencySamples` -- `sdk_internet_telemetry_test.go`

Same pattern as device latency but for internet latency samples.

### Funding

#### `TestE2E_Funder` -- `funder_test.go`

Tests the account funder service that tops up SOL balances. Verifies metrics and error tracking.

### Backward Compatibility

#### `TestE2E_BackwardCompatibility` -- `compatibility_test.go`

Tests CLI backward compatibility across released versions. Discovers available versions, then runs read and write operations per version. Maintains a `knownIncompatibilities` map for expected failures:

```go
var knownIncompatibilities = map[string]string{
    "write/multicast_group_create": "0.8.1",
    "write/device_set_health":     "0.8.6",
    "write/global_config_set":     "0.8.7",
}
```

### Stress Testing

#### `TestE2E_DeviceStress` -- `device_stress_test.go`

Build tag: `stress`. Spawns a configurable number of client containers against a device and tracks creation/connection metrics under load.

### QA Tests (External Environments)

Build tag: `qa`. These tests run against real deployed environments (devnet, testnet, mainnet-beta) rather than local Docker devnets. Configured via flags: `-hosts`, `-port`, `-env`.

| Test | Description |
|------|-------------|
| `TestQA_UnicastConnectivity` | Pairwise unicast connectivity between all clients |
| `TestQA_MulticastConnectivity` | Multicast subscriber connectivity per host |
| `TestQA_MulticastMultiGroupSimultaneous` | Multiple multicast groups at the same time |
| `TestQA_MulticastAddGroupToExistingUser` | Add a multicast group to an already-connected user |
| `TestQA_MulticastPublisherMultipleGroups` | Publisher subscribing to multiple groups |
| `TestQA_AllDevices_UnicastConnectivity` | Unicast connectivity to every device in batches |

## Internal Packages (`e2e/internal/`)

### `devnet/`

Core devnet orchestration. Key types:

- **`Devnet`** -- Manages the full devnet lifecycle (ledger, manager, controller, activator, devices, clients, networks)
- **`DevnetSpec`** -- Configuration for a devnet (deploy ID, networks, component specs)
- **`Device`** / **`DeviceSpec`** -- Arista cEOS device containers
- **`Client`** / **`ClientSpec`** -- DoubleZero client containers
- **`Ledger`** -- Solana test validator
- **`Manager`** -- CLI manager container
- **`Controller`** -- Config distribution container
- **`Activator`** -- Device activation container
- **`Funder`** -- Account funder container
- **`CYOANetwork`** / **`DefaultNetwork`** -- Docker networks

Also contains: `builder.go` (container image builds), `compat.go` (compatibility helpers), `smartcontract_*.go` (onchain program interaction).

### `devnet/cmd/`

CLI commands for standalone devnet management (used by `dev/dzctl`):
`root.go`, `start.go`, `stop.go`, `destroy.go`, `build.go`, `add-device.go`, `add-client.go`, `start-ledger.go`, `deploy-programs.go`

### `docker/`

Docker utilities:
- **`exec.go`** -- `Exec()`, `ExecReturnJSONList()`, `ExecReturnObject[T]()` for running commands in containers
- **`subnet.go`** -- `SubnetAllocator` for deterministic, non-overlapping subnet allocation across parallel tests
- **`build.go`** / **`pull.go`** / **`run.go`** -- Container image building, pulling, and running

### `arista/`

- **`commands.go`** -- Arista CLI command builders and JSON response structs (`ShowIPBGPSummary`, `ShowIpRoute`, `ShowIPMroute`, `ShowPIMNeighbors`)

### `fixtures/`

- **`render.go`** -- Template rendering with helpers (`seq`, `add`, `sub`) from embedded fixture files
- **`diff.go`** -- `DiffCLITable()` for comparing pipe-delimited CLI output tables (ignores volatile fields like `Last Session Update`)

### `allocation/`

- **`verifier.go`** -- `ResourceSnapshot` capture and assertion helpers (`CaptureSnapshot`, `AssertAllocated`, `AssertDeallocated`)

### `poll/`

- **`until.go`** -- `Until(ctx, condition, timeout, interval)` polling helper

### `netutil/`

- **`ip.go`** -- `DeriveIPFromCIDR()`, `ParseCIDR()`
- **`multicast.go`** -- Multicast address utilities

### `solana/`

- **`keypair.go`** -- Solana keypair loading and base58 encoding

### `random/`

- **`shortid.go`** -- `ShortID()` for generating unique test identifiers

### `logging/`

- **`testcontainers.go`** -- Bridges testcontainers logs to `slog`

### `rpc/`

- **`agent.go`** -- RPC client for doublezero-agent on devices
- **`netlink.go`** -- Netlink-related RPC helpers
- **`traceroute.go`** -- Traceroute RPC helpers
- **`metrics.go`** -- Metrics collection RPC

### `prometheus/`

- **`metrics.go`** -- Prometheus metric query helpers

### `qa/`

- **`test.go`** / **`client.go`** -- QA test framework and client abstraction
- **`client_unicast.go`** / **`client_multicast.go`** -- QA unicast/multicast client helpers
- **`device_assignment.go`** -- Device assignment logic for QA tests
- **`metrics.go`** -- QA metrics collection
- **`slices.go`** -- Slice utilities

### `gomod/`

- **`gomod.go`** -- Go module path resolution utilities

## Common Test Pattern

Most tests follow this structure:

```go
func TestE2E_Something(t *testing.T) {
    t.Parallel()

    // 1. Create devnet
    deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
    log := newTestLoggerForTest(t)
    dn, err := devnet.New(devnet.DevnetSpec{
        DeployID:  deployID,
        DeployDir: t.TempDir(),
        // ... component specs
    }, log, dockerClient, subnetAllocator)

    // 2. Start devnet (ledger, manager, controller, etc.)
    err = dn.Start(t.Context(), nil)

    // 3. Add devices and clients
    device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{...})
    client, err := dn.AddClient(t.Context(), devnet.ClientSpec{...})

    // 4. Connect user
    dn.ConnectIBRLUserTunnel(t, client)
    err = client.WaitForTunnelUp(t.Context(), 90*time.Second)

    // 5. Assert state via subtests
    t.Run("check_post_connect", func(t *testing.T) {
        // Verify agent config, routes, BGP, tunnels
        err := dn.WaitForAgentConfigMatchViaController(t, device.ID, expectedConfig)
        require.NoError(t, err)
    })
}
```

The `NewSingleDeviceSingleClientTestDevnet(t)` helper wraps steps 1-3 for the common single-device case.

## Fixtures

Test fixtures live in `e2e/fixtures/` and are embedded via `fs.go`. They are organized by test scenario:

```
fixtures/
├── ibrl/                                  # Basic IBRL
│   ├── doublezero_agent_config_user_added.tmpl
│   ├── doublezero_agent_config_user_removed.tmpl
│   ├── doublezero_agent_config_peer_removed.tmpl
│   ├── doublezero_agent_config_drained.tmpl
│   ├── doublezero_user_list_user_added.tmpl
│   └── ...
├── ibrl_with_allocated_addr/              # IBRL with allocated IP
├── multicast/                             # Multicast
├── ibrl_multicast_coexistence/            # Coexistence scenarios
└── ...
```

Templates use Go `text/template` syntax and are rendered with dynamic values (client IPs, device IPs, tunnel numbers) via `fixtures.Render()`.
