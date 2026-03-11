# Telemetry Write Timestamp Index

## Summary

**Status: Implemented**

Add a companion timestamp index account to telemetry latency sample accounts so that the actual wall-clock time of each sample can be reliably determined, even when the writing agent experiences downtime mid-epoch.

Today, sample timestamps are inferred as `start_timestamp + index * sampling_interval`. This breaks when the agent stops and resumes — samples after the gap are assigned incorrect timestamps with no way for consumers to detect or correct the error.

## Motivation

The telemetry program stores latency samples as a flat array of `u32` RTT values with a single `start_timestamp_microseconds` set on the first write. Consumers reconstruct per-sample timestamps by assuming uniform spacing at `sampling_interval_microseconds`.

This assumption fails when the agent on a device restarts, crashes, or is intentionally stopped for maintenance. After a gap, the agent resumes appending samples, but the implicit timestamp calculation shifts all subsequent samples earlier than their true measurement time. There is no signal in the account data to indicate that a gap occurred or how long it lasted.

This affects:

- **Reward calculations**: Latency percentiles (p95/p99) computed per-epoch may include samples attributed to the wrong time windows.
- **Observability**: Dashboards and monitoring tools that plot latency over time will show misleading timelines.
- **Debugging**: Operators investigating network issues cannot correlate samples with real-world events when timestamps are wrong.

## New Terminology

- **Timestamp index**: A companion account containing a sequence of `(sample_index, timestamp_microseconds)` entries, one per write batch. Each entry records the sample index and wall-clock time at the start of a write batch, allowing consumers to reconstruct accurate per-sample timestamps.
- **Timestamp index entry**: A single `(u32, u64)` pair in the timestamp index — 12 bytes.

## Alternatives Considered

### Do nothing

Consumers have no way to detect or correct gaps. The problem worsens as the network grows and agent restarts become more frequent.

### Gap markers (sentinel values)

When the agent resumes after downtime, it backfills `u32::MAX` sentinel values for each missed sampling interval before writing real samples. This preserves the implicit timestamp model — every slot has a value, so `start_timestamp + index * sampling_interval` remains correct.

**Pros**: No schema change, no program change, deploy by updating agent behavior only.

**Cons**: Wastes onchain storage proportional to gap duration. A 1-hour gap at 5-second intervals writes 720 useless values (2.8 KB). A 12-hour gap writes 8,640 values (33.6 KB), consuming nearly a quarter of the 35,000 sample capacity. The agent must also track its own last-write time to calculate missed slots, which introduces its own failure modes.

### Per-sample timestamps

Store `(timestamp_us: u64, rtt_us: u32)` per sample instead of just `u32`.

**Pros**: Every sample is self-describing.

**Cons**: 3x storage increase (12 bytes vs 4 bytes per sample). Significantly increases onchain rent costs and reduces the number of samples that fit in a single account.

### Inline timestamp index

Store the timestamp index inside the samples account itself, interleaved between the header and the samples region.

**Pros**: Single account — no companion account to manage or fetch.

**Cons**: Each write requires a `memmove` to shift existing samples forward by 12 bytes to make room for the new index entry. This scales linearly with sample count (up to ~140 KB at capacity). It also changes the samples account layout, requiring new account type discriminators and breaking the clean separation between sample data and metadata.

## Detailed Design

### Architecture

The timestamp index is stored in a separate companion account, derived as a PDA from the samples account's public key. The samples account layout is completely unchanged.

```
Samples account (unchanged):   [header] [samples...]
Timestamp index account (new): [header] [entries...]
```

On each write instruction, the program appends samples to the samples account and appends a timestamp index entry to the companion account, both in the same instruction.

### Timestamp Index Account

#### PDA Derivation

The timestamp index account is derived from the samples account:

**Device latency timestamp index:**
```
Seeds: [b"telemetry", b"tsindex", samples_account_pk]
```

**Internet latency timestamp index:**
```
Seeds: [b"telemetry", b"tsindex", samples_account_pk]
```

Both use the same seed prefix since the samples account public key is unique.

#### Account Layout

```rust
pub struct TimestampIndexHeader {
    pub account_type: AccountType,          // 1 byte (new discriminator)
    pub samples_account_pk: Pubkey,         // 32 bytes
    pub next_entry_index: u32,              // 4 bytes
    pub _unused: [u8; 64],                  // 64 bytes (reserved)
}
// Total header: 101 bytes
```

Followed by a flat array of entries:

```rust
pub struct TimestampIndexEntry {
    pub sample_index: u32,                  // 4 bytes
    pub timestamp_microseconds: u64,        // 8 bytes
}
// Total per entry: 12 bytes
```

- `sample_index`: The value of `next_sample_index` on the samples account at the time of the write (i.e., the index of the first sample in this batch).
- `timestamp_microseconds`: The wall-clock time provided by the agent for this batch.

Entries are append-only and naturally ordered by `sample_index`.

#### Maximum Entries

`MAX_TIMESTAMP_INDEX_ENTRIES = 10_000` entries is enforced. At one entry per write batch, this supports well beyond a 48-hour epoch even with per-second writes. Total max account size: `101 + 10,000 * 12 = ~120 KB`.

### Account Type Enum

```rust
pub enum AccountType {
    DeviceLatencySamplesV0 = 1,
    InternetLatencySamplesV0 = 2,
    DeviceLatencySamples = 3,
    InternetLatencySamples = 4,
    TimestampIndex = 5,                     // new
}
```

Only one new discriminator is needed since the timestamp index account is structurally identical for both device and internet latency — the `samples_account_pk` field links it to the correct samples account.

### Instruction Changes

#### Initialize

New instruction `InitializeTimestampIndex`:

- **Accounts**: `[timestamp_index_account, samples_account, agent, system_program]`
- **Args**: None (all data derived from the samples account)
- **Behavior**: Creates the timestamp index PDA, sets `samples_account_pk` from the provided samples account, sets `next_entry_index = 0`.

This can be called alongside the existing `InitializeDeviceLatencySamples` / `InitializeInternetLatencySamples` instructions, or as a separate transaction.

#### Write

The existing `WriteDeviceLatencySamples` and `WriteInternetLatencySamples` instructions are extended to accept an optional additional account:

- **Accounts**: `[samples_account, agent, system_program, timestamp_index_account (optional)]`
- **Args**: Unchanged — `start_timestamp_microseconds` and `samples` are already provided.

When the timestamp index account is present:

1. Validate it is the correct PDA derived from the samples account.
2. Validate its `samples_account_pk` matches.
3. Append a new entry: `(sample_index: current next_sample_index, timestamp_microseconds: args.start_timestamp_microseconds)`.
4. Increment `next_entry_index`.
5. Resize the account if needed.

When the timestamp index account is absent, the write proceeds as today — this ensures backward compatibility with agents that haven't been updated.

### Timestamp Reconstruction

Consumers reconstruct per-sample timestamps as follows:

```
For sample at index i:
  1. Find the timestamp index entry (si, ts) where si <= i
     and the next entry's si > i (or it's the last entry).
  2. timestamp_of_sample_i = ts + (i - si) * sampling_interval_microseconds
```

Gaps are detected when the elapsed time between consecutive timestamp index entries exceeds the expected time based on sample count:

```
For consecutive entries (si_a, ts_a) and (si_b, ts_b):
  expected_elapsed = (si_b - si_a) * sampling_interval
  actual_elapsed = ts_b - ts_a
  gap = actual_elapsed - expected_elapsed
  if gap > threshold:
    // gap detected between samples si_a and si_b
```

### Agent Changes

The agent requires minimal changes:

1. Call `InitializeTimestampIndex` after initializing each samples account at the start of an epoch.
2. Pass the timestamp index account as an additional account in write instructions.

No behavioral changes are needed — the agent already provides `start_timestamp_microseconds` on every write call. Previously, the program only used this value to set the header field on the first write (when it was zero) and discarded it on subsequent writes. Now, it also records each batch's timestamp in the companion account.

### SDK Changes

Each SDK (Go, Python, TypeScript) needs:

1. A new account type constant for `TimestampIndex`.
2. New deserialization functions/structs for the timestamp index account.
3. A helper function to reconstruct per-sample timestamps given a samples account and its companion timestamp index.
4. Existing deserialization for samples accounts remains completely unchanged.

## Impact

- **Onchain program**: One new account type, one new initialize instruction, extended write instructions to optionally accept the companion account. Moderate change.
- **Samples accounts**: No changes whatsoever — existing layout, PDA derivation, and deserialization are untouched.
- **SDKs**: New deserialization for the timestamp index account and timestamp reconstruction helpers. Binary test fixtures need regeneration for the new account type.
- **Storage overhead**: 12 bytes per write batch + 101 byte header per companion account. At typical write intervals (every 30-60 seconds), a 48-hour epoch produces ~3,000-6,000 entries = 36-72 KB per companion account.
- **Compute overhead**: Append-only writes to the companion account — no memmove or shifting.
- **Agent**: Pass one additional account on initialize and write calls.
- **Transaction size**: One additional account (32 bytes) in each write transaction.

## Security Considerations

No new attack surfaces are introduced. The timestamp values are self-reported by the agent, same as today. The program does not validate timestamp ordering or reasonableness — this is consistent with the existing trust model where agents are trusted contributors (per rfc4).

The `MAX_TIMESTAMP_INDEX_ENTRIES` limit prevents a misbehaving agent from growing the timestamp index unboundedly.

The companion account is validated via PDA derivation and the `samples_account_pk` field, preventing an agent from associating a timestamp index with the wrong samples account.

## Backward Compatibility

- **Existing samples accounts**: Completely unaffected. No layout changes, no new account type discriminators.
- **Existing write instructions**: Continue to work without the timestamp index account. Agents that haven't been updated simply don't create or pass the companion account.
- **Rollout**: The program upgrade adds the new instruction and extends existing write instructions. Agents can be updated independently — until updated, they continue writing without timestamp indices. No migration of existing accounts is needed.
- **SDKs**: Existing deserialization paths are unchanged. The timestamp index is an additive feature — consumers that don't need it can ignore the companion accounts entirely.

## Open Questions

None at this time.
