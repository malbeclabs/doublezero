# RFC 17 — User BGP Status

## Summary

**Status: `Draft`**

This RFC closes the loop between Serviceability, DZD configuration, and the doublezerod
client connection by persisting the real BGP session state of each user onchain.
Today a user account reaches Activated status once resources are allocated and config
is pushed to the device, but there is no record of whether the user's doublezerod
actually established the BGP session. This gap makes it impossible to distinguish
users who are configured but never connected from users with healthy sessions.

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
- **Metrics Publisher** — keypair registered as `metrics_publisher_pk` on the Device account,
  already used to authenticate telemetry writes to the telemetry program and state-ingest server.

## Alternatives Considered

- **Store status in S3/ClickHouse only** — already done for raw socket stats. Not queryable
  onchain and not accessible to other onchain programs.
- **Separate PDA account per user** — avoids resizing the User account but adds a new account
  type and complicates reads. Rejected for simplicity.

## Detailed Design

### User account: new fields

Add three fields to the end of the `User` struct:

1. `bgp_status: BGPStatus` (1 byte, Borsh) — current session state:

       Unknown = 0  (default, backwards-compatible)
       Up      = 1
       Down    = 2

2. `last_bgp_up_at: i64` (8 bytes, Unix timestamp in seconds) — the last time `bgp_status`
   transitioned to `Up`. Zero means the session has never been observed Up.

3. `last_bgp_reported_at: i64` (8 bytes, Unix timestamp in seconds) — the last time the
   telemetry agent successfully wrote a BGP status change for this user. Updated only
   when `bgp_status` transitions to a different value. Consumers can use this field to
   detect agent silence: if `last_bgp_reported_at` is older than a threshold, the
   `bgp_status` value should be treated as stale rather than authoritative, avoiding
   false `Up` readings when the agent has stopped reporting.

The `SetUserBGPStatus` instruction reallocates the account by 17 bytes on first write
(1 + 8 + 8), with the metrics publisher covering any additional rent. `last_bgp_up_at`
and `last_bgp_reported_at` are both updated only when the status value changes.

### New instruction: SetUserBGPStatus (variant 94)

Accounts: user (writable), device (readonly), metrics_publisher (signer + writable).

Validation: signer == device.metrics_publisher_pk, user.device_pk == device, user.status == Activated.

### Telemetry collector

After each BGP socket collection tick in `collectBGPStateSnapshot`:

1. Fetch activated users for this device from the serviceability program.
2. Map each user to its BGP peer IP: `overlay_dst_ip = user.TunnelNet[0:4]`, last octet +1.
3. For each user: Up if a socket with matching RemoteIP exists, Down otherwise.
4. Enqueue one `SetUserBGPStatus` transaction per user into a non-blocking background
   worker. The worker retries failed submissions independently so that a single RPC
   error or congested transaction does not delay other users or block the collection
   tick. The metrics publisher keypair is already loaded in the telemetry agent.

The raw TCP snapshot upload to S3 continues unchanged.

## Impact

- Serviceability program: one new instruction, seventeen new bytes on User accounts (1 byte `bgp_status` + 8 bytes `last_bgp_up_at` + 8 bytes `last_bgp_reported_at`).
- Telemetry agent: one extra RPC call per collection tick to fetch users; N transactions
  per tick (one per activated user on the device).
- Read SDKs (Go, TypeScript, Python): update User deserialization for the new field.

## Security Considerations

Authorization reuses the existing `metrics_publisher_pk` trust model, already enforced
by the telemetry program for latency sample writes. No new trust boundaries are introduced.

## Backward Compatibility

Existing User accounts are 17 bytes shorter. The first SetUserBGPStatus write reallocates
the account. Until then, readers should treat the missing fields as Unknown and zero
respectively. SDK deserializers must handle the shorter account gracefully.

## Rollout

Steps must be executed in order. Each step is a prerequisite for the next.

**Step 1 — Serviceability program upgrade**
Deploy the new program version with the `SetUserBGPStatus` instruction and the two new
fields on the `User` struct. No existing accounts are migrated; the new fields are
written lazily on first call.

**Step 2 — SDK updates**
Update read-only SDKs (Go, TypeScript, Python) to deserialize `bgp_status` and
`last_bgp_up_at`, defaulting both to zero when the account is shorter than the new
size. Must ship before step 3 to avoid deserialization errors in consumers that read
User accounts.

**Step 3 — Telemetry agent update**
Deploy the updated telemetry agent with the `SetUserBGPStatus` write client. From this
point on, each BGP collection tick writes status onchain. No rollback concern: if the
agent is reverted, fields simply stop updating and remain at their last known values.

**Step 4 — Verification**
On a devnet device with at least one activated user:
- Confirm `bgp_status` transitions to `Up` when the user's doublezerod is connected.
- Confirm `last_bgp_up_at` is set and matches the observed connection time.
- Disconnect the user and confirm `bgp_status` transitions to `Down` on the next tick.

## Open Questions

- Should there be a grace period before marking a session `Down`? A single missed tick
  due to a transient collection error would incorrectly transition an active user to
  `Down`. One option is to require N consecutive `Down` observations before writing.
- Since the agent only writes on status changes, `last_bgp_reported_at` will not
  advance for stable sessions, making it impossible to distinguish a healthy long-lived
  `Up` session from a silent agent. Should the agent periodically send a reconfirmation
  write (e.g., every N days) even when the status has not changed, to keep
  `last_bgp_reported_at` fresh and preserve staleness detection?
- Should we implement per-user rate limiting to prevent RPC saturation caused by
  constant BGP flaps? A user cycling Up/Down rapidly would generate a transaction on
  every tick; a cooldown window or minimum time-between-writes per user account could
  bound the worst-case submission rate.
- How should recurring circuit flaps be handled? A user whose BGP session repeatedly
  drops and recovers within short windows may indicate an unstable circuit rather than
  a transient error. Should the data model track a flap counter or a flap rate to
  distinguish these users from those with a single clean Up or Down transition? And
  should repeated flapping trigger any action, such as surfacing the user for manual
  review?
