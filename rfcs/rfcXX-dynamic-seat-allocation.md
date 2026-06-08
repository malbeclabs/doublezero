# RFC XX: Dynamic Seat Allocation

## Summary

**Status: `Draft`**

A DoubleZero AccessPass is currently bound to a single `(client_ip, user_payer)` pair, and the Feed
Oracle creates exactly one AccessPass and one serviceability `User` per IP. A buyer must therefore know
the client's public IP before access can be granted, and one purchase serves exactly one machine.

This RFC introduces dynamic seat allocation: a buyer purchases one seat that admits several users, and a
node onboards without registering its IP ahead of time. It adds per-category user caps to the AccessPass
(enforced for seat passes), reuses the existing dynamic IP-less AccessPass primitive, adds a
pubkey-keyed `ClientSeatV2` to the Feed Subscription Program, and adds an authenticated on-demand
`/user/create` endpoint to the Feed Oracle that provisions a serviceability `User` using the request's
observed source IP.

## Motivation

Two limitations block common subscriber workflows today:

1. **The client IP must be known up front.** The AccessPass PDA is derived from `(client_ip,
   user_payer)`, so the buyer, or the Feed Oracle acting on their behalf, must know the machine's exact
   public IP before granting access. Operators behind NAT, with addresses assigned late in provisioning,
   or running fleets of machines cannot be onboarded cleanly.

2. **One seat serves one user.** A `ClientSeat` in the Feed Subscription Program is keyed on the
   subscriber's IP, and the Feed Oracle creates one AccessPass and one `User` per seat. An operator
   running several nodes must buy a seat per node; there is no concept of a seat that admits N machines.

3. **Onboarding needs a funded keypair.** Creating a `User` with `doublezero connect` is a transaction
   the operator's own keypair must pay for, so that keypair has to hold SOL. Having the Feed Oracle
   create the `User` instead makes onboarding gasless for the operator: the oracle signs and pays the
   fee, and the operator's seat keypair never needs funding.

The serviceability program already contains most of the primitives this needs. An AccessPass can be
created with `client_ip = 0.0.0.0` and `ALLOW_MULTIPLE_IP` set, and `create_user` already
accepts either the exact-IP PDA or the `UNSPECIFIED` PDA when validating a new `User`. The
`AccessPassType::EdgeSeat(Pubkey)` variant already exists. What is missing is a way to bound how many
users a single pass admits, a seat account identified by a key rather than an IP, and a path to create
the `User` at connect time from the node's own IP. This RFC supplies those pieces and wires them
together.

## New Terminology

- **Feed Oracle** — the off-chain service that watches the Feed Subscription Program and provisions
  access on the DoubleZero ledger.
- **Feed Subscription Program** — the Solana program that sells and settles feed subscription seats.
- **Seat** — a purchased subscription entitlement. **`ClientSeatV2`** is the new seat account, keyed on a
  client public key rather than an IP.
- **Dynamic AccessPass** — an AccessPass created with `client_ip = 0.0.0.0` (UNSPECIFIED) and
  `ALLOW_MULTIPLE_IP` set, so users from any source IP may attach.
- **EdgeSeat AccessPass** — an AccessPass whose `accesspass_type` is `EdgeSeat(seat_pubkey)`. Per-category
  user caps are enforced only for this type.
- **Per-category caps** — separate current and maximum user counts for IBRL (unicast) users and for
  multicast users.

## Alternatives Considered

- **Enforce caps for all AccessPass types.** Applying a `max_users` to every pass (Prepaid, validator,
  RPC) would retroactively cap existing single-user passes and risk regressions. Gating enforcement on
  `EdgeSeat` confines the new behavior to seats created for this feature.

- **Carry `client_ip` in the signed `/user/create` request.** The client could resolve and send its own
  IP, signed, and the oracle would trust it. This binds the IP against replay, but it reintroduces "the
  client must determine its IP" and adds a field to the signed message. This RFC uses the observed source
  IP (see Security Considerations for the tradeoff); binding the IP into the signature is listed as a
  future hardening.

## Detailed Design

### End-to-end flow

1. A buyer creates a `ClientSeatV2` in the Feed Subscription Program, keyed on their client public key
   `C`, specifying `max_unicast_users` and `max_multicast_users`, on a chosen device.
2. The Feed Oracle observes the new seat and creates a **dynamic EdgeSeat AccessPass** on the DoubleZero
   ledger: PDA `accesspass(UNSPECIFIED, C)`, `accesspass_type = EdgeSeat(seat_pda)`,
   `allow_multiple_ip = true`, the two maxes copied from the seat, `user_payer = C`, `owner = oracle`.
3. On each node, the operator configures the seat keypair `C` as the doublezero client keypair and runs
   `doublezero enable`.
4. `enable` resolves the node's public IP the same way `connect` does, lists serviceability `User`s, and
   finds none for that IP.
5. `enable` calls `POST {feed_oracle_url}/user/create` with the JSON body
   `{"pubkey": C, "timestamp": T, "signature": S}`, where `S = sign("POST:/user/create/{T}")` under the
   seat key `C`.
6. The Feed Oracle verifies `T` is within 30 seconds and `S` against `C`, observes the request's source
   IP `ip`, finds the AccessPass at `accesspass(UNSPECIFIED, C)`, resolves the seat's device, and creates
   a multicast subscriber `User` for `ip`. The operation is idempotent. User creation invokes
   `accesspass.try_add_user(Multicast)`, which enforces `multicast_user_count < max_multicast_users`
   because the pass is `EdgeSeat`.
7. `enable` turns on the reconciler; the daemon brings up the tunnel and routes.

### A. Serviceability — per-category user caps on AccessPass

File: `smartcontract/programs/doublezero-serviceability/src/state/accesspass.rs`.

Append four fields to `AccessPass`, after `tenant_allowlist`, so existing accounts remain
deserialize-compatible:

| Field                  | Type  | Meaning                                  | Default on read |
| ---------------------- | ----- | ---------------------------------------- | --------------- |
| `unicast_user_count`   | `u16` | Current live non-multicast users         | `0`             |
| `max_unicast_users`    | `u16` | Cap on non-multicast users (EdgeSeat)    | `1`             |
| `multicast_user_count` | `u16` | Current live multicast users             | `0`             |
| `max_multicast_users`  | `u16` | Cap on multicast users (EdgeSeat)        | `1`             |

The existing `connection_count: u16` is unchanged and remains the combined live-user total for every
pass. The four new fields are an EdgeSeat-scoped refinement that splits that total by category. Defaults
are applied in the manual `TryFrom<&[u8]>` exactly as `client_ip` already uses
`unwrap_or(Ipv4Addr::UNSPECIFIED)`: counts read back as `0`, maxes as `1`, so a pre-existing pass behaves
as a single-user pass (which only matters under EdgeSeat enforcement).

Add two methods:

```rust
// EdgeSeat passes count and enforce per category; all other types are a no-op and succeed.
pub fn try_add_user(&mut self, user_type: UserType) -> Result<(), DoubleZeroError> {
    if let AccessPassType::EdgeSeat(_) = self.accesspass_type {
        if user_type == UserType::Multicast {
            if self.multicast_user_count >= self.max_multicast_users {
                return Err(DoubleZeroError::AccessPassMaxMulticastUsersExceeded);
            }
            self.multicast_user_count += 1;
        } else {
            if self.unicast_user_count >= self.max_unicast_users {
                return Err(DoubleZeroError::AccessPassMaxUnicastUsersExceeded);
            }
            self.unicast_user_count += 1;
        }
    }
    Ok(())
}

pub fn remove_user(&mut self, user_type: UserType) {
    if let AccessPassType::EdgeSeat(_) = self.accesspass_type {
        if user_type == UserType::Multicast {
            self.multicast_user_count = self.multicast_user_count.saturating_sub(1);
        } else {
            self.unicast_user_count = self.unicast_user_count.saturating_sub(1);
        }
    }
}
```

These methods deliberately do **not** touch `connection_count`; the existing increment and decrement of
that field stay where they are so universal behavior is preserved. Two new error variants
(`AccessPassMaxUnicastUsersExceeded`, `AccessPassMaxMulticastUsersExceeded`) are added to `error.rs`,
distinct from the device-scoped `MaxUnicastUsersExceeded` so failures are attributable to the seat cap.

**Hook points** (already present in the code):

- `processors/user/create_core.rs` increments `connection_count` and sets status when all validations
  pass (the line `accesspass.connection_count += 1`). Call `accesspass.try_add_user(user_type)?` next to
  it, so the per-category cap is checked before the user is written. The same function already derives
  both `accesspass(client_ip, owner)` and `accesspass(UNSPECIFIED, owner)` and accepts either, and
  validates `accesspass.user_payer == effective_owner`. This RFC also removes the block here that locked
  an unspecified-IP pass to its first user's IP (and removes the now-unused `IS_DYNAMIC` flag); the only
  remaining IP rule is the existing check that rejects a mismatched `client_ip` when `allow_multiple_ip()`
  is not set. Seat passes set `allow_multiple_ip`, so they admit any source IP; non-seat passes keep their
  exact-IP binding.
- `processors/user/delete.rs` decrements `connection_count` with `saturating_sub(1)`. Call
  `accesspass.remove_user(user_type)` alongside it.

**Instruction args.** `SetAccessPassArgs` in `processors/accesspass/set.rs` already derives
`BorshDeserializeIncremental`; add `max_unicast_users` and `max_multicast_users` with
`#[incremental(default = 1)]`. The processor persists them on create and updates them on set, leaving the
live counts untouched (those are managed only by user create/delete). `EdgeSeat` is already validated
there to carry a non-default pubkey; the set-time logic that derived the removed `IS_DYNAMIC` flag from a
`client_ip == UNSPECIFIED` value also goes away.

**CLI.** `smartcontract/cli/src/accesspass/set.rs` gains `--max-unicast-users` and
`--max-multicast-users`; `get.rs` and `list.rs` render the four new fields.

**SDKs.** Add the four fields to the AccessPass layouts in Go
(`smartcontract/sdk/go/serviceability/state.go`), Python
(`sdk/serviceability/python/serviceability/state.py`), and TypeScript
(`sdk/serviceability/typescript/serviceability/state.ts`), then regenerate the shared binary fixtures
with `make generate-fixtures` (generator at
`sdk/serviceability/testdata/fixtures/generate-fixtures`). The Python and TypeScript readers are
defensive, so older fixtures continue to parse.

### B. Serviceability SDK — UNSPECIFIED-first AccessPass lookup

File: `smartcontract/sdk/rs/src/commands/accesspass/get.rs`.

`GetAccessPassCommand::execute` derives the PDA from `(client_ip, user_payer)` and fetches it. Change it
so that when `client_ip != UNSPECIFIED` it first tries the dynamic PDA
`get_accesspass_pda(UNSPECIFIED, user_payer)` and, only if that account is absent, falls back to the
exact-IP PDA. This lets the read path find a shared seat pass first. The connect command's
`check_accesspass` (called from `client/doublezero/src/command/connect.rs`) goes through this command and
benefits automatically. The onchain `create_user` already tries both PDAs, so this change aligns the
read side with the write side; it does not alter onchain behavior.

### C. Client — `doublezero enable` auto-onboarding

File: `client/doublezero/src/command/enable.rs`.

`enable` today checks the daemon, and if the reconciler is not already enabled, enables it. Restructure
it as follows:

1. Resolve the client IP with `super::helpers::resolve_client_ip(controller)` — the same helper `connect`
   uses, which reads `v2_status.client_ip` from the daemon. The daemon discovers that IP via
   `DiscoverClientIP` (default-route source hint, then `ifconfig.me` fallback), so NAT is handled exactly
   as it is for `connect`.
2. List serviceability `User`s (`ListUserCommand`) and look for one whose `client_ip` matches. If found,
   skip to step 4.
3. If none is found and a `feed_oracle_url` is configured for the environment, request on-demand
   creation: build `timestamp = now` (unix seconds), sign the ASCII bytes `"POST:/user/create/{timestamp}"`
   with the client keypair, and `POST {feed_oracle_url}/user/create` with a JSON body carrying `pubkey`,
   `timestamp`, and `signature` — where `pubkey` is the client keypair's public key (the seat key, equal
   to the AccessPass `user_payer`).
   Log any non-2xx response and retry with exponential backoff (the `backon` crate), capped at a
   30-second interval per attempt, for up to 5 minutes total.
4. Call `controller.enable()` regardless of the `/user/create` outcome, so the reconciler is turned on
   even if user creation could not be completed in time.

The pre-step is gated so it does not affect non-feed users: a normal IBRL user runs `connect` first, so a
`User` already exists and step 2 short-circuits. The `/user/create` call is attempted only when no `User`
exists for the IP and a `feed_oracle_url` is present. Because `enable` proceeds regardless, a missing or
unreachable oracle never blocks turning on the reconciler. The CLI reuses its existing HTTP client
machinery for the outbound request.

### D. Config — Feed Oracle URL per environment

Files: `config/src/env.rs`, `config/src/constants.rs`.

Add `feed_oracle_url: String` to `NetworkConfig` and populate it per environment in
`Environment::config()`, mirroring how the ledger and Solana URLs are wired. Add the constants
`ENV_MAINNET_BETA_FEED_ORACLE_URL`, `ENV_TESTNET_FEED_ORACLE_URL`, `ENV_DEVNET_FEED_ORACLE_URL`, and
`ENV_LOCAL_FEED_ORACLE_URL`.

The DNS records do not exist yet. The proposed convention, to be confirmed and provisioned during
implementation, is `http://feed-oracle.<env>.<doublezero-domain>` for MainnetBeta, Testnet, and Devnet,
and `http://localhost:<port>` for Local. Honor an environment-variable override (for example
`DZ_FEED_ORACLE_URL`) consistent with the existing `DZ_LEDGER_RPC_URL` and `DZ_SOLANA_RPC_URL` overrides.

### E. Feed Subscription Program — `ClientSeatV2`

Files under `programs/shred-subscription/src/` (`state/`, `instruction/`, `processor/`).

Add `ClientSeatV2`, modeled on `ClientSeat` but keyed on a client public key instead of an IP:

| Aspect            | `ClientSeat` (v1)                                  | `ClientSeatV2`                                         |
| ----------------- | -------------------------------------------------- | ----------------------------------------------------- |
| PDA seeds         | `[b"client_seat", device_key, client_ip_bits]`     | `[b"client_seat_v2", device_key, client_pubkey]`      |
| Identity field    | `client_ip_bits: u32`                              | `client_pubkey: Pubkey`                               |
| User limits       | none                                               | `max_unicast_users: u16`, `max_multicast_users: u16`  |
| Discriminator     | `dz::account::client_seat`                         | `dz::account::client_seat_v2`                         |
| Lifecycle fields  | tenure, funding, settlement, escrow, price, etc.   | same (shared lifecycle)                               |

`device_key` is retained because it determines which device a created `User` attaches to. Add an
instruction `InitializeClientSeatV2 { client_pubkey, max_unicast_users, max_multicast_users }`, mirroring
`InitializeClientSeat` (`instruction/mod.rs` plus a processor handler analogous to
`try_initialize_client_seat`).

The program supports both seat types simultaneously: existing v1 instructions and handlers are unchanged,
and v2 is added beside them. The funding, settlement, and tenure logic is the largest surface; v2 seats
flow through the same lifecycle, differing only in identity and the new max fields. The settlement and
auction integration for v2 is the heaviest implementation item and should be scoped carefully (see
Impact).

### F. Feed Oracle changes

Files under `crates/shred-oracle/src/` (`dz_ledger.rs`, `oracle.rs`, a new HTTP-API module, `main.rs`,
config).

**1. Create an EdgeSeat AccessPass for each v2 seat.** Today `dz_ledger.rs::set_access_pass` builds a
`SetAccessPass` instruction with `accesspass_type = Prepaid`, a specific `client_ip`, and
`allow_multiple_ip = false`. When the oracle observes a `ClientSeatV2`, it instead creates or updates a
dynamic EdgeSeat pass:

- `accesspass_type = EdgeSeat(client_seat_v2_pda)`
- `client_ip = UNSPECIFIED`
- `allow_multiple_ip = true`
- `max_unicast_users` and `max_multicast_users` copied from the seat (using the extended
  `SetAccessPassArgs` from section A)
- `user_payer = client_pubkey` (the seat's key, rather than the oracle's key or the validator's withdraw
  authority used in the v1 path)

The oracle signs as the GlobalState `feed_authority` (or a foundation-allowlist member, as it already
must be to set passes today). `set_access_pass` records `owner = payer = oracle`, while `user_payer` is
the seat key — `process_set_access_pass` already supports this split and lets the feed authority create
and later update passes it owns.

**2. Stop auto-creating users for v2 seats.** `oracle.rs::create_serviceability_users` currently creates
serviceability `User`s automatically via `create_subscribe_user`. Branch on seat version: v1 `ClientSeat`
keeps today's automatic path unchanged; v2 `ClientSeatV2` creates no users automatically — they arrive
on demand through `/user/create`.

**3. Add the `/user/create` HTTP API.** The oracle runs only a Prometheus metrics server today (plain
Hyper, `/metrics`). Add a separate HTTP service on its own configurable port, spawned as a tokio task in
`main.rs` next to the existing crankers; the metrics server stays metrics-only.

- **Request:** `POST /user/create` with a JSON body:
  ```json
  { "pubkey": "<base58>", "timestamp": <unix seconds>, "signature": "<base58>" }
  ```
- **Authentication:**
  1. Reject if `|now - timestamp| > 30s`.
  2. Verify the ed25519 `signature` over the exact ASCII bytes `"POST:/user/create/{timestamp}"` against
     `pubkey`. This is a plain ed25519 verification (the repo already depends on `solana-sdk` and
     `brine-ed25519`), not the Solana off-chain-message envelope; the client in section C signs the same
     plain bytes.
- **Client IP:** the new `User`'s `client_ip` is the request's observed TCP source IP. The client does
  not send an IP (see Security Considerations for the replay implications).
- **Resolution:** look up the `ClientSeatV2` for `pubkey` (the oracle indexes seats it watches; the seat
  is also reachable from the AccessPass `EdgeSeat(seat_pda)` value) to obtain its `device_key`. Find the
  AccessPass at `get_accesspass_pda(UNSPECIFIED, pubkey)`; return `404` if it is missing.
- **User creation:** create a multicast subscriber `User` for the observed IP using the same
  `create_subscribe_user` path used today (owner override = `pubkey`, the seat's device, the configured
  feed multicast group). The oracle signs the transaction; the override is permitted because the oracle
  is the feed authority. Creation runs through `create_user_core`, so `try_add_user(Multicast)` enforces
  the multicast cap.
- **Idempotency:** if the user PDA `get_user_pda(client_ip, Multicast)` already exists, return success
  without resubmitting.
- **Responses:** `200` on success or already-exists; `401` on authentication failure; `404` when no
  matching seat or AccessPass exists; `5xx` on transaction failure.

## Impact

- **Serviceability program.** Four appended AccessPass fields, two small methods, two error variants, a
  hook in `create_user_core` and `delete`, and two new `SetAccessPassArgs` fields. The change is additive
  and confined to the access-pass and user paths.
- **CLI and SDKs.** New flags on `access-pass set`, new display fields, three SDK layout updates, and a
  fixture regeneration.
- **Client.** A bounded pre-step in `enable` and one outbound HTTP request.
- **Config.** One new `NetworkConfig` field and four constants.
- **Feed Subscription Program.** A new account and instruction plus settlement and auction integration
  for v2 seats — the largest single work item.
- **Feed Oracle.** Changed AccessPass creation, a version branch in user provisioning, and a new
  authenticated HTTP service.

This spans two repositories and well exceeds the project's ~500-line-per-PR guideline, so it should be
delivered as a sequence of PRs, roughly: (1) AccessPass caps, methods, set args, CLI, SDK, and fixtures;
(2) the `GetAccessPassCommand` fallback; (3) config constants; (4) client `enable` plus its HTTP call;
(5) `ClientSeatV2` and its initialize instruction, with settlement integration possibly split out; (6)
oracle EdgeSeat AccessPass creation and the v2 no-auto-create branch; (7) the oracle `/user/create` API.

## Security Considerations

- **Unencrypted transport plus observed source IP.** The `/user/create` server is plain HTTP, and the
  signed message binds only the timestamp. An on-path observer who captures a valid request can replay it
  within the 30-second window; because the IP is taken from the connection rather than the signed
  payload, a replay from the attacker's own source address would create a `User` for that address under
  the victim's seat, consuming one slot of the per-category cap. The per-category caps bound the blast
  radius, and the short window limits the opportunity. Binding the IP (and/or pubkey) into the signed
  message is a straightforward future hardening if the residual risk is unacceptable.
- **Privileged signer.** The oracle is the feed authority and can create users with an arbitrary owner.
  `/user/create` gates that capability behind possession of the seat key: only a holder of the private
  key for `pubkey` can produce a valid signature.
- **Cap enforcement is onchain.** `try_add_user` runs inside `create_user_core`, so the limit holds
  regardless of how the user-create transaction is submitted, not only through the oracle.
- **Clock skew.** The 30-second tolerance assumes loosely synchronized clocks between clients and the
  oracle.

## Backward Compatibility

- **AccessPass accounts.** The four fields are appended and default to safe values on read (counts `0`,
  maxes `1`); the SDK readers already tolerate missing trailing fields. `connection_count` semantics are
  unchanged. Enforcement is gated on `EdgeSeat`, so Prepaid, validator, and RPC passes are unaffected.
- **`GetAccessPassCommand`.** The new dynamic-first lookup falls back to the exact-IP PDA, so existing
  per-IP passes still resolve.
- **Seats.** `ClientSeat` (v1) is fully retained, including the Feed Oracle's automatic user creation, so
  current subscribers see no change. `ClientSeatV2` is opt-in.
- **`doublezero enable`.** The added pre-step is a no-op for users that already have a `User` for their
  IP and for environments without a configured `feed_oracle_url`, so existing flows are preserved.

## Out of Scope

- **Device discovery.** Before a node's container is running, the operator may not know which device to
  connect to. Discovering or auto-selecting the device is out of scope here; for a seat, the device is
  fixed by the `ClientSeatV2` `device_key` chosen at purchase.
- **Partial withdrawal.** Withdrawing against only some of the passes purchased under a seat is out of
  scope for this RFC.

## Open Questions

- The exact signature scheme for `/user/create` — plain ASCII bytes (as specified) versus the Solana
  off-chain-message envelope — should be confirmed against how operators are expected to generate the
  signature.
- Whether `/user/create` ever needs to provision non-multicast (IBRL) users; this RFC assumes multicast
  subscribers, matching the Feed Oracle's current behavior.
- The precise `enable` gating when no seat or oracle is configured, so the command degrades gracefully
  rather than erroring for non-feed users.
- IP reuse while a stale user is still active: if a client IP is reassigned to a different node while an
  active-but-stale `User` for that IP still exists, the idempotent `/user/create` returns the existing
  user rather than provisioning the new node. Resolving this depends on BGP status marking the stale user
  down, and is deferred until BGP status information is fully enabled on mainnet.
- The settlement and auction integration for `ClientSeatV2`, which is the largest implementation surface
  and may warrant its own design note.
- The final DNS hostnames and base domain for the Feed Oracle constants.
