# RFC-29: DoubleZero Edge for Builders

**Status:** Implemented
**Author:** Andrew McConnell

## Summary

DoubleZero Edge distributes real-time, machine-generated data around the world
through a dedicated fiber network. This RFC proposes DoubleZero Edge for Builders: a way for anyone to deploy a real-time
data feed on DoubleZero Edge by posting a 2Z bond. It starts with market data, but
it can be adapted to carry any kind of real-time feed.

The builder defines and runs the feed. The network carries the data, and the
protocol automates payment and holds the bond, which makes spam expensive.

This RFC is a starting point. We expect it to change as the testnet implementation
runs and we learn what works.

## Motivation

Building a private global fiber network takes years and a lot of capital. A builder
on DoubleZero uses the network that already exists and spends its effort on the
data.

Today the core team adds every feed on DoubleZero Edge, which makes us the
bottleneck on which feeds exist. Anyone willing to put capital behind a feed should
be able to list it. The protocol keeps only what it has to own: transport,
correctness checks, and the bond.

## Model

A builder deploys a feed and is responsible for:

1. **Feed definition.** The upstream source, the schema, the update cadence, and
   what the feed promises subscribers.
2. **Feed operation.** Getting the data, publishing it, and keeping it correct and
   available.

The protocol carries the feed and automates payment. It does not get the data, and
it does not run the site subscribers buy from. It does check that a feed matches
its wire spec in [edge-feed-spec](https://github.com/malbeclabs/edge-feed-spec) and meets the service level its builder declared. Where a public
feed carries the same data, it compares the two. See
[Data integrity](#data-integrity).

### Permissionless creation

Anyone who posts the bond and meets the eligibility rules can deploy a feed. There
is no approval committee and no governance vote.

### Shared infrastructure

Feeds run on DoubleZero Edge, the protocol's fast, deterministic transport.
Builders use it instead of running their own.

### Third-party frontends

Builders build their own sites and interfaces. The protocol does not ship a
frontend for builder-deployed feeds.

## Spec

1. **Bond.** A builder posts a 2Z bond before deploying a feed. The bond makes spam
   expensive and gives the builder something to lose if it breaks the rules. It
   grows with the rate the feed commits to, so a builder using more of the network
   has more at risk. A feed that goes over its committed rate degrades the network
   for everyone else.

   The tiers and the 2Z each one requires are configuration, not part of this
   spec. The program's admin sets them onchain and can change them without a
   program upgrade. Each cluster's values are recorded where that cluster is
   configured. On testnet, the 2Z used for bonds carries no value, and the tiers
   are assigned for illustrative purposes only.

   A change to a tier's requirement does not reach a bond that is already funded.
   That bond keeps the requirement it was funded at for as long as it stays
   funded, so a builder's terms can't get worse after it has paid in full. A bond
   that is still short follows the current requirement. Otherwise a builder could
   open bonds at an old, lower requirement and fund them later.

2. **One feed per bond.** A builder running a second feed posts a second bond. Each
   feed has its own collateral, so losing one bond never touches another.

3. **Minimum hold.** A builder can't withdraw from a bond for 180 days after first
   funding it. Later deposits to that bond don't restart the clock, so a top-up after a requirement change
   can't push the withdrawal date out. After 180 days, a builder can withdraw
   anything above the current requirement.

   The clock starts at the bond, not at deployment, because the bond lives on
   Solana and the feed lives on the DZ ledger. The bond program never sees a
   deployment. Starting the clock there would need a cross-chain signal, and the
   builder already controls the timing by choosing when to deploy.

4. **Revenue split.** `10%` of what a feed collects converts to 2Z and burns.
   What's left splits 50/50 between the builder and DoubleZero network
   contributors, and the builder's half is paid in 2Z. The burn comes first, and
   the split applies to what's left. An even split
   matches what current data publishers receive, and what other
   projects with similar builder-deployed structures pay their operators.

5. **Currency.** Everything the protocol holds or pays out is 2Z: the bond, both
   burns, and the builder's share. Subscribers pay in USDC, and that is the only
   place USDC appears. The protocol does the conversion, so the builder never
   handles USDC.

6. **Eligibility.** A feed has to be sold to outside subscribers and follow a spec
   in [edge-feed-spec](https://github.com/malbeclabs/edge-feed-spec). At
   deployment it declares a service level: update cadence, completeness, and
   availability. If a public feed carries the same data, the declaration also
   names that feed and how far the builder's feed may diverge from it. The
   declaration is a commitment the protocol measures, not marketing copy. Specs
   live in [edge-feed-spec](https://github.com/malbeclabs/edge-feed-spec) and builders don't write them, so a feed is checked
   against a standard it didn't set.

   A feed a builder runs only for itself is out of scope, and so is a multicast
   deployment with no spec. Both belong on DoubleZero's multicast service. The
   reference designs in
   [edge-multicast-ref](https://github.com/malbeclabs/edge-multicast-ref) make a
   conforming publisher cheap to build.

   Today's [specs](https://github.com/malbeclabs/edge-feed-spec) cover market data. See [Dependencies](#dependencies).

7. **Forfeiture.** A builder can lose its bond on the grounds in
   [Forfeiture](#forfeiture).

8. **Subscriber entitlement.** The builder decides who gets a seat on its feed. It
   can run its own entitlement system against the protocol's onchain primitives,
   or use DoubleZero's purchasing and guided access. Either way, the seat, the
   payment, and the metering stay with the protocol. See
   [Subscriber entitlement](#subscriber-entitlement).

## Dependencies

Real-time data other than market data needs a wire spec that doesn't exist yet.
[edge-feed-spec](https://github.com/malbeclabs/edge-feed-spec) covers market data: top-of-book, midpoint, market-by-order,
market-by-price, order intent, perp stats, and the reference data they share.
Nothing there carries arbitrary payloads.

Edge for Builders launches on the market data specs that exist. The
[conformance suite](https://github.com/malbeclabs/edge-feed-spec/tree/main/tools/conformance)
does not yet check order intent or perp stats, so feeds on those specs wait for
their checkers.

To carry
anything else, [edge-feed-spec](https://github.com/malbeclabs/edge-feed-spec) needs a generic envelope: the standard DoubleZero
Edge datagram header, followed by a payload the publisher defines and the transport
does not inspect. The header is what makes [conformance](https://github.com/malbeclabs/edge-feed-spec/tree/main/tools/conformance) testable, so a feed of
arbitrary data is still checked at the datagram level. That spec comes before
other kinds of feeds can run, and it is separate work from this RFC.

## Feed lifecycle

A feed is deployed, runs, and eventually retires.

A builder can halt a feed and resume it later, for example to switch upstream
sources without redeploying. Retiring is permanent: subscribers get thirty days'
notice, then publishing stops. Releasing the bond at retirement is not built
yet. Until it is, the builder can withdraw only what exceeds the requirement,
after the hold.

## Data integrity

Systems that calculate against an external price need that price to be hard to
manipulate, because a manipulated price can make them insolvent. A data feed is
different. The protocol carries the data but doesn't calculate anything from it.

So there is no solvency risk. What's left is a quality question, and the protocol
answers it by measuring rather than judging. Every feed comes with something to
measure against: the spec it follows in [edge-feed-spec](https://github.com/malbeclabs/edge-feed-spec) and the service level it declared. Together
they say what a subscriber is buying: the wire format, update cadence,
completeness, and availability. DoubleZero measures each feed against them and
publishes the results.

If a public feed covers the same data, the builder names it in the declaration
along with how far its feed may diverge. DoubleZero runs that comparison and
publishes the result too. The builder picks both the comparison feed and the
threshold, so this is not an independent audit. It is a public one: subscribers
can see the bar each builder set for itself and decide what it's worth.

The protocol does not decide whether a feed's numbers are right. It reports how a
feed differs from what its builder promised, and subscribers draw their own
conclusions.

## Forfeiture

Forfeiture is there to stop behavior that threatens the protocol's correctness,
uptime, or performance. It is not for enforcing opinions about feed quality. In a
healthy network it should rarely or never be used.

Some grounds can be checked automatically:

1. **Conformance failure.** A feed fails the [conformance suite](https://github.com/malbeclabs/edge-feed-spec/tree/main/tools/conformance) for its spec and the
   builder doesn't fix it.
2. **Service level breach.** A feed misses the service level its builder declared,
   including the allowed divergence from a named public feed. A feed that promises
   an update every five milliseconds and goes dark for half an hour has broken its
   own terms, whatever the reason.

Conformance failures and service level breaches could result in forfeiture in the
future. At the time of this writing, the mechanisms to enforce programmatic
forfeiture are still under development and pending community feedback.

Other grounds need someone's judgment:

3. **Network harm.** A builder degrades DoubleZero Edge for other users. Context
   matters here. Going over a limit might be a bug the builder found rather than a
   choice it made, and those deserve different responses.
4. **Bad data.** A feed publishes wrong data that the [conformance](https://github.com/malbeclabs/edge-feed-spec/tree/main/tools/conformance) and service level
   checks miss. That happens whenever no public feed carries the same data to
   compare against. Setting a low bar and meeting it is fine. Publishing wrong
   numbers is not.

During the testnet period, the core protocol team handles these cases by removing
the publisher or subscriber involved. See [Revocation](#revocation).

A forfeited bond is burned, not paid to anyone. Paying it out would reward people
for another builder's failure, which gives them a reason to cause it. Burning it
keeps forfeiture a deterrent and not a way to make money.

## Permissionless in practice

Anyone can deploy a feed through code. In practice, cost and effort keep the group
of builders small, and we think that's the right outcome.

- **Capital.** The bond keeps out casual developers and small teams. We expect the
  requirement to come down as the network matures.
- **Work.** Deploying a feed is easy. Making it worth buying is hard. The builder
  gets the data, markets it, and builds the interface, and most of that happens
  outside the protocol.
- **Conformance.** A feed has to match a published wire spec in
  [edge-feed-spec](https://github.com/malbeclabs/edge-feed-spec) and be sold to others.
  A builder who wants multicast for something else should use DoubleZero's
  multicast service.
- **Forfeiture risk.** Once forfeiture is enforced, a builder that runs a feed badly
  can lose part of its bond. That risk lasts as long as the feed runs.

## Subscriber entitlement

The builder decides who can hold a seat on its feed. The protocol provisions the
seat and automates payment, but doesn't decide who gets one.

For each feed, the builder picks one of two paths.

**Its own entitlement system.** The builder applies whatever criteria it needs and
admits subscribers itself. The only requirement is that admission goes through the
protocol's onchain primitives: a seat is an `AccessPass` entry, created by the same
instruction no matter who calls it. Today that instruction needs the
`ACCESS_PASS_ADMIN` permission, so a builder's entitlement service needs it
granted before it can admit anyone.

**DoubleZero purchasing and guided access.** The subscriber signs up at
doublezero.xyz, pays, and receives a seat. Anyone who completes the process is
admitted.
A builder with no criteria of its own gets a working feed without running an
entitlement service.

Both paths write the same onchain records. Seats are still the billing unit, so the
revenue split in clause 4 applies no matter who admitted the subscriber, and
metering works the same way.

This belongs to the builder for two reasons.

First, a builder with regulatory obligations has to control its own subscriber
list. We can't run know-your-customer checks for a builder, and we can't vouch for
who a builder admitted. Without control of admission, that builder can't use this
at all.

Second, we shouldn't dictate a builder's business model. A builder posts a 2Z bond
to use the network as its own, and choosing its customers for it would undercut
that. Other projects with similar builder-deployed structures work the same way:
whoever defines the market decides who is in it.

### Revocation

During the testnet period, the core protocol team will remove any publisher or
subscriber that shows abusive behavior, at its discretion. This applies no matter
which path admitted the subscriber.
