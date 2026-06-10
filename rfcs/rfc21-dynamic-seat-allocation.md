# RFC 21: Dynamic Seat Allocation

## Summary

**Status: `Draft`**

A DoubleZero AccessPass is currently bound to a single `(client_ip, user_payer)` pair, and the Feed
Oracle creates exactly one AccessPass and one serviceability `User` per IP. A buyer must therefore know
the client's public IP before access can be granted, and one purchase serves exactly one machine.

This RFC introduces dynamic seat allocation: a buyer purchases one entitlement that admits several
users, and a node onboards without registering its IP or device ahead of time. It adds per-category user
caps to the AccessPass (enforced for seat passes), reuses the existing dynamic IP-less AccessPass
primitive, and adds a `ConnectionTicket` account to the Feed Subscription Program. The ticket takes
custody of USDC up front and encodes the seat's caps, its funding authority, and the service key. A Feed
Oracle loop (`process_connection_ticket_requests`, modeled on the existing instant-allocation processing)
turns each ticket into an EdgeSeat AccessPass; a second loop reconciles the DoubleZero ledger's `User`
accounts against tickets, composing a per-connection `ClientSeat` and `PaymentEscrow` and then funding
several epochs of service from the ticket's custody — granting the seat and collecting the active epoch's
payment into settlement — for each node that actually connects. The existing `SetAccessPass` airdrop is
scaled by the seat's caps so the service keypair holds enough SOL to provision its own users through the
standard `doublezero connect` workflow. No new endpoint is added to the Feed Oracle. The entire
`ConnectionTicket` surface and the oracle's new loops are gated behind the `development` cargo feature.

## Motivation

Three limitations block common subscriber workflows today:

1. **The client IP must be known up front.** The AccessPass PDA is derived from `(client_ip,
   user_payer)`, so the buyer, or the Feed Oracle acting on their behalf, must know the machine's exact
   public IP before granting access. Operators behind NAT, with addresses assigned late in provisioning,
   or running fleets of machines cannot be onboarded cleanly.

2. **One purchase serves one machine, on one fixed device.** A `ClientSeat` in the Feed Subscription
   Program is keyed on the subscriber's IP, and the Feed Oracle creates one AccessPass and one `User` per
   seat. An operator running several nodes must buy a seat per node; there is no concept of a single
   purchase that admits N machines. The device is also pinned at purchase, so an operator cannot let a
   node attach to whichever device its latency resolves.

3. **Onboarding needs a funded keypair.** Creating a `User` with `doublezero connect` is a transaction
   the operator's own keypair must pay for, so that keypair has to hold SOL. The serviceability program
   already addresses this for ordinary passes: `process_set_access_pass` airdrops enough lamports to the
   `user_payer` account to cover `User`-account rent and a configured amount for connect transactions.
   Scaling that airdrop by the seat's caps funds the service keypair to pay for every node it provisions,
   so the operator never has to source SOL and the standard connect path is reused. This is preferred
   over a bespoke oracle endpoint that creates the `User` on the operator's behalf, which would add an
   authenticated HTTP surface for no benefit over the existing onchain airdrop.

The serviceability program already contains most of the primitives this needs. An AccessPass can be
created with `client_ip = 0.0.0.0` and `ALLOW_MULTIPLE_IP` set, and `create_user` already
accepts either the exact-IP PDA or the `UNSPECIFIED` PDA when validating a new `User`. The
`AccessPassType::EdgeSeat(Pubkey)` variant already exists. What is missing is a way to bound how many
users a single pass admits, a purchase object that custodies funds and is not bound to an IP or device,
and a path that draws on the existing per-connection `ClientSeat`/`PaymentEscrow`/settlement machinery as
nodes connect. This RFC supplies those pieces and wires them together.

## New Terminology

- **Feed Oracle** — the off-chain service that watches the Feed Subscription Program and provisions
  access on the DoubleZero ledger.
- **Feed Subscription Program** — the Solana program that sells and settles feed subscription seats.
- **ConnectionTicket** — the new, development-gated purchase object. Keyed on the service key, it
  custodies USDC and carries `max_unicast_users`, `max_multicast_users`, `funding_authority_key`, and
  `service_key`. It is not bound to an IP or device.
- **Per-connection seat** — the existing `ClientSeat` (plus its `PaymentEscrow`), created on demand by
  the Feed Oracle for each connected `(device, client_ip)`. The ticket sits above these; one ticket
  yields up to its cap of per-connection seats.
- **Service key** — the doublezero client keypair the operator configures (`= User.owner =
  AccessPass.user_payer`). The holder connects, disconnects, and moves nodes.
- **Funding authority** — the entity that funded the ticket's USDC. It becomes each per-connection
  `PaymentEscrow`'s `withdraw_authority_key`, so it can request withdrawal and receive refunds.
- **Dynamic AccessPass** — an AccessPass created with `client_ip = 0.0.0.0` (UNSPECIFIED) and
  `ALLOW_MULTIPLE_IP` set, so users from any source IP may attach.
- **EdgeSeat AccessPass** — an AccessPass whose `accesspass_type` is `EdgeSeat(connection_ticket_pda)`.
  Per-category user caps are enforced only for this type.
- **Per-category caps** — separate current and maximum user counts for IBRL (unicast) users and for
  multicast users.

## Alternatives Considered

- **A pubkey-keyed `ClientSeatV2` seat.** An earlier version of this design added a `ClientSeatV2`
  account keyed on the client public key, created directly by the buyer and turned into an EdgeSeat pass
  by the oracle, with the device fixed at purchase. The `ConnectionTicket` model is preferred: a
  custody-holding ticket that sits above the existing per-connection `ClientSeat`s decouples purchase
  from connection, lets the device float (it is resolved by latency at connect time and may change), and
  reuses the v1 seat, escrow, and settlement machinery per connection instead of forking a parallel v2
  lifecycle.

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

The `ConnectionTicket` account, its instructions (sections D), and the Feed Oracle's new reconciliation
(section E) are gated behind the `development` cargo feature, following the same pattern the program
already uses for the feed-registry instructions. The serviceability changes (sections A, B, C) are
additive and backward-compatible and are **not** gated.

### End-to-end flow

1. A buyer calls `InitializeConnectionTicket { max_unicast_users, max_multicast_users, usdc_amount }` in
   the Feed Subscription Program. This creates a `ConnectionTicket` keyed on the service key `C`, funds a
   ticket-owned USDC token account with `usdc_amount`, and records the funding authority, the service
   key, and the two caps.
2. The Feed Oracle's `process_connection_ticket_requests` loop (section E.1) observes the new ticket and
   creates a **dynamic EdgeSeat AccessPass** on the DoubleZero ledger: PDA `accesspass(UNSPECIFIED, C)`,
   `accesspass_type = EdgeSeat(ticket_pda)`, `allow_multiple_ip = true`, the two maxes copied from the
   ticket, `user_payer = C`, `owner = oracle`. The `SetAccessPass` airdrop funds `C`, scaled by
   `max_unicast_users + max_multicast_users`, so the service keypair holds enough SOL to pay for every
   node it will provision. The oracle also allowlists the feed multicast group on the pass
   (`mgroup_sub_allowlist`) so the subscribe in step 4 is permitted. This step happens before the operator
   connects, so the pass is already in place when `connect` looks it up.
3. On each node, the operator configures the service keypair `C` as the doublezero client keypair and
   runs `doublezero connect` (multicast subscriber). The device is resolved by the standard latency-based
   device selection at connect time; it is not fixed at purchase and may differ per node or change on
   reconnect.
4. `connect` resolves the node's public IP, looks up the AccessPass with the UNSPECIFIED-first lookup
   (section B) and finds the dynamic EdgeSeat pass at `accesspass(UNSPECIFIED, C)`, then submits a
   `create_user` transaction paid by the funded service keypair `C`.
5. Onchain, `create_user_core` invokes `accesspass.try_add_user(Multicast)`, which enforces
   `multicast_user_count < max_multicast_users` because the pass is `EdgeSeat`, and writes the `User`
   (`owner = C`).
6. The Feed Oracle's reconcile loop (section E.2) fetches all `User` accounts on the DoubleZero ledger,
   joins `User.owner == ticket.service_key`, and for each newly observed `User` composes the
   per-connection accounts and funds them from the ticket. It first ensures the
   `ClientSeat(device_pk, client_ip)` and its `PaymentEscrow(withdraw_authority =
   ticket.funding_authority_key)` exist (via the existing `InitializeClientSeat` /
   `InitializePaymentEscrow`), then calls `AllocateClientSeatFromConnectionTicket { client_ip_bits,
   num_epochs }`. That instruction funds `num_epochs` of service into the escrow from the ticket's custody
   (using the same proration as the instant-allocation path), grants the seat (`tenure_epochs`,
   `active_epoch`, `subscription_start_slot`, plus the device-capacity check), and collects the active
   epoch's payment into the settled `ShredDistribution`.
7. The operator turns on the reconciler with the normal `doublezero enable`; the daemon brings up the
   tunnel and routes.
8. If the operator later disconnects a node, moves it to another machine, or it reconnects to a different
   device, the reconcile loop observes the `User` disappear or its `device_pk`/`client_ip` change and
   allocates a fresh seat for the new `(device, client_ip)`. The stale per-connection seat is left to
   lapse through the normal settlement flow; there is no path that refunds its USDC back into the ticket.
   Funds that were never allocated to a seat remain in the ticket and are recoverable by the funding
   authority with `WithdrawConnectionTicketUsdc`. No client action beyond the normal connect/disconnect is
   required; the operator's activity on the ledger is what drives the oracle to sync.

### NAT semantics

A dynamic ticket admits several nodes, but each node must present its own public IP. DoubleZero connects
nodes over GRE tunnels. GRE has no TCP/UDP ports, so a NAT can only map one address to one address; it
cannot multiplex many nodes onto a single public address by port. Only one node per host public IP can
hold a tunnel at a time. An operator running several nodes behind a single NAT public IP cannot bring
them all up simultaneously; each node needs a distinct public address.

### A. Serviceability — per-category user caps on AccessPass

File: `smartcontract/programs/doublezero-serviceability/src/state/accesspass.rs`. This section is **not**
behind the `development` feature; it is an additive, backward-compatible change to the serviceability
program.

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
rent-exemption sizing and the per-connect `user_airdrop_lamports` scale per user — so the service keypair
(`user_payer = C`) holds enough SOL to pay for the `create_user` transaction of every node it provisions.
Non-`EdgeSeat` passes keep today's fixed airdrop. The top-up semantics are preserved, so re-setting a
pass (for example when the oracle updates the caps) refills the service keypair toward its scaled target.
This is what makes the service keypair self-funding for the standard `doublezero connect` path; the
oracle makes no separate SOL transfer. This SOL airdrop is independent of the ticket's USDC custody: SOL
pays for gas, USDC pays the subscription fee.

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
funded service keypair `C` as the doublezero client keypair and provisions each node with the standard
`doublezero connect` (multicast subscriber). `connect` resolves the node's public IP the same way it
always does (via the daemon's `DiscoverClientIP`, so NAT is handled as today) and selects a device with
the standard latency-based device resolution — the device is not fixed by the purchase and may differ per
node or change on reconnect. Its `check_accesspass` finds the dynamic seat pass through the section-B
UNSPECIFIED-first lookup. The `create_user` transaction is paid by `C`, which the scaled `SetAccessPass`
airdrop has already funded. `doublezero enable` is unchanged: it still just turns on the reconciler.
There is no client-to-oracle call; the operator's connect, disconnect, and move activity on the ledger is
what drives the oracle to sync (section E).

### D. Feed Subscription Program — `ConnectionTicket`

Files under `programs/shred-subscription/src/` (`state/`, `instruction/`, `processor/`). This entire
section is gated behind the `development` cargo feature, mirroring the existing feed-registry
instructions: the new instruction enum variants are `#[cfg(feature = "development")]`, as are their
processor dispatch arms, imports, and the new state module. A mainnet-beta build excludes them entirely.

**`ConnectionTicket` account.** Modeled on the existing `Pod`/`Zeroable` `#[repr(C, align(8))]` state
(see `state/client_seat.rs` and `state/payment_escrow.rs`):

| Aspect          | `ConnectionTicket`                                                              |
| --------------- | ------------------------------------------------------------------------------- |
| Discriminator   | `dz::account::connection_ticket::v1`                                            |
| PDA seeds       | `[b"connection_ticket", service_key]`                                           |
| Identity        | `service_key: Pubkey` (the doublezero client keypair)                           |
| Funding         | `funding_authority_key: Pubkey` (becomes each escrow's withdraw authority)      |
| Balance         | `usdc_balance: u64` — on-account ledger, kept in lockstep with the token PDA    |
| Caps            | `max_unicast_users: u16`, `max_multicast_users: u16`                            |
| USDC custody    | a program token PDA whose authority is the ticket PDA; the ticket stores its bump |

The ticket is not bound to an IP or device. USDC custody is a program-owned token PDA (the same
convention the device-history USDC account uses), with `usdc_balance` mirroring its balance — credited on
deposit and debited on allocation and withdrawal in the same instruction as the matching token transfer.
The ticket carries no settlement state of its own; settlement and proration live on the per-connection
`ClientSeat`s.

**`InitializeConnectionTicket { service_key, max_unicast_users, max_multicast_users, usdc_amount }`.**
Modeled on `try_initialize_client_seat` plus the `FundPaymentEscrowUsdc` SPL transfer. On first call it
creates the ticket PDA and its USDC token account and records `service_key`, `funding_authority_key`, and
the two caps; on later calls it is idempotent and simply **tops up** `usdc_balance` (the same funding
authority is required). It transfers `usdc_amount` USDC into the ticket's token account, rejects a
zero-amount deposit, and splits the writable rent **payer** from the read-only **funding authority** that
signs the transfer. The instruction is deliberately basic: it takes custody of USDC and stores the caps;
it performs no allocation or settlement.

**`AllocateClientSeatFromConnectionTicket { client_ip_bits, num_epochs }`** (oracle-driven; the shred
oracle is a read-only signer that only authorizes the call). The instruction the oracle calls in step 6,
for each newly observed `User`. The `ClientSeat(device_pk, client_ip)` and its
`PaymentEscrow(withdraw_authority = ticket.funding_authority_key)` must be **composed ahead of the call**
by the existing `InitializeClientSeat` / `InitializePaymentEscrow`; this instruction validates their
fields rather than creating them. It then:

- funds `num_epochs × prorated_price` of service into the escrow from the ticket's custody, reverting if
  `ticket.usdc_balance` cannot cover it (the per-epoch price uses the same `prorated_usdc_amount_ceil`
  calculation as `try_request_instant_seat_allocation`, over the slots remaining in the epoch);
- **grants** the seat — `tenure_epochs = 1`, `active_epoch`, `subscription_start_slot`, the device
  seat-capacity check, and the granted-seat accounting — mirroring `try_request_instant_seat_allocation`;
- **collects** the active epoch's payment into the settled `ShredDistribution` (`client_seat_count`,
  `collected_usdc_payments`), mirroring `try_ack_instant_seat_allocation`.

Pre-funding `num_epochs` lets a seat survive multiple epochs without a per-epoch oracle call; the active
epoch is paid immediately, and the remainder sits in the escrow for the normal settlement flow to draw
on. The price, proration, and escrow reuse the per-connection `ClientSeat`'s existing fields and methods,
so a ticket-funded seat flows through the same settlement and auction logic as any other.

**`WithdrawConnectionTicketUsdc { usdc_amount }`.** The funding authority withdraws **unallocated** ticket
USDC back to its own token account. It rejects an amount larger than `usdc_balance` and debits the ledger
in lockstep with the transfer. This is the ticket-level capital-recovery path; funds already committed to
a per-connection escrow are recovered through that escrow's existing withdrawal (also to the funding
authority), not through the ticket.

**Move and disconnect.** There is no instruction that moves USDC from a seat or escrow back into a
ticket. On a move or disconnect the oracle allocates a fresh seat for the new `(device, client_ip)` and
lets the stale seat lapse through the normal settlement flow; unallocated ticket USDC remains recoverable
by the funding authority via `WithdrawConnectionTicketUsdc`.

The v1 `ClientSeat` instant/batch/fund/withdraw instructions are unchanged; the ticket flow is additive
and dev-gated, and reuses their machinery rather than duplicating it.

### E. Feed Oracle changes

Files under `crates/shred-oracle/src/` (`dz_ledger.rs`, `oracle.rs`, `chain.rs`). The oracle crate's
`development` feature forwards to the program's (`development =
["doublezero-shred-subscription/development"]`), and the new loops below are guarded by
`#[cfg(feature = "development")]`, exactly like the existing `reconcile_feeds()` call. A default
(mainnet) build excludes them.

**E.1. `process_connection_ticket_requests` — create an EdgeSeat AccessPass for each ticket.** A new loop
modeled directly on `process_instant_allocation_requests` (`oracle.rs`), invoked from the
`handle_open_for_requests` loop on the same `request_poll_interval` and during the same `OpenForRequests`
phase. It fetches `ConnectionTicket` accounts with a new chain helper `get_connection_tickets()` modeled
on `get_instant_allocation_requests()` (`chain.rs`) — `getProgramAccounts` plus a `ConnectionTicket`
discriminator memcmp, skipping zero-lamport accounts. Unlike instant-allocation requests, tickets are
**persistent** (they are not consumed by the oracle), so the loop **reconciles** idempotently — probe,
then create only what is missing — rather than draining one-shot requests.

For each ticket it ensures the EdgeSeat AccessPass exists on the DoubleZero ledger. It probes with
`fetch_access_pass_user_payer(UNSPECIFIED, service_key)` (`dz_ledger.rs`); if the pass is absent it
creates it. Today `dz_ledger.rs::set_access_pass` hardcodes `accesspass_type = Prepaid`, a specific
`client_ip`, and `allow_multiple_ip = false`; add a sibling helper (e.g. `set_edge_seat_access_pass`)
that builds a `SetAccessPass` with:

- `accesspass_type = EdgeSeat(connection_ticket_pda)`
- `client_ip = UNSPECIFIED` (PDA `get_accesspass_pda(UNSPECIFIED, service_key)`)
- `allow_multiple_ip = true`
- `max_unicast_users` and `max_multicast_users` copied from the ticket (the extended `SetAccessPassArgs`
  from section A)
- `user_payer = service_key` (the ticket's service key, rather than the oracle's key or the validator's
  withdraw authority used in the v1 path)

The oracle signs as the GlobalState `feed_authority` (or a foundation-allowlist member, as it already
must be to set passes today). `set_access_pass` records `owner = payer = oracle`, while `user_payer` is
the service key — `process_set_access_pass` already supports this split and lets the feed authority create
and later update passes it owns. Creating the pass funds the service keypair automatically: the
`SetAccessPass` airdrop (scaled by the caps, section A) tops up `user_payer = service_key` so it can pay
for its own `create_user` transactions. The oracle makes no separate SOL transfer. Because the operator
subscribes to the feed multicast group through the standard `connect`, the loop also allowlists that group
on the pass with the existing `add_mgroup_sub_allowlist` (`dz_ledger.rs`), or the onchain subscribe check
rejects the subscription (`processors/multicastgroup/subscribe.rs` requires the group be present in the
pass's `mgroup_sub_allowlist`). Like the instant path, each step is wrapped in `retry_idempotent_send` and
retried on the next poll. **This loop does not create users** — the operator runs `doublezero connect`
itself (section C).

**E.2. Reconcile users against tickets and fund their seats.** A second loop fetches all `User` accounts
on the DoubleZero ledger with `fetch_all_users()` (this is RPC-intensive — it pulls every `User`
regardless of owner) and joins them against the tickets by `User.owner == ticket.service_key`. For each
joined `User` it composes the per-connection accounts and funds them from the ticket: it ensures the
`ClientSeat(device_pk, client_ip)` and `PaymentEscrow(withdraw_authority = ticket.funding_authority_key)`
exist (via the existing `InitializeClientSeat` / `InitializePaymentEscrow`), then calls
`AllocateClientSeatFromConnectionTicket { client_ip_bits, num_epochs }` (section D), which funds
`num_epochs` of service from the ticket, grants the seat, and collects the active epoch into the settled
`ShredDistribution`. When a previously seen `User` disappears (disconnect) or its `device_pk`/`client_ip`
changes (move), the loop allocates a seat for the new `(device, client_ip)` and lets the stale seat lapse
through settlement; it does not refund into the ticket. This passive reconciliation is the "poke": the
operator's ordinary connect, disconnect, and move activity on the ledger is what the oracle observes and
syncs; there is no client-to-oracle call.

**E.3. Leave the v1 auto-create path unchanged.** `oracle.rs::create_serviceability_users` continues to
create serviceability `User`s automatically via `create_subscribe_user` for v1 `ClientSeat`s. The ticket
flow does **not** auto-create users — each node is provisioned by its operator through the standard
`doublezero connect` workflow (section C), paid by the funded service keypair — and instead reconciles
the resulting `User`s after the fact.

## Impact

- **Serviceability program.** Four appended AccessPass fields, two small methods, two error variants, a
  hook in `create_user_core` and `delete`, two new `SetAccessPassArgs` fields, and the EdgeSeat-scoped
  scaling of the existing `SetAccessPass` airdrop. The change is additive, ungated, and confined to the
  access-pass and user paths.
- **CLI and SDKs.** New flags on `access-pass set`, new display fields, three SDK layout updates, and a
  fixture regeneration. The `GetAccessPassCommand` UNSPECIFIED-first lookup is the only client-facing
  change.
- **Client.** No changes beyond the SDK lookup; provisioning uses the standard `doublezero connect` with
  latency-based device selection.
- **Feed Subscription Program.** A new development-gated `ConnectionTicket` account and three
  instructions (`InitializeConnectionTicket`, `AllocateClientSeatFromConnectionTicket`,
  `WithdrawConnectionTicketUsdc`) that reuse the existing per-connection
  `ClientSeat`/`PaymentEscrow`/settlement machinery.
- **Feed Oracle.** Two new development-gated loops: `process_connection_ticket_requests` for
  ticket-to-EdgeSeat-AccessPass creation (with feed-group allowlisting and the auto-scaled airdrop), and
  an RPC-intensive full-`User` reconciliation that composes and funds per-connection seats. The v1
  auto-create path is unchanged. No new HTTP service.

This spans two repositories and well exceeds the project's ~500-line-per-PR guideline, so it should be
delivered as a sequence of PRs, roughly: (1) AccessPass caps, methods, set args, scaled airdrop, CLI,
SDK, and fixtures; (2) the `GetAccessPassCommand` fallback; (3) the `ConnectionTicket` account with its
three instructions (dev-gated); (4) the oracle's `process_connection_ticket_requests` AccessPass loop
(dev-gated); (5) the oracle's user-reconciliation funding loop (dev-gated). The on-chain instructions
in (3) landed together in doublezero-shreds PR 465.

## Security Considerations

- **No new provisioning surface.** Users are created by the operator's own funded keypair through the
  standard `connect` path, using the same authorization as any other user. There is no bespoke HTTP
  endpoint to secure and no signed, replayable provisioning request. The oracle learns of connects,
  disconnects, and moves passively, by reconciling ledger `User` accounts. This is the main reason the
  design prefers passive reconciliation over the earlier oracle `/user/create` endpoint, which carried an
  observed-source-IP replay window (see Alternatives Considered).
- **Cap enforcement is onchain.** `try_add_user` runs inside `create_user_core`, so the per-category
  limit holds regardless of how the user-create transaction is submitted.
- **USDC custody.** The ticket holds USDC in a program token PDA whose authority is the ticket PDA; only
  program instructions move it, and the on-account `usdc_balance` ledger is debited in the same
  instruction as every transfer so it cannot drift. The funding authority can withdraw any **unallocated**
  ticket balance at will via `WithdrawConnectionTicketUsdc`. Each per-connection escrow's withdraw
  authority is pinned to the ticket's `funding_authority_key`, so funds already committed to a seat are
  likewise recoverable only by the funder.
- **Caps are advisory in the subscription program.** `AllocateClientSeatFromConnectionTicket` does not
  bound the number of seats it funds by the ticket's `max_*_users`; those caps are copied onto the
  EdgeSeat AccessPass, and the per-category limit is enforced where users are actually created, in
  `create_user_core` (section A).
- **Seat funding (SOL).** Creating an EdgeSeat pass airdrops SOL to the service keypair, scaled by its
  caps. That SOL is controlled by the service keyholder and is spendable on anything, not just `connect`.
  The airdrop cost is borne by the pass creator (the feed authority) and is bounded by the caps, so a
  larger ticket costs proportionally more to fund.
- **RPC load.** The user-reconciliation loop fetches every `User` on the DoubleZero ledger each cycle.
  This is an RPC and processing cost that grows with the network's user count and should be bounded
  (paginated or made incremental) before mainnet. It is dev-gated for now.

## Backward Compatibility

- **AccessPass accounts.** The four fields are appended and default to safe values on read (counts `0`,
  maxes `1`); the SDK readers already tolerate missing trailing fields. `connection_count` semantics are
  unchanged. Cap enforcement and the airdrop scaling are both gated on `EdgeSeat`, so Prepaid, validator,
  and RPC passes are unaffected and keep the existing fixed airdrop.
- **`GetAccessPassCommand`.** The new dynamic-first lookup falls back to the exact-IP PDA, so existing
  per-IP passes still resolve.
- **`ConnectionTicket` and its instructions.** Gated behind the `development` feature and excluded from
  mainnet-beta builds, so they cannot affect a production program. The v1 `ClientSeat` lifecycle,
  including the Feed Oracle's automatic user creation, is fully retained, so current subscribers see no
  change.
- **`doublezero connect` / `enable`.** Both are unchanged. A seat node provisions through the same
  `connect` path as any other user; `enable` still only turns on the reconciler.

## Out of Scope

- **Device discovery before connect.** Before a node's container is running, the operator may not know
  which device it will attach to. The device is now resolved by the standard latency-based selection at
  connect time and may differ per node or change on reconnect; any richer auto-selection or
  pre-provisioning of the device is out of scope here.
- **Refunding committed seat funds into a ticket.** The funding authority can withdraw a ticket's
  unallocated balance (`WithdrawConnectionTicketUsdc`) and can withdraw from a per-connection escrow
  through the existing path, but moving USDC from a seat or escrow back into the ticket — so a moved or
  disconnected seat's remainder is recycled within the ticket — is out of scope for this RFC.

## Open Questions

- Whether seats ever need to provision non-multicast (IBRL) users; this RFC assumes multicast
  subscribers, matching the Feed Oracle's current behavior.
- IP reuse while a stale user is still active: if a client IP is reassigned to a different node while an
  active-but-stale `User` for that IP still exists, the `User` PDA (keyed on `(client_ip, user_type)`)
  already exists, so the new node's `connect` collides with it rather than provisioning cleanly.
  Resolving this depends on BGP status marking the stale user down, and is deferred until BGP status
  information is fully enabled on mainnet.
- How the scaled SOL airdrop and the ticket's USDC custody should be tuned for large tickets, and whether
  spent-down balances should be topped up on a schedule or only when the pass/ticket is re-set.
- Bounding the RPC cost of the full-`User` reconciliation (pagination or an incremental/cursor approach)
  before this leaves the `development` feature.
- How many epochs the oracle should pre-fund per `AllocateClientSeatFromConnectionTicket` call
  (`num_epochs`) and how it re-funds seats as epochs are consumed; if a device is at seat capacity the
  allocation reverts, so the oracle must retry once capacity frees up.
- Whether and how to reclaim pre-funded-but-unused escrow epochs when a node moves or disconnects, given
  there is no refund-into-ticket path today.
- The settlement and auction integration for ticket-funded per-connection seats, which is the largest
  implementation surface and may warrant its own design note.
