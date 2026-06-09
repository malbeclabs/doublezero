# RFC: Feed Subscription System

## Summary

**Status: `Draft`**

This RFC defines the DoubleZero Edge **feed-subscription** system. This is an evolution of the existing shred subscription system, with the updated system including - an onchain catalog of purchasable feeds (the `feed-subscription` program), per-feed purchase and entitlement, an offchain oracle (`feed-oracle`) that reads the onchain catalog and provisions network access for funded seats.

The body of this RFC describes the desired end state. The existing shred subscription architecture on which this RFC is based is summarized in Appendix A for context. Shreds is retrofitted as the first registered feed.

## Motivation

The subsystem was originally built to deliver one product, Solana shreds, over a small set of multicast groups fixed in oracle configuration: a global shreds group plus per-region rebroadcast (`rebop`) groups, each assigned to a seat by its exchange. DoubleZero now wants to allow users to access to an arbitrary set of feeds from a catalog: additional market-data feeds (the Hyperliquid and Phoenix perp feeds in the near term), and later third-party and prediction-market feeds, with flexibility in how buyers pay. The shred-specific special-casing does not scale to that. There is no catalog, the multicast groups are oracle configuration, and the purchase path is wired to a single product.

There is also a documentation gap. No RFC describes this subsystem, even though it is one of the more intricate parts of the system: an epoch state machine, a seat auction, settlement, a pricing algorithm, validator-reward distribution, and an offchain bridge to network access. This RFC fills that gap and, framed around the generalization and rename, becomes the architecture of record going forward.

## New Terminology

- **Feed:** A named, priced subscription target identified by exactly one multicast group.
- **Feed catalog / registry:** The set of feeds, held onchain in a `FeedRegistry` account and managed via the admin CLI.
- **Feed pricing mode:** How a feed is priced. `DynamicDeviceSeats` (the existing device-seat machinery; used by shreds) or `Flat` (a fixed per-subscription-epoch price).
- **Client seat:** A funded subscription on a device, tracked onchain (`ClientSeat`), with an associated USDC payment escrow.
- **Subscription epoch:** The billing period, aligned to a DoubleZero epoch.
- **Feed oracle (today: shred oracle):** The offchain service that drives the epoch state machine and pricing, reads the onchain feed catalog, and provisions network access for funded seats.
- **Serviceability access pass:** The DoubleZero Ledger access primitive whose per-seat multicast subscribe allowlist the oracle writes.
- **Rebop:** DoubleZero's regional rebroadcast of shreds, delivered over per-region multicast groups and assigned to a seat by the seat's exchange.

## Alternatives Considered

**Do nothing (stay shred-specific).** Rejected; it cannot serve a catalog of feeds.

**Greenfield service instead of generalizing in place.** Build a new entitlement service rather than extending the existing program and oracle. Rejected. The oracle is already deployed and owns the serviceability bridge, the idempotent reconciliation, the retry logic, and the formal specs; a greenfield service would re-implement all of it.

**Offchain-only catalog.** Keep the catalog purely in oracle configuration or a database. Rejected; the set of purchasable feeds would not be onchain-auditable or readable by other onchain logic.

**Feed as a bundle of multicast groups.** Model a feed as a set of groups (for example, a regional bundle). Rejected. Keeping feed identity equal to exactly one multicast group makes the group the account's identity and guarantees uniqueness. Bundles are better expressed at the entitlement layer as a set of feeds.

**Repurpose existing device/seat state.** Rejected. A feed is parallel to, not entangled with, the device-seat machinery, which this RFC leaves untouched.

**Onchain enforcement of per-feed payment.** Out of scope. Payment validation remains offchain, consistent with the protocol's direct-payment model; this RFC models the catalog and entitlement, not billing enforcement.

**Defer the rename.** Keep the `shred-*` names indefinitely. Rejected as the end state; once feeds are first-class the shred-specific naming is misleading. The rename is staged so it does not interleave with the feature work.

## Detailed Design

This section describes the target state.

### Architecture overview

```
            GENERALIZED ARCHITECTURE — Feed Subscription   (✦ = new)

   Buyer / validator host        Operator           Feed publishers ✦
   ┌────────────────────────┐   ┌────────────┐     ┌──────────────────┐
   │ CLI: pay / ✦ buy feed  │   │ admin CLI ✦│     │ per-feed supply: │
   │ doublezerod daemon     │   └─────┬──────┘     │ shreds, perp, …  │
   └──────────┬─────────────┘  (a) register/       └────────┬─────────┘
              │ (1) pay /       enable/price                │ push feeds
              │     ✦ buy feed        │                     ▼
              ▼                       ▼          ┌──────────────────────┐
   ┌─────────────────────────────────────┐       │ DZ Edge (data plane) │
   │ SOLANA                              │       │ delivers ALL         │
   │ feed-subscription program ✦         │       │ purchased feeds      │
   │  ClientSeat · PaymentEscrow         │       └───────────▲──────────┘
   │  ShredDistribution · pricing        │      (5) subscribe│
   │  ✦ Feed + FeedRegistry              │      (6) tunnel   │
   │  ✦ Buy / Withdraw Feed instructions │       ┌───────────┴──────────┐
   └────┬─────────────▲──────────────────┘       │ DoubleZero Ledger    │
        │ (2) read    │ (3)                      │ serviceability:      │
        │ seats+feeds │ drive                    │  AccessPass +        │
        ▼             │ epoch                    │  allowlist (per      │
   ┌──────────────────┴─────────────┐            │  purchased feed)     │
   │ feed-oracle (offchain) ✦       │            └────────────▲─────────┘
   │  oracle key · pricing · rewards│   (4) write allowlist   │
   │  ✦ read catalog → provision    │     for purchased feeds─┘
   │  ✦ seat → feeds subscription   │
   │  + rebop (exchange groups)     │
   └────────────────────────────────┘

   ✦ Feed + FeedRegistry — onchain catalog of purchasable feeds (shreds + new)
   ✦ Buy / Withdraw Feed IX — per-feed purchase for Flat-priced feeds
   (a) operator registers feeds onchain via the admin CLI (oracle-signed)
   ✦ subscriptions are driven by the catalog (unioned with rebop), so a seat
     is provisioned for exactly the feeds its purchase entitles
```

### The onchain feed catalog

A **feed** is a named, priced subscription target identified by exactly one multicast group, where the group is the feed account's identity. One feed per group. Feeds are held in per-feed accounts and enumerated through a single global **`FeedRegistry`** account: a client reads the registry once to get every feed's group, then fetches each feed. The registry is capped at 256 feeds; this limit can be raised or replaced with a growable index in the future.

Each feed carries its multicast group, an enabled flag, a pricing mode, a per-epoch price (meaningful only for `Flat` feeds), and a display name, and is identifiable by its PDA. Removal is a **soft disable** (a flag), not deletion, so a feed keeps its registry slot; the shreds (`DynamicDeviceSeats`) feed cannot be disabled. A feed's name and pricing mode are immutable after registration; its enabled flag and `Flat` price are mutable.

```
                ONCHAIN FEED REGISTRY — data model & access

   ┌────────────────────────────────┐
   │ feed oracle key (only writer)  │   via admin CLI
   └───────────────┬────────────────┘
                   │  initialize registry · register feed
                   │  set feed enabled · update flat price
                   ▼
  ┌──────────────────────────────────────────────────────────────────────┐
  │ feed-subscription program (Solana)                                   │
  │                                                                      │
  │  FeedRegistry  (singleton)                                           │
  │  ┌───────────────────────────────────────┐                           │
  │  │ enumeration index: every feed's group │                           │
  │  └─────────────────────┬─────────────────┘                           │
  │                        │ each group → its Feed account               │
  │           ┌────────────┼─────────────┐                               │
  │           ▼            ▼             ▼                               │
  │      ┌─────────┐  ┌─────────┐  ┌─────────┐    each Feed:             │
  │      │ Feed    │  │ Feed    │  │ Feed    │      group (= identity)   │
  │      │ shreds  │  │ perp    │  │   …     │      enabled flag         │
  │      │ Dynamic │  │ Flat    │  │ Flat    │      pricing_mode         │
  │      │ DevSeats│  │         │  │         │      flat price · name    │
  │      └─────────┘  └─────────┘  └─────────┘                           │
  └──────────────────────────────────────────────────────────────────────┘
           ▲                                     │ group pubkey identifies
  (read 1  │ then read each Feed)                │ a multicast group on …
  registry │                                     ▼
  ┌────────┴────────────────┐          ┌───────────────────────────────┐
  │ clients enumerate feeds │          │ DoubleZero Ledger             │
  │ (oracle, admin, others) │          │ multicast groups — NOT read   │
  └─────────────────────────┘          │ by the program; oracle        │
                                       │ validates before registering  │
                                       └───────────────────────────────┘
```

The program performs no onchain validation of the multicast group, because groups live on the separate DoubleZero Ledger, which the program cannot read. The group is stored as supplied; the oracle validates it against the ledger before registering a feed. Feed management (initialize the registry, register a feed, enable or disable a feed, update a `Flat` price) is authorized to the feed oracle key.

### Pricing modes

`DynamicDeviceSeats` keeps shreds on the existing device-seat pricing path: price is computed per metro and device by the existing machinery and is not stored on the feed. `Flat` gives new feeds a fixed whole-dollar USDC price per subscription epoch, stored on the feed. New feeds are `Flat`; shreds is the only `DynamicDeviceSeats` feed for now.

### Per-feed purchase and entitlement

Buy and Withdraw feed instructions, backed by a payment escrow, let a buyer purchase a `Flat` feed. The open design question is the linkage from a purchase to the set of feeds a seat is entitled to (see Open Questions); the registry defines what feeds exist, not who is entitled to them.

### Oracle responsibilities

Unchanged: drive the epoch state machine, pricing, validator-reward distribution, and settlement. New: read the onchain feed catalog and drive each funded seat's serviceability subscriptions from it rather than from a hardcoded group.

### Delivery and regional scoping (rebop)

Shreds is delivered as a global feed plus per-region `rebop` rebroadcast groups assigned by a seat's exchange. In the near term the oracle continues to source rebop groups from its existing per-exchange configuration and unions them with the catalog-derived feeds. In the future, shreds can be decomposed into separate feeds (one global, one per region), eliminating the special-case rebop path. Whether regional scoping becomes an onchain feed attribute or stays an offchain delivery concern is unresolved (see Open Questions).

### Rename: shred-subscription to feed-subscription, shred-oracle to feed-oracle

The rename touches crate names, binary names, oracle environment variables, the Go SDK package path, infra deployment manifests, and documentation. The onchain program ID and account layouts are expected to be unchanged; this should be confirmed before execution. The rename is sequenced as a dedicated change once feeds are first-class, so it does not interleave with the feature work.

### Components

The design requires the following components:
- Onchain feed registry
- Oracle reads onchain feed catalog for subscription provisioning
- Seat-to-feed subscription wiring
- Buy Feed instruction
- Withdraw Feed instruction
- CLI: list, buy, withdraw, update
- Oracle dynamic API to manage feeds
- Rename to feed-subscription
- E2E: purchase multiple feeds for one pubkey

## Impact

**Codebase.** Broad but mostly additive: the onchain program (new feed accounts, instructions, buy/withdraw), the oracle (catalog-driven subscriptions), the admin CLI, the regenerated IDL and Go SDK, and the formal specs where touched. The rename has the widest blast radius and is the main non-additive change.

**Operational.** Coordinated onchain-plus-oracle deploys for each component, one-time registry initialization, and the shreds retrofit. Infra deployment manifests change as feeds move from CLI arguments to the catalog, and again at the rename.

**Performance.** Negligible onchain; feed enumeration is one registry read plus one read per feed. The oracle's per-cycle work is unchanged in shape.

**User experience.** Buyers gain a catalog of feeds and per-feed purchase; integrators gain a single onchain source of truth for available feeds.

## Security Considerations

Feed management is gated to the oracle key, concentrating trust there. The program cannot read the DoubleZero Ledger, so multicast groups are validated offchain by the oracle before registration. Payment validation is offchain, consistent with the protocol's direct-payment model. The feed registry has a finite, oracle-only-writable capacity. Disabling a feed could in principle strand subscribers; today `Flat` feeds have no subscription mechanic so disabling one strands nothing, and the `DynamicDeviceSeats` (shreds) feed cannot be disabled. Per-feed purchase introduces escrow handling that must reuse the existing audited escrow patterns. The rename must not change the program ID, authority assumptions, or account layouts.

## Backward Compatibility

Feed additions are additive and do not change existing instructions or account layouts; existing deployments keep working until the registry is initialized and shreds is retrofitted as the first feed. The rename is the compatibility-sensitive part: environment variable names, binary and image names, crate names, and the Go SDK import path change, and consumers must be updated in step. The onchain program ID and account layouts are expected to remain stable across the rename. Each component ships behind a coordinated deploy window.

## Open Questions

- **Seat-to-feed linkage for per-feed purchase.** Where a purchase records which feeds it entitles: a field on the seat, an offchain mapping, or a separate onchain entitlement account. This is the core unresolved design choice for `Flat`-feed purchase.
- **Regional scoping onchain versus offchain.** Rebop scopes feeds to exchanges offchain; the onchain feed does not carry exchange information. Decide whether scoping is a permanent delivery concern or an onchain feed attribute.
- **Pricing-mode duplication.** The oracle defines its own pricing-mode enum; it must stay consistent with the onchain enum, or consume it directly once the two are coupled.
- **Fiat payment path.** How a fiat rail plugs into per-feed purchase while the seat remains the onchain authority.
- **Program ID on rename.** Confirm the rename keeps the program ID and authority configuration unchanged.
- **Usage metering.** Bandwidth or usage-based billing is a possible future direction, out of scope here.

## Appendix A: Shreds architecture (baseline)

The subsystem today spans three layers across two chains:

- **Shred-subscription program (Solana).** Tracks `ClientSeat` and `PaymentEscrow` (USDC) accounts, per-metro per-epoch pricing (a base metro price plus a per-device premium), and a per-epoch `ShredDistribution` that aligns the subscription epoch to a DoubleZero epoch and collects payments for reward distribution. An `ExecutionController` runs the epoch state machine (closed for requests, updating prices, open for requests) and tracks settlement. Seats are allocated per device by tenure, with instant-allocation and withdrawal request flows. The subscribable unit is the device; there is no feed.
- **Shred oracle (offchain).** Holds the program's oracle key. Drives the epoch state machine and pricing, distributes validator rewards, converts collected USDC to 2Z, and bridges to access: it reads funded seats on Solana and writes the matching `AccessPass` and multicast subscribe allowlist on the DoubleZero Ledger.
- **Serviceability program (DoubleZero Ledger).** The access primitive. The oracle-owned `AccessPass` carries the per-seat `mgroup_sub_allowlist`; the oracle adds and removes multicast groups to match each funded seat.

Shreds is delivered over a global multicast group passed to the oracle as configuration, with per-region `rebop` rebroadcast groups layered on by exchange.

```
                 CURRENT ARCHITECTURE — Shred Subscription

   Buyer / validator host                      Publishers / validators
   ┌─────────────────────────┐                 ┌──────────────────────┐
   │ doublezero-solana CLI   │                 │ shred supply         │
   │ doublezerod daemon      │                 └───────────┬──────────┘
   └───────────┬─────────────┘                             │ push shreds
               │ (1) pay (USDC)                            ▼
               ▼                               ┌──────────────────────┐
   ┌─────────────────────────────┐             │ DZ Edge (data plane) │
   │ SOLANA                      │             │ multicast delivery   │
   │ shred-subscription program  │             └───────────▲──────────┘
   │  ClientSeat · PaymentEscrow │           (5) subscribe │
   │  ShredDistribution          │           (6) tunnel    │
   │  pricing · epoch phases     │             ┌───────────┴─────────┐
   └────┬───────────────▲────────┘             │ DoubleZero Ledger   │
        │ (2) read      │ (3) drive epoch,     │ serviceability:     │
        │ funded seats  │ price, settle,       │ AccessPass +        │
        ▼               │ rewards              │ mgroup_sub_allowlist│
   ┌────────────────────┴────────┐             └────────────▲────────┘
   │ shred-oracle (offchain)     │  (4) write AccessPass    │
   │  oracle key · pricing algo  │      + allowlist ────────┘
   │  validator rewards          │◀── S3 rewards leaves
   │  USDC → 2Z (revenue dist)   │◀── revenue distribution
   │  cfg: --multicast-group     │
   │       --exchange-mcast      │
   └─────────────────────────────┘

   (1) buyer funds a seat (ClientSeat + USDC escrow) via the CLI
   (2) oracle reads funded seats and epoch state from the program
   (3) oracle drives the epoch state machine, pricing, settlement, rewards
   (4) oracle writes the seat's AccessPass + allowlist on the DoubleZero Ledger
   (5) the host's reconciler sees access and subscribes to the multicast group
   (6) DZ Edge brings up the GRE tunnel and shreds are delivered
```
