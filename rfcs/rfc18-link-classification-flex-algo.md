# RFC-18: Link Classification — Flex-Algo

## Summary

**Status: `Draft`**

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

DoubleZero contributors operate links with different physical characteristics — low latency, high bandwidth, or both. Today all traffic uses the same IS-IS topology, so every service follows the same paths regardless of what those paths are optimized for. This RFC introduces a link classification model that allows DZF to assign named topology labels to links onchain and use IS-IS Flexible Algorithm (flex-algo) to compute separate constraint-based forwarding topologies per label. Different traffic classes — VPN unicast and IP multicast — can then use different topologies.

**Deliverables:**
- `TopologyInfo` onchain account — DZF creates this to define a topology, with auto-assigned admin-group bit (from the `AdminGroupBits` `ResourceExtension`), flex-algo number, and derived color
- `link_topologies: Vec<Pubkey>` field on the serviceability link account — references assigned topologies; capped at 8 entries; only the first entry is used by the controller in this RFC
- Controller feature config file (`features.yaml`) — loaded at startup; gates flex-algo topology config, link admin-group tagging, and BGP color community stamping independently; replaces any onchain feature flag for this capability
- Controller logic — translates topologies into IS-IS TE admin-groups on interfaces, generates flex-algo topology definitions, configures `system-colored-tunnel-rib` as the BGP next-hop resolution source, and applies BGP color extended community route-maps per tunnel; all conditioned on the controller config

**Scope:**
- Delivers traffic-class-level segregation: multicast vs. VPN unicast at the network level
- Per-tenant unicast path differentiation via `include_topologies: Vec<Pubkey>` on the `Tenant` account — all unicast tenants receive color 1 (UNICAST-DEFAULT) by default; `include_topologies` overrides this to assign specific topologies and steer the tenant onto a designated forwarding plane

---

## Motivation

The DoubleZero network carries two distinct traffic types today: VPN unicast (tenants connected in IBRL mode) and IP multicast. Both follow the same IS-IS topology, where link metrics are derived from measured latency. Every service takes the lowest-latency path — there is no differentiation.

As the network grows, traffic types have different requirements. Latency-sensitive multicast benefits from low-latency links. Higher-latency, higher-bandwidth links that do not win the latency-based SPF are chronically underutilized — yet they may be exactly what certain tenants need. Business requirements, not just latency, should determine which traffic uses which links. A single shared topology cannot serve both simultaneously. This RFC solves the first layer of this problem: separating traffic by class (multicast vs. unicast) at the network level, so that a set of links can be reserved for multicast use while unicast routes around them. Per-tenant path differentiation — where individual tenant VRFs are steered onto different constrained topologies — is a distinct problem addressed architecturally here but deferred in implementation.

Without a steering mechanism, all tenants compete for the same links, and contributors have no way to express that a link is intended for a particular class of traffic. The result is no way to differentiate service quality as the network scales.

IS-IS Flexible Algorithm provides the routing mechanism: each flex-algo defines a constrained topology using TE admin-groups as include/exclude criteria. What does not yet exist is a way to assign admin-group membership to links onchain, so the controller can apply the correct device configuration automatically rather than requiring per-device manual config. This RFC defines that model.

---

## New Terminology

- **Admin-group** — An IS-IS TE attribute assigned to a physical interface that flex-algo algorithms use as include/exclude constraints. Also called "affinity" in some implementations. Arista EOS supports bits 0–127.
- **BGP color extended community** — A BGP extended community (`Color:CO(00):<N>`) set on VPN-IPv4 routes inbound on the client-facing BGP session. The color value matches the EOS flex-algo color, enabling per-route algorithm selection at devices receiving the route via VPN-IPv4.
- **Controller feature config** — A YAML file loaded by the controller at startup that gates flex-algo topology config, link tagging, and color community stamping independently. Controls the staged rollout of flex-algo to the network without requiring onchain transactions.
- **EOS color value** — An integer assigned to a flex-algo definition in EOS (`color <N>` under `flex-algo`). Causes EOS to install that algorithm's computed tunnels in `system-colored-tunnel-rib` keyed by (endpoint, color). Derived as `admin_group_bit + 1`; not stored separately.
- **Flex-algo** — IS-IS Flexible Algorithm (RFC 9350). Each algorithm defines a constrained topology (metric type + admin-group include/exclude rules) and computes an independent SPF. Nodes with the same flex-algo compute consistent paths across the topology. Arista EOS supports flex-algo numbers 128–255.
- **Link topology** — A DZF-defined constrained IS-IS forwarding plane assigned to a link via an admin-group. Determines which flex-algo topologies include or exclude the link.
- **Topology constraint** — Each `TopologyInfo` defines either an `IncludeAny` or `Exclude` constraint. `IncludeAny`: only links explicitly tagged with this topology participate. `Exclude`: all links except those tagged with this topology participate. UNICAST-DEFAULT uses `IncludeAny`.
- **system-colored-tunnel-rib** — An EOS system RIB auto-populated when flex-algo definitions carry a `color` field. Keyed by (endpoint, color). Used by BGP next-hop resolution to steer VPN routes onto constrained topologies based on the BGP color extended community carried on the route.
- **Topology vs color** — In this RFC, *topology* refers to a DZF-defined constrained IS-IS forwarding plane (a `TopologyInfo` account). *Color* refers to the EOS/BGP mechanism used to steer traffic onto a topology: the `color` field in an EOS flex-algo definition, the `EOS color value` derived as `admin_group_bit + 1`, and the BGP color extended community (`Color:CO(00):<N>`) stamped on VPN routes. Every DZF topology has a corresponding EOS color, but the two concepts are distinct.
- **UNICAST-DEFAULT** — The reserved default topology. MUST be the first topology created by DZF and MUST be assigned admin-group bit 0, flex-algo 128, and color 1. These values are protocol invariants — the controller resolves the default tenant topology by looking up the `TopologyInfo` where `admin_group_bit == 0`, not by creation order. Applied to all links eligible for the default unicast topology. Flex-algo 128 uses `include-any UNICAST-DEFAULT`, so only explicitly tagged links participate in the unicast topology. Untagged links are excluded from unicast but remain available to multicast via IS-IS algo 0.
- **UNICAST-DRAINED** — The reserved drain topology. MUST be the second topology created by DZF and MUST be assigned admin-group bit 1, flex-algo 129, and color 2. These values are protocol invariants — the controller resolves the drained topology by looking up the `TopologyInfo` where `admin_group_bit == 1`. Constraint MUST be `Exclude`: only links tagged with UNICAST-DRAINED are excluded from each topology's constrained SPF. Drain is additive — adding UNICAST-DRAINED to `link_topologies` does not remove other topology assignments; the link's permanent tags remain unchanged. The controller injects `exclude {{ $drainBit }}` into every `include-any` flex-algo definition, so a drained link is pruned from all include-any topologies unconditionally (RFC 9350 §5.2.1: `exclude` is evaluated before `include-any` and MUST take precedence). To drain a link, add the UNICAST-DRAINED pubkey to `link_topologies`; to restore, remove it.

---

## Scope and Limitations

| Scenario | This RFC | Notes |
|---|---|---|
| Default unicast topology via UNICAST-DEFAULT | ✅ | Core deliverable; all unicast-eligible links must be explicitly tagged |
| Multicast uses all links (algo 0) | ✅ | Natural PIM RPF behavior; includes both tagged and untagged links; no config required |
| Multiple links in the same topology | ✅ | All tagged links participate together in the constrained topology |
| New links excluded from unicast by default | ✅ | `include-any` strictly excludes untagged links — verified in lab. New links must be explicitly tagged before they carry unicast traffic |
| Per-tenant unicast path differentiation | ✅ | Architecture proven in lab (BGP color extended communities + `system-colored-tunnel-rib`). All unicast tenants receive color 1 (UNICAST-DEFAULT) by default; `include_topologies` overrides this to steer onto specific topologies |
| Exclude a link from multicast | ❌ | PIM RPF uses IS-IS algo 0 unconditionally. No EOS mechanism can redirect multicast away from specific links within the current architecture |
| Automated link selection by bandwidth or type | ❌ | Link tagging is manual DZF policy at this stage. `link.bandwidth` and `link.link_type` exist onchain and can drive automated selection in a future RFC |

The ❌ limitations are architectural, not implementation gaps in this RFC. The multicast exclusion limitation is fundamental to PIM RPF and is not addressable without a different multicast architecture (e.g., MVPN). Automated link selection is deferred until per-tenant topologies (e.g., Shelby) require it.

**Shelby bandwidth assumption:** The Shelby topology, when implemented in a future RFC, will be built from links tagged with the SHELBY admin-group, selected based on 100Gbps physical capacity. That RFC will assume the full 100Gbps of each qualifying link is available to Shelby traffic — no bandwidth reservation or admission control is enforced. Whether capacity sharing, reservation, or isolation is appropriate for Shelby is deferred to that RFC.

---

## Alternatives Considered

### Do nothing
Continue relying on a single IS-IS topology for all traffic. All services — VPN unicast and IP multicast — share the same paths, competing for the same links. Contributors have no way to express that a link is intended for a particular traffic class. This is the current state of the production network. It is rejected because traffic class differentiation is a stated requirement as the network grows: latency-sensitive multicast and unicast tenants with different path requirements cannot both be optimally served by the same topology.

### vpn-unicast-rib (rejected)
An alternative design considered during development used a user-defined `vpn-unicast-rib` with `source-protocol isis flex-algo preference 50` to steer VPN unicast onto constrained topologies. This approach works for a single shared topology but is architecturally incompatible with per-tenant path differentiation: adding `color` to a flex-algo definition (required for per-route algorithm selection via BGP color extended communities) moves tunnels from `system-tunnel-rib` to `system-colored-tunnel-rib`, making them invisible to a user-defined tunnel RIB. The two approaches are mutually exclusive. This RFC commits to the `system-colored-tunnel-rib` approach to avoid a future rework.

### Future paths

Three mechanisms are deferred. Each addresses a distinct escalation in steering granularity beyond what this RFC delivers:

| Mechanism | Granularity | Solves | Complexity | Trigger to adopt |
|---|---|---|---|---|
| CBF with non-default VRFs | Per-tenant VRF, per-DSCP | Different constrained topology per tenant with DSCP-based sub-steering | Medium — TCAM profile change; builds on flex-algo topologies defined here | First tenant requiring DSCP-level path differentiation within a VRF |
| SR-TE | Per-prefix or per-flow | Explicit path control with segment lists; per-prefix or per-DSCP steering independent of IGP topology | High — controller must compute or define explicit segment lists per policy, and set BGP Color Extended Community on routes per-tenant | Per-prefix SLA requirements, or when per-tenant flex-algo topology is insufficient |
| RSVP-TE | Per-LSP (P2P unicast) or per-tree (P2MP multicast) | Hard bandwidth reservation with admission control | High — RSVP-TE on all path devices, IS-IS TE bandwidth advertisement, controller logic to provision per-tenant tunnel interfaces | SLA-backed bandwidth guarantees where admission control is required, not just path preference |

An `exclude_topologies: Vec<Pubkey>` field on `Tenant` is a natural extension of the `include_topologies` model defined here — it would allow a tenant to explicitly avoid certain topologies. This is deferred; no network infrastructure changes are required to add it when needed.

---

## Detailed Design

### Link Topology Model

#### TopologyInfo account

DZF creates a `TopologyInfo` PDA per topology. It stores the topology name and auto-assigned routing parameters. The program MUST auto-assign the lowest available admin-group bit from the `AdminGroupBits` `ResourceExtension` account, and derive the corresponding flex-algo number and color using the formula:

```
admin_group_bit  = next available bit from AdminGroupBits ResourceExtension (0–127)
flex_algo_number = 128 + admin_group_bit
color            = admin_group_bit + 1   (derived, not stored)
```

This formula ensures the admin-group bit, flex-algo number, and color are always in the EOS-supported ranges (bits 0–127, algos 128–255, color 1–4294967295) and are derived consistently from each other. The color is not stored onchain — it is computed by the controller wherever needed.

The `AdminGroupBits` `ResourceExtension` is a persistent bitmap on `GlobalState` that tracks allocated admin-group bits across the lifetime of the program, including bits from deleted topologies. This ensures bits are never reused after deletion — reusing a bit before all devices have had their config updated would cause those devices to apply the new topology's constraints to interfaces still carrying the old bit's admin-group. The bitmap survives PDA deletion, which a PDA-scan approach cannot guarantee.

```rust
#[derive(BorshSerialize, BorshDeserialize, Debug)]
pub enum TopologyConstraint {
    IncludeAny = 0,  // only tagged links participate in the topology
    Exclude    = 1,  // all links except tagged participate in the topology
}

#[derive(BorshSerialize, BorshDeserialize, Debug)]
pub struct TopologyInfo {
    pub name: String,                    // e.g. "unicast-default"
    pub admin_group_bit: u8,             // auto-assigned from AdminGroupBits ResourceExtension, 0–127
    pub flex_algo_number: u8,            // auto-assigned, 128–255; always 128 + admin_group_bit
    pub constraint: TopologyConstraint, // IncludeAny or Exclude
}
```

PDA seeds: `[b"topology", name.as_bytes()]`. `TopologyInfo` accounts MUST only be created or updated by foundation keys.

Name length MUST NOT exceed 32 bytes, enforced by the program on `create`. This keeps PDA seeds well within the 32-byte limit and ensures the admin-group alias name is reasonable in EOS config.

The program MUST validate `admin_group_bit <= 127` on `create` and MUST return an explicit error if all 128 slots are exhausted. This is a hard constraint: EOS supports bits 0–127 only, and `128 + 127 = 255` is the maximum representable value in `flex_algo_number: u8`.

#### link_topologies field on Link

A `link_topologies: Vec<Pubkey>` field is added to the serviceability `Link` account, capped at 8 entries. Each entry holds the pubkey of a `TopologyInfo` PDA. The field appends to the end of the serialized layout, defaulting to an empty vector on existing accounts.

The cap of 8 exists to keep the `Link` account size deterministic on-chain. Only the first entry (`link_topologies[0]`) is used by the controller in this RFC — multiple entries are reserved for future multi-topology-per-link support (e.g., a link participating in both UNICAST-DEFAULT and SHELBY topologies simultaneously, as validated in lab testing).

**Auto-tagging at activation:** when DZF activates a link, the activation processor MUST automatically set `link_topologies[0]` to the UNICAST-DEFAULT `TopologyInfo` pubkey (resolved by PDA seeds `[b"topology", b"unicast-default"]`). This preserves the existing contributor workflow — a link that passes DZF validation carries unicast traffic without any additional manual step. Foundation keys may subsequently override `link_topologies` to assign a different topology or remove the default tag for specialized links (e.g. multicast-only).

`link_topologies` overrides MUST only be made by keys in the DZF foundation allowlist. Contributors MUST NOT set this field directly.

```rust
// Foundation-only fields
if globalstate.foundation_allowlist.contains(payer_account.key) {
    if let Some(link_topologies) = value.link_topologies {
        link.link_topologies = link_topologies;
    }
}
```

#### CLI

**Color lifecycle:**

```
doublezero link topology create --name <NAME> --constraint <include-any|exclude>
doublezero link topology update --name <NAME>
doublezero link topology delete --name <NAME>
doublezero link topology clear  --name <NAME>
doublezero link topology list
```

- `create` — creates a `TopologyInfo` PDA; allocates the lowest available admin-group bit from the `AdminGroupBits` `ResourceExtension`; derives and stores flex-algo number; stores the specified constraint (`include-any` or `exclude`). MUST fail if the name already exists. The first topology created MUST be named `unicast-default` and will be allocated bit 0 — this is a protocol invariant and the program MUST enforce it by rejecting any `create` instruction where the `AdminGroupBits` bitmap is empty and the name is not `unicast-default`. The second topology created MUST be named `unicast-drained` and will be allocated bit 1 — this is a protocol invariant and the program MUST enforce it by rejecting any `create` instruction where only bit 0 is allocated and the name is not `unicast-drained`. Device impact is controlled entirely by the controller feature config — no device config is generated until `flex_algo.enabled: true` is set in the config file.
- `update` — reserved for future use; all fields are immutable after creation. No device config change.
- `delete` — removes the `TopologyInfo` PDA onchain. MUST fail if any link still references this topology (use `clear` first). On the next reconciliation cycle, the controller removes the deleted topology's admin-group alias and flex-algo definition from all devices. Admin-group bits from deleted topologies MUST NOT be reused — the `AdminGroupBits` `ResourceExtension` bitmap persists allocated bits permanently.
- `clear` — removes this topology from all links currently assigned to it, setting `link_topologies` to an empty vector on each. This is a multi-transaction sweep — one `LinkUpdateArgs` instruction is submitted per assigned link; it is not atomic. If the sweep fails partway through, the operator MUST re-run `clear`; the operation is idempotent and will only submit instructions for links that still reference the topology. The `delete` guard (which rejects if any link still references the topology) is the safety net — partial completion is safe because a re-run will clear the remaining references before deletion is attempted. On the next reconciliation cycle, the controller re-applies only the remaining topologies on each affected interface — if other topologies remain, `traffic-engineering administrative-group <remaining>` is applied; if no topologies remain, `no traffic-engineering administrative-group` is applied.
- `list` — fetches all `TopologyInfo` accounts and all `Link` accounts and groups links by topology. SHOULD emit a warning if any topology has fewer links tagged than the minimum required for a connected topology.

```
NAME               CONSTRAINT    FLEX-ALGO   ADMIN-GROUP BIT   COLOR   LINKS
default            —             —           —                 —           link-abc123, link-def456
unicast-default    include-any   128         0                 1           link-xyz789
```

**Link topology assignment:**

```
doublezero link update --pubkey <PUBKEY> --link-topology <NAME>
doublezero link update --pubkey <PUBKEY> --link-topology default
doublezero link update --code  <CODE>   --link-topology <NAME>
```

- `--link-topology <NAME>` MUST resolve the topology name to the corresponding `TopologyInfo` PDA pubkey before submitting the instruction — the onchain field stores pubkeys, not names. Sets `link_topologies[0]`.
- `--link-topology default` sets `link_topologies` to an empty vector, removing any topology assignment. Use with caution — an untagged link will not participate in the unicast topology.
- `doublezero link get` and `doublezero link list` MUST include `link_topologies` in their output, showing the resolved topology names (or "default"). A link activated after UNICAST-DEFAULT is created will immediately display `link-topology: unicast-default` — no additional operator action is required.

**Link drain and restore:**

```
doublezero link drain   --pubkey <PUBKEY>
doublezero link drain   --code   <CODE>
doublezero link restore --pubkey <PUBKEY>
doublezero link restore --code   <CODE>
```

- `drain` appends the UNICAST-DRAINED `TopologyInfo` pubkey to `link_topologies`. The link's existing topology assignments are unchanged. On the next reconciliation cycle, the controller detects the UNICAST-DRAINED entry and EOS applies `exclude <drained-bit>` in each include-any flex-algo definition, pruning the link from all constrained topologies.
- `restore` removes the UNICAST-DRAINED pubkey from `link_topologies`. The link's permanent topology assignments remain in place and are immediately re-eligible in constrained SPF on the next reconciliation cycle.
- Both commands MUST be restricted to foundation keys. `drain` MUST be idempotent — if the link is already drained, it MUST succeed silently.

#### Tenant topology assignment

An `include_topologies: Vec<Pubkey>` field is added to the serviceability `Tenant` account. Each entry holds the pubkey of a `TopologyInfo` PDA. All unicast tenants receive color 1 (UNICAST-DEFAULT) by default; setting `include_topologies` overrides this to assign specific topologies based on business requirements. The field appends to the end of the serialized layout, defaulting to an empty vector on existing accounts.

```rust
#[derive(BorshSerialize, BorshDeserialize, Debug)]
pub struct Tenant {
    // ... existing fields ...
    pub include_topologies: Vec<Pubkey>,  // appended; defaults to []
}
```

`include_topologies` MUST only be set by foundation keys. This is a routing policy decision — contributors MUST NOT be able to steer their own traffic onto a different topology by modifying this field.

When a tenant has one entry in `include_topologies`, the controller resolves the `TopologyInfo` PDA and stamps its color on inbound routes for that tenant. When a tenant has multiple entries, the controller stamps all corresponding color values — EOS then selects the best available colored tunnel by IGP metric (lowest metric wins; highest color number breaks ties). This enables a fallback chain: if the preferred topology's tunnel becomes unavailable, EOS automatically falls back to the next-best color on the same prefix without the route going unresolved. This behavior has been verified in lab testing.

**CLI:**

```
doublezero tenant update --code <CODE> --include-topologies <NAME>[,<NAME>]
doublezero tenant update --code <CODE> --include-topologies default
```

- `--include-topologies <NAME>[,<NAME>]` resolves each topology name to the corresponding `TopologyInfo` PDA pubkey before submitting the instruction.
- `--include-topologies default` sets `include_topologies` to an empty vector, reverting the tenant to the default color 1 (UNICAST-DEFAULT).
- `doublezero tenant get` and `doublezero tenant list` MUST display `include_topologies` showing resolved topology names (or "default").

---

### Controller Feature Configuration

Flex-algo rollout is controlled by a YAML configuration file loaded by the controller at startup via the `-features-config` flag. This separates three distinct concerns that need to be rolled out independently:

1. **Topology config** — flex-algo definitions, IS-IS TE, and BGP next-hop resolution config pushed to all devices
2. **Link admin-group tagging** — applying admin-group attributes to specific interfaces
3. **Color community stamping** — stamping BGP color extended communities on inbound tenant routes

Config changes require a controller restart. This is intentional — restart triggers an immediate reconciliation cycle that applies or reverts the updated config.

```yaml
features:
  flex_algo:
    enabled: true                    # pushes topology config to all devices; false reverts all flex-algo device config
    link_tagging:
      exclude:
        links:
          - <link pubkey>            # never apply admin-group on this link, overrides onchain assignment
    community_stamping:
      all: false                     # if true, stamps all tenants on all devices
      tenants:
        - <pubkey>                   # stamp this tenant on all devices
      devices:
        - <pubkey>                   # stamp all tenants on this device
      exclude:
        devices:
          - <pubkey>                 # never stamp on this device; takes precedence over all positive rules
```

**Rollout sequence:**

The config file decouples onchain state preparation from device deployment. DZF can create topologies and tag links before any device receives flex-algo config, then enable features progressively:

1. DZF creates `TopologyInfo` accounts; links activated after this point are automatically tagged `UNICAST-DEFAULT` — no device impact while `enabled: false`
2. Set `enabled: true` and restart the controller — topology config is pushed to all devices on the next reconciliation cycle. No admin-group tagging or community stamping yet
3. Verify all devices show correct flex-algo state (`show isis flex-algo`, `show tunnel rib system-colored-tunnel-rib brief`)
4. Add specific links to the tagging config or leave the exclude list empty to tag all onchain-assigned links — restart controller
5. Add tenants or devices to `community_stamping` — restart controller. Stamping can be rolled out per-tenant or per-device to control which traffic begins using constrained topologies

**Precedence for community stamping:** a device is stamped if `all: true`, OR its pubkey is in `devices`, OR the tenant's pubkey is in `tenants` — unless the device's pubkey is in `exclude.devices`, which overrides all positive rules.

**Asymmetric routing:** if community stamping is enabled on some devices but not others, routes entering the network at unstamped devices will carry no color community. These routes fall through to `tunnel-rib system-tunnel-rib` and resolve via IS-IS SR (algo-0) tunnels. This is expected behaviour during a phased rollout, not an error condition.

**Revert behaviour:** when `enabled` is set to `false` and the controller is restarted, the controller generates the full set of `no` commands to remove all flex-algo config from all devices on the next reconciliation cycle: `no router traffic-engineering`, `no flex-algo` definitions, `no next-hop resolution ribs`, and removal of `set extcommunity color` from all route-maps.

**Single controller:** today there is a single controller instance; the config file approach is straightforward. Multiple controller instances would require config consistency across instances. This is deferred to a future RFC addressing decentralised controller architecture.

---

### IS-IS Flex-Algo Topology

Each link topology maps to an IS-IS TE admin-group bit via the `TopologyInfo` account. The controller MUST read `link.link_topologies[0]`, resolve the `TopologyInfo` PDA, and apply the corresponding admin-group to the physical interface — unless the link's pubkey is in `link_tagging.exclude.links`.

| Topology | Constraint | Admin-group bit | Flex-algo number | Color | Forwarding scope |
|---|---|---|---|---|---|
| (untagged) | — | — | — (algo 0) | — | All links |
| unicast-default | include-any | 0 | 128 | 1 | Only UNICAST-DEFAULT tagged links |
| unicast-drained | exclude | 1 | 129 | 2 | All links except UNICAST-DRAINED tagged links |

The flex-algo definition MUST be configured on each DZD by the controller. The `color` field MUST be included and set to `admin_group_bit + 1`. The constraint type determines whether `include any` or `exclude` is used. Using UNICAST-DEFAULT as an example:

```
router traffic-engineering
   administrative-group alias UNICAST-DEFAULT group 0
   flex-algo
      flex-algo 128 unicast-default
         administrative-group include any 0
         color 1
```

Flex-algo 128 ("unicast-default") computes an IS-IS SPF over only those links tagged `UNICAST-DEFAULT`. The `color 1` field causes EOS to install these tunnels in `system-colored-tunnel-rib` keyed by (endpoint, 1). Devices that participate in flex-algo 128 advertise both an algo-0 node-segment and an algo-128 node-segment via their loopback.

**Operational implication:** Links are automatically tagged `UNICAST-DEFAULT` at activation — no manual DZF step is required for the common case. The contributor workflow is unchanged: a link that passes DZF validation immediately participates in the unicast topology. The `link topology list` command SHOULD warn if a topology appears disconnected based on the set of tagged links, and SHOULD warn if any activated links have an empty `link_topologies` (indicating a link activated before this RFC that has not been migrated).

#### Universal participation requirement

Flex-algo MUST be enabled on every device in the network, not only on devices that have colored links. A device that does not participate in a flex-algo does not advertise a node-SID for that algorithm, so other devices cannot include it in the constrained SPF and cannot steer VPN traffic to it via the constrained topology. VPN routes to a non-participating device will not resolve via the colored tunnel RIB and will fall back to the next resolution source. The controller MUST therefore push the flex-algo definitions and BGP next-hop resolution config to all devices when `enabled: true`. Admin-group tagging on interfaces is applied only to links with a non-empty `link_topologies` that are not in the `link_tagging.exclude.links` list.

#### Multicast path isolation

Multicast (PIM) resolves via the IS-IS unicast RIB (algo 0), which uses all links regardless of topology assignment. This is inherent to how PIM RPF works — it is not affected by the BGP next-hop resolution profile, and `next-hop resolution ribs` does not support multicast address families. Multicast isolation does not depend on any additional configuration — PIM RPF resolves via the unicast RIB regardless of how VPN unicast is steered.

| Service | Path | Links in path |
|---|---|---|
| VPN unicast | flex-algo 128 (`system-colored-tunnel-rib`, color 1) | Tagged links only |
| Multicast (PIM, default VRF) | IS-IS algo 0 (unicast RIB) | All links |

---

### BGP Color Extended Community

VPN-IPv4 routes MUST carry a BGP color extended community (`Color:CO(00):<N>`) inbound on the client-facing BGP session. The color value matches the EOS flex-algo `color` field for the target topology. At receiving devices, BGP next-hop resolution uses `system-colored-tunnel-rib` to match the (next-hop, color) pair to a flex-algo tunnel.

#### Next-hop resolution

All devices MUST be configured with the following BGP next-hop resolution profile when `enabled: true`:

```
router bgp 65342
   address-family vpn-ipv4
      next-hop resolution ribs tunnel-rib colored system-colored-tunnel-rib tunnel-rib system-tunnel-rib
```

`system-colored-tunnel-rib` is auto-populated by EOS when flex-algo definitions carry a `color` field. A VPN route carrying `Color:CO(00):1` resolves its next-hop through the color-1 (unicast-default, algo 128) tunnel to that endpoint. Routes without a color community fall through to `tunnel-rib system-tunnel-rib`, which is auto-populated by IS-IS SR (algo-0) tunnels. `system-connected` is deliberately omitted — this ensures all VPN traffic uses MPLS forwarding (either colored flex-algo or algo-0 SR) and never falls back to plain IP. Verified in lab testing: with only `tunnel-rib colored system-colored-tunnel-rib` configured, uncolored VPN routes are received but their next-hops cannot be resolved and they never make it into the VRF routing table.

#### Inbound route-map color stamping

The controller already generates a `RM-USER-{{ .Id }}-IN` route-map per tunnel, applied inbound on each client-facing BGP session. This route-map currently sets standard communities identifying the user as unicast or multicast and tagging the originating exchange. The color extended community is added as an additional `set` statement in this same route-map, applied only to unicast tunnels and only when the controller config enables stamping for the tenant and device:

```
route-map RM-USER-{{ .Id }}-IN permit 10
   match ip address prefix-list PL-USER-{{ .Id }}
   match as-path length = 1
   set community 21682:{{ if eq true .IsMulticast }}1300{{ else }}1200{{ end }} 21682:{{ $.Device.BgpCommunity }}
   {{- if and $.Config.FlexAlgo.Enabled (not .IsMulticast) $.LinkTopologies ($.Config.FlexAlgo.CommunityStamping.ShouldStamp .TenantPubKey $.Device.PubKey) }}
   set extcommunity color {{ .TenantTopologyEosColorValues }}
   {{- end }}
```

`.TenantTopologyEosColorValues` is resolved by the controller from the tunnel's tenant:
- If `tenant.include_topologies` is non-empty, resolve each `TopologyInfo` PDA and compute `AdminGroupBit + 1` for each. All resolved color values are stamped in a single `set extcommunity color` statement (e.g., `set extcommunity color 1 color 2`).
- If `tenant.include_topologies` is empty, use the default unicast color: resolve the `TopologyInfo` where `admin_group_bit == 0` (UNICAST-DEFAULT, color 1).

When multiple colors are stamped, EOS selects the colored tunnel with the lowest IGP metric to the next-hop. If two colors tie on metric, the highest color number wins. If a preferred color's tunnel becomes unavailable (e.g., the destination withdraws its node-segment for that algorithm), EOS automatically falls back to the next-best available color — the route remains installed throughout with no disruption. This fallback behavior has been verified in lab testing.

Multicast tunnels do not receive the color community — multicast RPF resolves via IS-IS algo 0 and does not use `system-colored-tunnel-rib`.

Routes arrive on the client-facing session, are stamped with both the standard community and the color extended community in a single pass, and are then advertised into VPN-IPv4 carrying both. No new route-map blocks or `network` statement changes are required.

---

### Controller Changes

All config changes are applied to `tunnel.tmpl`. Five additions are required. All blocks are conditioned on `$.Config.FlexAlgo.Enabled` — when disabled, no flex-algo config is generated and the controller generates `no` commands to remove any previously-pushed flex-algo config.

#### 1. Interface admin-group tagging

Inside the existing `{{- range .Device.Interfaces }}` block, after the `isis metric` / `isis network point-to-point` lines, add admin-group config for physical IS-IS links:

```
{{- if and .Ip.IsValid .IsPhysical .Metric .IsLink (not .IsSubInterfaceParent) (not .IsCYOA) (not .IsDIA) }}
   traffic-engineering
   {{- if and .LinkTopologies (not ($.Config.FlexAlgo.LinkTagging.IsExcluded .PubKey)) }}
   traffic-engineering administrative-group {{ $.Strings.Join " " ($.Strings.ToUpperEach .LinkTopologyNames) }}
   {{- else }}
   no traffic-engineering administrative-group
   {{- end }}
{{- end }}
```

`.LinkTopologies` is the resolved list of `TopologyInfo` accounts from `link.link_topologies`; it is empty when `link_topologies` is empty. `.LinkTopologyNames` is the corresponding list of names. The controller renders all topologies as admin-group names in a space-separated list in a single command — EOS overwrites the existing admin-group assignment with exactly this set. This means:
- A link transitioning from two topologies to one re-applies only the surviving topology, atomically replacing the previous set
- A link losing its last topology receives `no traffic-engineering administrative-group`
- The targeted `no traffic-engineering administrative-group <NAME>` command is never used, avoiding the EOS behavior where it would remove all groups regardless of the name specified

Interface-level admin-group tagging is conditioned on `$.Config.FlexAlgo.Enabled` alone — since an interface may have topologies assigned onchain while the feature is disabled.

#### 2. router traffic-engineering block

Add after the `router isis 1` block, conditional on topologies being defined and the feature being enabled:

```
{{- if and $.Config.FlexAlgo.Enabled .LinkTopologies }}
router traffic-engineering
   router-id ipv4 {{ .Device.Vpn4vLoopbackIP }}
   {{- range .LinkTopologies }}
   administrative-group alias {{ $.Strings.ToUpper .Name }} group {{ .AdminGroupBit }}
   {{- end }}
   !
   flex-algo
   {{- $drainBit := $.DrainedAdminGroupBit }}
   {{- range .LinkTopologies }}
      flex-algo {{ .FlexAlgoNumber }} {{ .Name }}
         {{- if eq .Constraint "include-any" }}
         administrative-group include any {{ .AdminGroupBit }} exclude {{ $drainBit }}
         {{- else }}
         administrative-group exclude {{ .AdminGroupBit }}
         {{- end }}
         color {{ .Color }}
   {{- end }}
{{- end }}
```

`.LinkTopologies` is the ordered list of `TopologyInfo` accounts, sorted by `AdminGroupBit`. `.Color` is computed as `AdminGroupBit + 1`. `$.DrainedAdminGroupBit` is the `AdminGroupBit` of the UNICAST-DRAINED `TopologyInfo`, resolved by PDA seeds `[b"topology", b"unicast-drained"]`. The flex-algo name (e.g., `unicast-default`) is the topology name stored in `TopologyInfo`.

When `$.Config.FlexAlgo.Enabled` is false, the controller generates `no router traffic-engineering` to remove any previously-pushed config.

#### 3. BGP next-hop resolution

Inside the existing `address-family vpn-ipv4` block:

```
   address-family vpn-ipv4
      {{- range .Vpnv4BgpPeers }}
      {{- if ne .PeerIP.String $.Device.Vpn4vLoopbackIP.String }}
      neighbor {{ .PeerIP }} activate
      {{- end }}
      {{- end }}
      {{- range .UnknownBgpPeers }}
      no neighbor {{ . }}
      {{- end }}
      {{- if and $.Config.FlexAlgo.Enabled .LinkTopologies }}
      next-hop resolution ribs tunnel-rib colored system-colored-tunnel-rib tunnel-rib system-tunnel-rib
      {{- end }}
   !
```

#### 4. IS-IS flex-algo advertisement and traffic-engineering

Inside the existing `router isis 1` block, under `segment-routing mpls`, add a `flex-algo` advertisement line per topology:

```
   segment-routing mpls
      no shutdown
      {{- range .LinkTopologies }}
      flex-algo {{ .Name }} level-2 advertised
      {{- end }}
```

After the `segment-routing mpls` block, add the `traffic-engineering` section:

```
{{- if and $.Config.FlexAlgo.Enabled .LinkTopologies }}
   traffic-engineering
      no shutdown
      is-type level-2
{{- end }}
```

Without both of these, the device does not advertise its flex-algo node-SIDs and does not include admin-group attributes in its IS-IS LSP. Other devices cannot include it in the constrained SPF and cannot steer VPN traffic to it.

#### 5. Loopback flex-algo node-segment

Inside the existing `{{- range .Device.Interfaces }}` block, extend the existing `node-segment` line on the Vpn4vLoopback interface:

```
{{- if and .IsVpnv4Loopback .NodeSegmentIdx }}
   node-segment ipv4 index {{ .NodeSegmentIdx }}
   {{- range .FlexAlgoNodeSegments }}
   node-segment ipv4 index {{ .NodeSegmentIdx }} flex-algo {{ .TopologyName }}
   {{- end }}
{{- end }}
```

Each Vpnv4 loopback must advertise one node-SID per flex-algo topology it participates in. Because node-SIDs must be globally unique per device, each (interface, topology) pair needs its own allocated index. A new `flex_algo_node_segments: Vec<FlexAlgoNodeSegment>` field MUST be added to the `Interface` account:

```rust
pub struct FlexAlgoNodeSegment {
    pub topology: Pubkey,       // TopologyInfo PDA pubkey
    pub node_segment_idx: u16,  // allocated from SegmentRoutingIds ResourceExtension
}
pub flex_algo_node_segments: Vec<FlexAlgoNodeSegment>,
```

At interface activation time, one `FlexAlgoNodeSegment` is allocated per known `TopologyInfo` account and appended to the list. Each `node_segment_idx` is allocated from the `SegmentRoutingIds` `ResourceExtension` account, following the same pattern as the existing `node_segment_idx`. Entries are deallocated on `remove`.

In the template, `.FlexAlgoNodeSegments` is accessed directly on the current interface (the `.` context within `{{- range .Device.Interfaces }}`). The controller populates this from the interface's onchain `flex_algo_node_segments` list during rendering, resolving the topology name from each entry's `TopologyInfo` pubkey. This is intentionally distinct from `.LinkTopologies` (which describes which topologies a specific link is tagged with) — the loopback template is concerned with which topologies this device participates in, not with link tagging.

**Migration for existing accounts:** A one-time `doublezero-admin` CLI migration command MUST be provided covering two tasks:

1. **Links** — iterate all existing `Link` accounts with `link_topologies = []` and set `link_topologies[0]` to the UNICAST-DEFAULT `TopologyInfo` pubkey. Links activated after this RFC are auto-tagged at activation; this migration covers links activated before the RFC was deployed.
2. **Vpnv4 loopbacks** — iterate all existing `Interface` accounts with `loopback_type = Vpnv4` and allocate a `FlexAlgoNodeSegment` entry for each known `TopologyInfo` account. Existing `node_segment_idx` assignments (algo-0) are unchanged — this is purely additive. Loopbacks activated after this RFC will have entries allocated at activation time.

The migration command MUST be idempotent — re-running it MUST skip already-migrated accounts and only process those still requiring migration. It MUST emit a summary on completion (e.g. `migrated 12 links, 4 loopbacks; skipped 3 already migrated`). A `--dry-run` flag MUST be supported to preview the accounts that would be migrated without applying any changes.

The controller MUST check at startup that no Vpnv4 loopback has an empty `flex_algo_node_segments` list when `flex_algo.enabled: true` is set. If any unset loopbacks are found, `enabled: true` MUST be treated as a no-op for that startup cycle — the controller MUST NOT push any flex-algo config to any device, MUST emit a prominent error identifying the unset loopbacks by pubkey, and MUST direct the operator to run the migration command and restart. The `features.yaml` flag is not modified. Flex-algo config will not be applied until the migration is complete and the controller is restarted cleanly. This prevents silently pushing a broken topology where some devices are unreachable via the constrained path.

Without a flex-algo node-SID on the loopback, remote devices cannot compute a valid constrained path to this device and VPN routes to it will not resolve via the colored tunnel RIB.

Interface admin-group blocks are conditional on `.LinkTopologies` being non-empty. The flex-algo node-segment lines within the loopback block are conditional on `.FlexAlgoNodeSegments` being non-empty — if the list is empty, the `range` loop produces no output and only the algo-0 `node-segment` line is rendered. Devices with no topologies defined produce identical config to today.

---

### SDK Changes

`TopologyInfo` MUST be added to the Go, Python, and TypeScript SDKs. The `link` deserialization structs MUST include the new `link_topologies: Vec<Pubkey>` field. The `tenant` deserialization structs MUST include the new `include_topologies: Vec<Pubkey>` field. Fixture files MUST be regenerated.

---

### Tests

#### Smart contract (integration tests)

**TopologyInfo lifecycle:**
- A foundation key MUST be able to create a `TopologyInfo` account with a name; admin-group bit MUST be allocated from the `AdminGroupBits` `ResourceExtension` starting at 0, and flex-algo number MUST be 128.
- Creating a second topology MUST allocate bit 1 from the `ResourceExtension` and flex-algo 129.
- Creating any topology before `unicast-default` (bitmap empty) with a name other than `unicast-default` MUST be rejected.
- Creating any topology as the second topology (only bit 0 allocated) with a name other than `unicast-drained` MUST be rejected.
- A non-foundation key MUST NOT be able to create a `TopologyInfo` account; the instruction MUST be rejected with an authorization error.
- All `TopologyInfo` fields are immutable after creation; an `update` instruction MUST be rejected or be a no-op.
- A non-foundation key MUST NOT be able to update a `TopologyInfo` account.
- `delete` MUST succeed when no links reference the topology; the `TopologyInfo` PDA MUST be removed onchain.
- `delete` MUST fail when one or more links still reference the topology.
- After `clear`, all links previously assigned the topology MUST have `link_topologies = []`.
- After `clear` followed by `delete`, the `TopologyInfo` PDA MUST be absent.
- Admin-group bits from deleted topologies MUST NOT be reused by subsequently created topologies; the `AdminGroupBits` `ResourceExtension` bitmap MUST persist the allocated bit after PDA deletion.
- After `delete`, the controller MUST NOT generate removal commands for the deleted topology's admin-group alias, flex-algo definition, or IS-IS TE config — device-side cleanup is deferred.

**Tenant topology assignment:**
- `include_topologies` MUST default to an empty vector on a newly created tenant account and on existing accounts deserialized from pre-upgrade binary data.
- A foundation key MUST be able to set `include_topologies` to a list of valid `TopologyInfo` pubkeys on any tenant.
- A non-foundation key MUST NOT be able to set `include_topologies`; the instruction MUST be rejected with an authorization error.
- Setting `include_topologies` to an empty vector MUST be accepted and revert the tenant to the default color 1 (UNICAST-DEFAULT).
- Setting `include_topologies` to a pubkey that does not correspond to a valid `TopologyInfo` account MUST be rejected.

**Link topology assignment:**
- `link_topologies` MUST default to an empty vector on existing accounts deserialized from pre-upgrade binary data.
- The activation processor MUST set `link_topologies[0]` to the UNICAST-DEFAULT `TopologyInfo` pubkey on every newly activated link. If the UNICAST-DEFAULT `TopologyInfo` account does not exist at activation time, the instruction MUST fail.
- A foundation key MUST be able to override `link_topologies[0]` to a valid `TopologyInfo` pubkey on any link.
- A contributor key MUST NOT be able to set `link_topologies`; the instruction MUST be rejected with an authorization error.
- Setting `link_topologies` to an empty vector from a non-empty value MUST be accepted and persist correctly.
- Setting `link_topologies[0]` to a pubkey that does not correspond to a valid `TopologyInfo` account MUST be rejected.
- `link_topologies` MUST NOT exceed 8 entries; an instruction submitting more than 8 MUST be rejected.

#### Controller (unit tests)

- A link with `link_topologies = []` MUST produce interface config with no `traffic-engineering administrative-group` line.
- A link with `link_topologies[0]` referencing a `TopologyInfo` with bit 0, name "unicast-default", and constraint `IncludeAny` MUST produce interface config with `traffic-engineering administrative-group UNICAST-DEFAULT`.
- A link in `link_tagging.exclude.links` MUST produce `no traffic-engineering administrative-group` regardless of onchain `link_topologies` assignment.
- Transitioning a link from a topology to default MUST produce a `no traffic-engineering administrative-group` diff.
- Transitioning a link from one topology to another MUST produce the correct remove/add diff.
- The `router traffic-engineering` block MUST include `color <admin_group_bit + 1>` on each flex-algo definition.
- Each `include-any` flex-algo definition MUST include `exclude <unicast-drained-bit>` in addition to `include any <topology-bit>`. An `exclude`-constraint topology MUST NOT have the drained-bit injected.
- A link with UNICAST-DRAINED in `link_topologies` alongside UNICAST-DEFAULT MUST produce interface config with `traffic-engineering administrative-group UNICAST-DEFAULT UNICAST-DRAINED`.
- Draining a link (adding UNICAST-DRAINED to `link_topologies`) MUST NOT remove other topology assignments from the interface config.
- Restoring a link (removing UNICAST-DRAINED from `link_topologies`) MUST produce interface config identical to a never-drained link with the same permanent topology assignments.
- The BGP `next-hop resolution ribs tunnel-rib colored system-colored-tunnel-rib tunnel-rib system-tunnel-rib` config MUST be generated correctly when `enabled: true`.
- A per-tunnel inbound route-map MUST include `set extcommunity color 1` for a unicast tenant with empty `include_topologies` when the tenant or device is in the `community_stamping` config and `.LinkTopologies` is non-empty.
- A per-tunnel inbound route-map MUST include `set extcommunity color 1 color 2` for a unicast tenant with `include_topologies` referencing two `TopologyInfo` accounts (bits 0 and 1).
- A per-tunnel inbound route-map MUST NOT include `set extcommunity color` when the device is in `community_stamping.exclude.devices`.
- A new `TopologyInfo` account detected on reconciliation MUST cause the controller to push updated config to all devices.
- **Config disabled:** With `TopologyInfo` accounts defined, links tagged, and `enabled: false`, the controller MUST generate `no router traffic-engineering`, no flex-algo IS-IS config, no `next-hop resolution ribs` line, and no `set extcommunity color` in any route-map. Device config MUST be identical to a network with no topologies defined.
- **Config enabled:** Setting `enabled: true` with existing `TopologyInfo` accounts and tagged links MUST cause the controller to generate the full flex-algo config block on the next reconciliation cycle.
- **Interface tagging independent of stamping:** Interface-level `traffic-engineering administrative-group` config MUST be generated based on `$.Config.FlexAlgo.Enabled` alone, regardless of `community_stamping` settings.

#### SDK (unit tests)

- `TopologyInfo` account MUST serialize and deserialize correctly via Borsh for all fields.
- `TopologyInfo` account MUST deserialize correctly from a binary fixture.
- `link_topologies` pubkey vector MUST be included in `link get` and `link list` output in all three SDKs, showing the topology names (resolved from `TopologyInfo`) or "default".
- The `list` command MUST display the derived color (`admin_group_bit + 1`) in output.

#### End-to-end (cEOS testcontainers)

- **Topology creation**: After a foundation key creates a `TopologyInfo` for "unicast-default" (bit 0, flex-algo 128, constraint include-any) and `enabled: true` is set, the controller MUST push `router traffic-engineering` config with `administrative-group alias UNICAST-DEFAULT group 0`, `flex-algo 128 unicast-default administrative-group include any 0 color 1`, and the BGP `next-hop resolution ribs` line to all devices.
- **Admin-group application**: After a foundation key sets `link_topologies[0]` on a link to the unicast-default `TopologyInfo` pubkey, `show traffic-engineering database` on the connected devices MUST reflect `UNICAST-DEFAULT` admin-group on the interface. Clearing the topology MUST remove the admin-group.
- **Link tagging exclude**: A link in `link_tagging.exclude.links` MUST NOT have an admin-group applied even when `link_topologies[0]` is set onchain.
- **Flex-algo topology**: With links tagged UNICAST-DEFAULT, `show isis flex-algo` on participating devices MUST show algo 128 including only UNICAST-DEFAULT links. Untagged links MUST be absent from the algo-128 LSDB view.
- **Colored tunnel RIB**: `show tunnel rib system-colored-tunnel-rib brief` MUST show (endpoint, color 1) entries for each participating device, resolving via unicast-default tunnels.
- **VPN unicast path selection**: A BGP VPN-IPv4 route carrying `Color:CO(00):1` MUST resolve its next-hop through the color-1 (unicast-default) tunnel in `system-colored-tunnel-rib`, traversing only UNICAST-DEFAULT tagged links.
- **Per-tenant topology — single**: A tenant with `include_topologies = [SHELBY pubkey]` MUST have `Color:CO(00):2` stamped on its inbound routes. A tenant with empty `include_topologies` (default) MUST have `Color:CO(00):1` (UNICAST-DEFAULT).
- **Per-tenant topology — multi**: A tenant with `include_topologies = [UNICAST-DEFAULT pubkey, SHELBY pubkey]` MUST have both `Color:CO(00):1` and `Color:CO(00):2` stamped. `show ip route vrf <tenant>` MUST show the lower-metric color tunnel selected for next-hop resolution.
- **Per-tenant topology — fallback**: Removing a device's node-segment for the preferred topology's algorithm MUST cause EOS to fall back to the next available color on the same prefix without the route going unresolved.
- **Community stamping — per device**: A tenant on a device in `community_stamping.devices` MUST have the color community on its inbound routes. The same tenant on a device NOT in the config MUST NOT have the color community.
- **Community stamping — exclude**: A device in `community_stamping.exclude.devices` MUST NOT have `set extcommunity color` applied regardless of `all` or `tenants` settings.
- **Multicast path isolation**: PIM RPF for a multicast source MUST continue to resolve via IS-IS algo 0 (all links, including both tagged and untagged) regardless of BGP next-hop resolution config.
- **Topology clear**: After `link topology clear --name unicast-default` removes the topology from all links, the controller MUST generate `no traffic-engineering administrative-group UNICAST-DEFAULT` on all previously-tagged interfaces on the next reconciliation cycle.
- **Link drain**: After `doublezero link drain` on a UNICAST-DEFAULT tagged link, `show traffic-engineering database` MUST show both `UNICAST-DEFAULT` and `UNICAST-DRAINED` admin-groups on the interface. `show isis flex-algo` MUST show the link absent from the algo-128 (unicast-default) constrained topology. The link MUST remain visible in algo-0.
- **Link restore**: After `doublezero link restore` on a drained link, `show traffic-engineering database` MUST show only the permanent topology tags. `show isis flex-algo` MUST show the link restored to the constrained topology.
- **Drain exclude precedence**: The `exclude UNICAST-DRAINED` constraint in each `include-any` flex-algo definition MUST take precedence over `include any <topology-bit>` — a link tagged with both UNICAST-DEFAULT and UNICAST-DRAINED MUST NOT appear in the algo-128 SPF (verified per RFC 9350 §5.2.1 exclude-before-include evaluation).
- **Revert**: Setting `enabled: false` and restarting the controller MUST result in all flex-algo config being removed from all devices on the next reconciliation cycle.

#### EOS Verification

The following show commands serve as the basis for assertions in the e2e tests. All commands are hardware-verified on chi-dn-dzd5–dzd8 (EOS 4.31.2F).

**Verify flex-algo participation and path selection:**
```
show isis flex-algo
show isis flex-algo path
```
MUST confirm algo 128 is advertised at Level-2, the `color` field is set, and the selected path includes only UNICAST-DEFAULT tagged links.

**Verify colored tunnel RIB population:**
```
show tunnel rib system-colored-tunnel-rib brief
show tunnel fib isis flex-algo
```
MUST confirm (endpoint, color 1) entries are present for each participating device, resolving via unicast-default tunnels.

**Verify node-segments include flex-algo SIDs:**
```
show isis segment-routing prefix-segments
```
MUST confirm each participating device advertises both an algo-0 index and a flex-algo index for each defined topology.

**Verify BGP next-hop resolution binding is active:**
```
show bgp instance
```
MUST confirm `address-family IPv4 MplsVpn` shows `Resolution RIBs: tunnel-rib colored system-colored-tunnel-rib, tunnel-rib system-tunnel-rib`.

**Verify VPN route color community and resolution:**
```
show bgp vpn-ipv4 detail
show ip route vrf <tenant> bgp
```
MUST confirm BGP VPN-IPv4 routes carry `Color:CO(00):1` and resolve next-hops through `system-colored-tunnel-rib` tunnels.

**Verify TE database admin-groups:**
```
show traffic-engineering database
show traffic-engineering interfaces
```
MUST confirm admin-group membership is visible only on interfaces with a non-empty `link_topologies`.

**Verify multicast RPF uses algo-0 (including colored links):**
```
show ip mroute
```
MUST confirm PIM RPF resolves via the IS-IS unicast RIB (algo 0). The incoming interface for a multicast source reachable via a colored link MUST be the colored interface, unchanged by BGP next-hop resolution config.

---

## Impact

### Codebase

- **serviceability** — new `TopologyInfo` PDA (foundation-managed, one per topology); new `AdminGroupBits` `ResourceExtension` account for persistent bit allocation; new `link_topologies: Vec<Pubkey>` field (cap 8) on `Link`; new `link_topologies: Option<Vec<Pubkey>>` field on `LinkUpdateArgs` with foundation-only write restriction; new `flex_algo_node_segments: Vec<FlexAlgoNodeSegment>` field on `Interface` (one entry per `TopologyInfo` topology, each with its own allocated node segment index); new `include_topologies: Vec<Pubkey>` field on `Tenant` with foundation-only write restriction.
- **controller** — new `-features-config` flag and `features.yaml` config file; reads `link.link_topologies[0]`, resolves `TopologyInfo` PDAs, generates IS-IS TE admin-group config on interfaces (respecting `link_tagging.exclude.links`), flex-algo definitions with `color` field, `system-colored-tunnel-rib` BGP resolution profile, and adds `set extcommunity color` to the existing per-tunnel inbound route-maps (`RM-USER-{{ .Id }}-IN`) for stamping-eligible tunnels; generates `no` commands for full revert when `enabled: false`.
- **CLI** — full topology lifecycle commands (`create`, `update`, `delete`, `clear`, `list`); `link update` gains `--link-topology`; `link get` / `link list` display the field including derived color; `link topology list` warns on disconnected topologies.
- **SDKs** — `TopologyInfo` added to all three language SDKs; `link_topologies` field added to link deserialization structs.

### Operational

**Deployment procedure:**

The following sequence MUST be followed when deploying this RFC to any environment:

1. Deploy the smart contract code update
2. Immediately create the UNICAST-DEFAULT topology via CLI: `doublezero link topology create --name unicast-default --constraint include-any` — this MUST happen before any new link activations are accepted. Link activation MUST fail with an explicit error (`"UNICAST-DEFAULT topology not found"`) if this step is skipped
3. Immediately create the UNICAST-DRAINED topology via CLI: `doublezero link topology create --name unicast-drained --constraint exclude` — this MUST happen before any additional topologies are created. The program enforces that the second topology is named `unicast-drained` and rejects any other name until this invariant is satisfied
4. Run the migration command to tag existing links and allocate loopback node segments for pre-existing accounts
5. Resume normal link activation workflow — new links will be auto-tagged at activation from this point

Attempting to activate a link between steps 1 and 2 will fail with a clear error. There is no silent partial state. Attempting to create a third topology before step 3 is complete will fail with a clear error enforcing the UNICAST-DRAINED invariant.

- Adding a new topology MUST NOT require a code change or deploy — DZF creates the `TopologyInfo` account via the CLI and the controller picks it up on the next reconciliation cycle once `enabled: true`.
- `link_topologies` appends to the serialized layout and defaults to an empty vector on existing accounts. Existing links activated before this RFC will have `link_topologies = []` and MUST be tagged with UNICAST-DEFAULT as part of the testnet rollout migration before `enabled: true` is set.
- The transition from no-color to color-1 on all tenant VRFs is a one-time controller config push. The template section order enforces the correct sequencing within a single reconciliation cycle: the `router traffic-engineering` block and `address-family vpn-ipv4 next-hop resolution` config appear before the `route-map RM-USER-*-IN` blocks in `tunnel.tmpl`, so EOS applies them top-to-bottom in the correct order. Applying the route-map before the RIB is configured would cause VPN routes to go unresolved.

### Testing

| Layer | Tests |
|---|---|
| Smart contract | `TopologyInfo` create/update/delete lifecycle; `AdminGroupBits` `ResourceExtension` allocation and no-reuse after deletion; foundation-only authorization; `link_topologies` assignment and clearing; delete blocked when links assigned; `clear` removes topology from all links; `link_topologies` cap at 8 enforced; `include_topologies` assignment on tenant; foundation-only authorization |
| Controller (unit) | Interface config with and without topology; link tagging exclude list respected; remove/add diff on topology change; `router traffic-engineering` block with `color` field; `system-colored-tunnel-rib` BGP resolution config; single-topology and multi-topology per-tenant stamping; community stamping per-tenant and per-device; stamping exclude respected; full revert on `enabled: false`; new topology triggers config push to all devices |
| SDK (unit) | `TopologyInfo` Borsh round-trip; `link_topologies` field in `link get` / `link list` output; `include_topologies` field in `tenant get` / `tenant list` output across Go, Python, and TypeScript; color displayed correctly |
| End-to-end (cEOS) | Topology create → controller config push verified; admin-group applied and removed on interface; link tagging exclude list verified; flex-algo 128 topology includes only UNICAST-DEFAULT links; `system-colored-tunnel-rib` populated; VPN unicast resolves via color-1 tunnel; per-tenant single and multi-topology stamping verified; topology fallback verified; community stamping per-device verified; stamping exclude verified; multicast RPF unchanged; topology clear removes admin-groups; full revert on `enabled: false` verified |

---

## Security Considerations

- `link_topologies` MUST only be writable by foundation keys. A contributor MUST NOT be able to tag their own link with a topology to influence path steering. The check mirrors the existing pattern used for `link.status` foundation-override.
- `TopologyInfo` accounts MUST only be created, updated, or deleted by foundation keys.
- The controller feature config file (`features.yaml`) is a local file on the controller host. Access to this file SHOULD be restricted to the operator running the controller. An attacker with write access to the config file could enable or disable flex-algo config or manipulate the stamping allowlist without an onchain transaction.
- If a foundation key is compromised, an attacker could reclassify links or create new topology definitions, causing traffic to be steered onto unintended paths. This is the same threat surface as other foundation-key-controlled fields. No new mitigations are introduced.

---

## Backward Compatibility

- `link_topologies` appends to the serialized layout and defaults to an empty vector on existing accounts. The onchain schema requires no migration; existing links MUST be tagged with UNICAST-DEFAULT via the `doublezero-admin` migration command as part of the rollout.
- Devices that do not receive updated config from the controller MUST continue to forward using IS-IS algo 0 only. The flex-algo topology is distributed — a device that does not participate is simply not included in the constrained SPF.
- The controller MUST push `system-colored-tunnel-rib` config and flex-algo definitions before activating per-VRF route-maps. Activating route-maps first causes VPN routes to carry a color community with no matching tunnel RIB entry, leaving them unresolved. The template section order enforces this within a single reconciliation cycle.
- The BGP next-hop resolution profile is applied globally. Per-tenant resolution profile overrides are not supported at this stage.

---

## Observability

Introducing multiple forwarding topologies over the same physical links has implications for the network visualization tools in the lake repository. Today, lake displays a single IS-IS topology where every link is treated equally. With flex-algo, a link's effective participation depends on its topology assignment and the traffic type being considered — an untagged link is present in the algo-0 view but absent from the algo-128 (unicast-default) view used by VPN unicast.

Areas that SHOULD evolve:

- **Topology view filtering** — the topology map SHOULD allow operators to switch between views: all links (algo 0), unicast topology (algo 128), or a per-service overlay. A link's topology assignment SHOULD be visually distinct and its inclusion or exclusion from each forwarding plane SHOULD be clear.
- **Link topology display** — link topology and admin-group membership SHOULD be surfaced on the topology map alongside existing link attributes (latency, capacity). This gives operators immediate visibility into how a link is classified without needing to query the CLI.
- **Service-aware path visualization** — when tracing a path between two nodes, the tool SHOULD reflect which topology that traffic type actually uses. A unicast path between two nodes may differ from the multicast path over the same physical graph.
- **Tenant / VRF context** — VPN unicast traffic is resolved per VRF. A future multi-VRF deployment with per-tenant topologies would require topology views to be scoped to a tenant, showing the paths available within that VRF's forwarding context.

---

## Open Questions

- **Topology naming convention**: The RFC defines `unicast-default` and `shelby` as the initial topologies. Should DZF adopt a consistent naming convention for future topologies? Options include:
  - **Functionality** (`unicast-default`, `low-latency`) — self-documenting from an operational perspective, but ties the name to a specific use-case that may evolve.
  - **Product/tenant names** (`shelby`, `shreds`) — scoped to a customer or service, which may be appropriate if topologies end up being per-tenant rather than network-wide.
  The name is stored onchain in `TopologyInfo` and appears in EOS config (converted to uppercase as the admin-group alias), so it SHOULD be stable once assigned.
