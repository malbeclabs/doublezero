# RFC-21: Multicast FPGA Filtering

## Summary

**Status: `Draft`**

Extends DoubleZero's FPGA filtering architecture to IP multicast so that a subscriber's received feed can be deduplicated, signature-verified, or otherwise filtered inline before it reaches the user. This RFC introduces a new user mode — **multicast-EF** — that places the user's GRE tunnel in a new filtration VRF. All multicast delivered to a multicast-EF subscriber transits the DZD's FPGA on the way to the user; that is the primary value of this RFC.

Because a multicast-EF user may also be a publisher (per RFC-15), the RFC also specifies the plumbing needed to keep publishing working when the user's tunnel lives in the filtration VRF: a second GRE wrap applied on the client so the publisher's multicast crosses the FPGA cable as unicast, a pair of transit tunnels on the DZD that decapsulates it back into default VRF, and an inter-VRF eBGP session that carries the publisher `/32` so cross-DZD PIM RPF works. These are supporting mechanisms; the filter applied to publisher-direction traffic is architecturally neutral.

Regular (non-EF) multicast users continue to work unchanged and interoperate with multicast-EF users. This RFC borrows the general FPGA routing architecture, VRF-lite split, and inter-VRF eBGP pattern defined in the FPGA Routing Architecture RFC (referred to as "the unicast EF RFC" below); readers should skim that first for context.

## Motivation

Solana multicast subscribers receive traffic from a set of publishers that is large, not fully trusted, and dynamic. Today the subscriber's receive path is the stock production multicast data plane — whatever arrives in default VRF is forwarded straight to the subscriber's GRE tunnel. Adding an FPGA inline on the receive side gives us a protocol-agnostic point to apply things like dedup, signature verification, and relevancy checks. Users adopt multicast-EF mode to get this protection on the groups they subscribe to.

A multicast-EF user's publisher-role traffic also transits the FPGA on its way out of the user's local DZD — a byproduct of placing the tunnel in the filtration VRF. Whether the filter image applies any checks in that direction (e.g. validating signatures at ingress so subscribers don't need to) is left to a future decision, orthogonal to the network architecture in this RFC. A misbehaving publisher can always be disconnected at the network layer; per-packet ingress filtering is defense-in-depth, not the primary value.

## New Terminology

- **Multicast-EF user** — a multicast user whose GRE tunnel lands in `vrf-mcast-filtration` so that received traffic transits the FPGA. Can play publisher and/or subscriber roles per RFC-15, exactly as a regular multicast user does.
- **`vrf-mcast-filtration`** — new VRF that hosts multicast-EF users' GRE tunnels on each DZD. Distinct from the unicast EF RFC's `vrf1-edge-filtration`.
- **FPGA-transit-pub tunnel** — a GRE tunnel in the default VRF (`Tunnel907` in examples below) whose far end is reachable through the FPGA cable. Decapsulates the inner GRE wrap that a multicast-EF user applies when publishing.
- **Inner GRE** — a second GRE header applied by the publishing client so the multicast payload is unicast-routable within `vrf-mcast-filtration`. Stripped by the FPGA-transit-pub tunnel on the default-VRF side.
- **FPGA cable** — same concept as the unicast EF RFC's "FPGA Loopback": a physical cable whose two ends are on different DZD sub-interfaces in different VRFs, with FPGA hardware in-line.

## Alternatives Considered

1. **Do nothing.** Subscribers continue to receive unfiltered multicast. Simplest, but provides no mechanism to cultivate better feeds for subscribers.

2. **Physically place the FPGA on the CYOA port.** All user traffic (EF and non-EF) transits the FPGA at the network edge. Simpler topology — no VRF split, no inner-GRE trickery. But: no per-user opt-in; an FPGA outage takes out all CYOA traffic for that DZD, not just EF users; no method for pass-around during FPGA upgrades or outages. Rejected.

3. **Centralized FPGA filter device.** Route all multicast through a single filter appliance. Eliminates per-DZD FPGA requirements but adds network latency as packets must go to a central location ot be filtere dbefore making their way to subscribers. Rejected for latency increase.

4. **Split tunnels: one for pub, one for sub.** A pub+sub user would land in two separate GRE tunnels, one in default VRF (pub) and one in `vrf-mcast-filtration` (sub). Avoids the inner-GRE trick entirely. Costs one extra tunnel slot per pub+sub user. Rejected due to increaesd tunnel usage.

5. **Route-leaking (`router general / leak routes`) instead of inter-VRF eBGP.** Alternative cross-VRF control-plane mechanism. Validated on devnet and works. Rejected in favor of inter-VRF eBGP for three reasons: the inter-VRF BGP session is a first-class liveness signal for the FPGA path (route-leaking has no equivalent); local-pref / MED on the session compose naturally with a future bypass-cable session option for FPGA failover; and inter-VRF eBGP mirrors the exact pattern established in the unicast EF RFC, so one mental model covers both features.

## Detailed Design

<p align="center">
  <img src="images/rfc-fpga-routing/Multicast%20Edge%20Filtering.png" alt="Multicast Edge Filtering architecture" width="900">
</p>
<p align="center"><em>Figure 1: Multicast EF architecture on one DZD. Default VRF (right) and <code>vrf-mcast-filtration</code> (left) are connected via an FPGA cable. Publisher double-GRE flows filt → default (inner-GRE unicast decapped by Tunnel907); subscriber native multicast flows default → filt. Inter-VRF eBGP rides on Tunnel907/Tunnel908.</em></p>

### VRF layout and sub-interfaces

A new VRF — `vrf-mcast-filtration` — is created on every DZD that supports EF multicast. It hosts EF multicast users' GRE tunnels (both publisher and subscriber role, per RFC-15 these may share one tunnel per user).

The FPGA cable is the same physical resource the unicast EF RFC introduces, with one VLAN sub-interface pair dedicated to multicast:

| VLAN | Filtration side | Default side |
|---|---|---|
| 30 | `ETH1.30` in `vrf-mcast-filtration`, `192.168.2.8/31`, PIM sparse-mode | `ETH3.30` in `default`, `192.168.2.9/31`, `ip igmp` + PIM sparse-mode |

One VLAN carries all traffic classes bidirectionally on this cable: publisher-direction inner-GRE unicast (filt → default), subscriber-direction native multicast (default → filt), plus the inter-VRF eBGP GRE overlay (Tunnel907/908, below). The FPGA distinguishes direction by port-pair; VLAN separation would not buy anything at the data plane and is not required by PIM's OIL-exclusion rule (PIM sees `Tunnel907` as the incoming interface for publisher multicast, not the underlying sub-interface, so a same-sub-interface subscriber egress is a distinct PIM interface and does not conflict).

The `/31` address is well-known, identical on every DZD — it never leaves the DZD on the wire, and both inner-GRE matching and the inter-VRF BGP next-hop rely on it being predictable.

### Tunnel907 and Tunnel908 — FPGA-transit-pub pair

A pair of GRE tunnels rides on top of the sub-interface:

| Tunnel | VRF | Address | Tunnel source/dest |
|---|---|---|---|
| `Tunnel907` | `default` | `10.99.0.1/31` | `src 192.168.2.9 dst 192.168.2.8` |
| `Tunnel908` | `vrf-mcast-filtration` | `10.99.0.0/31` | `src 192.168.2.8 dst 192.168.2.9`, `tunnel underlay vrf vrf-mcast-filtration` |

`Tunnel907` is the decapsulation endpoint for publisher inner-GRE arriving from the filtration VRF (see publisher traffic flow below). `Tunnel908` is its bidirectional peer; it carries the inter-VRF eBGP session (see control plane) and exists so that control-plane traffic in either direction terminates at a tunnel interface with a matching local IP. The `tunnel underlay vrf` directive is required because `Tunnel908`'s source IP (`192.168.2.8`) lives in `vrf-mcast-filtration`, not in default.

`Tunnel907` has `pim ipv4 border-router` configured. This makes PIM treat publisher multicast as FHR-attached on arrival, required for `(S, G)` state to build correctly. Matches the directive the controller already emits on production multicast publisher tunnels (see `controlplane/controller/internal/controller/templates/tunnel.tmpl`).

`Tunnel908` MUST NOT have PIM enabled. It's a control-plane-only interface (BGP TCP). Enabling `pim sparse-mode` on it causes filtration VRF's PIM to treat it as a downstream neighbor and add it to the `(S, G)` OIL whenever default VRF sends an `(S, G)` Join for a local publisher. The result is a multicast forwarding loop through the FPGA. Leave PIM off Tunnel908.

### Control plane: inter-VRF eBGP

Publisher `/32`s reach PIM's RPF resolution in default VRF via an inter-VRF eBGP session between `Tunnel907` (default VRF) and `Tunnel908` (filtration VRF). The session uses asdot notation with a per-DZD sub-ASN on the filtration side, mirroring the unicast EF RFC pattern:

```
router bgp 65342
   bgp asn notation asdot
   neighbor 10.99.0.0 remote-as 65342.{{ .Device.Id }}
   neighbor 10.99.0.0 description MCAST-FILT-FPGA-LOOPBACK
   address-family ipv4
      neighbor 10.99.0.0 activate
      neighbor 10.99.0.0 route-map RM-MCAST-FILTER-FPGA-LOOPBACK-IN in
      neighbor 10.99.0.0 route-map RM-MCAST-FILTER-FPGA-LOOPBACK-OUT out
   !
   vrf vrf-mcast-filtration
      neighbor 10.99.0.1 remote-as 65342
      neighbor 10.99.0.1 description DEFAULT-MCAST-FILT-FPGA-LOOPBACK
      neighbor 10.99.0.1 local-as 65342.{{ .Device.Id }} no-prepend replace-as
      address-family ipv4
         neighbor 10.99.0.1 activate
         neighbor 10.99.0.1 route-map RM-MCAST-FILTER-FPGA-LOOPBACK-IN in
         neighbor 10.99.0.1 route-map RM-MCAST-FILTER-FPGA-LOOPBACK-OUT out
```

Route-maps `RM-MCAST-FILTER-FPGA-LOOPBACK-IN/OUT` set `local-preference 1000` inbound and `metric 100` outbound, so that a future bypass-cable session composes cleanly as the backup path (same convention as the unicast EF RFC's `RM-FILTER-FPGA-LOOPBACK-*`).

On EOS 4.31.2F, `local-as <asdot> no-prepend replace-as` MUST be configured per-neighbor. The VRF-level form the unicast EF RFC template suggests is rejected on this release. Newer releases may behave differently; the template should account for both.

A publisher `/32` originates at the publisher's BGP session in `vrf-mcast-filtration`, eBGP-crosses to default VRF via the above session, and from there propagates cross-DZD via the existing `Ipv4BgpPeers` iBGP mesh (activated under `address-family ipv4` per the controller template). No vpn-ipv4 mesh involvement and no additional infrastructure beyond what already exists.

### PIM

Default VRF's PIM configuration is unchanged: sparse-mode on WAN/DZX and user-facing interfaces, anycast RP at `Loopback1000` (`10.0.0.0/32`). Sparse-mode is added on `ETH3.30` (the FPGA sub-interface) for the agent-managed `ip igmp static-group` to populate the OIL toward multicast-EF users. MSDP is unchanged end-to-end — the existing cross-DZD mesh on `Ipv4BgpPeers` handles source discovery as today, and no new MSDP sessions are introduced by this RFC.

In `vrf-mcast-filtration`:

- PIM sparse-mode on `ETH1.30` (the FPGA sub-interface) and all user tunnel interfaces.
- The same anycast RP address as default VRF (`10.0.0.0`), **not** configured on a local loopback. Filtration VRF reaches the RP via a static unicast route:

  ```
  ip route vrf vrf-mcast-filtration 10.0.0.0/32 ETH1.30
  ```

  PIM in filtration VRF treats default VRF's anycast loopback as its upstream RP across the FPGA cable. This avoids making filtration VRF itself an anycast member, which would prevent Join propagation across the VRF boundary.

- A multicast-RIB catch-all so arrivals on `ETH1.30` (post-FPGA) pass RPF without polluting the unicast RIB with per-publisher entries:

  ```
  router multicast
     vrf vrf-mcast-filtration
        ipv4
           rpf route 0.0.0.0/0 ETH1.30
  ```

- No inter-VRF MSDP peering. Filtration VRF is not an RP, so there is nothing for inter-VRF MSDP to exchange. The `L` (Source-attached) flag that appears on filtration VRF `(S, G)` state from MSDP-learned sources is harmless — the OIL toward the FPGA is populated by the signaling bridge below, not by filtration-VRF PIM.

### Signaling bridge: agent-managed `ip igmp static-group`

Default VRF's OIL for an EF multicast group must include `ETH3.30` so that multicast egresses toward the FPGA. PIM Joins originating in filtration VRF do propagate to default VRF via the FPGA cable, but because default VRF is itself the RP, Joins terminate at the RP rather than registering the inbound interface as downstream OIL.

We work around this with a DZD-side signaling bridge driven by the doublezero agent. For each group that at least one on-device multicast-EF user is subscribed to, the agent configures:

```
interface ETH3.30
   ip igmp static-group <group>
```

This adds `ETH3.30` to default VRF's `(*, G)` OIL without needing PIM signaling to cross the VRF boundary. When the last on-device multicast-EF user drops the group, the agent removes the entry.

Subscriber group membership is state the agent already tracks for tunnel provisioning, so this is a narrow extension — no new state source, no per-packet logic.

### Traffic flows

#### Multicast-EF user receiving (subscriber role)

The primary scenario: publisher is either a regular (non-EF) multicast user or a multicast-EF user on another DZD, subscriber is a multicast-EF user.

```
Multicast enters the subscriber's DZD on the WAN/DZX as any cross-DZD
multicast does today (PIM/MSDP in default VRF).

On the subscriber's DZD:
  1. Default VRF (S, G) OIL includes Tunnel300 (intra-DZ regular subs) and
     ETH3.30 (multicast-EF subs, via the agent-managed static-group)
  2. Packet egresses ETH3.30 as native L3 multicast
  3. FPGA processes the packet in the default→filt direction — dedup,
     signature verification, or whatever the filter image implements
  4. Packet arrives on ETH1.30 in vrf-mcast-filtration
  5. Filtration VRF PIM replicates to multicast-EF user tunnels based on
     (*, G) state from the users' own PIM Joins
  6. Outer GRE encap over the user's client tunnel
```

#### Multicast-EF user sending (publisher role)

The user's client wraps outbound multicast in a second GRE header before the outer client-tunnel GRE. The inner GRE has a unicast destination so the packet becomes unicast-routable within `vrf-mcast-filtration` rather than being short-circuited by PIM.

```
Client's doublezerod wraps each outbound multicast packet:
  [Multicast payload]
  → wrap in inner GRE (src=192.168.2.8 dst=192.168.2.9)
  → wrap in outer GRE (normal client tunnel to DZD)

On the sender's DZD:
  1. CYOA receives outer GRE (default VRF underlay)
  2. Outer GRE decap via the user's tunnel → packet in vrf-mcast-filtration
  3. Inner packet is unicast to 192.168.2.9 → routed out ETH1.30
  4. FPGA processes in the filt→default direction (filter image may or may
     not apply checks; architecturally neutral)
  5. Packet arrives on ETH3.30 in default VRF and matches Tunnel907's
     tunnel src/dst → inner GRE stripped → bare multicast in default VRF
  6. Default VRF PIM processes as any other multicast source: (S, G)
     built, forwarded to WAN/DZX and local OIL
```

The user's `/32` travels through the inter-VRF eBGP session (below) so remote DZDs' default VRFs can RPF to it — same as any other cross-DZD multicast source.

A regular (non-EF) multicast user's outbound traffic skips the double-GRE trick: data enters default VRF directly via the client tunnel, just like today.

#### Same-DZD publisher → subscriber hairpin

When a multicast-EF user subscribes to a group published on the same DZD:

- Regular publisher: default VRF PIM's (S, G) OIL includes `Tunnel300`, any user subscriber tunnels in default VRF, and `ETH3.30` (for multicast-EF subscribers). Single FPGA traversal (default→filt).
- Multicast-EF publisher (a multicast-EF user on this DZD also publishing the group): data transits the FPGA twice — once filt→default (ingress via the double-GRE flow above), once default→filt (egress toward the subscriber). Both traversals share the same physical cable on VLAN 30 in opposite directions; the FPGA tells them apart by port-pair.

### Client-side changes (doublezerod)

Detailed doublezerod work is deferred to an update to this RFC. At the wire level:

| Traffic | Encapsulation |
|---|---|
| Multicast-EF user → DZD (PIM, BGP, non-multicast) | Single GRE |
| DZD → multicast-EF user (multicast data on the subscriber path) | Single GRE |
| Multicast-EF user → DZD (multicast data + heartbeats, when the user is publishing) | Double GRE — outer is the normal client tunnel; inner src `192.168.2.8`, dst `192.168.2.9` |
| Regular multicast user → DZD (all traffic) | Single GRE (existing, unchanged) |

The inner-GRE source and destination addresses are fleet-wide constants: they never leave the DZD on the wire, and Arista's GRE tunnel matching is strict on both endpoints. Publishing clients hardcode them.

Subscribers on the receive path run unchanged — the client has no awareness that its DZD applies FPGA filtering. The double-GRE wrap only applies when the client is publishing.

#### Kernel-native double-GRE via nested tunnels (no app-level changes)

The double-GRE wrap can be produced entirely by the host kernel via a second GRE tunnel sitting on top of the normal client tunnel. This lets a user application send multicast with no DoubleZero-specific socket options — no `IP_MULTICAST_IF`, no bind to a specific source IP, no packet crafting. The app just does what any standard UDP multicast sender does on Linux.

Setup on the publishing host (what doublezerod would install at connect time, given the user's DZ IP):

```
# 1. Inner GRE tunnel (the "FPGA transit" endpoint)
ip tunnel add dz-inner mode gre local 192.168.2.8 remote 192.168.2.9 ttl 32
ip addr add 192.168.2.8/32 dev dz-inner
ip link set dz-inner up

# 2. Move the user's DZ IP onto dz-inner (was on dz-sub)
ip addr del <DZ-IP>/32 dev dz-sub
ip addr add <DZ-IP>/32 dev dz-inner

# 3. Routes
ip route replace 192.168.2.9/32 dev dz-sub            # outer-of-inner goes out dz-sub
ip route replace 239.0.0.0/8 dev dz-inner src <DZ-IP> # multicast goes out dz-inner with DZ-IP as source
```

The `src <DZ-IP>` hint on the multicast route is the key — it tells the kernel which source IP to use for packets taking that route, without the application needing to bind to it. A client app then just does:

```python
sock = socket.socket(AF_INET, SOCK_DGRAM)
sock.setsockopt(IPPROTO_IP, IP_MULTICAST_TTL, N)   # standard multicast hygiene, not DZ-specific
sock.sendto(payload, ('239.x.x.x', port))
```

Kernel chains: route lookup for `239.x.x.x` → `dz-inner` + `src <DZ-IP>` → inner GRE wrap → route lookup for `192.168.2.9` → `dz-sub` → outer GRE wrap → physical wire. Number of GRE headers is dictated entirely by how many tunnel interfaces the packet traverses on its way out.

The client's view is indistinguishable from sending multicast on any ordinary Linux host. All DoubleZero-specific knowledge lives in the network stack configuration (tunnels + routes) that doublezerod sets up once at connection time.

Unicast from the client (BGP, ordinary traffic) bypasses `dz-inner` because neither the destination route nor the source IP points at it — BGP uses the link-local `/31` on `dz-sub`, ordinary unicast uses the default route or other interface-specific routes. Only traffic in `239.0.0.0/8` gets double-wrapped.

Validated 2026-04-22 on bm8 as a mock user. Equivalent `ip(6)tables`/PBR approaches exist if more fine-grained selection is needed, but the simple routing model above is sufficient for the multicast-EF flow.

#### Kernel-bypass TX paths (AF_XDP)

Kernel-bypass TX paths — Agave's `agave-xdp` crate being the concrete example — build GRE frames in userspace from netlink-observed tunnel state. The routing configuration above works with them unmodified; no DoubleZero-specific client code is required.

`agave-xdp` today handles single-GRE TX transparently (scrapes `IFLA_GRE_*` off GRE netdevs, writes `[Eth][IP][GRE][inner]` at TX time) but explicitly rejects GRE-over-GRE with a warning. A demo patch (<100 LOC across three files, no DoubleZero-specific branches) lifts that rejection: `interface_gre_route_info` recurses once, resolving the `dz-inner` → `dz-sub` → physical chain to a `{ inner_tunnel, outer_tunnel, physical_mac }` triple at route-lookup time, and packet construction writes both IP+GRE layers in one pass. This is parity with what the Linux kernel already does via `ip_local_out` re-entry — Agave was missing the capability, not gaining a DZ-specific feature.

The same pattern applies to any non-Agave kernel-bypass path; the client-side contract in this RFC does not require per-fast-path DoubleZero awareness.

### Relationship to the FPGA Routing Architecture RFC

This RFC builds directly on concepts introduced in the unicast EF RFC:

| Shared concept | Source |
|---|---|
| FPGA cable as an inline L2 filter between VRFs on the same DZD | Unicast EF RFC §Detailed Design |
| Per-VRF sub-ASN via BGP asdot notation | Unicast EF RFC §Detailed Design |
| Primary/backup path selection via `local-preference 1000` in / `metric 100` out on the FPGA-loopback session | Unicast EF RFC §Detailed Design |
| Inter-VRF eBGP as the control plane for cross-VRF reachability | Unicast EF RFC §Detailed Design |
| Per-DZD opt-in to EF mode | Unicast EF RFC §Detailed Design |

A DZD that supports only unicast EF, only multicast EF, or both is a valid deployment — the two features share the physical FPGA cable but are otherwise independent.

## Impact

- **controlplane/controller**: new tunnel-template paths that emit the VLAN 30 sub-interface pair, Tunnel907/Tunnel908, the inter-VRF eBGP session, and the filtration-VRF PIM RP + static route + multicast RPF catch-all. Agent must emit `ip igmp static-group <G>` on `ETH3.30` for each group an on-device multicast-EF user is subscribed to, and remove it when the last such user drops the group.
- **smartcontract**: a new connection mode — multicast-EF — parallel to the existing multicast mode. A multicast-EF user uses the same publisher/subscriber-group fields RFC-15 already defined; the distinction from a regular multicast user is which VRF the controller places the tunnel in. Exact field shape is implementation work, but the mode must be distinguishable onchain so the controller emits the EF-specific config.
- **client/doublezerod, client/doublezero CLI**: double-GRE wrap on outbound multicast when the user is multicast-EF and publishing (follow-on RFC). Receive path and control-plane paths unchanged.
- **activator**: per-DZD resource tracking for multicast-EF users, analogous to the unicast EF RFC's requirements.

Performance impact is dominated by the FPGA itself; the routing plumbing here adds a pair of GRE encap/decap operations per packet on the publisher path and a single FPGA transit on the subscriber path. Neither is expected to be material on the DZDs.

## Security Considerations

The FPGA is the filter point. The architecture guarantees that a multicast-EF user cannot receive multicast without it transiting the FPGA: multicast enters `vrf-mcast-filtration` only via `ETH1.30`, enforced by the `rpf route 0.0.0.0/0 ETH1.30` catch-all, which drops any multicast arriving on a user tunnel claiming to be a source. A publishing multicast-EF user cannot bypass its own ingress-direction FPGA traversal either — the only route for its inner-GRE unicast within `vrf-mcast-filtration` is out `ETH1.30`.

The threat model this RFC addresses is malicious or malfunctioning multicast data (spoofed sources, duplicates, replayed packets) as seen from the perspective of a multicast-EF subscriber. It does not address compromise of the FPGA itself or of the DZD control plane; those are covered under general DoubleZero operational security.

Regular (non-EF) multicast users can send into the network as they do today. Their traffic is filterable at any multicast-EF user's DZD on the receive side but does not transit an FPGA on its way to regular subscribers. This is a conscious trade-off: the network-layer `disconnect` action to deal with misbehaving publishers, not on per-packet ingress filtering, since a misbehaving publisher would still be abusing the CYOA port and DZD switching resources even with inbound filtering.

## Backward Compatibility

Existing multicast users (including the pub+sub combinations RFC-15 enables) continue to operate unchanged. Multicast-EF is a new opt-in mode — onchain, config-generation, and client-side. A DZD without any multicast-EF users has no `vrf-mcast-filtration` configuration and no FPGA traffic.

Mixed deployments (regular publisher + multicast-EF subscriber, and vice versa) are expected to be the common case and are a first-class flow in the traffic-flow section above.

## Open Questions

- **FPGA-down failover.** The unicast EF RFC defines an Inter-VRF Loopback bypass that diverts traffic around a failed FPGA. For multicast, a bypass would deliver unfiltered traffic to multicast-EF subscribers — possibly acceptable, possibly not. Needs a policy decision (fail-open vs. fail-closed) before implementation. If fail-open, a second sub-interface pair on a bypass cable (e.g. VLAN 40) would mirror the unicast EF bypass mechanism; the existing FPGA-loopback route-maps (`local-pref 1000 in / metric 100 out`) compose cleanly with a backup session of lower preference.
- **Production platform validation.** Architecture validated on 7130LBR / EOS 4.31.2F with a passthrough FPGA image. Production DZDs run 7280R3 / 7800R3; a platform-parity pass is needed before rollout, particularly around GRE-in-GRE forwarding in hardware and whether `local-as ... replace-as` is accepted at VRF level (it's per-neighbor on 4.31.2F).
- **Scale.** Prefix-list size, BGP convergence with validator-count publishers, and agent-driven static-group churn rate under rapid subscribe/unsubscribe all need a scale pass.
- **Real FPGA filter image.** All testing to date has been with a passthrough FPGA. The filter logic (dedup, signature verification) is a separate workstream; the network architecture in this RFC is independent of what the FPGA does to packets in flight.
