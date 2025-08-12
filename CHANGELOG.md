# Changelog

All notable changes to this project will be documented in this file.

## [v0.5.0] (https://github.com/malbeclabs/doublezero/compare/client/v0.4.0...client/v0.5.0) – 2025-08-11

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
    - Adds the environement (devnet/testnet/mainnet) to the ripeatlas measurement description.
    - Now funded by the funder
- **Serviceability Model Improvements**
    - Device extended to add DZD metadata (including Interfaces)
    - DZX Link types added (clearly distinguished from WAN links)
- **Network Controller Improvements**
    - doublezero-controller now manages more of the DZD configuration, including:
        - DNS servers
        - NTP servers
        - DZ wan interfaces
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
