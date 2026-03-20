# RFC-18: Link Classification — Flex-Algo

## Summary

**Status: `Draft`**

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

DoubleZero contributors operate links with different physical characteristics — low latency, high bandwidth, or both. Today all traffic uses the same IS-IS topology, so every service follows the same paths regardless of what those paths are optimized for. This RFC introduces a link classification model that allows DZF to assign named color labels to links onchain and use IS-IS Flexible Algorithm (flex-algo) to compute separate constraint-based forwarding topologies per color. Different traffic classes — VPN unicast and IP multicast — can then use different topologies.

**Deliverables:**
- `LinkColorInfo` onchain account — DZF creates this to define a color, with auto-assigned admin-group bit (from the `AdminGroupBits` `ResourceExtension`), flex-algo number, and derived EOS color value
- `link_colors: Vec<Pubkey>` field on the serviceability link account — references assigned colors; capped at 8 entries; only the first entry is used by the controller in this RFC
- Controller feature config file (`features.yaml`) — loaded at startup; gates flex-algo topology config, link admin-group tagging, and BGP color community stamping independently; replaces any onchain feature flag for this capability
- Controller logic — translates colors into IS-IS TE admin-groups on interfaces, generates flex-algo topology definitions, configures `system-colored-tunnel-rib` as the BGP next-hop resolution source, and applies BGP color extended community route-maps per tunnel; all conditioned on the controller config

**Scope:**
- Delivers traffic-class-level segregation: multicast vs. VPN unicast at the network level
- Per-tenant unicast path differentiation via `include_topology_colors: Vec<Pubkey>` on the `Tenant` account — all unicast tenants receive color 1 (UNICAST-DEFAULT) by default; `include_topology_colors` overrides this to assign specific color values and steer the tenant onto a designated topology

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
- **Link color** — A DZF-defined label assigned to a link that maps to an IS-IS TE admin-group. Determines which flex-algo topologies include or exclude the link.
- **Link color constraint** — Each `LinkColorInfo` defines either an `IncludeAny` or `Exclude` constraint. `IncludeAny`: only links explicitly tagged with this color participate in the topology. `Exclude`: all links except those tagged with this color participate. UNICAST-DEFAULT uses `IncludeAny`.
- **system-colored-tunnel-rib** — An EOS system RIB auto-populated when flex-algo definitions carry a `color` field. Keyed by (endpoint, color). Used by BGP next-hop resolution to steer VPN routes onto constrained topologies based on the BGP color extended community carried on the route.
- **UNICAST-DEFAULT** — The reserved default color. MUST be the first color created by DZF and MUST be assigned admin-group bit 0, flex-algo 128, and EOS color value 1. These values are protocol invariants — the controller resolves the default tenant color by looking up the `LinkColorInfo` where `admin_group_bit == 0`, not by creation order. Applied to all links eligible for the default unicast topology. Flex-algo 128 uses `include-any UNICAST-DEFAULT`, so only explicitly tagged links participate in the unicast topology. Untagged links are excluded from unicast but remain available to multicast via IS-IS algo 0.

---

## Scope and Limitations

| Scenario | This RFC | Notes |
|---|---|---|
| Default unicast topology via UNICAST-DEFAULT color | ✅ | Core deliverable; all unicast-eligible links must be explicitly tagged |
| Multicast uses all links (algo 0) | ✅ | Natural PIM RPF behavior; includes both tagged and untagged links; no config required |
| Multiple links with the same color | ✅ | All tagged links participate together in the constrained topology |
| New links excluded from unicast by default | ✅ | `include-any` strictly excludes untagged links — verified in lab. New links must be explicitly tagged before they carry unicast traffic |
| Per-tenant unicast path differentiation | ✅ | Architecture proven in lab (BGP color extended communities + `system-colored-tunnel-rib`). All unicast tenants receive color 1 (UNICAST-DEFAULT) by default; `include_topology_colors` overrides this with specific colors to steer onto a designated topology |
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
| CBF with non-default VRFs | Per-tenant VRF, per-DSCP | Different constrained topology per tenant with DSCP-based sub-steering | Medium — TCAM profile change; builds on flex-algo colors defined here | First tenant requiring DSCP-level path differentiation within a VRF |
| SR-TE | Per-prefix or per-flow | Explicit path control with segment lists; per-prefix or per-DSCP steering independent of IGP topology | High — controller must compute or define explicit segment lists per policy, and set BGP Color Extended Community on routes per-tenant | Per-prefix SLA requirements, or when per-tenant flex-algo color is insufficient |
| RSVP-TE | Per-LSP (P2P unicast) or per-tree (P2MP multicast) | Hard bandwidth reservation with admission control | High — RSVP-TE on all path devices, IS-IS TE bandwidth advertisement, controller logic to provision per-tenant tunnel interfaces | SLA-backed bandwidth guarantees where admission control is required, not just path preference |

An `exclude_topology_colors: Vec<Pubkey>` field on `Tenant` is a natural extension of the `include_topology_colors` model defined here — it would allow a tenant to explicitly avoid certain topologies. This is deferred; no network infrastructure changes are required to add it when needed.

---

## Detailed Design

### Link Color Model

#### LinkColorInfo account

DZF creates a `LinkColorInfo` PDA per color. It stores the color's name and auto-assigned routing parameters. The program MUST auto-assign the lowest available admin-group bit from the `AdminGroupBits` `ResourceExtension` account, and derive the corresponding flex-algo number and EOS color value using the formula:

```
admin_group_bit  = next available bit from AdminGroupBits ResourceExtension (0–127)
flex_algo_number = 128 + admin_group_bit
eos_color_value  = admin_group_bit + 1   (derived, not stored)
```

This formula ensures the admin-group bit, flex-algo number, and EOS color value are always in the EOS-supported ranges (bits 0–127, algos 128–255, color 1–4294967295) and are derived consistently from each other. The EOS color value is not stored onchain — it is computed by the controller wherever needed.

The `AdminGroupBits` `ResourceExtension` is a persistent bitmap on `GlobalState` that tracks allocated admin-group bits across the lifetime of the program, including bits from deleted colors. This ensures bits are never reused after deletion — reusing a bit before all devices have had their config updated would cause those devices to apply the new color's constraints to interfaces still carrying the old bit's admin-group. The bitmap survives PDA deletion, which a PDA-scan approach cannot guarantee.

```rust
#[derive(BorshSerialize, BorshDeserialize, Debug)]
pub enum LinkColorConstraint {
    IncludeAny = 0,  // only tagged links participate in the topology
    Exclude    = 1,  // all links except tagged participate in the topology
}

#[derive(BorshSerialize, BorshDeserialize, Debug)]
pub struct LinkColorInfo {
    pub name: String,                    // e.g. "unicast-default"
    pub admin_group_bit: u8,             // auto-assigned from AdminGroupBits ResourceExtension, 0–127
    pub flex_algo_number: u8,            // auto-assigned, 128–255; always 128 + admin_group_bit
    pub constraint: LinkColorConstraint, // IncludeAny or Exclude
}
```

PDA seeds: `[b"link-color-info", name.as_bytes()]`. `LinkColorInfo` accounts MUST only be created or updated by foundation keys.

Name length MUST NOT exceed 32 bytes, enforced by the program on `create`. This keeps PDA seeds well within the 32-byte limit and ensures the admin-group alias name is reasonable in EOS config.

The program MUST validate `admin_group_bit <= 127` on `create` and MUST return an explicit error if all 128 slots are exhausted. This is a hard constraint: EOS supports bits 0–127 only, and `128 + 127 = 255` is the maximum representable value in `flex_algo_number: u8`.

#### link_colors field on Link

A `link_colors: Vec<Pubkey>` field is added to the serviceability `Link` account, capped at 8 entries. Each entry holds the pubkey of a `LinkColorInfo` PDA. An empty vector indicates no color is assigned. The field appends to the end of the serialized layout, defaulting to an empty vector on existing accounts.

The cap of 8 exists to keep the `Link` account size deterministic on-chain. Only the first entry (`link_colors[0]`) is used by the controller in this RFC — multiple entries are reserved for future multi-color-per-link support (e.g., a link participating in both UNICAST-DEFAULT and SHELBY topologies simultaneously, as validated in lab testing).

`link_colors` MUST only be set by keys in the DZF foundation allowlist. Contributors MUST NOT set this field. Link tagging is a DZF policy decision — there is no automated selection based on `link.bandwidth` or `link.link_type` at this stage.

```rust
// Foundation-only fields
if globalstate.foundation_allowlist.contains(payer_account.key) {
    if let Some(link_colors) = value.link_colors {
        link.link_colors = link_colors;
    }
}
```

#### CLI

**Color lifecycle:**

```
doublezero link color create --name <NAME> --constraint <include-any|exclude>
doublezero link color update --name <NAME>
doublezero link color delete --name <NAME>
doublezero link color clear  --name <NAME>
doublezero link color list
```

- `create` — creates a `LinkColorInfo` PDA; allocates the lowest available admin-group bit from the `AdminGroupBits` `ResourceExtension`; derives and stores flex-algo number; stores the specified constraint (`include-any` or `exclude`). MUST fail if the name already exists. The first color created MUST be named `unicast-default` and will be allocated bit 0 — this is a protocol invariant and the program MUST enforce it by rejecting any `create` instruction where the `AdminGroupBits` bitmap is empty and the name is not `unicast-default`. Device impact is controlled entirely by the controller feature config — no device config is generated until `flex_algo.enabled: true` is set in the config file.
- `update` — reserved for future use; all fields are immutable after creation. No device config change.
- `delete` — removes the `LinkColorInfo` PDA onchain. MUST fail if any link still references this color (use `clear` first). On the next reconciliation cycle, the controller removes the deleted color's admin-group alias and flex-algo definition from all devices. Admin-group bits from deleted colors MUST NOT be reused — the `AdminGroupBits` `ResourceExtension` bitmap persists allocated bits permanently.
- `clear` — removes this color from all links currently assigned to it, setting `link_colors` to an empty vector on each. This is a multi-transaction sweep — one `LinkUpdateArgs` instruction is submitted per assigned link; it is not atomic. If the sweep fails partway through, the operator MUST re-run `clear`; the operation is idempotent and will only submit instructions for links that still reference the color. The `delete` guard (which rejects if any link still references the color) is the safety net — partial completion is safe because a re-run will clear the remaining references before deletion is attempted. On the next reconciliation cycle, the controller re-applies only the remaining colors on each affected interface — if other colors remain, `traffic-engineering administrative-group <remaining>` is applied; if no colors remain, `no traffic-engineering administrative-group` is applied.
- `list` — fetches all `LinkColorInfo` accounts and all `Link` accounts and groups links by color. SHOULD emit a warning if any color has fewer links tagged than the minimum required for a connected topology.

```
NAME               CONSTRAINT    FLEX-ALGO   ADMIN-GROUP BIT   EOS COLOR   LINKS
default            —             —           —                 —           link-abc123, link-def456
unicast-default    include-any   128         0                 1           link-xyz789
```

**Link color assignment:**

```
doublezero link update --pubkey <PUBKEY> --link-color <NAME>
doublezero link update --pubkey <PUBKEY> --link-color default
doublezero link update --code  <CODE>   --link-color <NAME>
```

- `--link-color <NAME>` MUST resolve the color name to the corresponding `LinkColorInfo` PDA pubkey before submitting the instruction — the onchain field stores pubkeys, not names. Sets `link_colors[0]`.
- `--link-color default` sets `link_colors` to an empty vector, removing any color assignment.
- `doublezero link get` and `doublezero link list` MUST include `link_colors` in their output, showing the resolved color names (or "default").

#### Tenant topology color assignment

An `include_topology_colors: Vec<Pubkey>` field is added to the serviceability `Tenant` account. Each entry holds the pubkey of a `LinkColorInfo` PDA. All unicast tenants receive color 1 (UNICAST-DEFAULT) by default; setting `include_topology_colors` overrides this to assign specific colors based on business requirements. The field appends to the end of the serialized layout, defaulting to an empty vector on existing accounts.

```rust
#[derive(BorshSerialize, BorshDeserialize, Debug)]
pub struct Tenant {
    // ... existing fields ...
    pub include_topology_colors: Vec<Pubkey>,  // appended; defaults to []
}
```

`include_topology_colors` MUST only be set by foundation keys. This is a routing policy decision — contributors MUST NOT be able to steer their own traffic onto a different topology by modifying this field.

When a tenant has one entry in `include_topology_colors`, the controller resolves the `LinkColorInfo` PDA and stamps its EOS color value on inbound routes for that tenant. When a tenant has multiple entries, the controller stamps all corresponding color values — EOS then selects the best available colored tunnel by IGP metric (lowest metric wins; highest color number breaks ties). This enables a fallback chain: if the preferred topology's tunnel becomes unavailable, EOS automatically falls back to the next-best color on the same prefix without the route going unresolved. This behavior has been verified in lab testing.

**CLI:**

```
doublezero tenant update --code <CODE> --include-topology-colors <NAME>[,<NAME>]
doublezero tenant update --code <CODE> --include-topology-colors default
```

- `--include-topology-colors <NAME>[,<NAME>]` resolves each color name to the corresponding `LinkColorInfo` PDA pubkey before submitting the instruction.
- `--include-topology-colors default` sets `include_topology_colors` to an empty vector, reverting the tenant to the default color 1 (UNICAST-DEFAULT).
- `doublezero tenant get` and `doublezero tenant list` MUST display `include_topology_colors` showing resolved color names (or "default").

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

The config file decouples onchain state preparation from device deployment. DZF can create colors and tag links before any device receives flex-algo config, then enable features progressively:

1. DZF creates `LinkColorInfo` accounts and assigns `link_colors` on links — no device impact while `enabled: false`
2. Set `enabled: true` and restart the controller — topology config is pushed to all devices on the next reconciliation cycle. No admin-group tagging or community stamping yet
3. Verify all devices show correct flex-algo state (`show isis flex-algo`, `show tunnel rib system-colored-tunnel-rib brief`)
4. Add specific links to the tagging config or leave the exclude list empty to tag all onchain-assigned links — restart controller
5. Add tenants or devices to `community_stamping` — restart controller. Stamping can be rolled out per-tenant or per-device to control which traffic begins using constrained topologies

**Precedence for community stamping:** a device is stamped if `all: true`, OR its pubkey is in `devices`, OR the tenant's pubkey is in `tenants` — unless the device's pubkey is in `exclude.devices`, which overrides all positive rules.

**Asymmetric routing:** if community stamping is enabled on some devices but not others, routes entering the network at unstamped devices will carry no color community and resolve via `system-connected` fallback. This is expected behaviour during a phased rollout, not an error condition.

**Revert behaviour:** when `enabled` is set to `false` and the controller is restarted, the controller generates the full set of `no` commands to remove all flex-algo config from all devices on the next reconciliation cycle: `no router traffic-engineering`, `no flex-algo` definitions, `no next-hop resolution ribs`, and removal of `set extcommunity color` from all route-maps.

**Single controller:** today there is a single controller instance; the config file approach is straightforward. Multiple controller instances would require config consistency across instances. This is deferred to a future RFC addressing decentralised controller architecture.

---

### IS-IS Flex-Algo Topology

Each link color maps to an IS-IS TE admin-group bit via the `LinkColorInfo` account. The controller MUST read `link.link_colors[0]`, resolve the `LinkColorInfo` PDA, and apply the corresponding admin-group to the physical interface — unless the link's pubkey is in `link_tagging.exclude.links`.

| Link color | Constraint | Admin-group bit | Flex-algo number | EOS color value | Topology |
|---|---|---|---|---|---|
| (untagged) | — | — | — (algo 0) | — | All links |
| unicast-default | include-any | 0 | 128 | 1 | Only UNICAST-DEFAULT tagged links |
| (future color) | include-any or exclude | 1 | 129 | 2 | Defined by constraint |

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

**Operational implication:** Every link intended to carry unicast traffic MUST be explicitly tagged `UNICAST-DEFAULT`. Links added to the network are excluded from the unicast topology by default until DZF assigns the color. The `link color list` command SHOULD warn if a color's topology appears disconnected based on the set of tagged links.

#### Universal participation requirement

Flex-algo MUST be enabled on every device in the network, not only on devices that have colored links. A device that does not participate in a flex-algo does not advertise a node-SID for that algorithm, so other devices cannot include it in the constrained SPF and cannot steer VPN traffic to it via the constrained topology. VPN routes to a non-participating device will not resolve via the colored tunnel RIB and will fall back to the next resolution source. The controller MUST therefore push the flex-algo definitions and BGP next-hop resolution config to all devices when `enabled: true`. Admin-group tagging on interfaces is applied only to links with a non-empty `link_colors` that are not in the `link_tagging.exclude.links` list.

#### Multicast path isolation

Multicast (PIM) resolves via the IS-IS unicast RIB (algo 0), which uses all links regardless of color. This is inherent to how PIM RPF works — it is not affected by the BGP next-hop resolution profile, and `next-hop resolution ribs` does not support multicast address families. Multicast isolation does not depend on any additional configuration — PIM RPF resolves via the unicast RIB regardless of how VPN unicast is steered.

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
      next-hop resolution ribs tunnel-rib colored system-colored-tunnel-rib system-connected
```

`system-colored-tunnel-rib` is auto-populated by EOS when flex-algo definitions carry a `color` field. A VPN route carrying `Color:CO(00):1` resolves its next-hop through the color-1 (unicast-default, algo 128) tunnel to that endpoint.

#### Inbound route-map color stamping

The controller already generates a `RM-USER-{{ .Id }}-IN` route-map per tunnel, applied inbound on each client-facing BGP session. This route-map currently sets standard communities identifying the user as unicast or multicast and tagging the originating exchange. The color extended community is added as an additional `set` statement in this same route-map, applied only to unicast tunnels and only when the controller config enables stamping for the tenant and device:

```
route-map RM-USER-{{ .Id }}-IN permit 10
   match ip address prefix-list PL-USER-{{ .Id }}
   match as-path length = 1
   set community 21682:{{ if eq true .IsMulticast }}1300{{ else }}1200{{ end }} 21682:{{ $.Device.BgpCommunity }}
   {{- if and $.Config.FlexAlgo.Enabled (not .IsMulticast) $.LinkColors ($.Config.FlexAlgo.CommunityStamping.ShouldStamp .TenantPubKey $.Device.PubKey) }}
   set extcommunity color {{ .TenantTopologyEosColorValues }}
   {{- end }}
```

`.TenantTopologyEosColorValues` is resolved by the controller from the tunnel's tenant:
- If `tenant.include_topology_colors` is non-empty, resolve each `LinkColorInfo` PDA and compute `AdminGroupBit + 1` for each. All resolved color values are stamped in a single `set extcommunity color` statement (e.g., `set extcommunity color 1 color 2`).
- If `tenant.include_topology_colors` is empty, use the default unicast color: resolve the `LinkColorInfo` where `admin_group_bit == 0` (UNICAST-DEFAULT, EOS color value 1).

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
   {{- if and .LinkColors (not ($.Config.FlexAlgo.LinkTagging.IsExcluded .PubKey)) }}
   traffic-engineering administrative-group {{ $.Strings.Join " " ($.Strings.ToUpperEach .LinkColorNames) }}
   {{- else }}
   no traffic-engineering administrative-group
   {{- end }}
{{- end }}
```

`.LinkColors` is the resolved list of `LinkColorInfo` accounts from `link.link_colors`; it is empty when `link_colors` is empty. `.LinkColorNames` is the corresponding list of names. The controller renders all colors as a space-separated list in a single command — EOS overwrites the existing admin-group assignment with exactly this set. This means:
- A link transitioning from two colors to one re-applies only the surviving color, atomically replacing the previous set
- A link losing its last color receives `no traffic-engineering administrative-group`
- The targeted `no traffic-engineering administrative-group <NAME>` command is never used, avoiding the EOS behavior where it would remove all groups regardless of the name specified

Interface-level admin-group tagging is conditioned on `$.Config.FlexAlgo.Enabled` alone — since an interface may have colors assigned onchain while the feature is disabled.

#### 2. router traffic-engineering block

Add after the `router isis 1` block, conditional on colors being defined and the feature being enabled:

```
{{- if and $.Config.FlexAlgo.Enabled .LinkColors }}
router traffic-engineering
   router-id ipv4 {{ .Device.Vpn4vLoopbackIP }}
   {{- range .LinkColors }}
   administrative-group alias {{ $.Strings.ToUpper .Name }} group {{ .AdminGroupBit }}
   {{- end }}
   !
   flex-algo
   {{- range .LinkColors }}
      flex-algo {{ .FlexAlgoNumber }} {{ .Name }}
         {{- if eq .Constraint "include-any" }}
         administrative-group include any {{ .AdminGroupBit }}
         {{- else }}
         administrative-group exclude {{ .AdminGroupBit }}
         {{- end }}
         color {{ .EosColorValue }}
   {{- end }}
{{- end }}
```

`.LinkColors` is the ordered list of `LinkColorInfo` accounts, sorted by `AdminGroupBit`. `.EosColorValue` is computed as `AdminGroupBit + 1`. The flex-algo name (e.g., `unicast-default`) is the color name stored in `LinkColorInfo`.

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
      {{- if and $.Config.FlexAlgo.Enabled .LinkColors }}
      next-hop resolution ribs tunnel-rib colored system-colored-tunnel-rib system-connected
      {{- end }}
   !
```

#### 4. IS-IS flex-algo advertisement and traffic-engineering

Inside the existing `router isis 1` block, under `segment-routing mpls`, add a `flex-algo` advertisement line per color:

```
   segment-routing mpls
      no shutdown
      {{- range .LinkColors }}
      flex-algo {{ .Name }} level-2 advertised
      {{- end }}
```

After the `segment-routing mpls` block, add the `traffic-engineering` section:

```
{{- if and $.Config.FlexAlgo.Enabled .LinkColors }}
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
   {{- range $.LinkColors }}
   {{- if .FlexAlgoNodeSegmentIdx }}
   node-segment ipv4 index {{ .FlexAlgoNodeSegmentIdx }} flex-algo {{ .Name }}
   {{- end }}
   {{- end }}
{{- end }}
```

The flex-algo node-segment index follows the same pattern as the existing algo-0 `node_segment_idx`: it is allocated from the `SegmentRoutingIds` `ResourceExtension` account at interface activation time (in `processors/device/interface/activate.rs`) and stored on the `Interface` account onchain. A new `flex_algo_node_segment_idx: u16` field MUST be added to the `Interface` account and allocated alongside `node_segment_idx` when the interface's loopback type is `Vpnv4`. It is deallocated on `remove`, following the existing pattern.

**Migration for existing interfaces:** Vpn4v loopback interfaces that were activated before this RFC will not have `flex_algo_node_segment_idx` allocated. Existing `node_segment_idx` assignments (algo-0, used today) are unchanged — this migration is purely additive. A one-time `doublezero-admin` CLI migration command MUST be provided to iterate all existing `Interface` accounts with `loopback_type = Vpnv4`, allocate a `flex_algo_node_segment_idx` for each, and persist the updated account. Loopbacks activated after this RFC will have `flex_algo_node_segment_idx` allocated at activation time alongside `node_segment_idx`.

The controller MUST check at startup, before enabling flex-algo, that no Vpn4v loopback has `flex_algo_node_segment_idx == 0`. If any unset loopbacks are found, the controller MUST refuse to apply flex-algo config and emit an error directing the operator to run the migration command. This prevents silently pushing a broken topology where some devices are unreachable via the constrained path.

Without a flex-algo node-SID on the loopback, remote devices cannot compute a valid constrained path to this device and VPN routes to it will not resolve via the colored tunnel RIB.

All blocks are conditional on `.LinkColors` being non-empty, so devices with no colors defined produce identical config to today.

---

### SDK Changes

`LinkColorInfo` MUST be added to the Go, Python, and TypeScript SDKs. The `link` deserialization structs MUST include the new `link_colors: Vec<Pubkey>` field. The `tenant` deserialization structs MUST include the new `include_topology_colors: Vec<Pubkey>` field. Fixture files MUST be regenerated.

---

### Tests

#### Smart contract (integration tests)

**LinkColorInfo lifecycle:**
- A foundation key MUST be able to create a `LinkColorInfo` account with a name; admin-group bit MUST be allocated from the `AdminGroupBits` `ResourceExtension` starting at 0, and flex-algo number MUST be 128.
- Creating a second color MUST allocate bit 1 from the `ResourceExtension` and flex-algo 129.
- A non-foundation key MUST NOT be able to create a `LinkColorInfo` account; the instruction MUST be rejected with an authorization error.
- All `LinkColorInfo` fields are immutable after creation; an `update` instruction MUST be rejected or be a no-op.
- A non-foundation key MUST NOT be able to update a `LinkColorInfo` account.
- `delete` MUST succeed when no links reference the color; the `LinkColorInfo` PDA MUST be removed onchain.
- `delete` MUST fail when one or more links still reference the color.
- After `clear`, all links previously assigned the color MUST have `link_colors = []`.
- After `clear` followed by `delete`, the `LinkColorInfo` PDA MUST be absent.
- Admin-group bits from deleted colors MUST NOT be reused by subsequently created colors; the `AdminGroupBits` `ResourceExtension` bitmap MUST persist the allocated bit after PDA deletion.
- After `delete`, the controller MUST NOT generate removal commands for the deleted color's admin-group alias, flex-algo definition, or IS-IS TE config — device-side cleanup is deferred.

**Tenant topology color assignment:**
- `include_topology_colors` MUST default to an empty vector on a newly created tenant account and on existing accounts deserialized from pre-upgrade binary data.
- A foundation key MUST be able to set `include_topology_colors` to a list of valid `LinkColorInfo` pubkeys on any tenant.
- A non-foundation key MUST NOT be able to set `include_topology_colors`; the instruction MUST be rejected with an authorization error.
- Setting `include_topology_colors` to an empty vector MUST be accepted and revert the tenant to the default color 1 (UNICAST-DEFAULT).
- Setting `include_topology_colors` to a pubkey that does not correspond to a valid `LinkColorInfo` account MUST be rejected.

**Link color assignment:**
- `link_colors` MUST default to an empty vector on a newly created link account and on existing accounts deserialized from pre-upgrade binary data.
- A foundation key MUST be able to set `link_colors[0]` to a valid `LinkColorInfo` pubkey on any link.
- A contributor key MUST NOT be able to set `link_colors`; the instruction MUST be rejected with an authorization error.
- Setting `link_colors` to an empty vector from a non-empty value MUST be accepted and persist correctly.
- Setting `link_colors[0]` to a pubkey that does not correspond to a valid `LinkColorInfo` account MUST be rejected.
- `link_colors` MUST NOT exceed 8 entries; an instruction submitting more than 8 MUST be rejected.

#### Controller (unit tests)

- A link with `link_colors = []` MUST produce interface config with no `traffic-engineering administrative-group` line.
- A link with `link_colors[0]` referencing a `LinkColorInfo` with bit 0, name "unicast-default", and constraint `IncludeAny` MUST produce interface config with `traffic-engineering administrative-group UNICAST-DEFAULT`.
- A link in `link_tagging.exclude.links` MUST produce `no traffic-engineering administrative-group` regardless of onchain `link_colors` assignment.
- Transitioning a link from a color to default MUST produce a `no traffic-engineering administrative-group` diff.
- Transitioning a link from one color to another MUST produce the correct remove/add diff.
- The `router traffic-engineering` block MUST include `color <admin_group_bit + 1>` on each flex-algo definition.
- The BGP `next-hop resolution ribs tunnel-rib colored system-colored-tunnel-rib system-connected` config MUST be generated correctly when `enabled: true`.
- A per-tunnel inbound route-map MUST include `set extcommunity color 1` for a unicast tenant with empty `include_topology_colors` when the tenant or device is in the `community_stamping` config and `LinkColors` is non-empty.
- A per-tunnel inbound route-map MUST include `set extcommunity color 1 color 2` for a unicast tenant with `include_topology_colors` referencing two `LinkColorInfo` accounts (bits 0 and 1).
- A per-tunnel inbound route-map MUST NOT include `set extcommunity color` when the device is in `community_stamping.exclude.devices`.
- A new `LinkColorInfo` account detected on reconciliation MUST cause the controller to push updated config to all devices.
- **Config disabled:** With `LinkColorInfo` accounts defined, links tagged, and `enabled: false`, the controller MUST generate `no router traffic-engineering`, no flex-algo IS-IS config, no `next-hop resolution ribs` line, and no `set extcommunity color` in any route-map. Device config MUST be identical to a network with no colors defined.
- **Config enabled:** Setting `enabled: true` with existing `LinkColorInfo` accounts and tagged links MUST cause the controller to generate the full flex-algo config block on the next reconciliation cycle.
- **Interface tagging independent of stamping:** Interface-level `traffic-engineering administrative-group` config MUST be generated based on `$.Config.FlexAlgo.Enabled` alone, regardless of `community_stamping` settings.

#### SDK (unit tests)

- `LinkColorInfo` account MUST serialize and deserialize correctly via Borsh for all fields.
- `LinkColorInfo` account MUST deserialize correctly from a binary fixture.
- `link_colors` pubkey vector MUST be included in `link get` and `link list` output in all three SDKs, showing the color names (resolved from `LinkColorInfo`) or "default".
- The `list` command MUST display the derived EOS color value (`admin_group_bit + 1`) in output.

#### End-to-end (cEOS testcontainers)

- **Color creation**: After a foundation key creates a `LinkColorInfo` for "unicast-default" (bit 0, flex-algo 128, constraint include-any) and `enabled: true` is set, the controller MUST push `router traffic-engineering` config with `administrative-group alias UNICAST-DEFAULT group 0`, `flex-algo 128 unicast-default administrative-group include any 0 color 1`, and the BGP `next-hop resolution ribs` line to all devices.
- **Admin-group application**: After a foundation key sets `link_colors[0]` on a link to the unicast-default `LinkColorInfo` pubkey, `show traffic-engineering database` on the connected devices MUST reflect `UNICAST-DEFAULT` admin-group on the interface. Clearing the color MUST remove the admin-group.
- **Link tagging exclude**: A link in `link_tagging.exclude.links` MUST NOT have an admin-group applied even when `link_colors[0]` is set onchain.
- **Flex-algo topology**: With links tagged UNICAST-DEFAULT, `show isis flex-algo` on participating devices MUST show algo 128 including only UNICAST-DEFAULT links. Untagged links MUST be absent from the algo-128 LSDB view.
- **Colored tunnel RIB**: `show tunnel rib system-colored-tunnel-rib brief` MUST show (endpoint, color 1) entries for each participating device, resolving via unicast-default tunnels.
- **VPN unicast path selection**: A BGP VPN-IPv4 route carrying `Color:CO(00):1` MUST resolve its next-hop through the color-1 (unicast-default) tunnel in `system-colored-tunnel-rib`, traversing only UNICAST-DEFAULT tagged links.
- **Per-tenant color — single**: A tenant with `include_topology_colors = [SHELBY pubkey]` MUST have `Color:CO(00):2` stamped on its inbound routes. A tenant with empty `include_topology_colors` (default) MUST have `Color:CO(00):1` (UNICAST-DEFAULT).
- **Per-tenant color — multi**: A tenant with `include_topology_colors = [UNICAST-DEFAULT pubkey, SHELBY pubkey]` MUST have both `Color:CO(00):1` and `Color:CO(00):2` stamped. `show ip route vrf <tenant>` MUST show the lower-metric color tunnel selected for next-hop resolution.
- **Per-tenant color — fallback**: Removing a device's node-segment for the preferred color algorithm MUST cause EOS to fall back to the next available color on the same prefix without the route going unresolved.
- **Community stamping — per device**: A tenant on a device in `community_stamping.devices` MUST have the color community on its inbound routes. The same tenant on a device NOT in the config MUST NOT have the color community.
- **Community stamping — exclude**: A device in `community_stamping.exclude.devices` MUST NOT have `set extcommunity color` applied regardless of `all` or `tenants` settings.
- **Multicast path isolation**: PIM RPF for a multicast source MUST continue to resolve via IS-IS algo 0 (all links, including both tagged and untagged) regardless of BGP next-hop resolution config.
- **Color clear**: After `link color clear --name unicast-default` removes the color from all links, the controller MUST generate `no traffic-engineering administrative-group UNICAST-DEFAULT` on all previously-tagged interfaces on the next reconciliation cycle.
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
MUST confirm each participating device advertises both an algo-0 index and a flex-algo index for each defined color.

**Verify BGP next-hop resolution binding is active:**
```
show bgp instance
```
MUST confirm `address-family IPv4 MplsVpn` shows `Resolution RIBs: tunnel-rib colored system-colored-tunnel-rib, system-connected`.

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
MUST confirm admin-group membership is visible only on interfaces with a non-empty `link_colors`.

**Verify multicast RPF uses algo-0 (including colored links):**
```
show ip mroute
```
MUST confirm PIM RPF resolves via the IS-IS unicast RIB (algo 0). The incoming interface for a multicast source reachable via a colored link MUST be the colored interface, unchanged by BGP next-hop resolution config.

---

## Impact

### Codebase

- **serviceability** — new `LinkColorInfo` PDA (foundation-managed, one per color); new `AdminGroupBits` `ResourceExtension` account for persistent bit allocation; new `link_colors: Vec<Pubkey>` field (cap 8) on `Link`; new `link_colors: Option<Vec<Pubkey>>` field on `LinkUpdateArgs` with foundation-only write restriction; new `flex_algo_node_segment_idx: u16` field on `Interface`; new `include_topology_colors: Vec<Pubkey>` field on `Tenant` with foundation-only write restriction.
- **controller** — new `-features-config` flag and `features.yaml` config file; reads `link.link_colors[0]`, resolves `LinkColorInfo` PDAs, generates IS-IS TE admin-group config on interfaces (respecting `link_tagging.exclude.links`), flex-algo definitions with `color` field, `system-colored-tunnel-rib` BGP resolution profile, and adds `set extcommunity color` to the existing per-tunnel inbound route-maps (`RM-USER-{{ .Id }}-IN`) for stamping-eligible tunnels; generates `no` commands for full revert when `enabled: false`.
- **CLI** — full color lifecycle commands (`create`, `update`, `delete`, `clear`, `list`); `link update` gains `--link-color`; `link get` / `link list` display the field including derived EOS color value; `link color list` warns on disconnected topologies.
- **SDKs** — `LinkColorInfo` added to all three language SDKs; `link_colors` field added to link deserialization structs.

### Operational

- DZF MUST create a `LinkColorInfo` account and assign `link_colors` on links before the controller applies TE admin-groups. Until a color is created and assigned, links behave as today.
- Adding a new color MUST NOT require a code change or deploy — DZF creates the `LinkColorInfo` account via the CLI and the controller picks it up on the next reconciliation cycle once `enabled: true`.
- `link_colors` appends to the serialized layout and defaults to an empty vector on existing accounts. No migration is required.
- The transition from no-color to color-1 on all tenant VRFs is a one-time controller config push. The template section order enforces the correct sequencing within a single reconciliation cycle: the `router traffic-engineering` block and `address-family vpn-ipv4 next-hop resolution` config appear before the `route-map RM-USER-*-IN` blocks in `tunnel.tmpl`, so EOS applies them top-to-bottom in the correct order. Applying the route-map before the RIB is configured would cause VPN routes to go unresolved.

### Testing

| Layer | Tests |
|---|---|
| Smart contract | `LinkColorInfo` create/update/delete lifecycle; `AdminGroupBits` `ResourceExtension` allocation and no-reuse after deletion; foundation-only authorization; `link_colors` assignment and clearing; delete blocked when links assigned; `clear` removes color from all links; `link_colors` cap at 8 enforced; `include_topology_colors` assignment on tenant; foundation-only authorization |
| Controller (unit) | Interface config with and without color; link tagging exclude list respected; remove/add diff on color change; `router traffic-engineering` block with `color` field; `system-colored-tunnel-rib` BGP resolution config; single-color and multi-color per-tenant stamping; community stamping per-tenant and per-device; stamping exclude respected; full revert on `enabled: false`; new color triggers config push to all devices |
| SDK (unit) | `LinkColorInfo` Borsh round-trip; `link_colors` field in `link get` / `link list` output; `include_topology_colors` field in `tenant get` / `tenant list` output across Go, Python, and TypeScript; EOS color value displayed correctly |
| End-to-end (cEOS) | Color create → controller config push verified; admin-group applied and removed on interface; link tagging exclude list verified; flex-algo 128 topology includes only UNICAST-DEFAULT links; `system-colored-tunnel-rib` populated; VPN unicast resolves via color-1 tunnel; per-tenant single and multi-color stamping verified; color fallback verified; community stamping per-device verified; stamping exclude verified; multicast RPF unchanged; color clear removes admin-groups; full revert on `enabled: false` verified |

---

## Security Considerations

- `link_colors` MUST only be writable by foundation keys. A contributor MUST NOT be able to tag their own link with a color to influence path steering. The check mirrors the existing pattern used for `link.status` foundation-override.
- `LinkColorInfo` accounts MUST only be created, updated, or deleted by foundation keys.
- The controller feature config file (`features.yaml`) is a local file on the controller host. Access to this file SHOULD be restricted to the operator running the controller. An attacker with write access to the config file could enable or disable flex-algo config or manipulate the stamping allowlist without an onchain transaction.
- If a foundation key is compromised, an attacker could reclassify links or create new color definitions, causing traffic to be steered onto unintended paths. This is the same threat surface as other foundation-key-controlled fields. No new mitigations are introduced.

---

## Backward Compatibility

- `link_colors` appends to the serialized layout and defaults to an empty vector on existing accounts. No migration is required for the onchain schema.
- Devices that do not receive updated config from the controller MUST continue to forward using IS-IS algo 0 only. The flex-algo topology is distributed — a device that does not participate is simply not included in the constrained SPF.
- The controller MUST push `system-colored-tunnel-rib` config and flex-algo definitions before activating per-VRF route-maps. Activating route-maps first causes VPN routes to carry a color community with no matching tunnel RIB entry, leaving them unresolved. The template section order enforces this within a single reconciliation cycle.
- The BGP next-hop resolution profile is applied globally. Per-tenant resolution profile overrides are not supported at this stage.

---

## Observability

Introducing multiple forwarding topologies over the same physical links has implications for the network visualization tools in the lake repository. Today, lake displays a single IS-IS topology where every link is treated equally. With flex-algo, a link's effective participation depends on its color and the traffic type being considered — an untagged link is present in the algo-0 view but absent from the algo-128 (unicast-default) view used by VPN unicast.

Areas that SHOULD evolve:

- **Topology view filtering** — the topology map SHOULD allow operators to switch between views: all links (algo 0), unicast topology (algo 128), or a per-service overlay. A link's color SHOULD be visually distinct and its inclusion or exclusion from each topology SHOULD be clear.
- **Link color display** — link color and admin-group membership SHOULD be surfaced on the topology map alongside existing link attributes (latency, capacity). This gives operators immediate visibility into how a link is classified without needing to query the CLI.
- **Service-aware path visualization** — when tracing a path between two nodes, the tool SHOULD reflect which topology that traffic type actually uses. A unicast path between two nodes may differ from the multicast path over the same physical graph.
- **Tenant / VRF context** — VPN unicast traffic is resolved per VRF. A future multi-VRF deployment with per-tenant colors would require topology views to be scoped to a tenant, showing the paths available within that VRF's forwarding context.

---

## Open Questions

- **Color naming convention**: The RFC defines `unicast-default` and `shelby` as the initial colors. Should DZF adopt a consistent naming convention for future colors? Options include:
  - **Functionality** (`unicast-default`, `low-latency`) — self-documenting from an operational perspective, but ties the name to a specific use-case that may evolve.
  - **Product/tenant names** (`shelby`, `shreds`) — scoped to a customer or service, which may be appropriate if colors end up being per-tenant rather than network-wide.
  The name is stored onchain in `LinkColorInfo` and appears in EOS config (converted to uppercase as the admin-group alias), so it SHOULD be stable once assigned.
