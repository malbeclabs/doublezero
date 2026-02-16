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
A signed data structure containing a DZD's geographic location (latitude and longitude) and a chain of latency relationship between two entities (DZD↔Probe or Probe↔Target) and is sent to the Probe or Target. This is sent via UDP to the next link in the chain (From DZD->Probe and From Probe->Target). RttNs is the sum of the reference rtt plus measured rtt, and lat/lng are copied from the reference.

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
    NumReferences   uint8     // Number of reference offsets in chain
    References      []Offset  // Reference offsets (empty for DZD→Probe)
}
```

**DZD-generated Offsets** contain no references (DZDs are roots of trust). <br> 
**Probe-generated Offsets** include references to DZD Offsets, enabling targets to verify the entire measurement chain.

> 💡 An enterprising user could use the existing link telemetry to confirm locations of DZDs relative to other DZDs. This is not covered by this RFC.

### Location Offset
The RTT to a target from the lat/lng in the Offset struct.

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

**Data Flows:**

_Ongoing:_
- **Probe Discovery (60s interval):** DZD queries onchain Probe accounts to discover child probes
- **Target Discovery (30s interval):** Probe queries onchain to discover its targets

_Async:_
- Client Oracle submits Target IPs that should have locations verified.

_Measurement Flow_
1. **DZD→Probe Measurement (10s interval):** DZD sends TWAMP probe, measures RTT
2. **Offset Generation:** DZD creates Offset with lat/lng, latency, timestamp, signs with Ed25519
3. **Dual Posting:** DZD submits samples to `ProbeLatencySamples` PDA onchain AND sends Offset to Probe via UDP
4. **Probe Caching:** Probe verifies DZD signature, caches Offset
5. **Probe→Target Measurement:** Probe measures RTT to target using TWAMP
6. **Composite Offset:** Probe creates new Offset with DZD Offset as reference, signs it
8. **Target Verification:** Target verifies signature chain, uses `lat/lng` + `rtt_ns` to determine location 

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
    pub code: String,                          // e.g., "ams-probe-01" (max 32 bytes)
    pub parent_devices: Vec<Pubkey>,           // DZDs that measure this probe
    pub metrics_publisher_pk: Pubkey,          // Signing key for telemetry
    pub reference_count: u32,                  // GeolocationTargets referencing this probe
}
```
**PDA Seeds:** `["doublezero", "probe", code.as_bytes()]`

##### GeolocationUser

```rust
pub enum GeolocationUserStatus {
    Activated = 0,
    Suspended = 1,  // Lack of Payment
}

pub struct GeolocationTarget {
    pub ip_address: Ipv4Addr,
    pub location_offset_port: u16,
    pub geoprobe_pk: Pubkey,                       // Which probe measures this target
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
        payment_status: GeolocationPaymentStatus,
        last_deduction_dz_epoch: Option<u64>,
    },
}
```

#### Access Control

| Instruction | Required Authority |
|---|---|
| InitProgramConfig | Program deploy authority (one-time) |
| CreateGeoProbe, UpdateGeoProbe, ActivateGeoProbe, SuspendGeoProbe, ResumeGeoProbe, DeleteGeoProbe | Foundation allowlist (via Serviceability CPI) |
| AddParentDevice, RemoveParentDevice | Foundation allowlist (via Serviceability CPI) |
| CreateGeolocationUser, UpdateGeolocationUser, DeleteGeolocationUser | Any signer (self-service, signer becomes owner) |
| AddTarget, RemoveTarget | GeolocationUser owner |
| UpdatePaymentStatus | Foundation allowlist (via Serviceability CPI) |

#### GeoProbe Onboarding

1. Foundation calls `CreateGeoProbe` with the probe's code, IP address, location offset port, exchange, and signing key. The probe's `exchange_pk` must reference an existing Serviceability Exchange account.
2. Foundation calls `AddParentDevice` for each DZD that should measure this probe. Each `device_pk` must reference an activated Device in the Serviceability Program.
4. DZDs poll onchain GeoProbe accounts (60s interval) to discover child probes. When a new activated probe appears with the DZD in its `parent_devices`, the DZD begins TWAMP measurements and Offset generation.

#### GeolocationUser Onboarding

1. User calls `CreateGeolocationUser` with a unique code and their 2Z token account. Status is set to `Activated` (no approval step). Payment status starts as `Delinquent` until the foundation or a sentinel confirms funding via `UpdatePaymentStatus`.
2. User calls `AddTarget` for each target to be measured, specifying the target's IP address, location offset port, and which exchange should perform the measurement. The cli will figure out the probe_pk associated with that exchange. The target's `probe_pk` must reference an activated GeoProbe, which increments that probe's `reference_count`.
3. Probes poll onchain GeolocationUser accounts to discover targets assigned to them. Only targets from users with `payment_status: Paid` are measured.
4. To stop measurement, user calls `RemoveTarget` (decrements probe `reference_count`) or `DeleteGeolocationUser` (requires all targets removed first).

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

#### GeolocationUser Account (Onchain in Serviceability)

Represents a user who wants to manage target IPs for geolocation measurement:

```rust
pub struct GeolocationUser {
    pub account_type: AccountType,           // New AccountType::GeolocationUser
    pub owner: Pubkey,                       // User who created this account
    pub index: u128,                         // Unique index for PDA
    pub bump_seed: u8,
    pub code: String,                        // User-chosen identifier (e.g., "my-super-fun-code")
    pub exchange_pk: Pubkey,                 // Exchange that will perform measurements
    pub target_ips: Vec<Ipv4Addr>,          // IP addresses to geolocate
    pub status: GeolocationUserStatus,       // Pending/Activated/Deleting
    pub reference_count: u32,
}
```

**PDA Seeds:** `["doublezero", "geolocation_user", code.as_bytes()]`

**Purpose:** Enables onchain configuration of which target IPs should be measured by which exchange's probes. Replaces hardcoded target IP lists.

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

**Serviceability Program:**
- `InitializeGeolocationUser` - Create geolocation user account
- `AddTargetIp` - Add target IP to user's measurement list
- `RemoveTargetIp` - Remove target IP
- `DeleteGeolocationUser` - Delete user account

**Telemetry Program:**
- `InitializeProbeLatencySamples` - Initialize DZD→Probe sample account for epoch
- `WriteProbeLatencySamples` - Write DZD→Probe RTT samples
- `InitializeGeolocationSamples` - Initialize Probe→Target sample account for epoch
- `WriteGeolocationSamples` - Write Probe→Target RTT samples

#### CLI Tool: doublezero-geolocation

Command-line tool for managing geolocation users and querying measurement data:

```bash
# User management
doublezero-geolocation user create --code gdpr-compliance --exchange xams
doublezero-geolocation user list
doublezero-geolocation user delete --code gdpr-compliance

# Target IP management
doublezero-geolocation add-target gdpr-compliance 203.0.113.42
doublezero-geolocation remove-target gdpr-compliance 203.0.113.42

# Query measurements
doublezero-geolocation get 12345                # All measurements for epoch
doublezero-geolocation get 12345 203.0.113.42  # Specific IP in epoch
```

**Benefits:**
- **Auditability:** Onchain records enable third-party verification of measurements
- **Historical Analysis:** Query past epochs for trend analysis
- **Analytics Integration:** Feed measurement data to Lake for insights
- **Dynamic Configuration:** Change target IPs without redeploying probe servers

## Backward Compatibility

- No breaking changes to existing telemetry infrastructure
- Telemetry agent remains functional with `probe_enabled: false`
- New Probe accounts coexist with existing Device accounts

## Open Questions

1. Should probes support IPv6? (Initially IPv4 only for simplicity)
2. What's the optimal cache size for Offset storage? (Testing will determine, starting with 10k entries)
3. Should probe metrics be posted onchain? (Yes for auditability; separate from target verification path)
4. How to handle probe key rotation? (Manual for POC, automated in MVP)
5. In the architecture diagram, the Probe has a line to the Target that says "Probe". Should this be TWAMP (requires configuration on the target), or ICMP, or a TCP syn/syn-ack on a port known to listen publicly? Or support all three options? For POC we can start with ICMP.
6. How should we handle the latency between the probe and device changing over time? Should we always use the most recent measurement? Should we use the average since the last Signed Offset was sent? The avg/min/max of the previous epoch? 

