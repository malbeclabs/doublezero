# RFC-18: Link Classification — Flex-Algo

## Summary

**Status: `Draft`**

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

DoubleZero contributors operate links with different physical characteristics — low latency, high bandwidth, or both. Today all traffic uses the same IS-IS topology, so every service follows the same paths regardless of what those paths are optimized for. This RFC introduces a link classification model that allows DZF to assign named color labels to links onchain and use IS-IS Flexible Algorithm (flex-algo) to compute separate constraint-based forwarding topologies per color. Different traffic classes — VPN unicast and IP multicast — can then use different topologies.

**Deliverables:**
- `LinkColorInfo` onchain account — DZF creates this to define a color, with auto-assigned admin-group bit, flex-algo number, and derived EOS color value
- `link_color` field on the serviceability link account — references the assigned color
- `FlexAlgo` feature flag (bit 2) in the existing `GlobalState.feature_flags` bitmask — gates all flex-algo device config; must be explicitly enabled by DZF before the controller pushes any flex-algo configuration to devices
- Controller logic — translates colors into IS-IS TE admin-groups on interfaces, generates flex-algo topology definitions, configures `system-colored-tunnel-rib` as the BGP next-hop resolution source, and applies BGP color extended community route-maps per VRF; all conditioned on the `FlexAlgo` feature flag

**Scope:**
- Delivers traffic-class-level segregation: multicast vs. VPN unicast at the network level
- All unicast tenants share a single constrained topology today — the architecture is forward-compatible with per-tenant path differentiation without rework
- Per-tenant steering (directing one tenant to a different constrained topology) requires adding a `topology_color` field to the `Tenant` account — deferred to a future RFC that builds on the link color model defined here

---

## Motivation

The DoubleZero network carries two distinct traffic types today: VPN unicast (tenants connected in IBRL mode) and IP multicast. Both follow the same IS-IS topology, where link metrics are derived from measured latency. Every service takes the lowest-latency path — there is no differentiation.

As the network grows, traffic types have different requirements. Latency-sensitive multicast benefits from low-latency links. Higher-latency, higher-bandwidth links that do not win the latency-based SPF are chronically underutilized — yet they may be exactly what certain tenants need. Business requirements, not just latency, should determine which traffic uses which links. A single shared topology cannot serve both simultaneously. This RFC solves the first layer of this problem: separating traffic by class (multicast vs. unicast) at the network level, so that a set of links can be reserved for multicast use while unicast routes around them. Per-tenant path differentiation — where individual tenant VRFs are steered onto different constrained topologies — is a distinct problem addressed architecturally here but deferred in implementation.

Without a steering mechanism, all tenants compete for the same links, and contributors have no way to express that a link is intended for a particular class of traffic. The result is no way to differentiate service quality as the network scales.

IS-IS Flexible Algorithm provides the routing mechanism: each flex-algo defines a constrained topology using TE admin-groups as include/exclude criteria. What does not yet exist is a way to assign admin-group membership to links onchain, so the controller can apply the correct device configuration automatically rather than requiring per-device manual config. This RFC defines that model.

---

## New Terminology

- **Link color** — A DZF-defined label assigned to a link that maps to an IS-IS TE admin-group. Determines which flex-algo topologies include or exclude the link.
- **Admin-group** — An IS-IS TE attribute assigned to a physical interface that flex-algo algorithms use as include/exclude constraints. Also called "affinity" in some implementations. Arista EOS supports bits 0–127.
- **Flex-algo** — IS-IS Flexible Algorithm (RFC 9350). Each algorithm defines a constrained topology (metric type + admin-group include/exclude rules) and computes an independent SPF. Nodes with the same flex-algo compute consistent paths across the topology. Arista EOS supports flex-algo numbers 128–255.
- **EOS color value** — An integer assigned to a flex-algo definition in EOS (`color <N>` under `flex-algo`). Causes EOS to install that algorithm's computed tunnels in `system-colored-tunnel-rib` keyed by (endpoint, color). Derived as `admin_group_bit + 1`; not stored separately.
- **system-colored-tunnel-rib** — An EOS system RIB auto-populated when flex-algo definitions carry a `color` field. Keyed by (endpoint, color). Used by BGP next-hop resolution to steer VPN routes onto constrained topologies based on the BGP color extended community carried on the route.
- **BGP color extended community** — A BGP extended community (`Color:CO(00):<N>`) set on VPN-IPv4 routes inbound on the client-facing BGP session. The color value matches the EOS flex-algo color, enabling per-route algorithm selection at devices receiving the route via VPN-IPv4.
- **UNICAST-DEFAULT** — The first color DZF creates. Auto-assigned admin-group bit 0, flex-algo 128, EOS color value 1. Applied to all links eligible for the default unicast topology. Flex-algo 128 uses `include-any UNICAST-DEFAULT`, so only explicitly tagged links participate in the unicast topology. Untagged links are excluded from unicast but remain available to multicast via IS-IS algo 0.
- **Link color constraint** — Each `LinkColorInfo` defines either an `IncludeAny` or `Exclude` constraint. `IncludeAny`: only links explicitly tagged with this color participate in the topology. `Exclude`: all links except those tagged with this color participate. UNICAST-DEFAULT uses `IncludeAny`.
- **FlexAlgo feature flag** — Bit 2 in `GlobalState.feature_flags`. When unset, the controller generates no flex-algo config regardless of how many `LinkColorInfo` accounts exist or how many links are tagged. Allows DZF to prepare onchain state independently of device rollout timing.

---

## Scope and Limitations

| Scenario | This RFC | Notes |
|---|---|---|
| Default unicast topology via UNICAST-DEFAULT color | ✅ | Core deliverable; all unicast-eligible links must be explicitly tagged |
| Multicast uses all links (algo 0) | ✅ | Natural PIM RPF behavior; includes both tagged and untagged links; no config required |
| Multiple links with the same color | ✅ | All tagged links participate together in the constrained topology |
| New links excluded from unicast by default | ✅ | `include-any` strictly excludes untagged links — verified in lab. New links must be explicitly tagged before they carry unicast traffic |
| Per-tenant unicast path differentiation | ⚠️ | Architecture proven in lab (BGP color extended communities + `system-colored-tunnel-rib`). Today all unicast tenants share one color. Per-tenant steering requires `topology_color` on `Tenant` account — deferred to a future RFC |
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
| Per-tenant flex-algo color | Per-tenant VRF | Different constrained topology per tenant | Low — add `topology_color` to `Tenant` account; controller generates per-VRF route-map with matching color | First tenant requiring path isolation from the default unicast topology |
| CBF with non-default VRFs | Per-tenant VRF, per-DSCP | Different constrained topology per tenant with DSCP-based sub-steering | Medium — TCAM profile change; builds on flex-algo colors defined here | First tenant requiring DSCP-level path differentiation within a VRF |
| SR-TE | Per-prefix or per-flow | Explicit path control with segment lists; per-prefix or per-DSCP steering independent of IGP topology | High — controller must compute or define explicit segment lists per policy, and set BGP Color Extended Community on routes per-tenant | Per-prefix SLA requirements, or when per-tenant flex-algo color is insufficient |
| RSVP-TE | Per-LSP (P2P unicast) or per-tree (P2MP multicast) | Hard bandwidth reservation with admission control | High — RSVP-TE on all path devices, IS-IS TE bandwidth advertisement, controller logic to provision per-tenant tunnel interfaces | SLA-backed bandwidth guarantees where admission control is required, not just path preference |

The lowest-cost next step for per-tenant differentiation is adding `topology_color: Option<Pubkey>` to the `Tenant` account. The controller reads it, resolves the `LinkColorInfo` PDA, and generates a VRF-specific route-map with the corresponding color value. No new network infrastructure is required — it builds directly on the `system-colored-tunnel-rib` mechanism defined here.

---

## Detailed Design

### Link Color Model

#### LinkColorInfo account

DZF creates a `LinkColorInfo` PDA per color. It stores the color's name and auto-assigned routing parameters. The program MUST auto-assign the next available admin-group bit (starting at 0) and the corresponding flex-algo number and EOS color value using the formula:

```
admin_group_bit  = next available bit in 0–127
flex_algo_number = 128 + admin_group_bit
eos_color_value  = admin_group_bit + 1   (derived, not stored)
```

This formula ensures the admin-group bit, flex-algo number, and EOS color value are always in the EOS-supported ranges (bits 0–127, algos 128–255, color 1–4294967295) and are derived consistently from each other. The EOS color value is not stored onchain — it is computed by the controller wherever needed.

```rust
#[derive(BorshSerialize, BorshDeserialize, Debug)]
pub enum LinkColorConstraint {
    IncludeAny = 0,  // only tagged links participate in the topology
    Exclude    = 1,  // all links except tagged participate in the topology
}

#[derive(BorshSerialize, BorshDeserialize, Debug)]
pub struct LinkColorInfo {
    pub name: String,                    // e.g. "unicast-default"
    pub admin_group_bit: u8,             // auto-assigned, 0–127
    pub flex_algo_number: u8,            // auto-assigned, 128–255; always 128 + admin_group_bit
    pub constraint: LinkColorConstraint, // IncludeAny or Exclude
}
```

PDA seeds: `[b"link-color-info", name.as_bytes()]`. `LinkColorInfo` accounts MUST only be created or updated by foundation keys.

Name length MUST NOT exceed 32 bytes, enforced by the program on `create`. This keeps PDA seeds well within the 32-byte limit and ensures the admin-group alias name is reasonable in EOS config.

The program MUST validate `admin_group_bit <= 127` on `create` and MUST return an explicit error if all 128 slots are exhausted. This is a hard constraint: EOS supports bits 0–127 only, and `128 + 127 = 255` is the maximum representable value in `flex_algo_number: u8`.

Admin-group bits from deleted colors MUST NOT be reused. Color deletion is not supported in this RFC, so this constraint applies to any future deletion implementation: reusing a bit before all devices have had their config updated would cause those devices to apply the new color's constraints to interfaces still carrying the old bit's admin-group. At current scale (128 available slots), exhaustion is not a practical concern.

#### link_color field on Link

A `link_color` field is added to the serviceability `Link` account. It holds the pubkey of the `LinkColorInfo` PDA for the assigned color, or `Pubkey::default()` if no color is assigned. The field appends to the end of the serialized layout, defaulting to `Pubkey::default()` on existing accounts.

`link_color` MUST only be set by keys in the DZF foundation allowlist. Contributors MUST NOT set this field. Link tagging is a DZF policy decision — there is no automated selection based on `link.bandwidth` or `link.link_type` at this stage.

```rust
// Foundation-only fields
if globalstate.foundation_allowlist.contains(payer_account.key) {
    if let Some(link_color) = value.link_color {
        link.link_color = link_color;
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

- `create` — creates a `LinkColorInfo` PDA; auto-assigns the next available admin-group bit and flex-algo number; stores the specified constraint (`include-any` or `exclude`). MUST fail if the name already exists. If the `FlexAlgo` feature flag is set, the controller will include this color in the `router traffic-engineering` block, IS-IS TE advertisement, and BGP next-hop resolution config on the next reconciliation cycle. No immediate device impact if the `FlexAlgo` feature flag is not set.
- `update` — reserved for future use; all fields are immutable after creation. No device config change.
- `delete` — removes the `LinkColorInfo` PDA onchain. MUST fail if any link still references this color (use `clear` first). **Device-side cleanup is deferred** — the controller does not generate `no` commands to remove the admin-group alias, flex-algo definition, IS-IS TE advertisement, or BGP next-hop resolution config from devices when a color is deleted. See below.
- `clear` — removes this color from all links currently assigned to it, setting their `link_color` to `Pubkey::default()`. This is a multi-transaction sweep — one `LinkUpdateArgs` instruction is submitted per assigned link; it is not atomic. On the next reconciliation cycle, the controller generates `no traffic-engineering administrative-group <NAME>` on all previously-colored interfaces.
- `list` — fetches all `LinkColorInfo` accounts and all `Link` accounts and groups links by color:

**Device-side cleanup on deletion is deferred.** The `delete` instruction removes the `LinkColorInfo` PDA onchain. The controller does not generate removal commands (`no administrative-group alias`, `no flex-algo`, etc.) when a color is deleted — device config is not cleaned up automatically. Safe device-side deletion would require the controller to surgically remove the admin-group alias, flex-algo definition, IS-IS TE advertisement, loopback node-segment, and BGP next-hop resolution config from all devices without disrupting surviving colors. EOS behavior complicates this: `no traffic-engineering administrative-group <NAME>` removes **all** admin-groups from the interface regardless of which name is specified — not just the named one. An interface that should retain a second color after deletion would need the surviving color re-applied atomically in the same reconciliation pass. The correct sequencing across a distributed reconciliation cycle has not been validated. Device cleanup on deletion is therefore deferred to a future enhancement. Colors SHOULD be treated as long-lived; the 128-slot limit is not a practical constraint at current scale.

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

- `--link-color <NAME>` MUST resolve the color name to the corresponding `LinkColorInfo` PDA pubkey before submitting the instruction — the onchain field stores a pubkey, not a name.
- `--link-color default` sets `link_color` to `Pubkey::default()`, removing any color assignment.
- `doublezero link get` and `doublezero link list` MUST include `link_color` in their output, showing the resolved color name (or "default").

---

### FlexAlgo Feature Flag

The `FeatureFlag` enum in `smartcontract/programs/doublezero-serviceability/src/state/feature_flags.rs` already provides a network-wide bitmask mechanism in `GlobalState`. This RFC adds bit 2:

```rust
pub enum FeatureFlag {
    OnChainAllocation = 0,
    RequirePermissionAccounts = 1,
    FlexAlgo = 2,                  // new
}
```

Only foundation keys can set this flag via the existing `SetFeatureFlags` instruction. The controller reads `GlobalState.feature_flags` from `ProgramData` on each reconciliation cycle and exposes it to the template as `.Features.FlexAlgo`.

**Rollout sequence:**

The `FlexAlgo` feature flag decouples onchain state preparation from device deployment. DZF can create colors and tag links before any device receives flex-algo config, then enable the flag when the network is ready.

1. DZF creates `LinkColorInfo` accounts and tags links with `link_color` — no device impact while the flag is unset
2. When ready to deploy to the network, DZF calls `SetFeatureFlags` with `FlexAlgo = true`
3. On the next reconciliation cycle, the controller generates and pushes all flex-algo config to all devices simultaneously

**All controller template blocks introduced by this RFC are conditioned on `and .Features.FlexAlgo .LinkColors`.** When the flag is unset, devices receive identical config to today regardless of onchain link color state.

### IS-IS Flex-Algo Topology

Each link color maps to an IS-IS TE admin-group bit via the `LinkColorInfo` account. The controller MUST read `link.link_color`, resolve the `LinkColorInfo` PDA, and apply the corresponding admin-group to the physical interface.

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

**Operational implication:** Every link intended to carry unicast traffic MUST be explicitly tagged `UNICAST-DEFAULT`. Links added to the network are excluded from the unicast topology by default until DZF assigns the color.

#### Universal participation requirement

Flex-algo MUST be enabled on every device in the network, not only on devices that have colored links. A device that does not participate in a flex-algo does not advertise a node-SID for that algorithm, so other devices cannot include it in the constrained SPF and cannot steer VPN traffic to it via the constrained topology. VPN routes to a non-participating device will not resolve via the colored tunnel RIB and will fall back to the next resolution source. The controller MUST therefore push the flex-algo definitions and BGP next-hop resolution config to all devices unconditionally. Admin-group tagging on interfaces MUST only be applied to links with a non-default color.

#### Multicast path isolation

Multicast (PIM) resolves via the IS-IS unicast RIB (algo 0), which uses all links regardless of color. This is inherent to how PIM RPF works — it is not affected by the BGP next-hop resolution profile, and `next-hop resolution ribs` does not support multicast address families. Multicast isolation does not depend on any additional configuration — PIM RPF resolves via the unicast RIB regardless of how VPN unicast is steered.

| Service | Path | UNICAST-DEFAULT links used? |
|---|---|---|
| VPN unicast | flex-algo 128 (`system-colored-tunnel-rib`, color 1) | Yes — only tagged links |
| Multicast (PIM, default VRF) | IS-IS algo 0 (unicast RIB) | Yes — all links including tagged |

---

### BGP Color Extended Community

VPN-IPv4 routes MUST carry a BGP color extended community (`Color:CO(00):<N>`) on export. The color value matches the EOS flex-algo `color` field for the target topology. At receiving devices, BGP next-hop resolution uses `system-colored-tunnel-rib` to match the (next-hop, color) pair to a flex-algo tunnel.

#### Next-hop resolution

All devices MUST be configured with the following BGP next-hop resolution profile:

```
router bgp 65342
   address-family vpn-ipv4
      next-hop resolution ribs tunnel-rib colored system-colored-tunnel-rib system-connected
```

`system-colored-tunnel-rib` is auto-populated by EOS when flex-algo definitions carry a `color` field. A VPN route carrying `Color:CO(00):1` resolves its next-hop through the color-1 (unicast-default, algo 128) tunnel to that endpoint.

#### Inbound route-map color stamping

The controller already generates a `RM-USER-{{ .Id }}-IN` route-map per tunnel, applied inbound on each client-facing BGP session. This route-map currently sets standard communities identifying the user as unicast or multicast and tagging the originating exchange. The color extended community is added as an additional `set` statement in this same route-map, applied only to unicast tunnels and only when link colors are defined:

```
route-map RM-USER-{{ .Id }}-IN permit 10
   match ip address prefix-list PL-USER-{{ .Id }}
   match as-path length = 1
   set community 21682:{{ if eq true .IsMulticast }}1300{{ else }}1200{{ end }} 21682:{{ $.Device.BgpCommunity }}
   {{- if and $.Features.FlexAlgo (not .IsMulticast) $.LinkColors }}
   set extcommunity color {{ .TenantTopologyEosColorValue }}
   {{- end }}
```

`.TenantTopologyEosColorValue` is resolved by the controller from the tunnel's tenant: if the tenant has a `topology_color` pubkey set, resolve the `LinkColorInfo` and compute `AdminGroupBit + 1`; otherwise use the default unicast color (color 1, the first `LinkColorInfo` by `AdminGroupBit`). Multicast tunnels do not receive the color community — multicast RPF resolves via IS-IS algo 0 and does not use `system-colored-tunnel-rib`.

Routes arrive on the client-facing session, are stamped with both the standard community and the color extended community in a single pass, and are then advertised into VPN-IPv4 carrying both. No new route-map blocks or `network` statement changes are required.

---

### Controller Changes

All config changes are applied to `tunnel.tmpl`. Five additions are required. All blocks are conditioned on `and .Features.FlexAlgo .LinkColors` — when the `FlexAlgo` feature flag is unset, no flex-algo config is generated regardless of onchain link color state.

#### 1. Interface admin-group tagging

Inside the existing `{{- range .Device.Interfaces }}` block, after the `isis metric` / `isis network point-to-point` lines, add admin-group config for physical IS-IS links:

```
{{- if and .Ip.IsValid .IsPhysical .Metric .IsLink (not .IsSubInterfaceParent) (not .IsCYOA) (not .IsDIA) }}
   traffic-engineering
   {{- if .LinkColor }}
   traffic-engineering administrative-group {{ $.Strings.ToUpper .LinkColor.Name }}
   {{- else }}
   no traffic-engineering administrative-group
   {{- end }}
{{- end }}
```

`.LinkColor` is nil when `link.link_color` is `Pubkey::default()`. The `no traffic-engineering administrative-group` line ensures stale config is removed when a color is cleared. Interface-level admin-group tagging is conditioned on `.Features.FlexAlgo` alone — not on `.LinkColors` — since an interface may have a color assigned onchain while the feature flag is unset.

#### 2. router traffic-engineering block

Add after the `router isis 1` block, conditional on colors being defined and the feature flag being set:

```
{{- if and .Features.FlexAlgo .LinkColors }}
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

#### 3. BGP next-hop resolution

Inside the existing `address-family vpn-ipv4` block, replace any existing `next-hop resolution ribs` line with:

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
      {{- if and .Features.FlexAlgo .LinkColors }}
      next-hop resolution ribs tunnel-rib colored system-colored-tunnel-rib system-connected
      {{- end }}
   !
```

#### 4. IS-IS flex-algo advertisement and traffic-engineering

Inside the existing `router isis 1` block, two additions are required, both conditional on colors being defined.

Under `segment-routing mpls`, add a `flex-algo` advertisement line per color so the device participates in each constrained topology and advertises the corresponding flex-algo to its IS-IS neighbors:

```
   segment-routing mpls
      no shutdown
      {{- range .LinkColors }}
      flex-algo {{ .FlexAlgoNumber }} level-2 advertised
      {{- end }}
```

After the `segment-routing mpls` block, add the `traffic-engineering` section that enables IS-IS TE on the device, which is required for admin-group advertisements to appear in the LSDB:

```
{{- if and .Features.FlexAlgo .LinkColors }}
   traffic-engineering
      no shutdown
      is-type level-2
{{- end }}
```

Without both of these, the device does not advertise its flex-algo node-SIDs and does not include admin-group attributes in its IS-IS LSP. Other devices cannot include it in the constrained SPF and cannot steer VPN traffic to it.

#### 5. Loopback flex-algo node-segment

Inside the existing `{{- range .Device.Interfaces }}` block, extend the existing `node-segment` line on the Vpn4vLoopback interface to also advertise a flex-algo node-SID per color:

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

**Migration for existing interfaces:** Vpn4v loopback interfaces that were activated before this RFC will not have `flex_algo_node_segment_idx` allocated. A one-time `doublezero-admin` CLI migration command MUST be provided to iterate all existing `Interface` accounts with `loopback_type = Vpnv4`, allocate a `flex_algo_node_segment_idx` for each, and persist the updated account. This migration MUST be run before the `FlexAlgo` feature flag is enabled; without it, existing devices will not advertise flex-algo node-SIDs and will be unreachable via the constrained topology.

Without a flex-algo node-SID on the loopback, remote devices cannot compute a valid constrained path to this device and VPN routes to it will not resolve via the colored tunnel RIB.

All seven blocks are conditional on `.LinkColors` being non-empty, so devices with no colors defined produce identical config to today.

---

### SDK Changes

`LinkColorInfo` MUST be added to the Go, Python, and TypeScript SDKs. The `link` deserialization structs MUST include the new `link_color` pubkey field. Fixture files MUST be regenerated.

---

### Tests

#### Smart contract (integration tests)

**LinkColorInfo lifecycle:**
- A foundation key MUST be able to create a `LinkColorInfo` account with a name; admin-group bit and flex-algo number MUST be auto-assigned starting at 0 and 128 respectively.
- Creating a second color MUST auto-assign bit 1 and flex-algo 129.
- A non-foundation key MUST NOT be able to create a `LinkColorInfo` account; the instruction MUST be rejected with an authorization error.
- All `LinkColorInfo` fields are immutable after creation; an `update` instruction MUST be rejected or be a no-op.
- A non-foundation key MUST NOT be able to update a `LinkColorInfo` account.
- `delete` MUST succeed when no links reference the color; the `LinkColorInfo` PDA MUST be removed onchain.
- `delete` MUST fail when one or more links still reference the color.
- After `clear`, all links previously assigned the color MUST have `link_color = Pubkey::default()`.
- After `clear` followed by `delete`, the `LinkColorInfo` PDA MUST be absent.
- Admin-group bits from deleted colors MUST NOT be reused by subsequently created colors.
- After `delete`, the controller MUST NOT generate removal commands for the deleted color's admin-group alias, flex-algo definition, or IS-IS TE config — device-side cleanup is deferred.

**Link color assignment:**
- `link_color` MUST default to `Pubkey::default()` on a newly created link account and on existing accounts deserialized from pre-upgrade binary data.
- A foundation key MUST be able to set `link_color` to a valid `LinkColorInfo` pubkey on any link.
- A contributor key MUST NOT be able to set `link_color`; the instruction MUST be rejected with an authorization error.
- Setting `link_color` to `Pubkey::default()` from a non-default color MUST be accepted and persist correctly.
- Setting `link_color` to a pubkey that does not correspond to a valid `LinkColorInfo` account MUST be rejected.

#### Controller (unit tests)

- A link with `link_color = Pubkey::default()` MUST produce interface config with no `traffic-engineering administrative-group` line.
- A link with `link_color` referencing a `LinkColorInfo` with bit 0, name "unicast-default", and constraint `IncludeAny` MUST produce interface config with `traffic-engineering administrative-group UNICAST-DEFAULT`.
- Transitioning a link from a color to default MUST produce a `no traffic-engineering administrative-group` diff.
- Transitioning a link from one color to another MUST produce the correct remove/add diff.
- The `router traffic-engineering` block MUST include `color <admin_group_bit + 1>` on each flex-algo definition.
- The BGP `next-hop resolution ribs tunnel-rib colored system-colored-tunnel-rib system-connected` config MUST be generated correctly.
- A per-tunnel inbound route-map MUST include `set extcommunity color 1` for unicast tunnels when the `FlexAlgo` flag is set and `LinkColors` is non-empty.
- A new `LinkColorInfo` account detected on reconciliation MUST cause the controller to push updated config to all devices.
- **Feature flag — unset:** With `LinkColorInfo` accounts defined, links tagged, and `FlexAlgo` feature flag unset, the controller MUST generate no `router traffic-engineering` block, no flex-algo IS-IS config, no `next-hop resolution ribs` line, and no `set extcommunity color` in any route-map. Device config MUST be identical to a network with no colors defined.
- **Feature flag — set:** Enabling the `FlexAlgo` flag with existing `LinkColorInfo` accounts and tagged links MUST cause the controller to generate the full flex-algo config block on the next reconciliation cycle.
- **Feature flag — interface tagging independent:** Interface-level `traffic-engineering administrative-group` config MUST be generated based on `Features.FlexAlgo` alone, regardless of whether `.LinkColors` is populated, ensuring tagged interfaces are correctly configured once the flag is set.

#### SDK (unit tests)

- `LinkColorInfo` account MUST serialize and deserialize correctly via Borsh for all fields.
- `LinkColorInfo` account MUST deserialize correctly from a binary fixture.
- `link_color` pubkey field MUST be included in `link get` and `link list` output in all three SDKs, showing the color name (resolved from `LinkColorInfo`) or "default".
- The `list` command MUST display the derived EOS color value (`admin_group_bit + 1`) in output.

#### End-to-end (cEOS testcontainers)

- **Color creation**: After a foundation key creates a `LinkColorInfo` for "unicast-default" (bit 0, flex-algo 128, constraint include-any), the controller MUST push `router traffic-engineering` config with `administrative-group alias UNICAST-DEFAULT group 0`, `flex-algo 128 unicast-default administrative-group include any 0 color 1`, and the BGP `next-hop resolution ribs` line to all devices.
- **Admin-group application**: After a foundation key sets `link_color` on a link to the unicast-default `LinkColorInfo` pubkey, `show traffic-engineering database` on the connected devices MUST reflect `UNICAST-DEFAULT` admin-group on the interface. Setting the color back to default MUST remove the admin-group.
- **Flex-algo topology**: With links tagged UNICAST-DEFAULT, `show isis flex-algo` on participating devices MUST show algo 128 including only UNICAST-DEFAULT links. Untagged links MUST be absent from the algo-128 LSDB view.
- **Colored tunnel RIB**: `show tunnel rib system-colored-tunnel-rib brief` MUST show (endpoint, color 1) entries for each participating device, resolving via unicast-default tunnels.
- **VPN unicast path selection**: A BGP VPN-IPv4 route carrying `Color:CO(00):1` MUST resolve its next-hop through the color-1 (unicast-default) tunnel in `system-colored-tunnel-rib`, traversing only UNICAST-DEFAULT tagged links.
- **Route-map color community**: `show bgp vpn-ipv4 detail` MUST show `Color:CO(00):1` on exported VPN-IPv4 routes from all tenant VRFs.
- **Multicast path isolation**: PIM RPF for a multicast source MUST continue to resolve via IS-IS algo 0 (all links, including both tagged and untagged) regardless of BGP next-hop resolution config.
- **Color clear**: After `link color clear --name unicast-default` removes the color from all links, the controller MUST generate `no traffic-engineering administrative-group UNICAST-DEFAULT` on all previously-tagged interfaces on the next reconciliation cycle.

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
MUST confirm admin-group membership is visible only on interfaces with a non-default link color.

**Verify multicast RPF uses algo-0 (including colored links):**
```
show ip mroute
```
MUST confirm PIM RPF resolves via the IS-IS unicast RIB (algo 0). The incoming interface for a multicast source reachable via a colored link MUST be the colored interface, unchanged by BGP next-hop resolution config.

---

## Impact

### Codebase

- **serviceability** — new `LinkColorInfo` PDA (foundation-managed, one per color); new `link_color: Pubkey` field on `Link`; new `link_color: Option<Pubkey>` field on `LinkUpdateArgs` with foundation-only write restriction; new `flex_algo_node_segment_idx: u16` field on `Interface`; `FlexAlgo = 2` added to `FeatureFlag` enum.
- **controller** — reads `GlobalState.feature_flags` to gate all flex-algo config on the `FlexAlgo` flag; reads `link.link_color`, resolves `LinkColorInfo` PDAs, generates IS-IS TE admin-group config on interfaces, flex-algo definitions with `color` field, `system-colored-tunnel-rib` BGP resolution profile, and adds `set extcommunity color` to the existing per-tunnel inbound route-maps (`RM-USER-{{ .Id }}-IN`).
- **CLI** — full color lifecycle commands (`create`, `update`, `delete`, `clear`, `list`); `link update` gains `--link-color`; `link get` / `link list` display the field including derived EOS color value.
- **SDKs** — `LinkColorInfo` added to all three language SDKs; `link_color` field added to link deserialization structs; `FlexAlgo` flag added to feature flag constants.

### Operational

- DZF MUST create a `LinkColorInfo` account and assign `link_color` on links before the controller applies TE admin-groups. Until a color is created and assigned, links behave as today.
- Adding a new color MUST NOT require a code change or deploy — DZF creates the `LinkColorInfo` account via the CLI and the controller picks it up on the next reconciliation cycle.
- `link_color` appends to the serialized layout and defaults to `Pubkey::default()` on existing accounts. No migration is required.
- The transition from no-color to color-1 on all tenant VRFs is a one-time controller config push. The template section order enforces the correct sequencing within a single reconciliation cycle: the `router traffic-engineering` block and `address-family vpn-ipv4 next-hop resolution` config appear before the `route-map RM-USER-*-IN` blocks in `tunnel.tmpl`, so EOS applies them top-to-bottom in the correct order. Applying the route-map before the RIB is configured would cause VPN routes to go unresolved.

### Testing

| Layer | Tests |
|---|---|
| Smart contract | `LinkColorInfo` create/update/delete lifecycle; foundation-only authorization; auto-assignment of bit and flex-algo number; `link_color` assignment and clearing; delete blocked when links assigned; `clear` removes color from all links |
| Controller (unit) | Interface config with and without color; remove/add diff on color change; `router traffic-engineering` block with `color` field; `system-colored-tunnel-rib` BGP resolution config; per-VRF route-map generation; new color triggers config push to all devices |
| SDK (unit) | `LinkColorInfo` Borsh round-trip; `link_color` field in `link get` / `link list` output across Go, Python, and TypeScript; EOS color value displayed correctly |
| End-to-end (cEOS) | Color create → controller config push verified; admin-group applied and removed on interface; flex-algo 128 topology with `color 1` excludes colored link; `system-colored-tunnel-rib` populated; VPN unicast resolves via color-1 tunnel; BGP VPN-IPv4 routes carry correct color community; multicast RPF unchanged on untagged links; color clear removes admin-groups from interfaces; onchain delete removes PDA |

---

## Security Considerations

- `link_color` MUST only be writable by foundation keys. A contributor MUST NOT be able to tag their own link with a color to influence path steering. The check mirrors the existing pattern used for `link.status` foundation-override.
- `LinkColorInfo` accounts MUST only be created, updated, or deleted by foundation keys.
- If a foundation key is compromised, an attacker could reclassify links or create new color definitions, causing traffic to be steered onto unintended paths. This is the same threat surface as other foundation-key-controlled fields. No new mitigations are introduced.

---

## Backward Compatibility

- The `link_color` field MUST default to `Pubkey::default()` (no color assigned) on existing accounts. No migration is required.
- Devices that do not receive updated config from the controller MUST continue to forward using IS-IS algo 0 only. The flex-algo topology is distributed — a device that does not participate is simply not included in the constrained SPF.
- The controller MUST push `system-colored-tunnel-rib` config and flex-algo definitions before activating per-VRF route-maps. Activating route-maps first causes VPN routes to carry a color community with no matching tunnel RIB entry, leaving them unresolved.
- `tunnel-rib` config generated by the controller uses a single BGP next-hop resolution profile applied globally. Per-tenant resolution profile overrides are not supported at this stage.

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
