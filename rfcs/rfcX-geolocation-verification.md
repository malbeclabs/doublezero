# Geo-Location Verification

## Summary

**Status: `Draft`**

This RFC introduces a geo-location verification system that validates the physical location of client devices using latency-based measurements through intermediate Probe servers. The system builds on DoubleZero's existing TWAMP telemetry infrastructure (RFC4) to provide cryptographically signed, onchain proof of approximate device location.

The system uses a three-tier measurement chain: DoubleZero Devices (DZDs) with precisely known locations measure latency to Probe servers, which measure latency to client devices. Each measurement is cryptographically signed and includes references to previous measurements, creating an auditable trail. Location is expressed as "z milliseconds away from latitude x, longitude y," enabling verification that devices are within specified geographic boundaries.

## Motivation

Users are interested using DZDs as reference points to determine approximate location for clients for things such as ensuring GDPR compliance. This leverages the verifiable network of DZDs and contributors and is a reasonable way to monetize the network.

Problems with current IP location services:
- IP geolocation databases are unreliable (30-50% accuracy for city-level)
- No audit trail exists 
- Data and methodology is controlled by centralized organizations without transparency
- A GPS based system requires servers to have access to datacenter roofs

Customers need a trustless, verifiable system that:
- Provides cryptographic proof stored onchain
- Leverages existing infrastructure (DZDs with known locations)
- Scales to large numbers of clients

### Solution Approach

This RFC leverages DoubleZero's existing TWAMP telemetry infrastructure to create a latency-based triangulation system. The speed of light through fiber (approximately 124 miles/millisecond) provides a physical upper bound on distance based on round-trip latency. While actual network paths include switching delays, latency measurements provide reliable approximations suitable for uses such as regulatory geo-fencing (e.g., "plausibly within EU" = <24ms RTT (1,500 miles max) from reference points).

## New Terminology

### dzProbe
A server that acts as an intermediary for latency measurements. dzProbes:
- Are bare metal servers. Ideally located within 1ms of a DZD
- Run a UDP listener (default port 8923) accepting signed Offset messages from DZDs
- Pull configuration from the DZ Ledger and measure latency to client devices specified there
- Generate composite Offset messages including references to DZD measurements attesting to the probe's location
- Are registered onchain in the Telemetry Program

### Offset
A signed data structure representing the latency relationship between two entities (DZD↔Probe or Probe↔Client) and is sent to the Probe or client.

```rust
struct LocationOffset {
    signature: [u8; 64],          // Ed25519 signature
    pubkey: [u8; 32],             // Signer's public key (DZD or Probe)
    lat: f64,                     // Reference point latitude (WGS84)
    lon: f64,                     // Reference point longitude (WGS84)
    rtt_ns: u64,                  // Measured RTT in nanoseconds
    num_references: u8,           // Number of reference offsets in chain
    references: Vec<Offset>,      // Reference offsets (empty for DZD→Probe)
}
```

**DZD-generated Offsets** contain no references (DZDs are roots of trust). <br> 
**Probe-generated Offsets** include references to DZD Offsets, enabling clients to verify the entire measurement chain.

> 💡 An enterprising user could use the existing link telemetry to confirm locations of DZDs relative to other DZDs. This is not covered by this RFC.

### Location Offset
The RTT to a client from the lat/lon in the Offset struct.

### Child Probe
A Probe assigned to a specific DZD for periodic latency measurement, defined onchain. DZDs only measure and send Offsets to their mated Probes.

**Child Criteria:**
- Probe must be within latency threshold of DZD (e.g., <1ms RTT, 62 miles)
- Each doublezero Exchange will have at least 1 Probe. For the POC we only need a single Probe in testnet.
- Foundation authority controls children assignments

### Latency Budget
Maximum acceptable RTT between client and reference point for geo-verification. Determines geographic radius. Examples:
- **50ms:** ~3,000km radius ("within EU")
- **100ms:** ~6,000km radius ("within North America")
- **10ms:** ~600km radius (city/metro area)

## Alternatives Considered

### Satus Quo: Centralized Location Service (Rejected)
**Pros:** Simple implementation, flexible, Already exists
**Cons:** Single point of failure, requires trust, no cryptographic proof
**Decision:** Rejected by potential users

### Direct DZD↔Client Measurement (Rejected)
**Pros:** Simpler, lower latency, lower cost
**Cons:** Control plane traffic in DZDs would not scale to moderate numbers of clients.
**Decision:** Rejected in order to prevent resource consumption on the resource-constrained DZD.

### GPS-Based Verification (Rejected)
**Pros:** More precise, well-established
**Cons:** Not available in typical data centers.
**Decision:** Rejected due to high infrastructure build cost.

### Probe-Based Triangulation (SELECTED)
**Pros:** Leverages existing infrastructure, no client modifications, cryptographic proof, onchain auditability, scalable, privacy-preserving
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
IP    │ │ Offset        Client IPs │  │ Measured             Offset │
      │ V             & DZD Pubkey │  │ Offset                      v
  ┌───────────┐                    │  │                        ┌───────────┐
  │           │────────────────────┘  │                        │           │
  │    DZ     │<──────────────────────┘                        │  Client   │
  │  Ledger   │<──────Submit Client IPs to be Measured─────────│  Oracle   │
  │           │<─────────────Confirm Against Ledger────────────│           │
  └───────────┘                                                └───────────┘
```

**Data Flows:**
_Ongoing:_
- **Probe Discovery (60s interval):** DZD queries onchain Probe accounts to discover child probes
- **Client Discovery (30s interval):** Client sends UDP packet to Probe requesting verification

_Async:_
- Client Oracle submits Client IPs that should have locations verified.

_Measurment Flow_
1. **DZD→Probe Measurement (10s interval):** DZD sends TWAMP probe, measures RTT
2. **Offset Generation:** DZD creates Offset with lat/lon, latency, timestamp, signs with Ed25519
3. **Dual Posting:** DZD submits samples to `ProbeLatencySamples` PDA onchain AND sends Offset to Probe via UDP
4. **Probe Caching:** Probe verifies DZD signature, caches Offset
5. **Probe→Client Measurement:** Probe measures RTT to client using TWAMP
6. **Composite Offset:** Probe creates new Offset with DZD Offset as reference, signs it
8. **Client Verification:** Client verifies signature chain, uses `lat/lon` + `rtt_ns` to determine location 

### Data Structures

#### Probe Account (Onchain in Servicability)

```rust
pub struct Probe {
    pub account_type: AccountType,           // New AccountType::Probe
    pub owner: Pubkey,                       // Resource Provider
    pub index: u128,                         // Unique index for PDA
    pub bump_seed: u8,
    pub exchange_pk: Pubkey,                 // Reference to Exchange account
    pub public_ip: Ipv4Addr,                 // Where probe listens
    pub status: ProbeStatus,                 // Pending/Activated/Suspended/Deleting
    pub code: String,                        // e.g., "ams-probe-01"
    pub parent_devices: Vec<Pubkey>,         // DZDs to measure this probe
    pub metrics_publisher_pk: Pubkey,        // For telemetry submissions
    pub reference_count: u32,
}
```

**PDA Seeds:** `["doublezero", "probe", index.to_le_bytes()]`

#### ProbeLatencySamples Account (Onchain in Telemetry)

Mirrors `DeviceLatencySamples` structure from RFC4:

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

#### GeolocationUser Account (Onchain in Serviceability)

Represents a user who wants to geolocate target IP addresses using the DoubleZero network:

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

**Purpose:** Probes read GeolocationUser accounts to discover which target IPs need geolocation measurements. The exchange_pk determines which exchange's probes will perform the measurements.

#### GeolocationSamples Account (Onchain in Telemetry)

Stores RTT measurements from Probes to target client IPs. Mirrors the structure of ProbeLatencySamples:

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

**Purpose:** Stores the actual Probe→Target IP latency measurements that can be queried via the CLI to determine approximate geolocation.

### Smart Contract Changes

#### New Instructions (Serviceability Program)

```rust
pub enum ServiceabilityInstruction {
    // Existing instructions...

    InitializeGeolocationUser {
        index: u128,
        code: String,
        exchange_pk: Pubkey,
    },

    AddTargetIp {
        target_ip: Ipv4Addr,
    },

    RemoveTargetIp {
        target_ip: Ipv4Addr,
    },

    DeleteGeolocationUser,
}
```

#### New Instructions (Telemetry Program)

```rust
pub enum TelemetryInstruction {
    // Existing instructions...

    InitializeProbe {
        index: u128,
        public_ip: Ipv4Addr,
        port: u16,
        code: String,
        latency_threshold_ns: u64,
        metrics_publisher_pk: Pubkey,
    },

    UpdateProbe {
        public_ip: Option<Ipv4Addr>,
        port: Option<u16>,
        status: Option<ProbeStatus>,
        latency_threshold_ns: Option<u64>,
    },

    AddMatedDevice { device_pk: Pubkey },
    RemoveMatedDevice { device_pk: Pubkey },

    InitializeProbeLatencySamples {
        epoch: u64,
        sampling_interval_microseconds: u64,
    },

    WriteProbeLatencySamples {
        start_timestamp_microseconds: u64,
        samples: Vec<u32>,
    },

    InitializeGeolocationSamples {
        epoch: u64,
        sampling_interval_microseconds: u64,
    },

    WriteGeolocationSamples {
        start_timestamp_microseconds: u64,
        samples: Vec<u32>,
    },
}
```

### Component Implementation

#### Telemetry Agent Extensions

**Configuration (`controlplane/telemetry/internal/telemetry/config.go`):**

```go
type Config struct {
    // Existing fields...
    ProbeEnabled           bool          // Default: false
    ProbeInterval          time.Duration // Default: 5s
    ProbeTimeout           time.Duration // Default: 2s
    ProbePacketSize        int           // Default: 2048 bytes
    ProbeUDPPort           uint16        // Default: 8923
    ProbesRefreshInterval  time.Duration // Default: 10s
}
```

**Modules:**
- `probe_discovery.go`: Discovers mated probes from onchain (~200 lines)
- `probe_pinger.go`: TWAMP measurements to probes (~300 lines)
- `offset_generator.go`: Creates signed Offsets (~150 lines)
- `probe_publisher.go`: Submits samples onchain and UDP (~200 lines)

#### Probe Server

New service deployed alongside DZDs in exchanges.

**File:** `probe-server/` (new directory)

**Components:**
- **UDP Listener:** Accepts DZD Offsets and client requests
- **Offset Cache:** Stores recent DZD Offsets (keyed by DZD pubkey)
- **Client Handler:** Measures RTT to clients, generates composite Offsets
- **Signature Verifier:** Validates Ed25519 signatures

**Language:** Rust (for performance and consistency with other infrastructure)

**Configuration:**
```yaml
listen_addr: "0.0.0.0:8923"
probe_pubkey_file: "/etc/probe/keypair.json"
max_offset_age_seconds: 60
cache_size: 10000
```

#### Client SDK

**File:** `sdk/rs/src/geolocation/` (new module)

**Components:**
- `offset.rs`: Offset struct with signature verification
- `client.rs`: UDP client for requesting Offsets
- `verifier.rs`: Chain verification and latency validation

**Example Usage:**

```rust
use doublezero_sdk::geolocation::{GeoVerifier, LatencyBudget};

let verifier = GeoVerifier::new();
let probe_addr = "probe.ams.doublezero.network:8923";

// Request location proof
let offset = verifier.request_offset(probe_addr)?;

// Verify signature chain
offset.verify_chain()?;

// Check latency budget
let budget = LatencyBudget::eu_compliance(); // 50ms
if offset.total_latency_ns() <= budget.max_ns {
    println!("Client is within EU");
}
```

#### CLI Tool: doublezero-geolocation

**File:** `client/doublezero-geolocation/` (new binary)

**Purpose:** Command-line tool for creating geolocation users, managing target IPs, and querying measurement data from the ledger.

**Commands:**

```bash
# Create a geolocation user with a unique code
doublezero-geolocation user create --code my-super-fun-code

# Add a target IP to be geolocated by a specific exchange
doublezero-geolocation add-target my-super-fun-code xams 203.0.113.42

# Remove a target IP
doublezero-geolocation remove-target my-super-fun-code xams 203.0.113.42

# Get all geolocation measurements for a specific epoch
doublezero-geolocation get 12345

# Get measurements for a specific target IP in an epoch
doublezero-geolocation get 12345 203.0.113.42

# List all geolocation users
doublezero-geolocation user list

# Delete a geolocation user
doublezero-geolocation user delete --code my-super-fun-code
```

**Example Workflow:**

```bash
# 1. Create a geolocation user account
$ doublezero-geolocation user create --code gdpr-compliance

# 2. Add target IPs to geolocate (using Amsterdam exchange)
$ doublezero-geolocation add-target gdpr-compliance xams 203.0.113.10
$ doublezero-geolocation add-target gdpr-compliance xams 198.51.100.25

# 3. Wait for probes to collect measurements (happens automatically)

# 4. Query measurements for current epoch
$ doublezero-geolocation get 12350

# Output shows RTT measurements from probe to each target IP
# Target: 203.0.113.10, Probe: ams-probe-01, Samples: 45
# Avg RTT: 12.5ms, P50: 12.3ms, P95: 14.2ms, P99: 15.8ms
#
# Target: 198.51.100.25, Probe: ams-probe-01, Samples: 45
# Avg RTT: 28.7ms, P50: 28.1ms, P95: 31.4ms, P99: 33.2ms

# 5. Query specific target IP
$ doublezero-geolocation get 12350 203.0.113.10
# Shows detailed samples for just that IP
```

**Data Flow:**

1. User creates `GeolocationUser` account via CLI with chosen code and exchange
2. User adds target IPs via `add-target` (associates IPs with exchange)
3. Probes in the specified exchange query `GeolocationUser` accounts onchain
4. Probes measure RTT to target IPs and write to `GeolocationSamples` accounts
5. User queries `GeolocationSamples` accounts by epoch and/or target IP via CLI

## Impact

## Security Considerations

### Threat Model

|       Threat            |             Mitigation                                            |
|-------------------------|-------------------------------------------------------------------|
| **Client IP Spoofing**  | Not addressed in POC/MVP; discussed in future work                |
| **Replay Attacks**      | Ongoing Probes are added to the ledger.                           |
| **Signature Forgery**   | Ed25519 signatures; DZD keys secured in telemetry agent           |
| **Probe Compromise**    | Client can use mulitple probes; onchain audit trail               |
| **DDoS by Probes**      | Rate limiting (10-60s probes)                                     |
| **DDoS on Probes**      | Rate limiting, firewall rules                                     |
| **Client False Claims** | Clients cannot forge Offsets; signature verification required     |

### Privacy Considerations

**Location Precision:** Offsets reveal approximate distance only, not exact coordinates

## POC Requirements

**Goal:** Single probe deployment for testing

1. Serviceability program changes:
   - GeolocationUser account
   - 4 new instructions (InitializeGeolocationUser/AddTargetIp/RemoveTargetIp/DeleteGeolocationUser)
2. Telemetry program changes:
   - Probe and ProbeLatencySamples accounts
   - GeolocationSamples account
   - 8 new instructions (InitializeProbe/UpdateProbe/AddMated/RemoveMated/InitProbeLatencySamples/WriteProbeLatencySamples/InitGeolocationSamples/WriteGeolocationSamples)
3. Telemetry agent extensions:
   - Probe discovery from onchain
   - TWAMP measurement to dzProbes
   - Offset generation and signing
   - Dual posting (onchain + UDP)
3. Probe server (`doublezero-probe-agent`):
   - TWAMP Reflector
   - UDP listener for DZD Offsets
   - Offset caching and verification
   - GeolocationUser discovery from onchain
   - Target IP RTT measurement
   - Composite Offset generation
   - GeolocationSamples submission onchain
   - Post Composite offset to Target
4. Client TWAMP Reflector + UDP Listener:
   - TWAMP Reflector
   - Receives Signed UDP Datagrams
5. CLI tool (doublezero-geolocation):
   - User management commands
   - Target IP management commands
   - Measurement query commands
   - Set up payer account
   - Probe Management Commands `doublezero probe list/create/update`
6. Deployment:
   - DockerNet or DevNet
   - 1 probe in testnet in Frankfurt (use `fra-tn-bm1` as probe?)


## MVP Requirements

**Goal:** Production-ready system

1. All POC components (above)
2. Probe infrastructure:
   - Deploy ~1 probe per DoubleZero exchange
   - Automated probe provisioning
4. Monitoring:
   - Lake pages for probe health
   - Alerting for offline probes, signature failures
   - GeolocationSamples data ingestion to analytics system

## Implementation Phases

### Phase 1: POC (Austin Offsite)

### Phase 2: MVP (Following Week)

**Week 5-6: Multi-Probe & Infrastructure**
- Multi-probe verification logic
- Terraform for probe deployment
- Deploy probes to 5-10 exchanges

## Testing Strategy

### Unit Tests
- Offset signature generation/verification
- ProbeDiscovery onchain parsing
- TWAMP measurement accuracy
- PDA derivation correctness

### Integration Tests
- Telemetry agent → Probe UDP communication
- Onchain sample submission and retrieval
- Client SDK → Probe verification flow
- Multi-probe consensus logic

### E2E Tests (devnet/testnet)
- Full DZD → Probe → Client chain
- Signature verification across components
- Probe child/un-child operations

### Performance Tests
- Probe server throughput (clients/second)

## Operational Considerations

### Probe Deployment
Initially DZ or Malbec will deploy probes. Eventually this should become the domain of Resource Providers

### Monitoring

**Metrics:**
- dzProbe availability (uptime %)
- DZD→Probe latency
- Per dzProbe # of targets
- Signature verification failures
- Offset cache hit rate

**Alerts:**
- dzProbe offline >5 minutes
- Signature verification failure rate >1%
- RTT to parent DZD exceeds threshold

### Key Management

**DZD Keys:** Existing `metrics_publisher_pk` used for Offset signing (no new key infrastructure)
**dzProbe Keys:** Generated during provisioning, stored in `/etc/probe/keypair.json`, backed up to Foundation secure storage

## Future Work

### IP Spoofing Mitigation
**Problem:** Malicious probe could be close to DZD but forward requests to distant actual probe

### Geographic Multi-Probe Triangulation
**Problem:** Single probe gives distance, not precise location
- Client sets up 3 probe targets with the same IP but different source dzProbes. DZ could provide an SDK performs trilateration from multiple distance measurements

## Backward Compatibility

- No breaking changes to existing telemetry infrastructure
- Telemetry agent remains functional with `probe_enabled: false`
- New Probe accounts coexist with existing Device accounts

## Open Questions

1. Should probes support IPv6? (Initially IPv4 only for simplicity)
2. What's the optimal cache size for Offset storage? (Testing will determine, starting with 10k entries)
3. Should probe metrics be posted onchain? (Yes for auditability; separate from client verification path)
4. How to handle probe key rotation? (Manual for POC, automated in MVP)
5. In the architecture diagram, the Probe has a line to the Target that says "Probe". Should this be TWAMP (requires configuration on the target), or ICMP, or a TCP syn/syn-ack on a port known to listen publicly? Or support all three options? For POC we can start with ICMP.
6. How should we handle the latency between the probe and device changing over time? Should we always use the most recent measurement? Should we use the average since the last Signed Offset was sent? The avg/min/max of the previous epoch? 

