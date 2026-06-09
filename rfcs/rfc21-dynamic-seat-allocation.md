# RFC 21: Dynamic Seat Allocation

## Summary

**Status: `Approved`**

A DoubleZero AccessPass is currently bound to a single `(client_ip, user_payer)` pair, and the Feed
Oracle creates exactly one AccessPass and one serviceability `User` per IP. A buyer must therefore know
the client's public IP before access can be granted, and one purchase serves exactly one machine.

This RFC introduces dynamic seat allocation: a buyer purchases one seat that admits several users, and a
node onboards without registering its IP ahead of time. It adds per-category user caps to the AccessPass
(enforced for seat passes), reuses the existing dynamic IP-less AccessPass primitive, adds a
pubkey-keyed `ClientSeatV2` to the Feed Subscription Program, and scales the existing serviceability
`SetAccessPass` airdrop by the seat's caps so the seat keypair holds enough SOL to provision its own
users through the standard `doublezero connect` workflow. No new endpoint is added to the Feed Oracle.

## Motivation

Three limitations block common subscriber workflows today:

1. **The client IP must be known up front.** The AccessPass PDA is derived from `(client_ip,
   user_payer)`, so the buyer, or the Feed Oracle acting on their behalf, must know the machine's exact
   public IP before granting access. Operators behind NAT, with addresses assigned late in provisioning,
   or running fleets of machines cannot be onboarded cleanly.

2. **One seat serves one user.** A `ClientSeat` in the Feed Subscription Program is keyed on the
   subscriber's IP, and the Feed Oracle creates one AccessPass and one `User` per seat. An operator
   running several nodes must buy a seat per node; there is no concept of a seat that admits N machines.

3. **Onboarding needs a funded keypair.** Creating a `User` with `doublezero connect` is a transaction
   the operator's own keypair must pay for, so that keypair has to hold SOL. The serviceability program
   already addresses this for ordinary passes: `process_set_access_pass` airdrops enough lamports to the
   `user_payer` account to cover `User`-account rent and a configured amount for connect transactions.
   Scaling that airdrop by the seat's caps funds the seat keypair to pay for every node it provisions, so
   the operator never has to source SOL and the standard connect path is reused. This is preferred over a
   bespoke oracle endpoint that creates the `User` on the operator's behalf, which would add an
   authenticated HTTP surface for no benefit over the existing onchain airdrop.

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

- **Have the Feed Oracle create the `User` on demand.** An earlier version of this design added an
  authenticated `POST /user/create` endpoint to the oracle: `doublezero enable` would resolve the node's
  IP, sign a request with the seat key, and the oracle would create the `User` from the observed source
  IP, signing and paying so onboarding was gasless. This was dropped. It adds an authenticated HTTP
  surface to the oracle and a replay window (the source IP is observed, not signed), and it diverges from
  the standard connect path. Scaling the existing `SetAccessPass` airdrop and letting the operator run
  `doublezero connect` achieves the same gasless-from-the-operator's-wallet outcome with no new endpoint.

## Detailed Design

### End-to-end flow

1. A buyer creates a `ClientSeatV2` in the Feed Subscription Program, keyed on their client public key
   `C`, specifying `max_unicast_users` and `max_multicast_users`, on a chosen device.
2. The Feed Oracle observes the new seat and creates a **dynamic EdgeSeat AccessPass** on the DoubleZero
   ledger: PDA `accesspass(UNSPECIFIED, C)`, `accesspass_type = EdgeSeat(seat_pda)`,
   `allow_multiple_ip = true`, the two maxes copied from the seat, `user_payer = C`, `owner = oracle`.
   The `SetAccessPass` airdrop funds `C`, scaled by `max_unicast_users + max_multicast_users`, so the seat
   keypair holds enough SOL to pay for every node it will provision. The oracle also allowlists the feed
   multicast group on the pass (`mgroup_sub_allowlist`) so the subscribe in step 4 is permitted.
3. On each node, the operator configures the seat keypair `C` as the doublezero client keypair and runs
   `doublezero connect` (multicast subscriber).
4. `connect` resolves the node's public IP, looks up the AccessPass with the UNSPECIFIED-first lookup
   (section B) and finds the dynamic EdgeSeat pass at `accesspass(UNSPECIFIED, C)`, then submits a
   `create_user` transaction paid by the funded seat keypair `C`.
5. Onchain, `create_user_core` invokes `accesspass.try_add_user(Multicast)`, which enforces
   `multicast_user_count < max_multicast_users` because the pass is `EdgeSeat`.
6. The operator turns on the reconciler with the normal `doublezero enable`; the daemon brings up the
   tunnel and routes.

### NAT semantics

A dynamic seat admits several nodes, but each node must present its own public IP. DoubleZero connects
nodes over GRE tunnels. GRE has no TCP/UDP ports, so a NAT can only map one address to one address; it
cannot multiplex many nodes onto a single public address by port. Only one node per host public IP can
hold a tunnel at a time. An operator running several nodes behind a single NAT public IP cannot bring
them all up simultaneously; each node needs a distinct public address.

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

**Seat funding (airdrop scaling).** `process_set_access_pass` already airdrops lamports to the
`user_payer` account when a pass is set: enough to rent-exempt the `User` accounts a connect will create
(`AIRDROP_USER_RENT_LAMPORTS_BYTES`) plus the configured `globalstate.user_airdrop_lamports`, topping the
account up to that target (`saturating_sub(user_payer.lamports())`). For an `EdgeSeat` pass, scale this
target by the seat's total admitted users (`max_unicast_users + max_multicast_users`) — both the
rent-exemption sizing and the per-connect `user_airdrop_lamports` scale per user — so the seat keypair
(`user_payer = C`) holds enough SOL to pay for the `create_user` transaction of every node it provisions.
Non-`EdgeSeat` passes keep today's fixed airdrop. The top-up semantics are preserved, so re-setting a
pass (for example when the oracle updates the caps) refills the seat toward its scaled target. This is
what makes the seat keypair self-funding for the standard `doublezero connect` path; the oracle makes no
separate transfer.

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
benefits automatically. This is the key client-side enabler for seats: without it, the standard
`doublezero connect` cannot find a node's dynamic seat pass (which lives at `accesspass(UNSPECIFIED, C)`,
not at the node's exact IP) and provisioning would fail. The onchain `create_user` already tries both
PDAs, so this change aligns the read side with the write side; it does not alter onchain behavior.

### C. Client — no changes beyond the SDK lookup

The client requires no new code for seats beyond the section-B lookup change. The operator configures the
funded seat keypair `C` as the doublezero client keypair and provisions each node with the standard
`doublezero connect` (multicast subscriber). `connect` resolves the node's public IP the same way it
always does (via the daemon's `DiscoverClientIP`, so NAT is handled as today), and its `check_accesspass`
finds the dynamic seat pass through the section-B UNSPECIFIED-first lookup. The `create_user` transaction
is paid by `C`, which the scaled `SetAccessPass` airdrop has already funded. `doublezero enable` is
unchanged: it still just turns on the reconciler. There is no client-to-oracle call.

### D. Feed Subscription Program — `ClientSeatV2`

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

### E. Feed Oracle changes

Files under `crates/shred-oracle/src/` (`dz_ledger.rs`, `oracle.rs`).

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

Creating the pass funds the seat keypair automatically: the `SetAccessPass` airdrop (scaled by the caps,
section A) tops up `user_payer = client_pubkey` so it can pay for its own `create_user` transactions. The
oracle makes no separate SOL transfer. Because the operator subscribes to the feed multicast group
through the standard `connect`, the oracle must also allowlist that group on the pass: add it to
`mgroup_sub_allowlist` (via the subscriber-allowlist instruction), or the onchain subscribe check rejects
the subscription (`processors/multicastgroup/subscribe.rs` requires the group be present in the pass's
`mgroup_sub_allowlist`).

**2. Stop auto-creating users for v2 seats.** `oracle.rs::create_serviceability_users` currently creates
serviceability `User`s automatically via `create_subscribe_user`. Branch on seat version: v1 `ClientSeat`
keeps today's automatic path unchanged; v2 `ClientSeatV2` creates no users automatically — each node is
provisioned by its operator through the standard `doublezero connect` workflow (section C), paid by the
funded seat keypair.

## Impact

- **Serviceability program.** Four appended AccessPass fields, two small methods, two error variants, a
  hook in `create_user_core` and `delete`, two new `SetAccessPassArgs` fields, and the EdgeSeat-scoped
  scaling of the existing `SetAccessPass` airdrop. The change is additive and confined to the access-pass
  and user paths.
- **CLI and SDKs.** New flags on `access-pass set`, new display fields, three SDK layout updates, and a
  fixture regeneration. The `GetAccessPassCommand` UNSPECIFIED-first lookup is the only client-facing
  change.
- **Client.** No changes beyond the SDK lookup; provisioning uses the standard `doublezero connect`.
- **Feed Subscription Program.** A new account and instruction plus settlement and auction integration
  for v2 seats — the largest single work item.
- **Feed Oracle.** Changed AccessPass creation (dynamic EdgeSeat pass, feed-group allowlisting, and the
  auto-scaled airdrop that funds the seat) and a version branch in user provisioning. No new HTTP service.

This spans two repositories and well exceeds the project's ~500-line-per-PR guideline, so it should be
delivered as a sequence of PRs, roughly: (1) AccessPass caps, methods, set args, scaled airdrop, CLI,
SDK, and fixtures; (2) the `GetAccessPassCommand` fallback; (3) `ClientSeatV2` and its initialize
instruction, with settlement integration possibly split out; (4) oracle EdgeSeat AccessPass creation
(with feed-group allowlisting) and the v2 no-auto-create branch.

## Security Considerations

- **No new provisioning surface.** Users are created by the operator's own funded keypair through the
  standard `connect` path, using the same authorization as any other user. There is no bespoke HTTP
  endpoint to secure and no signed, replayable provisioning request. This is the main reason the design
  prefers funding the seat keypair over the earlier oracle `/user/create` endpoint, which carried an
  observed-source-IP replay window (see Alternatives Considered).
- **Cap enforcement is onchain.** `try_add_user` runs inside `create_user_core`, so the per-category
  limit holds regardless of how the user-create transaction is submitted.
- **Seat funding.** Creating an EdgeSeat pass airdrops SOL to the seat keypair, scaled by its caps. That
  SOL is controlled by the seat keyholder (the operator who purchased the seat) and is spendable on
  anything, not just `connect`. The airdrop cost is borne by the pass creator (the feed authority) and is
  bounded by the seat purchase and the caps, so a larger seat costs proportionally more to fund.

## Backward Compatibility

- **AccessPass accounts.** The four fields are appended and default to safe values on read (counts `0`,
  maxes `1`); the SDK readers already tolerate missing trailing fields. `connection_count` semantics are
  unchanged. Cap enforcement and the airdrop scaling are both gated on `EdgeSeat`, so Prepaid, validator,
  and RPC passes are unaffected and keep the existing fixed airdrop.
- **`GetAccessPassCommand`.** The new dynamic-first lookup falls back to the exact-IP PDA, so existing
  per-IP passes still resolve.
- **Seats.** `ClientSeat` (v1) is fully retained, including the Feed Oracle's automatic user creation, so
  current subscribers see no change. `ClientSeatV2` is opt-in.
- **`doublezero connect` / `enable`.** Both are unchanged. A seat node provisions through the same
  `connect` path as any other user; `enable` still only turns on the reconciler.

## Out of Scope

- **Device discovery.** Before a node's container is running, the operator may not know which device to
  connect to. Discovering or auto-selecting the device is out of scope here; for a seat, the device is
  fixed by the `ClientSeatV2` `device_key` chosen at purchase.
- **Partial withdrawal.** Withdrawing against only some of the passes purchased under a seat is out of
  scope for this RFC.

## Open Questions

- Whether seats ever need to provision non-multicast (IBRL) users; this RFC assumes multicast
  subscribers, matching the Feed Oracle's current behavior.
- IP reuse while a stale user is still active: if a client IP is reassigned to a different node while an
  active-but-stale `User` for that IP still exists, the `User` PDA (keyed on `(client_ip, user_type)`)
  already exists, so the new node's `connect` collides with it rather than provisioning cleanly.
  Resolving this depends on BGP status marking the stale user down, and is deferred until BGP status
  information is fully enabled on mainnet.
- How the scaled airdrop should be tuned for large seats, and whether spent-down seat balances should be
  refilled on a schedule or only when the pass is re-set.
- The settlement and auction integration for `ClientSeatV2`, which is the largest implementation surface
  and may warrant its own design note.
