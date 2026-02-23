# Geo-Location Verification

## Summary

**Status: `Draft`**

This RFC introduces a geo-location verification system that validates the physical location of target devices using latency-based measurements through intermediate Probe servers. The system builds on DoubleZero's existing TWAMP telemetry infrastructure (RFC4) to provide cryptographically signed, onchain proof of approximate device location.

The system uses a three-tier measurement chain: DoubleZero Devices (DZDs) with precisely known locations measure latency to Probe servers, which measure latency to target devices. Each measurement is cryptographically signed and includes references to previous measurements, creating an auditable trail. Location is expressed as "z milliseconds away from latitude x, longitude y," enabling verification that devices are within specified geographic boundaries.

## Motivation

Users are interested in using DZDs as reference points to determine approximate location for targets for things such as ensuring GDPR compliance. This leverages the verifiable network of DZDs and contributors and is a reasonable way to monetize the network.

Problems with current IP location services:
- IP geolocation databases are unreliable (30-50% accuracy for city-level)
- No audit trail exists 
- Data and methodology is controlled by centralized organizations without transparency
- A GPS based system requires servers to have access to datacenter roofs

Customers need a trustless, verifiable system that:
- Provides cryptographic proof stored onchain
- Leverages existing infrastructure (DZDs with known locations)
- Scales to large numbers of targets

### Solution Approach

This RFC leverages DoubleZero's existing TWAMP telemetry infrastructure to create a latency-based triangulation system. The speed of light through fiber (approximately 124 miles/millisecond) provides a physical upper bound on distance based on round-trip latency. While actual network paths include switching delays, latency measurements provide reliable approximations suitable for uses such as regulatory geo-fencing (e.g., "plausibly within EU" = <24ms RTT (1,500 miles max) from reference points).

## New Terminology

### geoProbe
A server that acts as an intermediary for latency measurements. geoProbes:
- Are bare metal servers. Ideally located within 1ms of a DZD
- Run a UDP listener (default port 8923) accepting signed Location Offset messages from DZDs
- Pull configuration from the DZ Ledger and measure latency to target devices specified there
- Generate composite Offset messages including references to DZD measurements attesting to the probe's location
- Are registered onchain in the Telemetry Program

### Location Offset
A signed data structure containing a DZD's geographic location (latitude and longitude) and a chain of latency relationships between two entities (DZD↔Probe or Probe↔Target) and is sent to the Probe or Target. This is sent via UDP to the next link in the chain (From DZD->Probe and From Probe->Target). RttNs is the sum of the reference rtt plus measured rtt, and lat/lng are copied from the reference.

```go
type LocationOffset struct {
    Signature       [64]byte  // Ed25519 signature
    AuthorityPubkey [32]byte  // Signer's public key (metrics publisher key or probe signing key)
    SenderPubkey    [32]byte  // Device public key (DZD or Probe)
    MeasurementSlot uint64    // Current DoubleZero Slot
    Lat             float64   // Reference point latitude (WGS84)
    Lng             float64   // Reference point longitude (WGS84)
    MeasuredRttNs   uint64    // Measured RTT in nanoseconds, minimum
    RttNs           uint64    // RTT to target in ns, from lat/lng
    TargetIP        uint32    // IPv4 Address from TWAMP measurement **NEW**
    NumReferences   uint8     // Number of reference offsets in chain
    References      []Offset  // Reference offsets (empty for DZD→Probe)
}
```

**DZD-generated Offsets** contain no references (DZDs are roots of trust). <br> 
**Probe-generated Offsets** include references to DZD Offsets, enabling targets to verify the entire measurement chain.

> 💡 An enterprising user could use the existing link telemetry to confirm locations of DZDs relative to other DZDs. This is not covered by this RFC.

### Child Probe
A geoProbe assigned to a specific DZD for periodic latency measurement, defined onchain. DZDs only measure and send `LocationOffset` datagrams to their child geoProbes.

**Child Criteria:**
- Probe must be within latency threshold of DZD (e.g., <1ms RTT, 62 miles)
- Each doublezero Exchange will have at least 1 Probe. For the POC we only need a single Probe in testnet.
- Foundation authority controls children assignments. This allows balancing loads on DZDs

## Alternatives Considered

### Status Quo: Centralized Location Service (Rejected)
**Pros:** Simple implementation, flexible, Already exists
**Cons:** Single point of failure, requires trust, no cryptographic proof
**Decision:** Rejected by potential users

### Direct DZD↔Target Measurement (Rejected)
**Pros:** Simpler, lower latency, lower cost
**Cons:** Control plane traffic in DZDs would not scale to moderate numbers of targets.
**Decision:** Rejected in order to prevent resource consumption on the resource-constrained DZD.

### GPS-Based Verification (Rejected)
**Pros:** More precise, well-established
**Cons:** Not available in typical data centers.
**Decision:** Rejected due to high infrastructure build cost.

### Probe-Based Triangulation (SELECTED)
**Pros:** Leverages existing infrastructure, cryptographic proof, onchain auditability, scalable, privacy-preserving
**Cons:** Infrastructure cost (need probe servers), less precise than GPS, additional latency
**Decision:** Selected. Best balance of security, verifiability, and operational simplicity.

## Detailed Design

### Architecture Overview

Outbound Probing Flow
```
  ┌──────────┐                  ┌───────────┐                  ┌───────────┐
  │          │<─────Reply───────│           │<─────Reply───────│           │
  │   DZD    │──────TWAMP──────>│   Probe   │──────Probe──────>│  Target   │
  │          │──Signed Offset──>│           │──Signed Offset──>│           │
  └──────────┘                  └───────────┘  w/ references   └───────────┘
      ^ │                          ^  │                             │
Child │ │ Measured                 │  │                      Report │
IP    │ │ Offset        Target IPs │  │ Measured             Offset │
      │ V (future)    & DZD Pubkey │  │ Offset                      v
  ┌───────────┐                    │  │ (future)               ┌───────────┐
  │           │────────────────────┘  │                        │           │
  │    DZ     │<──────────────────────┘                        │  Client   │
  │  Ledger   │<──────Submit Target IPs to be Measured─────────│  Oracle   │
  │           │<─────────────Confirm Against Ledger────────────│           │
  └───────────┘                                                └───────────┘
```

Inbound Probing Flow
```
  ┌──────────┐                  ┌───────────┐                  ┌───────────┐
  │          │<─────Reply───────│           │<──Signed Probe───│           │
  │   DZD    │──────TWAMP──────>│   Probe   │───Signed Reply──>│  Target   │
  │          │──Signed Offset──>│           │                  │           │
  └──────────┘                  └───────────┘                  └───────────┘
      ^ │ Measured                 ^  │                             │
Child │ │ Offset to                │  │                     Reports │
IP    │ │ Probes    Target Pubkeys │  │ Measured            Latency │
      │ V            & DZD Pubkey  │  │ Offset                      v
  ┌───────────┐                    │  │ (future)               ┌───────────┐
  │           │────────────────────┘  │                        │           │
  │    DZ     │<──────────────────────┘                        │  Client   │
  │  Ledger   │<──Submit Target Pubkeys Allowed to Measure─────│  Oracle   │
  │           │<───────Get Probe Offset From Ledger────────────│           │
  └───────────┘                                                └───────────┘
```

**Data Flows:**

_Ongoing:_
- **Probe Discovery (60s interval):** DZD queries onchain Probe accounts to discover child probes
- **Target Discovery (30s interval):** Probe queries onchain to discover its targets

_Async:_
- Client Oracle submits Target IPs that should have locations verified.
- Client Oracle submits Target Pubkeys that Probe will reflect Signed Probe Messages from 

_Outbound Measurement Flow_
1. **DZD→Probe Measurement (10s interval):** DZD sends TWAMP probe, measures RTT
2. **Offset Generation:** DZD creates Offset with lat/lng, latency, timestamp, signs with Ed25519
3. **Dual Posting:** DZD submits samples to `ProbeLatencySamples` PDA onchain AND sends Offset to Probe via UDP
4. **Probe Caching:** Probe verifies DZD signature, caches Offset
5. **Probe→Target Measurement:** Probe measures RTT to target using TWAMP
6. **Composite Offset:** Probe creates new Offset with DZD Offset as reference, signs it
7. **Target Verification:** Target verifies signature chain, uses `lat/lng` + `rtt_ns` to determine location

_Inbound Measurement Flow_
1. **DZD→Probe Measurement (10s interval):** DZD sends TWAMP probe, measures RTT
2. **Offset Generation:** DZD creates Offset with lat/lng, latency, timestamp, signs with Ed25519
3. **Dual Posting:** DZD submits samples to `ProbeLatencySamples` PDA onchain
4. **Target Probes:** Target sends a signed probe message, containing its Pubkey as well as Ed25519 signature of the probe packet contents.
5. **Probe Might Reply:** Probe receives probe, and verifies that the Pubkey is registered. If so, it embeds the original probe message into its response, appends its Pubkey, an Ed25519 signature of the whole packet, and sends it.
6. **Target Verifies Reply:** Target verifies that the reply is for the message it sent, and has not been tampered with. Forwards to Client Oracle.
7. **Client Oracle Gets Probe Offset:** Client Oracle gets the probe's LocationOffset from the DZ ledger, and uses that along with the Target's reported latency to compute location.

### Smart Contract Changes

This change introduces a new **Geolocation Program** deployed separately from the Serviceability Program. It manages GeoProbe infrastructure and GeolocationUser accounts. The Geolocation Program references the Serviceability Program's `GlobalState` via CPI to verify foundation allowlist membership for administrative operations (probe management, billing). GeolocationUser accounts are self-service — any signer can create one without foundation approval.

> ⚠️ _MVP Constraint:_ Each exchange has 0 or 1 probe.
> ⚠️ _MVP Constraint:_ The LocationOffset chain is limited to 2 offsets. 
> ⚠️ _MVP Constraint:_ The TWAMP port is set to 862 on both GeoProbe and GeolocationTarget, and is not configurable in the smart contract.

#### Account Types

##### ProgramConfig

```rust
pub struct GeolocationProgramConfig {
    pub account_type: AccountType,              // AccountType::ProgramConfig
    pub bump_seed: u8,
    pub version: Version,
    pub min_compatible_version: Version,
    pub serviceability_program_id: Pubkey,      // For CPI to read foundation allowlist
}
```
**PDA Seeds:** `["doublezero", "programconfig"]`

##### GeoProbe

```rust
pub struct GeoProbe {
    pub account_type: AccountType,             // AccountType::GeoProbe
    pub owner: Pubkey,                         // Resource provider
    pub bump_seed: u8,
    pub exchange_pk: Pubkey,                   // Reference to Serviceability Exchange account
    pub public_ip: Ipv4Addr,                   // Where probe listens
    pub location_offset_port: u16,             // UDP listen port (default 8923)
    pub metrics_publisher_pk: Pubkey,          // Signing key for telemetry
    pub reference_count: u32,                  // GeolocationTargets referencing this probe
    // Variable-length fields must be at the end for Borsh deserialization
    pub code: String,                          // e.g., "ams-probe-01" (max 32 bytes)
    pub parent_devices: Vec<Pubkey>,           // DZDs that measure this probe
}
```
**PDA Seeds:** `["doublezero", "probe", code.as_bytes()]`

##### GeolocationUser

```rust
pub enum GeolocationUserStatus {
    Activated = 0,
    Suspended = 1,  // Lack of Payment
}
pub enum GeoLocationTargetType {
    Outbound = 0,   // GeoProbe sends TWAMP
    Inbound = 1,    // Target Sends SignedTWAMP
}

pub struct GeolocationTarget {
    pub target_type: GeoLocationTargetType,
    pub ip_address: Ipv4Addr,       // Only for Outbound
    pub location_offset_port: u16,  // Only for Outbound
    pub target_pk: Pubkey,          // Only for Inbound
    pub geoprobe_pk: Pubkey,        // Which probe measures this target
}

pub enum GeolocationPaymentStatus {
    Delinquent = 0,
    Paid = 1,
}

pub enum GeolocationBillingConfig {
    FlatPerEpoch(FlatPerEpochConfig),
}

pub struct GeolocationUser {
    pub account_type: AccountType,              // AccountType::GeolocationUser
    pub owner: Pubkey,                          // User who created this account (signer)
    pub bump_seed: u8,
    pub code: String,                          // Unique identifier for PDA (max 32 bytes)
    pub token_account: Pubkey,                 // 2Z token account for billing
    pub payment_status: GeolocationPaymentStatus,
    pub billing: GeolocationBillingConfig,
    pub status: GeolocationUserStatus,
    pub targets: Vec<GeolocationTarget>,
}
```
**PDA Seeds:** `["doublezero", "geouser", code.as_bytes()]`

#### Instructions
> ⚠️ _Note:_ Program instructions are shown here defined inline for readability, but in the implementation each instruction will take a single struct containing all the arguments.

```rust
pub enum GeolocationInstruction {
    // --- Program Management ---
    InitProgramConfig {                             // variant 0
        serviceability_program_id: Pubkey,
    },

    // --- GeoProbe Management (foundation-gated via Serviceability CPI) ---
    CreateGeoProbe {                                // variant 1
        code: String,
        public_ip: Ipv4Addr,
        location_offset_port: u16,
        metrics_publisher_pk: Pubkey,
    },
    UpdateGeoProbe {                                // variant 2
        public_ip: Option<Ipv4Addr>,
        location_offset_port: Option<u16>,
        metrics_publisher_pk: Option<Pubkey>,
    },
    DeleteGeoProbe {},                              // variant 3
    AddParentDevice { device_pk: Pubkey },          // variant 4
    RemoveParentDevice { device_pk: Pubkey },       // variant 5

    // --- GeolocationUser Management (self-service) ---
    CreateGeolocationUser {                         // variant 6
        code: String,
        token_account: Pubkey,
    },
    UpdateGeolocationUser {                         // variant 7
        token_account: Option<Pubkey>,
    },
    DeleteGeolocationUser {},                       // variant 8
    AddTarget {                                     // variant 9
        ip_address: Ipv4Addr,
        location_offset_port: u16,
        exchange_pk: Pubkey,                        // Program looks up the probe at this exchange
    },
    RemoveTarget {                                  // variant 10
        target_ip: Ipv4Addr,
        exchange_pk: Pubkey,
    },

    // --- Billing (foundation-gated via Serviceability CPI) ---
    UpdatePaymentStatus {                           // variant 11
        payment_status: u8,                         // GeolocationPaymentStatus as u8 for wire format consistency
        last_deduction_dz_epoch: Option<u64>,
    },

    // --- Program Config Update ---
    UpdateProgramConfig {                           // variant 12
        serviceability_program_id: Option<Pubkey>,
    },
}
```

#### Access Control

| Instruction                                     | Required Authority |
|-------------------------------------------------|-------------------------------------------------|
| InitProgramConfig                               | Program deploy authority (one-time)             |
| CreateGeoProbe, UpdateGeoProbe, DeleteGeoProbe  | Foundation allowlist (via Serviceability CPI)   |
| AddParentDevice, RemoveParentDevice             | Foundation allowlist (via Serviceability CPI)   |
| CreateGeolocationUser, UpdateGeolocationUser,   | Any signer (self-service, signer becomes owner) |
| DeleteGeolocationUser                                                                            
| AddTarget, RemoveTarget                         | GeolocationUser owner                           |
| UpdatePaymentStatus                             | Foundation allowlist (via Serviceability CPI)   |

#### GeoProbe Onboarding

1. Foundation calls `CreateGeoProbe` with the probe's code, IP address, location offset port, exchange, and signing key. The probe's `exchange_pk` must reference an existing Serviceability Exchange account.
2. Foundation calls `AddParentDevice` for each DZD that should measure this probe. Each `device_pk` must reference an activated Device in the Serviceability Program.
4. DZDs poll onchain GeoProbe accounts (60s interval) to discover child probes. When a new activated probe appears with the DZD in its `parent_devices`, the DZD begins TWAMP measurements and Offset generation.

#### GeolocationUser Onboarding

1. User calls `CreateGeolocationUser` with a unique code and their 2Z token account. Status is set to `Activated` (no approval step). Payment status starts as `Delinquent` until the foundation or a sentinel confirms funding via `UpdatePaymentStatus`.
2. User calls `AddTarget` for each target to be measured, specifying the target's IP address, location offset port, and which exchange should perform the measurement. The cli will figure out the probe_pk associated with that exchange. The target's `probe_pk` must reference an activated GeoProbe, which increments that probe's `reference_count`.
3. Probes poll onchain GeolocationUser accounts to discover targets assigned to them. Only targets from users with `payment_status: Paid` are measured.
4. To stop measurement, user calls `RemoveTarget` (decrements probe `reference_count`) or `DeleteGeolocationUser` (requires all targets removed first).

### Signed Probes

Signed probes support the **inbound probing flow**, where the Target initiates latency measurement toward the Probe.

The mechanism extends DoubleZero's existing TWAMP-light implementation (`tools/twamp/pkg/light/`) with new signed variants. New `SignedSender` and `SignedReflector` types are added to the package, reusing the NTP timestamp encoding, kernel timestamping, and socket infrastructure.

#### Packet Formats

**SignedProbePacket (108 bytes)** — sent from Target to Probe:

```go
type SignedProbePacket struct {
    Seq          uint32    // Bytes 0-3: Sequence number (big-endian)
    Sec          uint32    // Bytes 4-7: NTP timestamp seconds
    Frac         uint32    // Bytes 8-11: NTP timestamp fractional
    SenderPubkey [32]byte  // Bytes 12-43: Target's Ed25519 public key
    Signature    [64]byte  // Bytes 44-107: Ed25519 signature over bytes 0-43
}
```

The signature covers `[Seq, Sec, Frac, SenderPubkey]` (bytes 0–43)

**SignedReplyPacket (204 bytes)** — sent from Probe to Target:

```go
type SignedReplyPacket struct {
    Probe           SignedProbePacket  // Bytes 0-107: Complete original signed probe (echoed)
    ReflectorPubkey [32]byte          // Bytes 108-139: Probe's Ed25519 Authority public key
    Signature       [64]byte          // Bytes 140-203: Ed25519 signature over bytes 0-139
}
```

The probe's signature covers `[Probe, ReflectorPubkey]` (bytes 0–139)

#### Interfaces

```go
// SignedSender is used by the Target to initiate inbound probing.
type SignedSender interface {
    Probe(ctx context.Context) (time.Duration, *SignedReplyPacket, error)
    Close() error
    LocalAddr() *net.UDPAddr
}

// SignedReflector is used by the Probe to respond to inbound probes.
type SignedReflector interface {
    Run(ctx context.Context) error
    Close() error
    LocalAddr() *net.UDPAddr
}
```

`SignedSender.Probe()` returns both the measured RTT and the `SignedReplyPacket`.

### Component Implementation

#### Telemetry Agent Extensions

**Configuration (`controlplane/telemetry/internal/telemetry/config.go`):**

```go
type Config struct {
    // Existing fields...
    ProbeEnabled                  bool          // Default: false
    ProbeInterval                 time.Duration // Default: 5s
    ProbeTimeout                  time.Duration // Default: 2s
    ProbePacketSize               int           // Default: 2048 bytes
    ProbeLocationOffsetPort       uint16        // Default: 8923
    ProbesRefreshInterval         time.Duration // Default: 10s
}
```

**Modules:**
`controlplane/telemetry/internal/geoprobe` (new directory) contains:
- `discovery.go`: Discovers child probes from onchain
- `pinger.go`: TWAMP measurements to probes
- `offset_generator.go`: Creates signed Offsets
- `publisher.go`: Sends Offset over UDP

#### geoProbe Server

New service deployed alongside DZDs in exchanges on separate bare metal servers.

**File:** 
`controlplane/telemetry/cmd/geo-probe-agent/` (new directory) contains:
`main.go` describing the new `doublezero-geoprobe-agent`
Reuses the new modules for the telemetry agent in `controlplane/telemetry/internal/geoprobe`

**Components:**
- **UDP Listener:** Accepts DZD Offsets
- **Offset Cache:** Stores recent DZD Offsets (keyed by DZD pubkey)
- **Target Handler:** Measures RTT to targets, generates composite Offsets
- **Signature Verifier:** Validates Ed25519 signatures
- **Signed TWAMP Reflector:** Responds to Probes from allowed Targets

**Language:** Go (for consistency with other infrastructure)

**Configuration:**
```yaml
listen_addr: "0.0.0.0:8923"
probe_pubkey_file: "/etc/probe/keypair.json"
max_offset_age_seconds: 60
cache_size: 10000
```

#### Example Target Software TWAMP Reflector + UDP Listener (Test Tool)

**Purpose:** Lightweight test tool for POC to receive and log signed LocationOffset messages from probes. Demonstrates end-to-end flow of geolocation verification.

**File:** `controlplane/telemetry/cmd/geoprobe-target/` (new directory)

**Language:** Go (consistent with existing controlplane telemetry code)

**Components:**
- **TWAMP Reflector:** Responds to probe RTT measurements
- **UDP Listener:** Receives signed LocationOffset messages on port 8923
- **Signature Verification:** Validates Ed25519 signatures from probes
- **Chain Verification:** Validates DZD→Probe reference signatures
- **Logging:** Outputs offset contents, RTT measurements, location interpretation, and signature/chain details
- **SDK:** Built on a new GeoLocation Go SDK (see smartcontract/sdk/go for examples)

**Configuration:**
```yaml
listen_addr: "0.0.0.0:8923"
twamp_listen_addr: "0.0.0.0:862"
log_format: "json"  # or "text"
verify_signatures: true
max_offset_age_seconds: 300
```

**Example Output:**
```
[2025-01-15 14:23:45] Received LocationOffset from Probe
  Probe: ams-probe-01 (FProbe123...xyz)
  Reference Point: 52.3676°N, 4.9041°E (Amsterdam)
  RTT to Target: 12.5ms
  Max Distance: 775 miles (1,247 km)

  DZD Reference Chain:
    DZD: fra-tn-bm1 (FDZD456...abc)
    DZD→Probe RTT: 0.8ms
    DZD Location: 50.1109°N, 8.6821°E (Frankfurt)

  Signature: VALID ✓
  Chain Verification: VALID ✓
```

**Usage:**
```bash
# Run target listener on default ports
./geoprobe-target --config config.yaml

# Run with custom ports
./geoprobe-target --udp-port 9000 --twamp-port 863
```

#### Example Target Software for Target Probe Senders

**Purpose:** Example code that sends signed TWAMP probes, and verifies received replies are valid.

**File:** `controlplane/telemetry/cmd/geoprobe-target-sender/` (new directory)

**Language:** Go (consistent with existing controlplane telemetry code)

**Components:**
- **Signed TWAMP Sender:** Sends Signed probes
- **Response Verification:** Validates Ed25519 signatures from probes
- **Logging:** Outputs RTT measurements, and signature details
- **SDK:** Built on a new GeoLocation Go SDK (see smartcontract/sdk/go for examples)

**Example Output:**
```
[2025-01-15 14:23:45] Received TWAMP Reply
  Probe: ams-probe-01 (FProbe123...xyz)
  RTT to Target: 12.5ms
  My Signature:    VALID ✓
  Probe Signature: VALID ✓
```

**Usage:**
```bash
./geoprobe-target-sender --probe_ip 111.222.333.444 --probe_pk FSM7vubNpDT7Z6ZwGLfbcAuqEciF8iDR6c4i8WRK6zmQ --keypair /path/to/keypair.json
```

#### CLI For DZ Ledger Management
PoC CLI will be a separate `doublezero-geolocation` CLI, future work will integrate it into `doublezero` CLI as `doublezero geolocation`

- New CLI module `smartcontract/cli/src/geolocation/probe/`
- Commands for probe management:
    - `doublezero-geolocation probe create` — creates a GeoProbe account (args: code, exchange, public-ip, port, metrics-publisher-pk)
    - `doublezero-geolocation probe update` — updates GeoProbe fields (public-ip, port, metrics-publisher-pk)
    - `doublezero-geolocation probe delete` — deletes a GeoProbe account
    - `doublezero-geolocation probe list` — lists all GeoProbe accounts
    - `doublezero-geolocation probe get` — gets a specific GeoProbe by code
    - `doublezero-geolocation probe add-parent` — adds a parent DZD to a GeoProbe
    - `doublezero-geolocation probe remove-parent` — removes a parent DZD from a GeoProbe
    - `doublezero-geolocation init-config` — initializes the GeolocationProgramConfig (one-time)
- Commands for user management: 
    - `doublezero-geolocation user create` - Creates a GeoLocation User (args: Code, token_account)
    - `doublezero-geolocation user list` - lists all GeoLocation users
    - `doublezero-geolocation user delete` - Deletes a GeoLocation User (must be that user or foundation)
    - `doublezero-geolocation add-target` - Adds targets to be covered by a user (Args: code, ip or pubkey)
    - `doublezero-geolocation remove-target` - removes a target from a user  (Args: code, ip or pubkey)
    - `doublezero-geolocation probe get` - Gets offset from a probe (args: Probe Code(s), optional: epoch)


### POC Requirements

#### Phase 1:
**Goal:** Single geoProbe deployment for testing
1. Telemetry agent extensions:
   - Command Line Argument for "Additional Child Probes"
        - Allows testing without onchain work
   - Extend TWAMP measurement to geoProbes
   - Generate Offset structure and sign
   - Post Offset Structure via UDP to Probe
2. geoProbe server (`doublezero-probe-agent`):
    - TWAMP Reflector
    - UDP listener for DZD Offsets
    - Command Line Argument for "Additional Parents" and "Additional Targets"
        - Allows testing without onchain work
    - Offset caching and verification
    - Measure IP RTT to targets Via TWAMP
    - Composite Offset generation
    - Post Composite offset via UDP to Target
    - Includes local logging with `-verbose` flag.
3. Example Target Software TWAMP Reflector + UDP Listener:
    - TWAMP Reflector
    - Receives Signed UDP Datagrams
    - Logs data
4. Deployment:
    - E2E test in Dockernet using command line flags
    - Manual Deployment of Snapshot Telemetry Agent `fra-dz001` in Testnet
    - Manual Deployment of Snapshot geoProbe Agent to `fra-tn-bm1` in Testnet
    - Target Deployment of Snapshot Example Target to `fra-tn-qa01` in Testnet

#### Phase 1.5:
**Goal:** Add "inbound mode" where geoprobes respond to SignedTWAMP probes

1. Implement Signed TWAMP
    - Create new TWAMP Sender
    - Create new TWAMP Reflector
2. Add Signed TWAMP reflector to `doublezero-geoprobe-agent`
    - Separate from standard TWAMP reflector for DZDs
    - Add commandline to allowlist additional pubkeys for SignedTWAMP
3. Create Example Target Software (Signed TWAMP Sender)
    - Uses CLI to specify probe to ping

#### Phase 2:
**Goal:** Pull configuration from onchain data.
1. Serviceability program changes:
    - GeolocationUser account
    - 4 new instructions (InitializeGeolocationUser/AddTargetIp/RemoveTargetIp/DeleteGeolocationUser)
    - geoProbe account
    - 4 new instructions (InitializeProbe/UpdateProbe/AddParent/RemoveParent)
2. Telemetry agent extensions:
    - Child geoProbe discovery from onchain
3. geoProbe server (`doublezero-probe-agent`):
    - Parent DZD Discovery from onchain
    - GeolocationUser and GeolocationTarget discovery from onchain
4. CLI tool (doublezero-geolocation):
   - User management commands
   - Target management commands
   - Only needs to see exchanges, locations, devices, and probes
   - Set up payer account
   - Probe Management Commands `doublezero probe list/create/update`
6. Deployment:
   - Updated E2E tests to use onchain configuration data.
   - Test in DockerNet and DevNet

### MVP Requirements

**Goal:** Production-ready system

1. All POC components (above)
2. Deploy Serviceability upgrades
2. Probe infrastructure:
   - Deploy ~1 probe per DoubleZero exchange
   - Automated probe provisioning
3. Monitoring:
   - Lake pages for probe health
   - Alerting for offline probes, signature failures

### Testing Strategy

#### Unit Tests
- Offset signature generation/verification
- ProbeDiscovery onchain parsing
- TWAMP measurement accuracy
- PDA derivation correctness

#### Integration Tests
- Telemetry agent → Probe UDP communication
- Onchain sample submission and retrieval
- Client SDK → Probe verification flow
- Multi-probe consensus logic

#### E2E Tests (devnet/testnet)
- Full DZD → Probe → Target chain
- Signature verification across components
- Probe child/un-child operations

#### Performance Tests
- Probe server throughput (targets/second)

### Operational Considerations

#### Probe Deployment
Initially DZ or Malbec will deploy probes. Eventually this should become the domain of Resource Providers

#### Monitoring

**Metrics:**
- geoProbe availability (uptime %)
- DZD→Probe latency
- Per geoProbe # of targets
- Signature verification failures
- Offset cache hit rate

**Alerts:**
- geoProbe offline >5 minutes
- Signature verification failure rate >1%
- RTT to parent DZD exceeds threshold

#### Key Management
**DZD Keys:** Existing `metrics_publisher_pk` used for Offset signing (no new key infrastructure)
**geoProbe Keys:** Generated during provisioning, stored in `/etc/probe/keypair.json`, backed up to Foundation secure storage

## Impact
_TODO: add this_
*Consequences of adopting this RFC.*
Discuss effects on:

* Existing codebase (modules touched, refactors required)
* Operational complexity (deployment, monitoring, costs)
* Performance (throughput, latency, resource usage)
* User experience or documentation
  Quantify impacts where possible; note any expected ROI.

## Security Considerations

|       Threat            |             Mitigation                                            |
|-------------------------|-------------------------------------------------------------------|
| **Target IP Spoofing**  | Not addressed in POC/MVP; discussed in future work                |
| **Replay Attacks**      | Ongoing Probes are added to the ledger.                           |
| **Signature Forgery**   | Ed25519 signatures; DZD keys secured in telemetry agent           |
| **Probe Compromise**    | Target can use multiple probes; onchain audit trail               |
| **DDoS by Probes**      | Rate limiting (10-60s probes)                                     |
| **DDoS on Probes**      | Rate limiting, firewall rules                                     |
| **Target False Claims** | Targets cannot forge Offsets; signature verification required     |


## Future Work

### IP Spoofing Mitigation
**Problem:** Malicious probe could be close to DZD but forward requests to distant actual probe

### Geographic Multi-Probe Triangulation
**Problem:** Single probe gives distance, not precise location
- User sets up 3 probe targets with the same IP but different source geoProbes. DZ could provide an SDK performs trilateration from multiple distance measurements

### Store Measurements to DZ Ledger

**Rationale:** POC uses UDP-only delivery for simplicity and rapid validation. Production systems requiring auditability, historical queries, or analytics integration may benefit from onchain measurement storage.

#### ProbeLatencySamples Account (Onchain in Telemetry)

Stores DZD→Probe RTT measurements:

```rust
pub struct ProbeLatencySamplesHeader {
    pub account_type: AccountType,           // New AccountType::ProbeLatencySamples
    pub epoch: u64,
    pub origin_device_agent_pk: Pubkey,      // DZD agent
    pub origin_device_pk: Pubkey,            // DZD
    pub target_probe_pk: Pubkey,             // Probe
    pub sampling_interval_microseconds: u64, // e.g., 5_000_000 = 5s
    pub start_timestamp_microseconds: u64,
    pub next_sample_index: u32,
}

pub struct ProbeLatencySamples {
    pub header: ProbeLatencySamplesHeader,
    pub samples: Vec<u32>,                   // RTT in microseconds, max 35k samples
}
```

**PDA Seeds:** `["doublezero", "probe_latency_samples", origin_device_pk, target_probe_pk, epoch]`

#### GeolocationSamples Account (Onchain in Telemetry)

Stores Probe→Target IP RTT measurements:

```rust
pub struct GeolocationSamplesHeader {
    pub account_type: AccountType,           // New AccountType::GeolocationSamples
    pub epoch: u64,
    pub probe_pk: Pubkey,                    // Probe performing measurements
    pub target_ip: Ipv4Addr,                 // IP being geolocated
    pub geolocation_user_pk: Pubkey,         // Reference to GeolocationUser account
    pub sampling_interval_microseconds: u64, // e.g., 5_000_000 = 5s
    pub start_timestamp_microseconds: u64,
    pub next_sample_index: u32,
}

pub struct GeolocationSamples {
    pub header: GeolocationSamplesHeader,
    pub samples: Vec<u32>,                   // RTT in microseconds, max 35k samples
}
```

**PDA Seeds:** `["doublezero", "geolocation_samples", probe_pk, target_ip.octets(), epoch.to_le_bytes()]`

#### New Instructions

**Telemetry Program:**
- `InitializeProbeLatencySamples` - Initialize DZD→Probe sample account for epoch
- `WriteProbeLatencySamples` - Write DZD→Probe RTT samples
- `InitializeGeolocationSamples` - Initialize Probe→Target sample account for epoch
- `WriteGeolocationSamples` - Write Probe→Target RTT samples

### Cross-Program Reference Counting

**Problem:** GeoProbes reference serviceability Devices via `parent_devices` to establish authoritative basis for their location. If a parent device is deleted or suspended in the serviceability program, the GeoProbe retains a stale reference, invalidating its location attestation.

**Current Behavior:** Phase 1 implementation validates parent devices exist and are activated only at `AddParentDevice` instruction time. This creates a "soft reference" that is not enforced onchain. The telemetry agent polls onchain for GeoProbe updates and periodically updates a cached copy of its associated device's lat/lng values. If the device disappears in the next poll cycle, the telemetry agent stops any geolocation measurements and begins raising errors. The geoprobe agent also stops any geolocation measurements associated with the removed device.

**Proposed Solution: Cross-Program Reference Counting via CPI**

1. **Serviceability Program Changes:**
   - Add `IncrementDeviceReferenceCount` instruction (foundation-gated)
   - Add `DecrementDeviceReferenceCount` instruction (foundation-gated)
   - Existing `DeleteDevice` already checks `device.reference_count > 0`

2. **Geolocation Program Changes:**
   - Update `AddParentDevice` to CPI `IncrementDeviceReferenceCount` before adding to `parent_devices`
   - Update `RemoveParentDevice` to CPI `DecrementDeviceReferenceCount` after removing from `parent_devices`
   - Add rollback logic if CPI fails

**Implementation Example:**
```rust
// In geolocation/processors/geo_probe/add_parent_device.rs
pub fn process_add_parent_device(...) -> ProgramResult {
    // ... existing validation ...

    // CPI to increment device reference count
    let increment_ix = create_increment_device_ref_count_instruction(
        &program_config.serviceability_program_id,
        device_account.key,
        payer_account.key,
    );
    invoke(&increment_ix, &[device_account.clone(), payer_account.clone()])?;

    // Only add to parent_devices if CPI succeeded
    probe.parent_devices.push(args.device_pk);
    try_acc_write(&probe, probe_account, payer_account, accounts)?;

    Ok(())
}
```
**Dependencies:**
- Requires serviceability program upgrade to expose reference count management

<br> 

## Backward Compatibility

- No breaking changes to existing telemetry infrastructure
- Telemetry agent remains functional with `probe_enabled: false`
- New Probe accounts coexist with existing Device accounts

## Open Questions

1. Should probes support IPv6? (Initially IPv4 only for simplicity)
2. What's the optimal cache size for Offset storage? (Testing will determine, starting with 10k entries)
3. Should probe metrics be posted onchain? (Yes for auditability; separate from target verification path)
4. How to handle probe key rotation? (Manual for POC, automated in MVP)
5. In the architecture diagram, the Probe has a line to the Target that says "Probe". Should this be TWAMP (requires configuration on the target), or ICMP, or a TCP syn/syn-ack on a port known to listen publicly? Or support all three options? For POC we can start with TWAMP.
6. How should we handle the latency between the probe and device changing over time? Should we always use the most recent measurement? (use minimum of last n received)

