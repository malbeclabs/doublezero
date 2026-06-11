# RFC 19 — User BGP Status

## Summary

**Status: `Implemented`**

This RFC closes the loop between Serviceability, DZD configuration, and the doublezerod
client connection by persisting the real BGP session state of each user onchain.
Today a user account reaches Activated status once resources are allocated and config
is pushed to the device, but there is no record of whether the user's doublezerod
actually established the BGP session. This gap makes it impossible to distinguish
users who are configured but never connected from users with healthy sessions.

Alongside the session state, the same write also carries the smoothed BGP TCP RTT
sampled from the device kernel's `tcp_info`. Surfacing RTT onchain gives any consumer
that already reads the User account live latency for the per-user GRE tunnel, with no
additional collection cost (the value is already available in the INET_DIAG snapshot
used to detect ESTABLISHED sessions).

## Motivation

A user's lifecycle in DoubleZero crosses three layers:

1. **Serviceability (onchain)** — tunnel, TunnelNet, DzIp are allocated and the user account reaches status = Activated.
2. **DZD configuration** — controller renders BGP neighbor config and pushes it to Arista.
3. **doublezerod connection** — the client establishes the GRE tunnel and BGP session.

Only after step 3 is the user truly connected. Today there is no observable onchain signal
for step 3. Persisting BGP status on the User account enables idle user detection and
connection diagnostics.

## New Terminology

- **BGP Status** — `Unknown` (default), `Up` (session ESTABLISHED), `Down` (no active session).
- **BGP RTT** — smoothed round-trip time of the per-user BGP TCP session in nanoseconds,
  sourced from the kernel's `tcp_info.rtt` (microseconds) via INET_DIAG netlink. The
  nanosecond unit matches `Link.delay_ns` and `Link.jitter_ns` for consistency with the
  rest of the onchain network model. `0` is the sentinel for "no sample observed".
- **Metrics Publisher** — keypair registered as `metrics_publisher_pk` on the Device account,
  already used to authenticate telemetry writes to the telemetry program and state-ingest server.

## Alternatives Considered

- **Store status in S3/ClickHouse only** — already done for raw socket stats. Not queryable
  onchain and not accessible to other onchain programs.
- **Separate `UserBGPSession` PDA per user** — isolates BGP state from the User account and
  avoids resizing it. Rejected because BGP status has a strict 1:1 relationship with the user,
  the Device account must always be read to verify write authority regardless, and splitting the
  data would require reading two accounts for every consumer that queries a user's connection
  state.

## Detailed Design

### User account: new fields

Add four fields to the end of the `User` struct:

1. `bgp_status: BGPStatus` (1 byte, Borsh) — current session state:

       Unknown = 0  (default, backwards-compatible)
       Up      = 1
       Down    = 2

2. `last_bgp_up_at: u64` (8 bytes, DZ ledger slot) — the last slot when `bgp_status`
  transitioned to `Up`. Zero means the session has never been observed Up.

3. `last_bgp_reported_at: u64` (8 bytes, DZ ledger slot) — the last slot when the
  telemetry agent successfully wrote a BGP status update for this user. Updated on
  every `SetUserBGPStatus` write, whether or not `bgp_status` changed. Consumers can
  use this field to detect agent silence: if `last_bgp_reported_at` is older than a
  threshold, the `bgp_status` value should be treated as stale rather than
  authoritative, avoiding false `Up` readings when the agent has stopped reporting.

4. `bgp_rtt_ns: u64` (8 bytes) — smoothed BGP TCP RTT in nanoseconds for the per-user
  GRE tunnel, as last reported by the device agent. Same unit as `Link.delay_ns` and
  `Link.jitter_ns`. `0` means no sample has been observed yet (a real BGP session
  over the GRE tunnel always measures a non-zero RTT once the kernel has produced a
  sample). The field is unconditionally overwritten on every write: a `Down`
  submission carries `bgp_rtt_ns = 0` so a stale sample cannot outlive the session.

The `SetUserBGPStatus` instruction reallocates the account by 25 bytes on first
write (1 + 8 + 8 + 8), with the metrics publisher covering any additional rent.
`last_bgp_up_at` is updated only when the status transitions to `Up`.

### New instruction: SetUserBGPStatus (variant TBD)

Accounts: user (writable), device (readonly), metrics_publisher (signer + writable).

Validation: signer == device.metrics_publisher_pk, user.device_pk == device.

Arguments: `bgp_status: BGPStatus` (1 byte) followed by `bgp_rtt_ns: u64` (8 bytes,
little-endian). The args struct is derived with `BorshDeserializeIncremental`, so
older publishers that send only the status byte continue to be accepted (`bgp_rtt_ns`
defaults to `0`). A new program receiving an old payload behaves the same as a new
program receiving a fresh `Down` write: the RTT field is set to `0`. Deploy order
between the program upgrade and the telemetry agent upgrade is therefore
unconstrained.

### Telemetry collector

After each BGP socket collection tick in `collectBGPStateSnapshot`:

1. Fetch activated users for this device from the serviceability program.
2. Map each user to its BGP peer IP: `overlay_dst_ip = user.TunnelNet[0:4]`, last octet +1.
3. For each user: Up if a socket with matching RemoteIP exists, Down otherwise.
   When Up, also read `tcp_info.rtt` (microseconds) from the same INET_DIAG netlink
   reply and convert to nanoseconds for `bgp_rtt_ns`. When Down, `bgp_rtt_ns` is
   submitted as `0` so the onchain value does not outlive the session.
4. For each user, submit `SetUserBGPStatus` if: (a) the computed status differs from
   the last known onchain value, or (b) the last write was more than a configurable
   interval ago (e.g., 1h), to keep `last_bgp_reported_at` fresh for staleness
   detection. RTT changes alone do not trigger a submission: RTT is noisy by nature
   and piggybacks on writes that would already happen, so its staleness window is
   bounded by the periodic refresh interval. Submissions are enqueued into a
   non-blocking background worker that retries independently so that a single RPC
   error does not delay other users or block the collection tick. The metrics
   publisher keypair is already loaded in the telemetry agent.

The raw TCP snapshot upload to S3 continues unchanged.

## Impact

- Serviceability program: one new instruction, twenty-five new bytes on User accounts (1 byte `bgp_status` + 8 bytes `last_bgp_up_at` + 8 bytes `last_bgp_reported_at` + 8 bytes `bgp_rtt_ns`).
- Telemetry agent: one extra RPC call per collection tick to fetch users; up to N transactions
  per tick (one per user whose status changed, or whose periodic refresh interval has elapsed).
- Read SDKs (Go, TypeScript, Python): update User deserialization for the four new fields.
- CLI: `doublezero user get` and `doublezero user list` surface BGP status and RTT
  (formatted as ms, with `-` when no sample has been observed). JSON output exposes the
  raw `bgp_rtt_ns` so downstream tooling can keep nanosecond precision.

## Security Considerations

Authorization reuses the existing `metrics_publisher_pk` trust model, already enforced
by the telemetry program for latency sample writes. No new trust boundaries are introduced.

## Backward Compatibility

Existing User accounts are 25 bytes shorter. The first SetUserBGPStatus write reallocates
the account. Until then, readers should treat the missing fields as Unknown and zero
respectively. SDK deserializers must handle the shorter account gracefully.

The instruction argument struct uses `BorshDeserializeIncremental`, so an upgraded
program continues to accept the legacy 1-byte (status-only) payload from an older
telemetry agent, defaulting `bgp_rtt_ns` to `0`. This removes any deploy-order
constraint between the serviceability upgrade and the telemetry agent upgrade.

## Rollout

Steps must be executed in order. Each step is a prerequisite for the next.

**Step 1 — Serviceability program upgrade**
Deploy the new program version with the `SetUserBGPStatus` instruction and the four new
fields on the `User` struct (`bgp_status`, `last_bgp_up_at`, `last_bgp_reported_at`,
`bgp_rtt_ns`). No existing accounts are migrated; the new fields are written lazily on
first call.

**Step 2 — SDK updates**
Update read-only SDKs (Go, TypeScript, Python) to deserialize all four new fields,
defaulting each to zero when the account is shorter than the new size. Must ship
before step 3 to avoid deserialization errors in consumers that read User accounts.

**Step 3 — Telemetry agent update**
Deploy the updated telemetry agent with the `SetUserBGPStatus` write client. From this
point on, each BGP collection tick writes status and RTT onchain. No rollback concern:
if the agent is reverted, fields simply stop updating and remain at their last known
values.

**Step 4 — Verification**
On a devnet device with at least one activated user:
- Confirm `bgp_status` transitions to `Up` when the user's doublezerod is connected.
- Confirm `last_bgp_up_at` is set and matches the observed connection time.
- Confirm `bgp_rtt_ns` is non-zero on Up writes and matches the kernel's
  `tcp_info.rtt` (microseconds) for the BGP socket multiplied by 1000.
- Disconnect the user and confirm `bgp_status` transitions to `Down` and
  `bgp_rtt_ns` is cleared to `0` on the next tick.

## Open Questions

- Should there be a grace period before marking a session `Down`? A single missed tick
  due to a transient collection error would incorrectly transition an active user to
  `Down`. One option is to require N consecutive `Down` observations before writing.
- Should we implement per-user rate limiting to prevent RPC saturation caused by
  constant BGP flaps? A user cycling Up/Down rapidly would generate a transaction on
  every state-change; a cooldown window or minimum time-between-writes per user account
  could bound the worst-case submission rate.
- How should recurring circuit flaps be handled? A user whose BGP session repeatedly
  drops and recovers within short windows may indicate an unstable circuit rather than
  a transient error. Should the data model track a flap counter or a flap rate to
  distinguish these users from those with a single clean Up or Down transition? And
  should repeated flapping trigger any action, such as surfacing the user for manual
  review?
