# RFC-22: Client-Originated PIM Register for Multicast Source Origination

## Summary

**Status: Implemented** (client + controller in PR #3959; this RFC in #3951)

When a DoubleZero client is both a publisher and a subscriber of the same multicast group over a single GRE tunnel (the simultaneous pub/sub case enabled by RFC-15), the source it publishes is not reliably distributed to the rest of the network. This RFC proposes that the client daemon (`doublezerod`) originate a PIM-SM **Register** for each of its published sources, sent as a periodic *beacon* to the Rendezvous Point (RP). Receiving the Register causes the device (which is the anycast RP) to originate the MSDP Source-Active (SA) message for that source, which is what propagates it across the mesh.

The behavior we need from the device is already proven — Arista's lab confirmed that once a Register destined to the RP is permitted in, the device sets its "may-notify-MSDP" (`N`) flag and originates the SA. The piece that is missing in production is that nothing in DoubleZero ever sends a Register. This RFC supplies it on the client, plus the one device-side ACL change required to accept it.

## Motivation

RFC-15 made it possible for one client to publish and subscribe to multicast simultaneously over a single GRE tunnel. That works for the data plane, but it introduced a control-plane failure for source origination that only manifests on dual-role tunnels.

On the device, every multicast publisher tunnel is configured with `pim ipv4 border-router`, and the device originates published sources into PIM/MSDP by acting as the first-hop/border router for traffic arriving natively on that tunnel. This works when the tunnel is publisher-only. But when the same tunnel is *also* a subscriber, the client runs PIM to send its `(*,G)` joins, which brings up a PIM neighbor on the tunnel. The source's RPF then resolves via that neighbor, and the device stops treating itself as the first-hop router for the source. It never sets the `N` flag and never originates the MSDP SA. The source forwards locally on that device but is invisible to every other RP in the mesh.

Because the device is one of many anycast RPs (every device owns `10.0.0.0/32` and they are glued together with MSDP), the only way a remote RP learns a source it is not directly attached to is via an MSDP SA. With no SA originated, remote subscribers only receive the source if a contiguous PIM `(S,G)` path happens to reach them. The result is partial, non-deterministic delivery.

This was observed on group `233.84.178.5`: traffic from a dual-role publisher reached the publisher's local region and some sites, but not others (for example, it reached Frankfurt but not New York), and the MSDP SA cache was empty network-wide. Direct evidence collected on the devices:

- The device is the DR on every GRE tunnel (`pim ipv4 dr-priority 4294967295`; the client advertises priority 1 and never wins). Per PIM-SM, the registering router is the DR on the source's link, so the device — not the client — is the de-facto first-hop router today, and it originates via `border-router`, not via a received Register.
- The device's PIM Register receive counter is 0 and the inbound-ACL deny counter for traffic to the RP is ~0. Nothing is sending a Register.
- On the broken group, the `(S,G)` entries carry flags `SP` (no `B`/`N`); on a working publisher-only group they carry `SBNP` (Border + may-notify-MSDP).
- A controlled test (temporarily removing the subscriber role so the tunnel became publisher-only) caused the neighbor to age out, the device to set `N`, originate the SA, and the previously-broken site began receiving. Restoring the subscriber role stopped SA origination but left the already-built trees up. This confirmed both the cause and that origination is the missing piece.

Any validator that both publishes and subscribes to the same group is affected. The current operational workaround (unsubscribe, wait, resubscribe) re-bootstraps origination but is fragile and does not survive any flap.

## New Terminology

- **FHR (First-Hop Router):** the router directly attached to a multicast source. In PIM-SM it is the one that encapsulates source traffic into Register messages to the RP.
- **Register / Register beacon:** a PIM-SM Register message (type 1) encapsulating an original multicast datagram, unicast to the RP. A *beacon* here means we send it periodically and ignore Register-Stop, rather than implementing the full FHR Register state machine.
- **Anycast RP:** every device shares the RP address `10.0.0.0/32`; MSDP distributes source knowledge between them.
- **MSDP SA (Source-Active):** the MSDP message an RP originates to tell other RPs that a source is active for a group.
- **`N` flag ("may notify MSDP"):** the device-side `(S,G)` flag indicating the device will originate an SA for that source. It is set when the device learns the source as a first-hop/border router or via a received Register.
- **Border-router suppression:** the condition where `pim ipv4 border-router` source injection does not occur because the source's RPF interface has an established PIM neighbor (the dual-role case).
- **Heartbeat:** the existing 4-byte UDP keepalive (`44 5A 00 01`, dst = group, port 5765, every 10s, TTL 32) the client already emits per published group to keep PIM `(S,G)` and MSDP SA state alive.

## Alternatives Considered

1. **Do nothing / manual workaround.** Keep using unsubscribe→wait→resubscribe to re-bootstrap origination. Rejected: fragile, manual, does not survive a flap or a new subscriber, and is the status quo we are fixing.
2. **Device-side fix only (have `border-router` originate despite the neighbor).** This would be ideal if available, but it depends entirely on Arista changing or exposing behavior we do not control, and it is unconfirmed. We have an open TAC case but cannot block on it.
3. **Permit the Register in the ACL alone (Arista's suggestion).** Necessary but not sufficient: with nothing sending a Register it changes nothing. This RFC includes the permit *and* the sender.
4. **Full PIM-SM FHR on the client.** Implement Register plus Register-Stop handling, the register-suppression timer, and null-Register keepalives (RFC 7761 §4.4.1). More correct, but it requires a brand-new inbound PIM read path on the client (today the PIM socket is send-only) and a full state machine. Rejected for now in favor of the beacon: we only need interoperability with EOS, not a complete FHR.
5. **Separate publisher and subscriber onto different tunnels/users.** Avoids the neighbor entirely, but reverses the single-tunnel model RFC-15 established and consumes additional tunnel/user budget per RFC-14. Rejected as the primary fix; it remains a fallback.
6. **Chosen: client-originated Register beacon (this RFC), with the device retaining `border-router` as a backstop.**

## Detailed Design

### Overview

The client adds a `RegisterSender` that, for each published group, periodically builds a PIM Register encapsulating the group's heartbeat packet and unicasts it to the RP over the GRE tunnel. The device permits that Register inbound and, as the anycast RP, originates the MSDP SA. Everything downstream (MSDP flooding, remote `(S,G)` joins, SPT forwarding of the real stream and the heartbeat) is the existing anycast-RP machinery and is unchanged.

The Register is a control-plane bootstrap only. The real data continues to reach the device natively over the GRE and is what flows down the SPTs network-wide; the Register's sole job is to make the device set `N` and originate (and keep re-originating) the SA.

### Why the heartbeat is the encapsulated packet

`doublezerod` does not originate or see the user's actual stream — the user's application sends it, and the daemon only installs a route pointing it out the tunnel. There is no packet-capture path in the client. The daemon *does* originate the heartbeat, so the heartbeat is the natural packet to encapsulate. This is sufficient because the Register only needs to make the device learn the `(S,G)`; the device reads the source and group out of the encapsulated IP header. The real data does not need to traverse the Register.

### Packet structure

A client Register, as it appears on the wire from client to device, is three IP layers deep. The kernel adds the outermost (GRE) layer on egress; the client builds the rest.

```
┌ IP (GRE delivery, outer)  src=client-underlay  dst=device-underlay  proto=47 (GRE)     ← kernel/GRE tunnel adds
├ GRE  (~4 bytes, protocol-type 0x0800 = IPv4)                                            ← kernel/GRE tunnel adds
│  ┌ IP (the Register)  src=client tunnel/overlay addr  dst=RP 10.0.0.0  proto=103 (PIM)  ← RegisterSender builds
│  ├ PIM header (4 B)  version=2, type=1 (Register), checksum
│  ├ Register flags (4 B)  B=0, N=0, + 30 reserved bits
│  │  ┌ IP (encapsulated original datagram)  src=DZ IP  dst=group  proto=17 (UDP)         ← RegisterSender builds
│  │  ├ UDP  sport=…, dport=5765
│  │  └ payload  44 5A 00 01   (the heartbeat)
```

Notes:
- The Register encapsulates the **complete original IP datagram** (its IP header plus everything above it), not just a payload and not "IP only". For the heartbeat that is IP + UDP + 4 bytes.
- The inner IP header is mandatory and must be correct: the device reads `src` (the source) and `dst` (the group) directly from it to create `(S,G)` state.
- The Register's own IP header is **unicast** to the RP, `proto 103`.
- Overhead is trivial because only the small heartbeat is ever encapsulated; there is no MTU concern. (Encapsulating the real stream would raise MTU questions; we explicitly do not.)

### Client: `RegisterSender`

A new component in `client/doublezerod/internal/pim` (for example `register.go`), wired through the existing dependency-injection pattern: a `RegisterWriter` interface in `services/base.go`, constructed in `runtime/run.go`, threaded through `CreateService` → `NewMulticastService`, mirroring `HeartbeatSender` and `PIMServer`.

- **Inputs:** tunnel interface, source IP (the DZ IP), published groups, RP address, cadence.
- **Behavior:** on each tick, for each published group, build a PIM Register encapsulating that group's heartbeat datagram and write it to the RP.
- **Cadence:** a dedicated, configurable interval, **default 60s**, decoupled from the 10s heartbeat. The Register only needs to keep the RP's origination state alive, and the RP's register keepalive is ~210s, so 60s leaves roughly 3.5 beacons of margin. This is deliberately much slower than the heartbeat to limit control-plane load on the device (see Impact). The per-publisher phase is staggered/jittered so beacons across many publishers do not synchronize into bursts. The cadence is a recovery-time-vs-device-load knob: a slower interval means a longer worst-case re-bootstrap after a flap (bounded by one interval).
- **Beacon semantics:** no inbound socket; ignore Register-Stop; no suppression timer; no null-Register probes.
- **Lifecycle:** start in the `isPublisher` branch of `Setup` (next to `heartbeat.Start`), stop in `Teardown` (next to `heartbeat.Close`), and add/remove groups in the publisher path of the service's update/reconcile.

### Client: PIM Register message

Extend `client/doublezerod/internal/pim/pim.go`:

- Add a serializable `RegisterMessage` for type `0x01` (the constant already exists; today there is no struct, encoder, or encapsulation).
- Layout: the existing 4-byte PIM common header, then 4 bytes of Register flags (Border bit `B=0`, Null-Register bit `N=0`, remaining bits reserved/zero), then the encapsulated original IP datagram as raw bytes.
- Reuse the existing `Checksum`, `PIMMessage.SerializeTo`, and the gopacket `SerializeBuffer` prepend/append helpers.
- Factor the heartbeat-datagram construction (inner IP + UDP + payload) into a shared helper used by both `HeartbeatSender` (sends it natively to the group) and `RegisterSender` (encapsulates it), so the two cannot drift.

### Client: egress without a host route

The Register is unicast to `10.0.0.0`. We must not install a route for `10.0.0.0` in the validator's routing table — it is RFC1918 space that may collide with the validator's own networks, and it pollutes their table.

- Open a raw `ip4:103` socket in the publisher branch (today only the subscriber opens one), separate from the subscriber PIM socket.
- Build the outer Register IP header with `dst = RP`, a normal TTL, and pin egress to the GRE tunnel via `ControlMessage.IfIndex` (the `RawConner` interface already exposes `SetControlMessage`), or equivalently `SO_BINDTODEVICE`.
- The GRE tunnel is point-to-point, so the kernel transmits to any destination out the interface with no route lookup. The packet egresses the tunnel; the kernel GRE-encapsulates it; the device terminates the tunnel and owns `10.0.0.0` locally, so it receives and processes the Register. Nothing is added to the host routing table.
- **Outer source address:** use the tunnel's local overlay address (the address the device already knows as the PIM neighbor on that tunnel) rather than the DZ IP, so the Register looks like it came from the expected on-link DR. To be confirmed in lab that EOS accepts it.

### Client: RP configuration

Promote the RP from the hardcoded `pim.RpAddress = 10.0.0.0` constant to a provisioned value: add `MulticastRpAddress` (default `10.0.0.0`) to the daemon's `ProvisionRequest`. This avoids constant drift and matches the device, which also references the RP. The constant remains the default.

### Device: controller template

In `controlplane/controller/internal/controller/templates/tunnel.tmpl`:

- Add to `SEC-USER-PUB-MCAST-IN`, before the final `deny ip any any`:
  ```
  permit pim any host 10.0.0.0
  ```
  This is the only thing standing between the client's Register and the RP today. It is additive and harmless to existing clients. (Optionally tighten `any` to the tunnel's expected overlay source; see Security Considerations.)
- Leave the `pim ipv4 border-router` block unchanged — it stays as a backstop (see Backward Compatibility).
- Optionally promote the literal `10.0.0.0` to a template variable fed from `GlobalConfig`, matching the new client field.

No `doublezero-agent` change: the agent only pushes rendered config, and receiving/processing a Register is native EOS PIM-SM behavior. No `models.go`/`server.go` change is required for the ACL edit (role is already inferred from the publisher/subscriber list lengths); a controller change is needed only if the RP is plumbed through provisioning.

### End-to-end data flow (steady state)

1. The user's application sends the real stream; the client routes it natively out the GRE (unchanged). The client emits the heartbeat natively to the group (unchanged).
2. The client additionally sends a Register per published group on the beacon interval (default 60s, staggered), encapsulating the heartbeat, unicast to the RP, pinned out the tunnel.
3. The device receives the Register (ACL now permits it) and, as anycast RP, sets the registered/`N` state for the `(S,G)` and originates the MSDP SA.
4. MSDP floods the SA across mesh-group `DZ-1`. Remote RPs with subscribers send `(S,G)` joins toward the source; SPTs build back to the source's device.
5. The native heartbeat and the real stream flow down those SPTs network-wide. The heartbeat keeps the data-plane keepalive alive at every hop; the Register beacon keeps the RP's origination (`N`/SA) alive so the SA keeps being re-originated.
6. The device discards the register-encapsulated copy (it has the native SPT copy), so there is no duplicate delivery.

### Testing

- **Unit:** `RegisterSender` produces a byte-correct PIM Register (common header, flags, encapsulated heartbeat datagram) and writes it with `dst = RP` and the tunnel `IfIndex`. Mirror `pim/server_test.go`.
- **Lab (decisive):**
  - On a dual-role tunnel, confirm the device sets `N`, originates the SA, and a previously broken remote site receives the stream.
  - Confirm the SA keeps re-originating while the beacon runs and ages out cleanly when it stops.
  - Confirm EOS tolerates a source that registers indefinitely (ignored Register-Stop) without adverse rate-limiting, log spam, or CPU impact.
  - Confirm no duplicate delivery (the RP drops the encapsulated copy).
  - Confirm EOS accepts the chosen outer source address.
- **Regression:** publisher-only tunnels still originate (border-router and Register both yield the same SA; idempotent).

## Impact

- **Codebase:** client — new `RegisterSender` and Register message type in `internal/pim`, a `RegisterWriter` interface and wiring in `services`/`runtime`, a publisher-branch raw socket, and a new optional `ProvisionRequest` field. Controller — a one-line ACL addition in `tunnel.tmpl` (plus optional RP template variable). No device-agent change.
- **Operational:** rollout is phased (see Backward Compatibility). No flag day. Monitoring can use the device `(S,G)` `N` flag and MSDP SA cache to verify origination.
- **Performance:** the only non-trivial cost is **control-plane Register processing on the RP**, since every publisher homed to a device converges its Registers (CPU-punted) on that one device, which also emits a Register-Stop for each. The rate is `publishers × groups-per-publisher ÷ cadence`. At the default 60s cadence (1 group/publisher) that is ~2.1 pps at 128 publishers, ~3.2 at 192, and ~8.3 at 500 — versus 12.8 / 19.2 / 50 pps if the cadence matched the 10s heartbeat. This is why the beacon is decoupled and slowed. The data plane is unchanged — the heartbeat and the real stream are untouched and hardware-forwarded; only the Register is punted.

**CoPP headroom (measured).** The devices run the standard EOS default `copp-system-policy` (confirmed identical on 7130LBR and 7280CR3A). There is no dedicated PIM CoPP class; a unicast Register to the RP loopback falls into a generic L3 punt class — `copp-system-l3slowpath`, `copp-system-ipunicast`, or worst case `copp-system-default` — each of which guarantees 1500 kbps and shapes to at least 25000 kbps. A Register is ~60-90 bytes, so at the 60s cadence even 500 publishers on one device is ~8.3 pps ≈ 7 kbps — under 0.5% of the guaranteed bandwidth of the most restrictive candidate class (which corresponds to a floor of ~1,800+ registers/sec). CoPP is therefore not a binding constraint at any realistic scale; the low, staggered cadence is cheap insurance against synchronized bursts in a shared class rather than a hard requirement.
- **User experience:** transparent. No CLI change and no change to how users connect (the RFC-15 `--publish`/`--subscribe` model is unchanged).

## Security Considerations

- **New inbound permit.** `permit pim any host 10.0.0.0` on `SEC-USER-PUB-MCAST-IN` allows PIM unicast from the tunnel to the RP. The trust boundary is the client tunnel, which is already semi-trusted and already permitted to send group data and PIM hellos. A client could attempt to register sources it is not authorized for, but the source address it can use is constrained (its allocated DZ IP, controlled via BGP/serviceability), and the outbound `multicast ipv4 boundary` ACL still limits which groups egress each tunnel. To reduce surface, the permit can be tightened from `any` to the tunnel's expected overlay source address.
- **Spoofed/abusive Registers.** Because we ignore Register-Stop, a client registers continuously by design; the RP must tolerate this. The blast radius of a misbehaving client is limited to groups it is already allowed to publish to (boundary ACL), so the Register does not grant new reach.
- **No new external surface.** The Register never leaves the DoubleZero tunnel mesh; it is not exposed to the public internet.

## Backward Compatibility

Belt-and-suspenders: `pim ipv4 border-router` remains on publisher tunnels permanently as a backstop. This makes every combination safe:

- **Old client + old device:** unchanged (today's behavior; dual-role still broken until updated).
- **New client + old device (no ACL permit):** the Register is dropped by the inbound ACL; the device falls back to `border-router`. No worse than today.
- **Old client + new device (ACL permit, border-router retained):** no Register is sent; `border-router` still originates for publisher-only. No regression.
- **New client + new device:** the Register originates the SA for all publishers, including dual-role; `border-router` is redundant-but-harmless for publisher-only (idempotent SA).

Phased rollout:
1. Ship the device template change (ACL permit). Additive, harmless.
2. Roll the client beacon out to publishers. Dual-role tunnels begin working as each publisher updates.
3. No removal of `border-router`; it stays as the backstop.

Relationship to prior RFCs: this builds directly on **RFC-15** (simultaneous pub/sub). The daemon provisioning API already carries `MulticastPubGroups`/`MulticastSubGroups`, and the smart contract already supports a user in both lists, so no contract change is required. A new optional `MulticastRpAddress` provisioning field is backward-compatible (defaults to `10.0.0.0`).

## Open Questions

- **Core behavior — resolved.** Confirmed end-to-end on cEOS: a heartbeat-fed Register makes the device set `N` and originate the SA on a dual-role tunnel (mroute flags `SNCP`), and it stays originated while the beacon runs. A two-device run shows the SA flooding to the second RP's `sa-cache`. See PR #3959.
- **Outer source address — resolved.** The Register uses the tunnel's local overlay address as the outer source and EOS accepts it (the `N` flag is set); the captured Register shows outer src = the tunnel overlay, dst = `10.0.0.0`.
- **ACL tightness — decided.** Shipped as `permit pim any host 10.0.0.0`. Constraining the source per tunnel remains a future hardening option (see Security Considerations).
- **EOS tolerance at scale — partially open.** Confirmed at devnet scale with no adverse rate-limiting or drops, and CoPP headroom is shown ample (see Impact). Not yet exercised at 128 / 192 / 500 publishers on a single device; the 60s cadence and per-publisher stagger are the levers if anything surprises.
- **RFC number** assigned: `22`.
