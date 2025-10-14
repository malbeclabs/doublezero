# Changelog

All notable changes to this project will be documented in this file.

## Unreleased

### Breaking

- Multicast group change: Regeneration of all multicast group allowlists required, as allowlists are now stored within each Access Pass instead of at the multicast group level.

### Changes

- CLI
    - Added a wait in the `disconnect` command to ensure the account is fully closed before returning, preventing failures during rapid disconnect/reconnect sequences.
    - Display multicast group memberships (publisher/subscriber) in AccessPass listings to improve visibility.
- Activator
    - Reduce logging noise when processing snapshot events
    - Wrap main select handler in loop to avoid shutdown on branch error
- Onchain programs
    - Remove user-level allowlist management from CLI and admin interfaces; manage multicast group allowlists through AccessPass.
    - Add Validate trait for core types (AccessPass, Contributor, Interface, etc.) and enforce runtime checks before account operations.
- Internet telemetry
    - Add circuit label to metrics; create a new metric for missing circuit samples
    - Create a new metric that tracks how long it takes collector tasks to run
    - Submit partitions of samples in parallel
    - Include circuit label on submitter error metric
- Monitor
    - Reduce logging noise in 2z oracle watcher
    - Include response body on 2z oracle errors
- Device controller
    - When a device is missing required loopback interfaces, provide detailed errors to agent instead of "<pubkey> not found". Also, log these conditions as warnings instead of errors, and don't emit "unknown pubkey requested" error metrics for these conditions
    - Add device info as labels to `controller_grpc_getconfig_requests_total` metric
- Device agents
    - Submit device-link telemetry partitions in parallel
- CLI
    - Allow AccessPass creation without 'client_ip'
    - Add 'allow_multiple_ip' argument to support AccessPass connections from multiple IPs
- Onchain programs
    - Enable AccessPass with 'client_ip=0.0.0.0' to dynamically learn the user’s IP on first connection
    - Enable AccessPass to support connections from multiple IPs (allowlist compatibility)
- Telemetry data API
    - Filter by contributor and link type


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
    Support for interface IP reclamation
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
    * Validate that `link.account_type` has type `AccountType::Link`
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
