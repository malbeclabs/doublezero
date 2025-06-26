# Device Telemetry Program

A Solana smart contract for collecting round-trip time (RTT) latency samples between two devices over a link during a specific epoch. Authorized telemetry agents initialize and write data to deterministic per-epoch accounts.

---

## Account Structure: `DzLatencySamples`

Stores metadata and RTT samples (in microseconds):

| Field | Type | Description |
| --- | --- | --- |
| `account_type` | `DzLatencySamples` enum | Type marker |
| `bump_seed` | `u8` | PDA bump seed |
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
| `samples` | `Vec<u32>` | RTT samples (µs) |

Constants:

- `MAX_SAMPLES = 35_000`
- `DZ_LATENCY_SAMPLES_HEADER_SIZE = 229` bytes

---

## Instruction: `InitializeDzLatencySamples`

Creates a new latency samples account for a specific device pair, link, and epoch.

### Arguments

```rust
pub struct InitializeDzLatencySamplesArgs {
    pub origin_device_pk: Pubkey,
    pub target_device_pk: Pubkey,
    pub link_pk: Pubkey,
    pub epoch: u64,
    pub sampling_interval_microseconds: u64,
}
```

### Accounts

| Index | Role | Signer | Writable | Description |
| --- | --- | --- | --- | --- |
| 0 | `latency_samples_account` | No | Yes | PDA to be created |
| 1 | `agent` | Yes | No | Must match origin device's publisher |
| 2 | `origin_device` | No | No | Must be activated |
| 3 | `target_device` | No | No | Must be activated |
| 4 | `link` | No | No | Must connect devices |
| 5 | `system_program` | No | No | System program for allocation |
| 6 | `serviceability_program` | No | No | Device/link registry owner |

### PDA Derivation

```
["dz_latency_samples", origin_device, target_device, link, epoch]
```

---

## Instruction: `WriteDzLatencySamples`

Appends RTT samples to an existing latency samples account.

### Arguments

```rust
pub struct WriteDzLatencySamplesArgs {
    pub start_timestamp_microseconds: u64,
    pub samples: Vec<u32>,
}
```

### Accounts

| Index | Role | Signer | Writable | Description |
| --- | --- | --- | --- | --- |
| 0 | `latency_samples_account` | No | Yes | Existing account to write to |
| 1 | `agent` | Yes | No | Must match `origin_device_agent_pk` |
| 2 | `system_program` | No | No | Used for rent adjustment if resizing |

### Behavior

- First write sets `start_timestamp_microseconds` if unset.
- Validates account ownership and agent authorization.
- Appends samples without exceeding `MAX_SAMPLES` or `MAX_ACCOUNT_ALLOC_BYTES` (10,240 bytes).
- Performs rent transfer and account resize if needed.

---

## Usage Flow

1. Devices and links are created and activated using the `doublezero_serviceability` program.
2. An authorized agent initializes the telemetry stream via `InitializeDzLatencySamples`.
3. The agent periodically calls `WriteDzLatencySamples` to append RTT measurements.
4. Consumers read the account off-chain to analyze latency data.

---

## Notes

- Designed for Solana's runtime constraints, including heap and account size limits.
- RTT values are stored as raw `u32` values in microseconds.
- This program does not perform aggregation, verification, or reward calculation — it is strictly responsible for on-chain collection and storage.

---

## Constants

- `MAX_SAMPLES = 35_000` — upper bound on total RTT samples.
- `MAX_ACCOUNT_ALLOC_BYTES = 10,240` — guardrail for realloc limits.
- `DZ_LATENCY_SAMPLES_HEADER_SIZE = 229` — base size excluding sample vector.
