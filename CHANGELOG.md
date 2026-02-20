# Changelog

All notable changes to this project will be documented in this file.

## Unreleased

### Breaking

- N/A

### Changes

- CLI
  - Write version outdated warning to stderr instead of stdout to avoid breaking `--json` output

## [v0.8.10](https://github.com/malbeclabs/doublezero/compare/client/v0.8.9...client/v0.8.10) – 2026-02-19

### Breaking

- N/A

### Changes

- Activator
  - removes accesspass monitor task (that expires access passes)
- Monitor
  - Add Prometheus metrics for multicast publisher block utilization (`doublezero_multicast_publisher_block_total_ips`, `doublezero_multicast_publisher_block_allocated_ips`, `doublezero_multicast_publisher_block_utilization_percent`) — enables Grafana alerting on IP pool exhaustion thresholds
- SDK (Go)
  - Add `GetMulticastPublisherBlockResourceExtension()` method to serviceability client for fetching the global multicast publisher IP allocation bitmap
  - Fix LinkDesiredStatus discriminants (hard-drained=6, soft-drained=7)
- Onchain Programs
  - Upgrade programs from system_instruction to solana_system_interface
  - Refactor user creation to validate all limits (max_users, max_multicast_users, max_unicast_users) before incrementing counters — improves efficiency by avoiding wasted work on validation failures and follows fail-fast best practice
  - Serviceability: `UnlinkDeviceInterface` now only allows `Activated` or `Pending` interfaces; when an associated link account is provided for an `Activated` interface, the link must be in `Deleting` status
  - Links and devices can no longer be deleted from `Activated` status — must be drained first; deletion is rejected with `InvalidStatus`
  - Contributors, locations, multicast groups, and users can now be deleted from any operational status (not just `Activated`); only `Deleting`/`Updating` states are blocked
  - SDK: `UnlinkDeviceInterfaceCommand` automatically discovers and passes associated link accounts
  - Serviceability: allow contributors to update prefixes when for IBRL when no users are allocated
- CLI
  - `doublezero status` now shows a `Tenant` column (between `User Type` and `Current Device`) with the tenant code associated with the user; empty when no tenant is assigned
- Client
  - Fix tunnel overlay address scope to prevent kernel from selecting link-local /31 as source for routed traffic
- E2E / QA Tests
  - Fix QA unicast test flake caused by RPC 429 rate limiting during concurrent user deletion — treat transient RPC errors as non-fatal in the deletion polling loop
  - Backward compatibility test: use `--status` instead of `--desired-status` for drain commands; fix version ranges (link drain compatible since v0.7.2, device drain since v0.8.1)
  - Remove devnet/testnet environment filter from `TestQA_MulticastPublisherMultipleGroups` — test now runs against all environments

## [v0.8.9](https://github.com/malbeclabs/doublezero/compare/client/v0.8.8...client/v0.8.9) – 2026-02-16

### Breaking

- N/A

### Changes

- Activator
  - Fail to start if any global config network blocks (`device_tunnel_block`, `user_tunnel_block`, `multicastgroup_block`, `multicast_publisher_block`) are unset (0.0.0.0/0)
  - Fix multicast publisher dz_ip leak in offchain deallocation — IPs from the global publisher pool were never freed on user deletion because the check required non-empty publishers, which the smartcontract already clears before allowing deletion
- Client
  - Fix heartbeat sender not restarting after disconnect due to poisoned done channel
- Onchain Programs
  - bugfix(serviceability): contributors can now update their interfaces, CYOA interfaces saved on create, physical interfaces remain after unlink ([#2993](https://github.com/malbeclabs/doublezero/pull/2993))
- Device controller
  - Reject users with BGP martian DZ IPs (RFC 1918, loopback, multicast, link-local, documentation nets, shared address space, reserved) to prevent invalid addresses from being advertised via BGP or permitted in device ACLs
- Claude
  - chore: update CLAUDE.md ([#2999](https://github.com/malbeclabs/doublezero/pull/2999))
- e2e
  - e2e: Add network contributor e2e flow tests ([#2997](https://github.com/malbeclabs/doublezero/pull/2997))

## [v0.8.8](https://github.com/malbeclabs/doublezero/compare/client/v0.8.7...client/v0.8.8) – 2026-02-13

### Breaking

- None for this release

### Changes

- Activator
  - Assign multicast publisher IPs from global pool in serviceability GlobalConfig instead of per-device blocks
- Client
  - Add multicast publisher heartbeat sender — sends periodic UDP packets to each multicast group to keep PIM (S,G) mroute state alive on devices
  - Fix panic in heartbeat sender when concurrent teardown requests race on close
- E2E tests
  - Add daily devnet QA test for device provisioning lifecycle (RFC12) — deletes/recreates device and links, restarts daemons with new pubkey via Ansible
- Serviceability: prevent creating or activating links on interfaces with CYOA or DIA assignments, and prevent setting CYOA/DIA on interfaces that are already linked
- CLI: add early validation in `link wan-create` and `link dzx-create` to reject interfaces with CYOA or DIA assignments

## [v0.8.7](https://github.com/malbeclabs/doublezero/compare/client/v0.8.6...client/v0.8.7) – 2026-02-10

### Breaking

- None for this release

### Changes

- SDK
  - Added Tenant to all sdks
- E2E tests
  - Added multi-tenancy deletion test coverage
- Telemetry
  - Add `doublezero-geoprobe-agent`, intermediary probe server for RFC16
  - Adds support for per-tenant metro routing policy
  - Add `--geoprobe-pubkey` flag to `doublezero-geoprobe-agent` for device identity
  - `LocationOffset` struct now includes `SenderPubkey` to distinguish individual devices that share the same signing authority
- Cli
  - Automatic detection of the authorized tenant is added.
  - The `delete tenant` command allows cascading deletion of users and access passes.
- Telemetry
  - extend device telemetry agent to measure RTT to child geoProbes via TWAMP, generate signed LocationOffset structures, and deliver them via UDP as per rfcs/rfc16-geolocation-verification.md
  - geoprobe-target: example target listener for geolocation verification with TWAMP reflector, UDP offset receiver, signature chain verification, distance calculation logging, and DoS protections (5-reference depth limit and per-source-IP rate limiting) (#2901)
- Onchain programs
  - feat(serviceability): add TenantBillingConfig and epoch tracking to UpdatePaymentStatus ([#2922](https://github.com/malbeclabs/doublezero/pull/2922))
  - feat(smartcontract): add payment_status, token_account fields and UpdatePaymentStatus instruction ([#2880](https://github.com/malbeclabs/doublezero/pull/2880))
  - fix(smartcontract): correctly ser/deser ops_manager_pk ([#2887](https://github.com/malbeclabs/doublezero/pull/2887))
  - Serviceability: add metro_routing and route_liveness boolean fields to Tenant for routing configuration
  - Serviceability: add Tenant account type with immutable code-based PDA derivation, VRF ID, administrator management, and reference counting for safe deletion
  - Serviceability: add TenantAddAdministrator and TenantRemoveAdministrator instructions for foundation-managed administrator lists
  - Serviceability: extend UserUpdate instruction to support tenant_pk field updates with automatic reference count management on old and new tenants (backward compatible with old format)
  - Serviceability: extend UserCloseAccount instruction to decrement tenant reference count when closing user with assigned tenant
  - Serviceability: add reference count validation in DeleteMulticastGroup to prevent deletion when active publishers or subscribers exist
  - Serviceability: fix multicast group closeaccount to use InvalidStatus error and remove redundant publisher/subscriber count check
  - Serviceability: add tenant_allowlist field to AccessPass to restrict which tenants can use specific access passes (backward compatible with existing accounts)
  - Serviceability: bypass validation for link delete ([#2934](https://github.com/malbeclabs/doublezero/pull/2934))
  - Serviceability: add per-device unicast and multicast user limits with separate counters and configurable max values ([RFC-14](rfcs/rfc14-per-device-unicast-multicast-user-limits.md))
  - Fix link device & link updates
- SDK
  - Add metro_routing and route_liveness fields to CreateTenantCommand and UpdateTenantCommand
  - Add CreateTenant, UpdateTenant (vrf_id only), DeleteTenant, GetTenant, and ListTenant commands with support for code or pubkey lookup
  - Add AddAdministratorTenant and RemoveAdministratorTenant commands for tenant administrator management
  - UpdateUserCommand extended with tenant_pk field and automatic tenant account resolution for reference counting
  - SetAccessPassCommand extended with tenant field to specify allowed tenant for access pass
  - TypeScript SDK updated with tenantAllowlist field in AccessPass interface and deserialization
- CLI
  - Fix `multicastgroup update` command to properly parse human-readable bandwidth values (e.g., "1Gbps", "100Mbps") in `--max-bandwidth` flag
  - Add --metro-route and --route-aliveness flags to tenant create and update commands
  - Add tenant subcommands (create, update, delete, get, list, add-administrator, remove-administrator) to doublezero and doublezero-admin CLIs
  - Support simultaneous publisher and subscriber multicast via `--publish` and `--subscribe` flags
  - Add `--max-unicast-users` and `--max-multicast-users` flags to `device update` command
  - Add filtering options and desired_status & metrics_publisher_pk field to device and link list commands
  - Added activation check for existing users before subscribing to new groups (#2782)
  - access-pass set: add --tenant argument to specify tenant code for access pass restriction (converts to tenant PDA onchain)
  - tenant list: improve output formatting with table support and JSON serialization options (--json, --json-compact)
  - default tenant support added to config
- SDK
  - Add read-only Go SDK (`revdist`) for the revenue distribution Solana program, with typed deserialization of all onchain accounts and Rust-generated fixture tests for cross-language compatibility
  - Add `revdist-cli` tool for inspecting onchain revenue distribution state
  - Add Python and TypeScript SDKs for serviceability, telemetry, and revdist programs with typed deserialization, RPC clients, PDA derivation, enum string types, and cross-language fixture tests
  - Add shared `borsh-incremental` library (Go, Python, TypeScript) for cursor-based Borsh deserialization with backward-compatible trailing field defaults
  - Add npm and PyPI publish workflows for serviceability and telemetry SDKs
  - DeleteUserCommand updated to wait for activator to process multicast user unsubscribe before deleting the user
- Device controller
  - Record successful GetConfig gRPC calls to ClickHouse for device telemetry tracking
  - Multi-tenancy vrf support added
  - Skip isis and pim config for CYOA/DIA tagged interfaces
- Onchain programs
  - Enforce that `CloseAccessPass` only closes AccessPass accounts when `connection_count == 0`, preventing closure while active connections are present.
- Monitor
  - Add sol-balance watcher to track SOL balances for configured accounts and export Prometheus metrics for alerting
- Client
  - Support simultaneous publisher and subscriber multicast in the daemon
- Telemetry
  - Add consecutive-loss-based sender eviction to the telemetry collector so broken TWAMP senders are recreated quickly instead of persisting until TTL expiry (`--max-consecutive-sender-losses`, default 30)
- E2E tests
  - e2e: add multi-tenancy VRF isolation test ([#2891](https://github.com/malbeclabs/doublezero/pull/2891))
  - Add backward compatibility test that validates older CLI versions against the current onchain program by cloning live state from testnet and mainnet-beta
  - QA multicast tests: add diagnostic dumps on failure (status, routes, latency, multicast reports, onchain user/device state), cleanup stale test groups at test start, and fix disconnect blocking on stuck daemon status

## [v0.8.6](https://github.com/malbeclabs/doublezero/compare/client/v0.8.5...client/v0.8.6) – 2026-02-04

### Breaking

- None for this release

### Changes

- CLI
  - Remove log noise on resolve route
  - `doublezero resource verify` command added to verify onchain resources
  - Enhance delete multicast group command to cascade into deleting AP entry (#2754)
- Onchain programs
  - Removed device and user allowlist functionality, updating the global state, initialization flow, tests, and processors accordingly, and cleaning up unused account checks.
  - Serviceability: require DeactivateMulticastGroup to only close multicast group accounts when both `publisher_count` and `subscriber_count` are zero, preventing deletion of groups that still have active publishers or subscribers.
  - Deprecated the user suspend status, as it is no longer used.
  - Serviceability: enforce that CloseAccountUser instructions verify the target user has no multicast publishers or subscribers (both `publishers` and `subscribers` are empty) before closing, and add regression coverage for this behavior.
  - Enhance access pass functionality with new Solana-specific types
  - fix default desired status
- Telemetry
  - Fix goroutine leak in TWAMP sender — `cleanUpReceived` goroutines now exit on `Close()` instead of living until process shutdown
- Client
  - Cache network interface index/name lookups in liveness UDP service to fix high CPU usage caused by per-packet RTM_GETLINK netlink dumps
  - Add observability to BGP handleUpdate: log withdrawal/NLRI counts per batch and track processing duration via `doublezero_bgp_handle_update_duration_seconds` histogram
- E2E tests
  - The QA alldevices test now skips devices that are not calling the controller
  - e2e: Expand RFC11 end-to-end testing ([#2801](https://github.com/malbeclabs/doublezero/pull/2801))
  - e2e(RFC11): add dz prefix rollover allocation test ([#2820](https://github.com/malbeclabs/doublezero/pull/2820))

## [v0.8.5](https://github.com/malbeclabs/doublezero/compare/client/v0.8.4...client/v0.8.5) – 2026-02-02

### Breaking

- None for this release

### Changes

- Smartcontract
  - fix(smartcontract): reserve first IP of DzPrefixBlock for device ([#2753](https://github.com/malbeclabs/doublezero/pull/2753))
- Client
  - Fix race in bgp status handling on peer deletion

## [v0.8.4](https://github.com/malbeclabs/doublezero/compare/client/v0.8.3...client/v0.8.4) – 2026-01-28

### Breaking

- None for this release

### Changes

- Telemetry
  - Force IPv4-only connections for gNMI tunnel client and fix TLS credential handling
- Client
  - Support simultaneous unicast and multicast tunnels in doublezerod
  - Support publishing and subscribing to multiple multicast groups simultaneously
- CLI
  - Support publishing and subscribing a user to multiple multicast groups via `--group` flag
  - Remove single tunnel constraint
- SDK
  - Go SDK can now perform batch writes to device.health and link.health as per rfc12
- Activator
  - fix(activator): add on-chain allocation support for users ([#2744](https://github.com/malbeclabs/doublezero/pull/2744))
  - On-chain allocation enabled
- Smartcontract
  - feat(smartcontract): add use_onchain_deallocation flag to MulticastGroup ([#2748](https://github.com/malbeclabs/doublezero/pull/2748))
- CLI
  - Remove restriction for a single tunnel per user; now a user can have a unicast and multicast tunnel concurrently (but can only be a publisher _or_ a subscriber) ([2728](https://github.com/malbeclabs/doublezero/pull/2728))

## [v0.8.3](https://github.com/malbeclabs/doublezero/compare/client/v0.8.2...client/v0.8.3) – 2026-01-22

- Data
  - Add indexer that syncs serviceability and telemetry data to ClickHouse and Neo4J

### Breaking

- None for this release

### Changes

- CLI
  - Remove log noise on resolve route
  - Add `global-config qa-allowlist` commands to manage QA identity allowlist to bypass status and max_users checks in QA
  - Add "-skip-capacity-check" flag to bypass status and max_users checks in QA to test devices that are still being provisioned
  - Remove "unknown" status from doublezero status command and implement "failed" and "unreachable" statuses
- Client
  - Enable route liveness passive-mode by default
  - Add `make install` make target. To build and deploy from source, users can now run `cd client && make build && make install` to install the doublezero and doublezerod binaries and the doublezerod systemd unit.
- Onchain programs
  - Serviceability: remove validation check for interface delete ([#2707](https://github.com/malbeclabs/doublezero/pull/2707))
  - Serviceability: interface-cyoa only on physical interfaces, don't require interfaces to be tagged, add same validation logic to update interface ([#2700](https://github.com/malbeclabs/doublezero/pull/2700))
  - Enforce Activated status check before suspending contributor, exchange, location, and multicastgroup accounts
  - Removed device and user allowlist functionality, updating the global state, initialization flow, tests, and processors accordingly, and cleaning up unused account checks.
  - Serviceability: require DeactivateMulticastGroup to only close multicast group accounts when both `publisher_count` and `subscriber_count` are zero, preventing deletion of groups that still have active publishers or subscribers.
  - Deprecated the user suspend status, as it is no longer used.
  - Serviceability: enforce that CloseAccountUser instructions verify the target user has no multicast publishers or subscribers (both `publishers` and `subscribers` are empty) before closing, and add regression coverage for this behavior.
  - Removed device and user allowlist functionality, updating the global state, initialization flow, tests, and processors accordingly, and cleaning up unused account checks.
  - SetGlobalConfig, ActivateDevice, UpdateDevice and CloseAccountDevice instructions updated to manage resource accounts.
  - Add option for Contributor B to reject a link created by Contributor A just as Contributor A can cancel its own created link
- Telemetry
  - Add gNMI tunnel client for state collection
- Activator
  - fix(activator): ip_to_index fn honors ip range #2658
- E2E tests
  - Add influxdb, prometheus, and device-health-oracle containers
  - Add interface lifecycle tests ([#2700](https://github.com/malbeclabs/doublezero/pull/2700))
  - Only fail QA alldevices test run if device status is "Activated" and max users > 0
- SDK
  - Commands for setting global config, activating devices, updating devices, and closing device accounts now manage resource accounts.
  - Serviceability: return error when GetProgramAccounts returns empty result instead of silently returning empty data
- Smartcontract
  - feat(smartcontract): RFC 11 activation for User entity
  - feat(smartcontract): RFC 11 add on-chain resource allocation for Link
- Device Health Oracle
  - Add new device-health-oracle component. See rfcs/rfc12-network-provisioning.md for details.
  - Calculate burn-in timestamp based from slot numbers (current minus 200_000 slots for provisioning, current minus 5_000 slots for maintenance)
- CI
  - Add separate apt repo for doublezero-testnet

## [v0.8.2](https://github.com/malbeclabs/doublezero/compare/client/v0.8.1...client/v0.8.2) – 2025-01-13

### Breaking

- None for this release

### Changes

- Client
  - Always delegate RouteAdd regardless of noUninstall flag
- Telemetry
  - Include solana vote pubkey in global monitor metrics
  - Run telemetry agent on pending and drained links

## [v0.8.1](https://github.com/malbeclabs/doublezero/compare/client/v0.8.0...client/v0.8.1) – 2025-01-12

### Breaking

### Changes

- Onchain programs
  - Serviceability: enforce that ActivateLink and CloseAccountLink instructions verify the provided side A/Z device accounts match the link's stored `side_a_pk` and `side_z_pk` before proceeding.
- CLI
  - Update contributor, device, exchange, link, location, and multicast group commands to ignore case when matching codes
  - ActivateMulticastGroup now supports on-chain IP allocation from ResourceExtension bitmap (RFC 11).
  - IP address lookup responses that do not contain a valid IPv4 address (such as upstream timeout messages) are now treated as retryable errors instead of being parsed as IPs.
  - `doublezero resource` commands added for managing ResourceExtension accounts.
  - Added health_oracle to the smart contract global configuration to manage and authorize health-related operations.
  - Added --ip-net support to create to match the existing behavior in update.
  - Use DZ IP for user lookup during status command instead of client IP
- Onchain programs
  - Fix CreateMulticastGroup to use incremented globalstate.account_index for PDA derivation instead of client-provided index, to ensure the contract is the authoritative source for account indices
  - Add on-chain validation to reject CloseAccountDevice when device has active references (reference_count > 0)
  - Allow contributor owner to update ops manager key
  - Add new arguments on create interface cli command
  - Serviceability: enforce that resume instructions for locations, exchanges, contributors, devices, links, and users only succeed when the account status is `Suspended`, returning `InvalidStatus` otherwise, and add tests to cover the new behavior.
  - RequestBanUser: only allow requests when user.status is Activated or Suspended; otherwise return InvalidStatus
  - Serviceability: require device interfaces to be in `Pending` status before they can be rejected, and add tests to cover the new status check
  - Add ResourceExtension to track IP/ID allocations. Foundation instructions added to create/allocate/deallocate.
  - ResourceExtension optimization using first_free_index for searching bitmaps
  - Added the **INSTRUCTION_GUIDELINES** document defining the standard for instruction creation.
  - Enforce best practices for instruction implementation across onchain programs
  - Add missing system program account owner checks in multiple instructions
  - Refactor codebase for improved maintainability and future development
  - Introduced health management for Devices and Links, adding explicit health states, authorized health updates, and related state, processor, and test enhancements.
  - Require that BanUser can only be executed when the target user's status is PendingBan, enforcing the expected user ban workflow (request-ban -> ban).
  - Introduce desired status to Link and Devices
  - Introduced health management for Devices and Links, adding explicit health states, authorized health updates, and related state, processor, and test enhancements.
  - Restrict DeleteDeviceInterface to interfaces in Activated or Unlinked status; attempting to delete interfaces in other statuses now fails with InvalidStatus.
  - Updated validation to allow public IP prefixes for CYOA/DIA, removing the restriction imposed by type-based checks.
  - Transit devices can now be provisioned without a public IP, aligning the requirements with their actual networking model and avoiding unnecessary configuration constraints.
  - Enforce that ActivateDeviceInterface only activates interfaces in Pending or Unlinked status, returning InvalidStatus for all other interface states
  - Introduce desired status to Link and Devices
- Internet Latency Telemetry
  - Fixed a bug that prevented unresponsive ripeatlas probes from being replaced
  - Fixed a bug that caused ripeatlas samples to be dropped when they were delayed to the next collection cycle
- Link & device Latency Telemetry
  - Telemetry data can now be received while entities are in provisioning and draining states.
- Device controller
  - Add histogram metric for GetConfig request duration
  - Add gRPC middleware for prometheus metrics
  - Add device status label to controller_grpc_getconfig_requests_total metric
  - Add logic to shutdown user BGP, IBGP sessions, MSDP neighbors, and ISIS when device.status is drained
- Device agents
  - Increase default controller request timeout in config agent
  - Initial state collect in telemetry agent
- Client
  - Route liveness treats peers that advertise passive mode as selectively passive; does not manage their routes directly.
  - Route liveness runs in passive mode for IBRL with allocated IP, if global passive mode is enabled.
  - Advertise peer client version with route liveness control packets.
  - Add `doublezero_bgp_routes_installed` gauge metric for number of installed BGP routes
  - Add route liveness gauges for in-memory maps
  - Route liveness sets set of routes configured as excluded to `AdminDown`.
  - Add histogram metric for BGP session establishment duration
  - For IBRL with allocated IP mode, resolve tunnel source IP from routing table via resolve-route API endpoint instead of using client IP to support clients behind NAT
  - Configure MTU down to 1476 on client tunnel in case path MTU discovery is not working
  - Increase route liveness max backoff duration
- Global monitor
  - Initial implementation
- Release
  - Publish a Docker image for core components.
- Telemetry
  - Refactor flow enricher
  - Add metrics to flow enricher
  - Add serviceability data fetching to flow enricher
  - Add flow-ingest service
  - Add annotation of flow records with serviceability data
  - Add pcap input and json ouput to flow enricher
  - Initial state-ingest service with client SDK
  - Collect BGP socket state from devices
- CI
  - Cancel existing e2e test runs on the push of new commits
- RFCs
  - RFC - Network Provisioning
  - RFC-11: Onchain Activation ([#2302](https://github.com/malbeclabs/doublezero/pull/2302))
- Monitor
  - Add link status to device-telemetry metrics to enable Grafana alerts to filter out links that are not in activated status
  - Add validation for 2Z oracle swapRate to ensure it is an unsigned integer, with warning logs and metrics for malformed values
- E2E tests
  - Add GetLatency call to qaagent
  - The QA alldevices test now considers device location and connects hosts to nearby devices
  - QA agent and tests now support doublezero connect ibrl's --allocate-addr flag
  - The QA alldevices test now publishes success/failure metrics to InfluxDB in support of rfc12
- Onchain programs
  - Fix CreateMulticastGroup to use incremented globalstate.account_index for PDA derivation instead of client-provided index, to ensure the contract is the authoritative source for account indices
  - ReactivateMulticastGroup now enforces that the multicast group status must be Suspended before reactivation, returning InvalidStatus otherwise; negative-path regression tests were added.

## [v0.8.0](https://github.com/malbeclabs/doublezero/compare/client/v0.7.1...client/v0.8.0) – 2025-12-02

### Breaking

- None for this release

### Changes

- RFCs
  - RFC-10: Version Compatibility Windows
- CLI
  - IP address lookups via ifconfig.me are retried up to 3 times to minimize transient network errors.
  - Added global `--no-version-warning` flag to the `doublezero` client and now emit version warnings to STDERR instead of STDOUT to improve scriptability and logging.
  - Add the ability to update a Device’s location, managing the reference counters accordingly.
  - Added support in the link update command to set a link’s status to soft_drained or hard_drained.
  - Added support for specifying `device_type` at creation, updating it via device update, and displaying it in list/detail outputs.
  - Add support for updating `contributor.ops_manager_key`.
  - Add migrate command to upgrade legacy user accounts from index-based PDAs to the new IP + connection-type scheme.
  - Enhance `access-pass list` with client-IP and user-payer filters
  - Support added to load keypair from stdin
- Client
  - Add route liveness fault-injection simulation tests.
  - Updated the `interface list` command to display all interfaces when no device is specified.
- Funder
  - Fund multicast group owners
- Onchain programs
  - Serviceability Program: Updated the device update command to allow modifying a device’s location.
  - Added new `soft-drained` and `hard-drained` link status values to serviceability to support traffic offloading as defined in RFC9.
  - Fix ProgramConfig resize during global state initialization.
  - Standardized the `device_type` enum to `Edge`, `Transit`, and `Hybrid`, added validation rules, and defaulted existing devices to `Hybrid` for backward compatibility.
  - Add `contributor.ops_manager_key` for authorizing incident management operations.
  - User Account: Replace global-index PDA generation with deterministic IP + connection-type seeds, eliminating user-creation race conditions.
  - Enable on-chain storage of InterfaceV2, allowing devices to register updated interface metadata
  - Serviceability: validate that a device's public IP doesn't clash with its dz_prefixes
- QA
  - Traceroute when packet loss is detected
- Tools
  - Add `solana-tpu-quic-ping` tool for testing Solana TPU-QUIC connections with stats emitted periodically
- Device controller
  - Handle new link.status values (soft-drained and hard-drained) as per [RFC9](https://github.com/malbeclabs/doublezero/blob/main/rfcs/rfc9-link-draining.md)
- Monitor
  - Export links data to InfluxDB
- Activator
  - Uses asynchronous coroutines instead of blocking operations and threads.

## [v0.7.1](https://github.com/malbeclabs/doublezero/compare/client/v0.7.0...client/v0.7.1) – 2025-11-18

### Breaking

- None for this release

### Changes

- RFCs
  - RFC9 Link Draining
- Client
  - Switch to 64 byte latency probes instead of 32 bytes
  - Route liveness admin-down signalling and ignore stale remote-down messages
  - Added an on-chain minimum supported CLI version to allow multiple CLI versions to operate simultaneously.
- Smart contract
  - Delay V2 Interface Activation Until All Clients Support V2 Reading
- Device controller
  - Now accepts the config agent's version in the grpc GetConfig call and includes it as a label in the controller_grpc_getconfig_requests_total metric
- Device Agent
  - Now sends its version to the controller in the grpc GetConfig call

## [v0.7.0](https://github.com/malbeclabs/doublezero/compare/client/v0.6.11...client/v0.7.0) – 2025-11-14

### Breaking

- Smart contract
  - Introduces CYOA and DIA as new possible interface types

### Changes

- Onchain programs
  - Check if `accesspass.owner` is equal to system program ([malbeclabs/doublezero#2088](https://github.com/malbeclabs/doublezero/pull/2088))
- CLI
  - Added support for specifying the interface type during interface creation and modification, introducing CYOA and DIA as new possible interface types.
  - Improve error message when connecting to a device that is at capacity or has max_users=0. Users now receive "Device is not accepting more users (at capacity or max_users=0)" instead of the confusing "Device not found" error when explicitly specifying an ineligible device.
  - Add `link latency` command to display latency statistics from the telemetry program. Supports filtering by percentile (p50, p90, p95, p99, mean, min, max, stddev, all), querying by link code or all links, and filtering by epoch. Resolves: [#1942](https://github.com/malbeclabs/doublezero/issues/1942)
  - Added `--contributor | -c` filter to `device list`, `interface list`, and `link list` commands. (#1274)
  - Validate AccessPass before client connection ([#1356](https://github.com/malbeclabs/doublezero/issues/1356))
- Client
  - Add initial route liveness probing, initially disabled for rollout
  - Add route liveness prometheus metrics

## [v0.6.11](https://github.com/malbeclabs/doublezero/compare/client/v0.6.10...client/v0.6.11) – 2025-11-13

### Breaking

- None for this release

### Changes

- Note that the changes from this release have been bundled into 0.7.0

## [v0.6.10](https://github.com/malbeclabs/doublezero/compare/client/v0.6.9...client/v0.6.10) – 2025-11-05

### Breaking

- None for this release

### Changes

- CI
  - Add automated compatibility tests in CI to validate all actual testnet and mainnet state against the current codebase, ensuring backward compatibility across protocol versions.
  - Add `--delay-override-ms` option to `doublezero link update`
  - Add ability to configure excluded routes
- Device controller
  - Remove the deprecated -enable-interfaces-and-peers flag
  - Use link.delay_override to set isis metric when in valid range. This provides a simple workflow for contributors to temporarily change a link's delay value without overwriting the existing value.
- Onchain programs
  - serviceability: add delay_override_ns field to link

## [v0.6.9](https://github.com/malbeclabs/doublezero/compare/client/v0.6.7...client/v0.6.9) – 2025-10-24

### Breaking

- None for this release

### Changes

- Onchain programs
  - serviceability: add auto-assignment and validation for exchange.bgp_community
  - serviceability: prevent device interface name duplication
  - Update serviceability and telemetry program instruction args to use the `BorshDeserializeIncremental` derive macro incremental, backward-compatible, deserialization of structs.
  - Add explicit signer checks for payer accounts across various processors to improve security and ensure correct transaction authorization.
- CLI
  - Removed `--bgp-community` option from `doublezero exchange create` since these values are now assigned automatically
  - Add `--next-bgp-community` option to `doublezero global-config set` so authorized users can control which bgp_community will be assigned next
- Tools
  - TWAMP: Verify that the sequence number and timestamp of the received packet matches those of the sent packet
  - Uping: Add minimal ICMP echo library for user-space liveness probing over doublezero interfaces, even when certain routes are not in the the kernel routing table.
- Device controller
  - Deprecate the -enable-interfaces-and-peers flag. The controller now always renders interfaces and peers
  - Intra-exchange routing policy, which uses the onchain exchange.bgp_community value to route traffic between users in the local exchange over the internet
- Monitor
  - Add metrics that detect when duplicate or out-of-range exchange.bgp_community values exist in serviceability

## [v0.6.8](https://github.com/malbeclabs/doublezero/compare/client/v0.6.6...client/v0.6.8) – 2025-10-17

### Breaking

- Multicast group change: Regeneration of all multicast group allowlists required, as allowlists are now stored within each Access Pass instead of at the multicast group level.

### Changes

- Onchain programs
  - Serviceability: enforce that ActivateLink and CloseAccountLink instructions verify the provided side A/Z device accounts match the link's stored `side_a_pk` and `side_z_pk` before proceeding.
- CLI
  - Added a wait in the `disconnect` command to ensure the account is fully closed before returning, preventing failures during rapid disconnect/reconnect sequences.
  - Display multicast group memberships (publisher/subscriber) in AccessPass listings to improve visibility.
  - Allow AccessPass creation without 'client_ip'
  - Add 'allow_multiple_ip' argument to support AccessPass connections from multiple IPs
  - Include validator pubkey in `export` output
  - Rename exchange.loc_id to exchange.bgp_community
  - `status` command now shows connected and lowest latency DZD
- Activator
  - Reduce logging noise when processing snapshot events
  - Wrap main select handler in loop to avoid shutdown on branch error
- Onchain programs
  - Remove user-level allowlist management from CLI and admin interfaces; manage multicast group allowlists through AccessPass.
  - Add Validate trait for core types (AccessPass, Contributor, Interface, etc.) and enforce runtime checks before account operations.
  - Fix: resize AccessPass account before serialization to prevent errors; standardized use of resize_account_if_needed across processors.
  - Enable AccessPass with 'client_ip=0.0.0.0' to dynamically learn the user’s IP on first connection
  - Enable AccessPass to support connections from multiple IPs (allowlist compatibility)
  - Rename exchange.loc_id to exchange.bgp_community, and change it from u32 to u16
- Internet telemetry
  - Add circuit label to metrics; create a new metric for missing circuit samples
  - Create a new metric that tracks how long it takes collector tasks to run
  - Submit partitions of samples in parallel
  - Include circuit label on submitter error metric
- Monitor
  - Reduce logging noise in 2z oracle watcher
  - Include response body on 2z oracle errors
  - Collect contributors and exchanges into InfluxDB
- Device controller
  - When a device is missing required loopback interfaces, provide detailed errors to agent instead of "<pubkey> not found". Also, log these conditions as warnings instead of errors, and don't emit "unknown pubkey requested" error metrics for these conditions
  - Add device info as labels to `controller_grpc_getconfig_requests_total` metric
- Device agents
  - Submit device-link telemetry partitions in parallel
- CLI
  - Allow AccessPass creation without 'client_ip'
  - Add 'allow_multiple_ip' argument to support AccessPass connections from multiple IPs
  - Rename exchange.loc_id to exchange.bgp_community
- Onchain programs
  - Enable AccessPass with 'client_ip=0.0.0.0' to dynamically learn the user’s IP on first connection
  - Enable AccessPass to support connections from multiple IPs (allowlist compatibility)
  - Rename exchange.loc_id to exchange.bgp_community, and change it from u32 to u16
- Telemetry data API
  - Filter by contributor and link type
- SDK/Go
  - String serialization for exchanges status
  - Exclude empty tags from influx serialization

## [v0.6.6](https://github.com/malbeclabs/doublezero/compare/client/v0.6.5...client/v0.6.6) – 2025-09-26

### Breaking

- None for this release

### Changes

- Monitor
  - Update 2Z oracle to emit response code metrics on errors too
- Activator
  - A mitigation was added to handle situations where blockchain updates are missed by the Activator due to timeouts on the websocket. This mitigation processes pending accounts on a 1-minute timer interval.
- CLI
  - Connect command updated to provide better user experience with regard to activator websocket timeouts (see above).

## [v0.6.5](https://github.com/malbeclabs/doublezero/compare/client/v0.6.4...client/v0.6.5) – 2025-09-25

### Breaking

- None for this release

### Changes

- CLI
  - Connect now waits for doublezerod to get all latencies
  - Latency command sorts unreachable to bottom
- Device controller
  - Update device template to set default BGP timers and admin distance
  - Update device template so all "default interface TunnelXXX" commands for user tunnels come before any other user tunnel config
- Activator
  - Fix access pass check status accounts list
- Onchain programs
  - Implemented strict validation to ensure that only AccessPass accounts **owned by the program** and of the correct type can be closed.
  - Fix Access Pass set Instruction.
  - Switched to using `account_close` helper for closing accounts instead of resizing and serializing.
  - Make interface name comparison case insensitive
- Onchain monitor
  - Check for unlinked interfaces in a link
  - Emit user events
  - Add watcher for 2Z/SOL swap oracle

## [v0.6.4](https://github.com/malbeclabs/doublezero/compare/client/v0.6.3...client/v0.6.4) – 2025-09-10

### Breaking

- None for this release

### Changes

- Onchain programs
  - Fix bug preventing re-opening of AccessPass after closure
  - sc/svc: guard against empty account data
- Device controller
  - Support server dual listening on TLS and non-TLS ports
- Device and Internet Latency Telemetry
  - Create one ripeatlas measurement per exchange instead of per exchange pair to avoid concurrent measurement limit

## [v0.6.3](https://github.com/malbeclabs/doublezero/compare/client/v0.6.2...client/v0.6.3) – 2025-09-08

### Breaking

- None for this release

### Changes

- Onchain programs
  - Expand DoubleZeroError with granular variants (invalid IPs, ASN, MTU, VLAN, etc.) and derive PartialEq for easier testing.
  - Rename Config account type to GlobalConfig for clarity and consistency.
  - Fix bug in user update that caused DZ IP to be 0.0.0.0
  - Add more descriptive error logging
  - Telemetry program: embed serviceability program ID via build feature instead of env variable
- Activator
  - Support for interface IP reclamation
  - Devices are now initialized with max_users = 0 by default.
  - Devices with max_users = 0 cannot accept user connections until updated.
- Onchain monitor
  - Emit metric for telemetry account not found in device and internet telemetry watchers
  - Emit metric with serviceability program onchain version
  - Delete telemetry counter metrics if circuit was deleted
- Telemetry
  - Fix dashboard API to handle partitioned query with no samples
  - Add summary view with committed RTT and jitter, compared to measured values
- Device agents
  - Remove log of keypair path on telemetry agent start up
  - Drop device telemetry samples if submission attempts exhausted and buffer is at capacity
- Device controller
  - Each environment can now have a different device BGP Autonomous System Number (ASN) per environment. (This is the remote ASN from the client's perspective.)
  - Add flag for enabling pprof for runtime profiling
- E2E tests
  - Updated unit tests and e2e tests to validate the new initialization and activation flow.
- Contributor Operations
  - Contributors must explicitly run device update to set a valid max_users and activate a Device.

## [v0.6.2](https://github.com/malbeclabs/doublezero/compare/client/v0.6.0...client/v0.6.2) – 2025-09-02

### Breaking

- None for this release

### Changes

- Onchain programs
  - Fix: Serviceability now correctly enforces device.max_users
  - Fix: Restored the `validator_pubkey` field from AccessPass. This field had been removed in the previous version but is required by Sentinel.
  - Fix: Skip client version check in `status` command to prevent version errors during automated state checks.
  - New instructions were added to support device interface create/update/delete that prevents a race condition that could cause some updates to be lost when changes were made in quick succession.
  - Add deserialization vector with capacity + 1 to avoid `memory allocation failed, out of memory` error
- CLI
  - Added filtering options to `access-pass list` and `user list` CLI commands.
  - New filters include access pass type (`prepaid` or `solana-validator`) and Solana identity public key.
  - Updated command arguments and logic, with tests adjusted to cover new options.
  - Contributors: Interface creation no longer takes an "interface type (physical/loopback)" argument. The type is now inferred from the interface name.
- Device controller
  - Use serviceability onchain delay for link metrics

## [v0.6.0](https://github.com/malbeclabs/doublezero/compare/client/v0.5.3...client/v0.6.0) – 2025-08-28

### Breaking

- Onchain programs
  - Implement access pass management commands and global state authority updates
  - Update access pass PDA function to include payer parameter

### Changes

- Onchain programs
  - Introducing new link instruction processor acceptance criteria
  - Add support for custom deserializers and add for pubkey fields
  - Move serialization and network_v4 to program-common
  - Refactor account type assertions in processors and state modules in serviceability program
  - Add validator identity to `SolanaValidator` type AccessPass.
- User client
  - Add access pass management commands to CLI
  - Restructuring device and global config CLI commands for better authority and interface management
  - Enhance the handling and display of access pass epoch information in the CLI
  - Configure CLI network settings with shorthand network code. Usage: `doublezero config set --env <testnet|mainnet-beta>`
  - Configure `doublezerod` network settings with shorthand network code. Usage `doublezerod --env <testnet|mainnet-beta>`
  - Add associated AccessPass to user commands.
- Activator
  - Introduce new user monitoring thread in activator for access pass functionality
  - Remove validator verification via gossip. This functionality is migrated to AccessPass.
- Device controller
  - Implement user tunnel ACLs in device agent configuration
  - Add "mpls icmp ttl-exceeded tunneling" config statement so intermediate hops in the doublezero network respond to traceroutes.
  - Set protocol timers for ibgp and isis to improve to speed up network re-convergence
  - Add TLS support to gRPC server
- Onchain monitor
  - Initial implementation and component release
  - Monitor onchain device telemetry metrics
  - Monitor onchain internet latency metrics
- E2E tests
  - Simplify fixtures with loop rollups
  - Add user ban workflow test
  - Deflake user reconnect race and device interface assigned IP race
  - Add single device stress test
  - Adjust user validation commands to use the new AccessPass column.
- CLI
  - Refactor: Updated `SetAccessPassCliCommand` (`doublezero access-pass set`) to use `--epochs` instead of `--last_access_epoch`, with sensible default values.
  - AccessPass now requires passing the validator identity for the `SolanaValidator` type.
- Device Agents
  - Periodically recreate telemetry agent sender instances in case of interface reconfiguration.
- Telemetry
  - Optimize onchain data dashboard API responses with field filtering
  - Optimize onchain data data CLI execution with parallel queries
  - Dashboard API support for circuit set partitioning using query parameters

## [v0.5.3](https://github.com/malbeclabs/doublezero/compare/client/v0.5.0...client/v0.5.3) – 2025-08-19

- **CLI & UX Improvements**
  - Improve sorting of device, exchange, link, location, and user displays
  - New installation package for the admin CLI for contributors based on controller/doublezero-admin
  - Do not allow users to connect to a device with zero available tunnel slots remaining
  - Improve handling of interface names for `doublezero device interface` commands
- **Serviceability Model Improvements**
  - funder: configure recipients as flag
  - sdk/rs: add record program handling
  - config: use ledger RPC LB endpoint
  - Validate account codes and replace whitespace
  - config: add ability to override DZ ledger RPC url; update URLs
  - Remove old CloseAccount instruction from both the smart contract and SDK client code
- **Network Controller Improvements**
  - Increase user tunnel slots per device from 64 to 128
  - Add flag controlling whether interfaces and peers are rendered to assist with testnet migration
- **Device and Internet Latency Telemetry**
  - Internet latency samples in data CLI and dashboard API
  - internet-latency-collector, telemetry data api/cli: collect internet latency between exchanges, not locations
  - internet-latency-collector: add ripeatlas credit metric
- **End-to-End Tooling**
  - New doublezero QA agent improves quality by thoroughly testing the software stack end-to-end in each doublezero environment (devnet, testnet, mainnet-beta) after each release.

## [v0.5.0](https://github.com/malbeclabs/doublezero/compare/client/v0.4.0...client/v0.5.0) – 2025-08-11

- **CLI & UX Improvements**
  - `doublezero connect` now waits for the user account to be visible onchain.
  - `doublezero device interface` commands. Interface names get normalized.
  - General improved consistency
  - Easy switching between devnet and testnet using the `--env` flag
- **Device Latency Telemetry**
  - Data CLI and API use epoch from ledger
  - Backpressure support to avoid continual buffer growth in error conditions
  - Link pubkey used for circuit uniqueness
- **Internet Latency Telemetry**
  - Adds the environement (devnet/testnet/mainnet-beta) to the ripeatlas measurement description.
  - Now funded by the funder
- **Serviceability Model Improvements**
  - Device extended to add DZD metadata (including Interfaces)
  - DZX Link types added (clearly distinguished from WAN links)
  - Removed foundation allowlist check, streamlining `link` workflow
  - Validate that `link.account_type` has type `AccountType::Link`
- **Network Controller Improvements**
  - doublezero-controller now manages more of the DZD configuration, including:
    - DNS servers
    - NTP servers
    - DZ WAN interfaces
    - Necessary loopback interfaces
    - BGP neighbor configuration
    - MSDP configuration
  - doublezero-activator now assigns IP addresses for use by the controller to give to DZD wan interfaces as well as loopbacks.

## [v0.4.0](https://github.com/malbeclabs/doublezero/compare/client/v0.3.0...client/v0.4.0) – 2025-08-04

This release adds contributor ownership, reference counting, and improved CLI outputs for devices and links. It introduces internet latency telemetry, with support for collection, Prometheus metrics, and writing samples to the ledger. Device telemetry now uses ledger epochs for network-wide consistency.

- **Serviceability Model Improvements**
  - Contributor creation includes an `owner` field; device/link registration enforces contributor consistency
  - Contributor field shown in CLI `list` and `get` commands for devices and links
  - `reference_count` added to contributors, devices, locations, and exchanges
  - New fields added to `Device` and `Link`, including an `interfaces` array for `Device`
  - Go SDK updated to support new DZD metadata account layouts
- **CLI & UX Improvements**
  - Provisioning (`connect`, `decommission`) UX improved: clearer feedback, better spinners, and more accurate status messages
  - `doublezero latency` output includes device code alongside pubkey
  - `doublezero device` and `doublezero link` commands updated to show new metadata fields
  - Added `doublezero device interface` subcommands for managing interfaces
  - `keygen` command now supports `--outfile` (`-o`) flag to generate keys directly to a file
- **Device Latency Telemetry**
  - Agent now uses ledger epoch instead of wallclock-based epoching
  - Account layout updated to move `epoch` after discriminator for efficient filtering
- **Internet Latency Telemetry**
  - Internet latency collectors write samples to the ledger using epoch-based partitioning
  - Telemetry program supports ingesting external control-plane latency samples
  - Prometheus metrics expose collector operation, failure rates, and credit balances
  - Go SDK support for initializing and submitting latency samples
- **End-to-End Tooling**
  - Multicast monitor utility added for provisioning validation
  - Multi-client e2e tests cover IBRL with and without IP allocation

## [v0.3.0](https://github.com/malbeclabs/doublezero/compare/client/v0.2.2...client/v0.3.0) - 2025-07-28

This release introduces network contributor registration, device interface management, and the initial telemetry system for link latency. Prometheus metrics were added to the activator and client for observability. Provisioning flows now enforce contributor presence and IP uniqueness per user.

- **Contributor Support**
  - Added CLI support for contributor management via `doublezero contributor`
  - Used to register network contributors in the DoubleZero system
- **Device Interface Management**
  - Added device interface CRUD commands for managing interfaces on a device
  - Interface metadata will be used by the controller to generate device configuration
- **Link Telemetry System**
  - Introduced TWAMP-based telemetry agent and onchain program for measuring RTT and packet loss between devices
  - Lays the foundation for performance-based rewards for bandwidth contributors
- **Prometheus Metrics**
  - Activator and client now export Prometheus metrics (build info, BGP session status)
- **Provisioning & Decommissioning**
  - Enforced one tunnel per user per IP address
  - Contributor field now required when creating devices and links
- **Activator**
  - Improved metrics and error handling
  - Added graceful shutdown and signal handling
- **Client**
  - Added `-json` output flag for `status` and `latency` commands
