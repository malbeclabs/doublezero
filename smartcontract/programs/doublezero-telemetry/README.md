# Device Telemetry Program

A Solana program for collecting RTT latency measurements between devices over a link, using preallocated per-epoch accounts and append-only writes by authorized agents.

---

## Account Structure: `DeviceLatencySamples`

Stores metadata and RTT samples (in microseconds):

| Field | Type | Description |
| --- | --- | --- |
| `account_type` | `DeviceLatencySamples` enum | Type marker |
| `epoch` | `u64` | Collection epoch |
| `origin_device_agent_pk` | `Pubkey` | Authorized agent |
| `origin_device_pk` | `Pubkey` | Sampling initiator |
| `target_device_pk` | `Pubkey` | Destination device |
| `origin_device_location_pk` | `Pubkey` | Location of origin |
| `target_device_location_pk` | `Pubkey` | Location of target |
| `link_pk` | `Pubkey` | Connecting link |
| `sampling_interval_microseconds` | `u64` | Sampling interval |
| `start_timestamp_microseconds` | `u64` | Set on first write |
| `next_sample_index` | `u32` | Current sample count |
| `samples` | `[u32; MAX_DEVICE_LATENCY_SAMPLES]` | Fixed-size region for RTT samples (`µs`) |

Constants:

- `MAX_DEVICE_LATENCY_SAMPLES = 35_000`
- `DEVICE_LATENCY_SAMPLES_HEADER_SIZE = 349` bytes

---

## Instruction: `InitializeDeviceLatencySamples`

Initializes a preallocated latency samples account for a specific device pair, link, and epoch. The account must be fully created and allocated by the client using `create_account_with_seed`.

### Arguments

```rust
pub struct InitializeDeviceLatencySamplesArgs {
    pub epoch: u64,
    pub sampling_interval_microseconds: u64,
}
```

### Accounts

| Index | Role | Signer | Writable | Description |
| --- | --- | --- | --- | --- |
| 0 | `latency_samples_account` | No | Yes | Seeded address to be created externally |
| 1 | `agent` | Yes | No | Must match origin device's publisher |
| 2 | `origin_device` | No | No | Must be activated |
| 3 | `target_device` | No | No | Must be activated |
| 4 | `link` | No | No | Must connect devices |
| 5 | `system_program` | No | No | System program for allocation |
| 6 | `serviceability_program` | No | No | Device/link registry owner |

### Address Derivation

The account is derived using `Pubkey::create_with_seed(agent, seed, program_id)` where seed is the first 32 characters of a base58-encoded SHA-256 hash over the following inputs:

```
[program_id, "telemetry", "device-latency-samples", origin_device, target_device, link, epoch]
```

```rust
let mut hasher = Sha256::new();
hasher.update(program_id.as_ref());
hasher.update(b"telemetry");
hasher.update(b"device-latency-samples");
hasher.update(origin_device.as_ref());
hasher.update(target_device.as_ref());
hasher.update(link.as_ref());
hasher.update(&epoch.to_le_bytes());

let hash = hasher.finalize();
let seed = bs58::encode(hash).into_string();
let seed = &seed[..32];
```

The resulting account address is fully deterministic and must be computed off-chain before calling `InitializeDeviceLatencySamples`.

---

## Instruction: `WriteDeviceLatencySamples`

Appends RTT samples to an existing latency samples account.

### Arguments

```rust
pub struct WriteDeviceLatencySamplesArgs {
    pub start_timestamp_microseconds: u64,
    pub samples: Vec<u32>,
}
```

### Accounts

| Index | Role | Signer | Writable | Description |
| --- | --- | --- | --- | --- |
| 0 | `latency_samples_account` | No | Yes | Existing account to write to |
| 1 | `agent` | Yes | No | Must match `origin_device_agent_pk` |

### Behavior

- First write sets `start_timestamp_microseconds` if unset.
- Validates account ownership and agent authorization.
- Appends samples up to the preallocated capacity (`MAX_DEVICE_LATENCY_SAMPLES`), within Solana's 10,240-byte data size limit.
- Fails if writes would exceed the fixed preallocated account size (10,240 bytes).

---

## Usage Flow

1. Devices and links are provisioned via the `doublezero_serviceability` program and must be in `Activated` or `Suspended` status.
2. An authorized agent initializes the telemetry stream via `InitializeDeviceLatencySamples`.
3. The agent periodically calls `WriteDeviceLatencySamples` to append RTT measurements.
4. Consumers read the account off-chain to analyze latency data.

---

## Notes

- Designed for Solana's runtime constraints, including heap and account size limits.
- RTT values are stored as raw `u32` values in microseconds.
- This program is solely responsible for on-chain collection and storage. Aggregation, verification, and incentives are out of scope.

---

## Constants

- `MAX_DEVICE_LATENCY_SAMPLES = 35_000` — upper bound on total RTT samples.
- `DEVICE_LATENCY_SAMPLES_HEADER_SIZE = 349` — size of the serialized header in bytes, excluding the sample region.
