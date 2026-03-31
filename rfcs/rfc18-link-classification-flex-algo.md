# RFC-18: Link Classification — Flex-Algo

## Summary

**Status: `Draft`**

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

DoubleZero contributors operate links with different physical characteristics — low latency, high bandwidth, or both. Today all traffic uses the same IS-IS topology, so every service follows the same paths regardless of what those paths are optimized for. This RFC introduces a link classification model that allows DZF to assign named topology labels to links onchain and use IS-IS Flexible Algorithm (flex-algo) to compute separate constraint-based forwarding topologies per label. Different traffic classes — VPN unicast and IP multicast — can then use different topologies.

**Deliverables:**
- `TopologyInfo` onchain account — DZF creates this to define a topology, with auto-assigned admin-group bit (from the `AdminGroupBits` `ResourceExtension`), flex-algo number, and derived color
- `link_topologies: Vec<Pubkey>` field on the serviceability link account — references assigned topologies; capped at 8 entries; only the first entry is used by the controller in this RFC
- Controller feature config file (`features.yaml`) — loaded at startup; gates flex-algo topology config and link admin-group tagging via a single `enabled` flag; BGP color community stamping has granular per-tenant and per-device control; replaces any onchain feature flag for this capability
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
- **UNICAST-DEFAULT** — The reserved default unicast topology. MUST exist before any link can be activated. The controller resolves it by name via PDA seeds `[b"topology", b"unicast-default"]`. It will naturally be allocated bit 0 since it must be created before any other topology, but its bit is not otherwise special — the controller uses the name, not the bit, to identify it. Applied to all links at activation. Flex-algo uses `include-any UNICAST-DEFAULT`, so only explicitly tagged links participate in the unicast topology. Untagged links are excluded from unicast but remain available to multicast via IS-IS algo 0.
- **UNICAST-DRAINED** — A controller-managed IS-IS TE admin-group alias used to drain a link from all constrained unicast topologies. It is not a `TopologyInfo` PDA. The controller hardcodes the UNICAST-DRAINED bit as a constant; the `AdminGroupBits` bitmap is pre-marked at program initialization to ensure this bit is never allocated to a user topology. The controller always defines the corresponding `administrative-group alias UNICAST-DRAINED group <bit>` on all devices and always injects `exclude <bit>` into every `include-any` flex-algo definition. When a link's `unicast_drained` field is `true`, the controller appends `UNICAST-DRAINED` to that interface's `traffic-engineering administrative-group` alongside its permanent topology tags; the permanent tags are not changed. Because `exclude` is evaluated before `include-any` in flex-algo SPF computation (RFC 9350 §5.2.1), the drained link is pruned from all constrained unicast topologies simultaneously. Multicast (algo-0) is unaffected. Drain is a contributor-writable boolean — contributors may drain their own links as a capacity management tool.

---

## Scope and Limitations

| Scenario | This RFC | Notes |
|---|---|---|
| Default unicast topology via UNICAST-DEFAULT | ✅ | Core deliverable; new links are automatically tagged UNICAST-DEFAULT at activation — no contributor action required |
| Multicast uses all links (algo 0) | ✅ | Natural PIM RPF behavior; includes both tagged and untagged links; no config required |
| Multiple links in the same topology | ✅ | All tagged links participate together in the constrained topology |
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

The cap of 8 exists to keep the `Link` account size deterministic onchain. Only the first entry (`link_topologies[0]`) is used by the controller in this RFC — multiple entries are reserved for future multi-topology-per-link support (e.g., a link participating in both UNICAST-DEFAULT and SHELBY topologies simultaneously, as validated in lab testing).

**Auto-tagging at activation:** when a contributor activates a link, the activation processor MUST automatically set `link_topologies[0]` to the UNICAST-DEFAULT `TopologyInfo` pubkey (resolved by PDA seeds `[b"topology", b"unicast-default"]`). The contributor workflow is unchanged — a link that passes DZF validation immediately carries unicast traffic without any additional step. Foundation keys may subsequently override `link_topologies` to assign a different topology. Links activated before this RFC was deployed have `link_topologies = []` and are handled by the migration command (see Migration for existing accounts).

`link_topologies` overrides MUST only be made by keys in the DZF foundation allowlist. Contributors MUST NOT set this field directly.

A `unicast_drained: bool` field is added to the `Link` account, appended to the serialized layout and defaulting to `false` on existing accounts. This field is contributor-writable — a contributor may drain their own link as a capacity management tool. When `true`, the controller appends `UNICAST-DRAINED` to the interface's admin-group config alongside the permanent topology tags. The field is set independently of `link.status` — a link can be `soft-drained` and `unicast_drained: true` simultaneously; the two are orthogonal.

```rust
// Contributor-writable
if let Some(unicast_drained) = value.unicast_drained {
    link.unicast_drained = unicast_drained;
}
```

```rust
// Foundation-only fields
if globalstate.foundation_allowlist.contains(payer_account.key) {
    if let Some(link_topologies) = value.link_topologies {
        link.link_topologies = link_topologies;
    }
}
```

#### CLI

**Topology lifecycle:**

```
doublezero link topology create --name <NAME> --constraint <include-any|exclude>
doublezero link topology update --name <NAME>
doublezero link topology delete --name <NAME>
doublezero link topology clear  --name <NAME>
doublezero link topology list
```

- `create` — creates a `TopologyInfo` PDA; allocates the lowest available admin-group bit from the `AdminGroupBits` `ResourceExtension`; derives and stores flex-algo number; stores the specified constraint (`include-any` or `exclude`). MUST fail if the name already exists. The `AdminGroupBits` bitmap is pre-marked at program initialization to reserve the UNICAST-DRAINED bit, ensuring it is never allocated to a user topology. Device impact is controlled entirely by the controller feature config — no device config is generated until `flex_algo.enabled: true` is set in the config file.
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
- `doublezero link get` and `doublezero link list` MUST include `link_topologies` in their output, showing the resolved topology names (or "default").

**Unicast drain:**

```
doublezero link update --code <CODE>   --unicast-drained true
doublezero link update --code <CODE>   --unicast-drained false
doublezero link update --pubkey <PUBKEY> --unicast-drained true
doublezero link update --pubkey <PUBKEY> --unicast-drained false
```

- `--unicast-drained true` sets `link.unicast_drained = true`. On the next reconciliation cycle the controller appends `UNICAST-DRAINED` to the interface's admin-group config. The link is excluded from all constrained unicast topologies for all tenants. Multicast (algo-0) is unaffected.
- `--unicast-drained false` clears the flag. The interface admin-group reverts to the permanent topology tags only on the next reconciliation cycle.
- `doublezero link get` and `doublezero link list` MUST display `unicast-drained: true/false`.


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
2. Set `enabled: true` and restart the controller — topology config and link admin-group tagging are pushed to all devices on the next reconciliation cycle. Verify all devices show correct flex-algo state (`show isis flex-algo`, `show tunnel rib system-colored-tunnel-rib brief`)
3. Add tenants or devices to `community_stamping` — restart controller. Stamping can be rolled out per-tenant or per-device to control which traffic begins using constrained topologies

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
| UNICAST-DRAINED | system constant | 1 | — (no flex-algo) | — | Exclude-only; injected into all include-any definitions |
| unicast-default | include-any | 0 | 128 | 1 | Only UNICAST-DEFAULT tagged links |

The flex-algo definition MUST be configured on each DZD by the controller. The `color` field MUST be included and set to `admin_group_bit + 1`. Every `include-any` flex-algo definition MUST include `exclude 1` (the UNICAST-DRAINED bit) — this is unconditional and not dependent on any link being drained. Using UNICAST-DEFAULT as an example:

```
router traffic-engineering
   administrative-group alias UNICAST-DEFAULT group 0
   administrative-group alias UNICAST-DRAINED group 1
   flex-algo
      flex-algo 128 unicast-default
         administrative-group include any 0 exclude 1
         color 1
```

Flex-algo 128 ("unicast-default") computes an IS-IS SPF over only those links tagged `UNICAST-DEFAULT`. The `color 1` field causes EOS to install these tunnels in `system-colored-tunnel-rib` keyed by (endpoint, 1). Devices that participate in flex-algo 128 advertise both an algo-0 node-segment and an algo-128 node-segment via their loopback.


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

`system-colored-tunnel-rib` is auto-populated by EOS when flex-algo definitions carry a `color` field. A VPN route carrying `Color:CO(00):1` resolves its next-hop through the color-1 (unicast-default, algo 128) tunnel to that endpoint. Routes without a color community fall through to `tunnel-rib system-tunnel-rib`, which is auto-populated by IS-IS SR (algo-0) tunnels. `system-connected` is deliberately omitted — this ensures all VPN traffic uses MPLS forwarding (either colored flex-algo or algo-0 SR) and never falls back to plain IP. Without `tunnel-rib system-tunnel-rib`, uncolored VPN routes would be received but their next-hops could not be resolved and they would never make it into the VRF routing table.

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
- If `tenant.include_topologies` is empty, use the default unicast color: resolve the UNICAST-DEFAULT `TopologyInfo` by name (`[b"topology", b"unicast-default"]`) and compute its color as `admin_group_bit + 1`.

Multicast tunnels do not receive the color community — multicast RPF resolves via IS-IS algo 0 and does not use `system-colored-tunnel-rib`.

---

### Controller Changes

All config changes are applied to `tunnel.tmpl`. Five additions are required. All blocks are conditioned on `$.Config.FlexAlgo.Enabled` — when disabled, no flex-algo config is generated and the controller generates `no` commands to remove any previously-pushed flex-algo config.

#### 1. Interface admin-group tagging

Inside the existing `{{- range .Device.Interfaces }}` block, after the `isis metric` / `isis network point-to-point` lines, add admin-group config for physical IS-IS links:

```
{{- if and .Ip.IsValid .IsPhysical .Metric .IsLink (not .IsSubInterfaceParent) (not .IsCYOA) (not .IsDIA) }}
   traffic-engineering
   {{- if and .LinkTopologies (not ($.Config.FlexAlgo.LinkTagging.IsExcluded .PubKey)) }}
   traffic-engineering administrative-group {{ $.Strings.Join " " ($.Strings.ToUpperEach .LinkTopologyNames) }}{{ if .UnicastDrained }} UNICAST-DRAINED{{ end }}
   {{- else }}
   no traffic-engineering administrative-group
   {{- end }}
{{- end }}
```

`.LinkTopologies` is the resolved list of `TopologyInfo` accounts from `link.link_topologies`; it is empty when `link_topologies` is empty. `.LinkTopologyNames` is the corresponding list of names. `.UnicastDrained` is `link.unicast_drained`. The controller renders all permanent topology names followed by `UNICAST-DRAINED` (if drained) in a single command — EOS overwrites the existing admin-group assignment with exactly this set. This means:
- A link transitioning from two topologies to one re-applies only the surviving topology, atomically replacing the previous set
- A link losing its last topology receives `no traffic-engineering administrative-group`
- Draining appends `UNICAST-DRAINED` to the existing set without altering permanent tags
- The targeted `no traffic-engineering administrative-group <NAME>` command is never used, avoiding the EOS behavior where it would remove all groups regardless of the name specified

Interface-level admin-group tagging is conditioned on `$.Config.FlexAlgo.Enabled` alone — since an interface may have topologies assigned onchain while the feature is disabled.

#### 2. router traffic-engineering block

Add after the `router isis 1` block, conditional on topologies being defined and the feature being enabled:

```
{{- if and $.Config.FlexAlgo.Enabled .LinkTopologies }}
router traffic-engineering
   router-id ipv4 {{ .Device.Vpn4vLoopbackIP }}
   administrative-group alias UNICAST-DRAINED group 1
   {{- range .LinkTopologies }}
   administrative-group alias {{ $.Strings.ToUpper .Name }} group {{ .AdminGroupBit }}
   {{- end }}
   !
   flex-algo
   {{- range .LinkTopologies }}
      flex-algo {{ .FlexAlgoNumber }} {{ .Name }}
         {{- if eq .Constraint "include-any" }}
         administrative-group include any {{ .AdminGroupBit }} exclude 1
         {{- else }}
         administrative-group exclude {{ .AdminGroupBit }}
         {{- end }}
         color {{ .Color }}
   {{- end }}
{{- end }}
```

`.LinkTopologies` is the ordered list of `TopologyInfo` accounts, sorted by `AdminGroupBit`. `.Color` is computed as `AdminGroupBit + 1`. `UNICAST-DRAINED group 1` is a system constant — always rendered first, not derived from any `TopologyInfo` PDA. The flex-algo name (e.g., `unicast-default`) is the topology name stored in `TopologyInfo`.

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

**Topology creation backfill:** When a new `TopologyInfo` account is created, the creation processor MUST iterate all existing `Interface` accounts with `loopback_type = Vpnv4` and allocate a `FlexAlgoNodeSegment` entry for the new topology on each. This is automatic — no operator action is required. The controller picks up the new entries on its next cycle and pushes the updated `node-segment` config to all devices. This ensures all devices always advertise a node-SID for every defined topology, maintaining the universal participation requirement.

In the template, `.FlexAlgoNodeSegments` is accessed directly on the current interface (the `.` context within `{{- range .Device.Interfaces }}`). The controller populates this from the interface's onchain `flex_algo_node_segments` list during rendering, resolving the topology name from each entry's `TopologyInfo` pubkey. This is intentionally distinct from `.LinkTopologies` (which describes which topologies a specific link is tagged with) — the loopback template is concerned with which topologies this device participates in, not with link tagging.

**Migration for existing accounts:** A one-time `doublezero-admin` CLI migration command MUST be provided covering two tasks:

1. **Links** — iterate all existing `Link` accounts with `link_topologies = []` and set `link_topologies[0]` to the UNICAST-DEFAULT `TopologyInfo` pubkey. Links activated after this RFC are auto-tagged at activation; this migration covers links activated before the RFC was deployed.
2. **Vpnv4 loopbacks** — iterate all existing `Interface` accounts with `loopback_type = Vpnv4` and allocate a `FlexAlgoNodeSegment` entry for each known `TopologyInfo` account. Existing `node_segment_idx` assignments (algo-0) are unchanged — this is purely additive. Loopbacks activated after this RFC will have entries allocated at activation time; topologies created after this RFC will be backfilled automatically at topology creation time.

The migration command MUST be idempotent — re-running it MUST skip already-migrated accounts and only process those still requiring migration. It MUST emit a summary on completion (e.g. `migrated 12 links, 4 loopbacks; skipped 3 already migrated`). A `--dry-run` flag MUST be supported to preview the accounts that would be migrated without applying any changes.

The controller MUST check at startup that no Vpnv4 loopback has an empty `flex_algo_node_segments` list when `flex_algo.enabled: true` is set. If any unset loopbacks are found, `enabled: true` MUST be treated as a no-op for that startup cycle — the controller MUST NOT push any flex-algo config to any device, MUST emit a prominent error identifying the unset loopbacks by pubkey, and MUST direct the operator to run the migration command and restart. The `features.yaml` flag is not modified. Flex-algo config will not be applied until the migration is complete and the controller is restarted cleanly. This prevents silently pushing a broken topology where some devices are unreachable via the constrained path.

Interface admin-group blocks are conditional on `.LinkTopologies` being non-empty. The flex-algo node-segment lines within the loopback block are conditional on `.FlexAlgoNodeSegments` being non-empty — if the list is empty, the `range` loop produces no output and only the algo-0 `node-segment` line is rendered. Devices with no topologies defined produce identical config to today.

---

### SDK Changes

`TopologyInfo` MUST be added to the Go, Python, and TypeScript SDKs. The `link` deserialization structs MUST include the new `link_topologies: Vec<Pubkey>` field. The `tenant` deserialization structs MUST include the new `include_topologies: Vec<Pubkey>` field. Fixture files MUST be regenerated.

---

### Tests

#### Smart contract (integration tests)

**TopologyInfo lifecycle:**
- A foundation key MUST be able to create a `TopologyInfo` account; admin-group bit MUST be allocated from the `AdminGroupBits` `ResourceExtension`. The first topology will receive bit 0 and flex-algo number 128.
- Creating a second topology MUST allocate bit 2 — the UNICAST-DRAINED bit is pre-marked in the bitmap at initialization and MUST NOT be allocated to a user topology.
- A non-foundation key MUST NOT be able to create a `TopologyInfo` account; the instruction MUST be rejected with an authorization error.
- All `TopologyInfo` fields are immutable after creation; an `update` instruction MUST be rejected or be a no-op.
- A non-foundation key MUST NOT be able to update a `TopologyInfo` account.
- `delete` MUST succeed when no links reference the topology; the `TopologyInfo` PDA MUST be removed onchain.
- `delete` MUST fail when one or more links still reference the topology.
- After `clear`, all links previously assigned the topology MUST have `link_topologies = []`.
- After `clear` followed by `delete`, the `TopologyInfo` PDA MUST be absent.
- Admin-group bits from deleted topologies MUST NOT be reused by subsequently created topologies; the `AdminGroupBits` `ResourceExtension` bitmap MUST persist the allocated bit after PDA deletion.
- After `delete`, the controller MUST remove the deleted topology's admin-group alias and flex-algo definition from all devices on the next reconciliation cycle — the `TopologyInfo` PDA is absent, so the controller no longer includes it in rendered config.

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

**Unicast drain:**
- `unicast_drained` MUST default to `false` on existing accounts deserialized from pre-upgrade binary data.
- A contributor key MUST be able to set `unicast_drained = true` on their own link.
- A contributor key MUST NOT be able to set `unicast_drained` on a link they do not own; the instruction MUST be rejected with an authorization error.
- Setting `unicast_drained = true` MUST NOT modify `link_topologies` or `link.status`.

#### Controller (unit tests)

- A link with `link_topologies = []` MUST produce interface config with no `traffic-engineering administrative-group` line.
- A link with `link_topologies[0]` referencing the UNICAST-DEFAULT `TopologyInfo` (name "unicast-default", constraint `IncludeAny`) MUST produce interface config with `traffic-engineering administrative-group UNICAST-DEFAULT`.
- A link in `link_tagging.exclude.links` MUST produce `no traffic-engineering administrative-group` regardless of onchain `link_topologies` assignment.
- Transitioning a link from a topology to default MUST produce a `no traffic-engineering administrative-group` diff.
- Transitioning a link from one topology to another MUST produce the correct remove/add diff.
- The `router traffic-engineering` block MUST always include `administrative-group alias UNICAST-DRAINED group 1` when `enabled: true`, regardless of whether any link is drained.
- The `router traffic-engineering` block MUST include `color <admin_group_bit + 1>` on each flex-algo definition.
- Each `include-any` flex-algo definition MUST include `exclude 1` unconditionally. An `exclude`-constraint topology MUST NOT have `exclude 1` injected.
- A link with `unicast_drained = true` and `link_topologies[0] = UNICAST-DEFAULT` MUST produce interface config `traffic-engineering administrative-group UNICAST-DEFAULT UNICAST-DRAINED`.
- A link with `unicast_drained = true` MUST NOT have `link_topologies` modified — permanent tags are unchanged.
- A link with `unicast_drained = false` after previously being `true` MUST produce interface config with only the permanent topology tags, identical to a never-drained link.
- The BGP `next-hop resolution ribs tunnel-rib colored system-colored-tunnel-rib tunnel-rib system-tunnel-rib` config MUST be generated correctly when `enabled: true`.
- A per-tunnel inbound route-map MUST include `set extcommunity color 1` for a unicast tenant with empty `include_topologies` when the tenant or device is in the `community_stamping` config and `.LinkTopologies` is non-empty.
- A per-tunnel inbound route-map MUST include `set extcommunity color 1 color 3` for a unicast tenant with `include_topologies` referencing two `TopologyInfo` accounts (bits 0 and 2 — the UNICAST-DRAINED bit is pre-marked and skipped by the allocator).
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

- **Topology creation**: After a foundation key creates a `TopologyInfo` for "unicast-default" (constraint include-any) and `enabled: true` is set, the controller MUST push `router traffic-engineering` config including `administrative-group alias UNICAST-DRAINED group 1`, `administrative-group alias UNICAST-DEFAULT group <bit>`, the corresponding `flex-algo` definition with `include any <bit> exclude 1 color <bit+1>`, and the BGP `next-hop resolution ribs` line to all devices.
- **Admin-group application**: After a foundation key sets `link_topologies[0]` on a link to the unicast-default `TopologyInfo` pubkey, `show traffic-engineering database` on the connected devices MUST reflect `UNICAST-DEFAULT` admin-group on the interface. Clearing the topology MUST remove the admin-group.
- **Link tagging exclude**: A link in `link_tagging.exclude.links` MUST NOT have an admin-group applied even when `link_topologies[0]` is set onchain.
- **Flex-algo topology**: With links tagged UNICAST-DEFAULT, `show isis flex-algo` on participating devices MUST show algo 128 including only UNICAST-DEFAULT links. Untagged links MUST be absent from the algo-128 LSDB view.
- **Colored tunnel RIB**: `show tunnel rib system-colored-tunnel-rib brief` MUST show (endpoint, color 1) entries for each participating device, resolving via unicast-default tunnels.
- **VPN unicast path selection**: A BGP VPN-IPv4 route carrying `Color:CO(00):1` MUST resolve its next-hop through the color-1 (unicast-default) tunnel in `system-colored-tunnel-rib`, traversing only UNICAST-DEFAULT tagged links.
- **Per-tenant topology — single**: A tenant with `include_topologies = [SHELBY pubkey]` MUST have `Color:CO(00):3` stamped on its inbound routes (SHELBY receives bit 2 — the UNICAST-DRAINED bit is pre-marked and skipped — giving color 3). A tenant with empty `include_topologies` (default) MUST have `Color:CO(00):1` (UNICAST-DEFAULT).
- **Per-tenant topology — multi**: A tenant with `include_topologies = [UNICAST-DEFAULT pubkey, SHELBY pubkey]` MUST have both `Color:CO(00):1` and `Color:CO(00):3` stamped. `show ip route vrf <tenant>` MUST show the lower-metric color tunnel selected for next-hop resolution.
- **Per-tenant topology — fallback**: Removing a device's node-segment for the preferred topology's algorithm MUST cause EOS to fall back to the next available color on the same prefix without the route going unresolved.
- **Community stamping — per device**: A tenant on a device in `community_stamping.devices` MUST have the color community on its inbound routes. The same tenant on a device NOT in the config MUST NOT have the color community.
- **Community stamping — exclude**: A device in `community_stamping.exclude.devices` MUST NOT have `set extcommunity color` applied regardless of `all` or `tenants` settings.
- **Multicast path isolation**: PIM RPF for a multicast source MUST continue to resolve via IS-IS algo 0 (all links, including both tagged and untagged) regardless of BGP next-hop resolution config.
- **Topology clear**: After `link topology clear --name unicast-default` removes the topology from all links, the controller MUST generate `no traffic-engineering administrative-group UNICAST-DEFAULT` on all previously-tagged interfaces on the next reconciliation cycle.
- **Unicast drain**: After `doublezero link update --unicast-drained true` on a UNICAST-DEFAULT tagged link, `show traffic-engineering database` MUST show both `UNICAST-DEFAULT` and `UNICAST-DRAINED` admin-groups on the interface. `show isis flex-algo` MUST show the link absent from the algo-128 (unicast-default) constrained topology. The link MUST remain visible in algo-0. `show ip mroute` MUST confirm multicast RPF is unchanged.
- **Unicast restore**: After `doublezero link update --unicast-drained false`, `show traffic-engineering database` MUST show only the permanent topology tags. `show isis flex-algo` MUST show the link restored to the constrained topology.
- **Drain exclude precedence**: The `exclude 1` clause in each `include-any` flex-algo definition MUST take precedence over `include any <topology-bit>` — a link with `unicast_drained = true` MUST NOT appear in any constrained unicast topology's SPF regardless of its permanent tags (RFC 9350 §5.2.1).
- **UNICAST-DRAINED alias always present**: With `enabled: true` and no links drained, `show traffic-engineering` MUST show `administrative-group alias UNICAST-DRAINED group 1` defined on all devices. The alias and `exclude 1` clauses are structural, not conditional on drain state.
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

- **serviceability** — new `TopologyInfo` PDA (foundation-managed, one per topology); new `AdminGroupBits` `ResourceExtension` account for persistent bit allocation (UNICAST-DRAINED bit pre-marked at initialization; user topologies start at bit 2); new `link_topologies: Vec<Pubkey>` field (cap 8) on `Link` with foundation-only write restriction; new `unicast_drained: bool` field on `Link` (contributor-writable, defaults to `false`); new `flex_algo_node_segments: Vec<FlexAlgoNodeSegment>` field on `Interface` (one entry per `TopologyInfo` topology, each with its own allocated node segment index); new `include_topologies: Vec<Pubkey>` field on `Tenant` with foundation-only write restriction.
- **controller** — new `-features-config` flag and `features.yaml` config file; reads `link.link_topologies[0]` and `link.unicast_drained`, resolves `TopologyInfo` PDAs, generates IS-IS TE admin-group config on interfaces (appending `UNICAST-DRAINED` when `unicast_drained = true`, respecting `link_tagging.exclude.links`), always defines `administrative-group alias UNICAST-DRAINED group 1` and injects `exclude 1` into every `include-any` flex-algo definition, `system-colored-tunnel-rib` BGP resolution profile, and adds `set extcommunity color` to the existing per-tunnel inbound route-maps (`RM-USER-{{ .Id }}-IN`) for stamping-eligible tunnels; generates `no` commands for full revert when `enabled: false`.
- **CLI** — full topology lifecycle commands (`create`, `update`, `delete`, `clear`, `list`); `link update` gains `--link-topology` and `--unicast-drained true|false`; `link get` / `link list` display both fields including derived color; `link topology list` warns on disconnected topologies.
- **SDKs** — `TopologyInfo` added to all three language SDKs; `link_topologies` and `unicast_drained` fields added to link deserialization structs.

### Operational

**Deployment procedure:**

The following sequence MUST be followed when deploying this RFC to any environment:

1. Deploy the smart contract code update
2. Create the UNICAST-DEFAULT topology via CLI: `doublezero link topology create --name unicast-default --constraint include-any` — this MUST happen before any new link activations are accepted. Link activation MUST fail with an explicit error (`"UNICAST-DEFAULT topology not found"`) if this step is skipped
3. Run the migration command to tag existing links and allocate loopback node segments for pre-existing accounts
4. Set `flex_algo.enabled: true` in `features.yaml` and restart the controller — this triggers the first push of flex-algo config to all devices
5. Resume normal link activation workflow — new links will be auto-tagged at activation from this point

Attempting to activate a link between steps 1 and 2 will fail with a clear error. There is no silent partial state. UNICAST-DRAINED requires no bootstrap step — the controller defines the alias and injects the exclude clause as system constants, regardless of whether any link is drained.

- Adding a new topology MUST NOT require a code change or deploy — DZF creates the `TopologyInfo` account via the CLI, the creation processor automatically backfills `FlexAlgoNodeSegment` entries on all existing Vpnv4 loopbacks, and the controller picks up the new topology and updated loopback entries on the next reconciliation cycle.

### Testing

| Layer | Tests |
|---|---|
| Smart contract | `TopologyInfo` create/update/delete lifecycle; `AdminGroupBits` `ResourceExtension` allocation and no-reuse after deletion; UNICAST-DRAINED bit pre-marked at program initialization; foundation-only authorization; `link_topologies` assignment and clearing; delete blocked when links assigned; `clear` removes topology from all links; `link_topologies` cap at 8 enforced; `include_topologies` assignment on tenant; `unicast_drained` contributor-writable set/unset on Link; `flex_algo_node_segments` allocated at loopback activation and deallocated on `remove`; topology creation triggers `FlexAlgoNodeSegment` backfill on all existing Vpnv4 loopbacks |
| Controller (unit) | Interface config with and without topology; link tagging exclude list respected; remove/add diff on topology change; `router traffic-engineering` block with `color` field; UNICAST-DRAINED alias and `exclude 1` always present in all `include-any` definitions regardless of drain state; `unicast_drained: true` appends UNICAST-DRAINED to interface admin-group; `unicast_drained: false` removes UNICAST-DRAINED from interface admin-group; loopback node-segment lines rendered correctly — one per topology from `flex_algo_node_segments`; `system-colored-tunnel-rib` BGP resolution config; single-topology and multi-topology per-tenant stamping; community stamping per-tenant and per-device; stamping exclude respected; startup check blocks `enabled: true` when any Vpnv4 loopback has empty `flex_algo_node_segments`; full revert on `enabled: false`; new topology triggers config push to all devices |
| SDK (unit) | `TopologyInfo` Borsh round-trip; `link_topologies` and `unicast_drained` fields in `link get` / `link list` output; `include_topologies` field in `tenant get` / `tenant list` output across Go, Python, and TypeScript; color displayed correctly |
| End-to-end (cEOS) | Topology create → controller config push verified; admin-group applied and removed on interface; UNICAST-DRAINED alias and `exclude 1` verified on all devices; unicast-drained link excluded from all constrained topologies but present in algo-0; link tagging exclude list verified; flex-algo 128 topology includes only UNICAST-DEFAULT links; `system-colored-tunnel-rib` populated; VPN unicast resolves via color-1 tunnel; per-tenant single and multi-topology stamping verified; topology fallback verified; community stamping per-device verified; stamping exclude verified; multicast RPF unchanged; new topology creation → node-segment lines appear on all devices; topology clear removes admin-groups; full revert on `enabled: false` verified |

---

## Security Considerations

- `link_topologies` MUST only be writable by foundation keys. A contributor MUST NOT be able to tag their own link with a topology to influence path steering. The check mirrors the existing pattern used for `link.status` foundation-override.
- `unicast_drained` is contributor-writable by design — contributors may drain their own links as a capacity management tool, consistent with soft-drain. A contributor draining their link removes it from all constrained unicast topologies for all tenants; this is an accepted operational risk and mirrors existing contributor control over link availability.
- `TopologyInfo` accounts MUST only be created, updated, or deleted by foundation keys.
- The controller feature config file (`features.yaml`) is a local file on the controller host. Access to this file SHOULD be restricted to the operator running the controller. An attacker with write access to the config file could enable or disable flex-algo config or manipulate the stamping allowlist without an onchain transaction.
- If a foundation key is compromised, an attacker could reclassify links or create new topology definitions, causing traffic to be steered onto unintended paths. This is the same threat surface as other foundation-key-controlled fields. No new mitigations are introduced.

---

## Backward Compatibility

- Devices that do not receive updated config from the controller MUST continue to forward using IS-IS algo 0 only. The flex-algo topology is distributed — a device that does not participate is simply not included in the constrained SPF.
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
