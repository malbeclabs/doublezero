# cEOS Multicast Data Plane Limitation

## Executive Summary

**The Arista cEOS (containerized EOS) platform does not support multicast data plane forwarding.** While the multicast control plane (PIM, MSDP, mroute tables) functions correctly, actual multicast traffic will not be forwarded between interfaces. This is a fundamental architectural limitation of cEOS that cannot be worked around through configuration or scripting.

**Impact:** The local devnet environment can only validate multicast control plane functionality. Data plane testing requires real hardware or potentially vEOS (VM-based EOS).

---

## Background

### Environment
- Arista cEOS-lab containers running EOS 4.33.1F
- GRE tunnels between devices and clients for user traffic
- PIM Sparse Mode with Anycast RP (10.0.0.0) on both devices
- MSDP for RP state synchronization

### What Works
- PIM neighbor formation on all interfaces including user tunnels
- MSDP peering between devices
- Multicast route (mroute) table population with correct OIF lists
- ISIS/iBGP underlay routing
- Tunnel interface creation and BGP peering

### What Doesn't Work
- Actual multicast packet forwarding from ingress to egress interfaces
- Traffic sent by publisher never reaches subscriber

---

## Technical Analysis

### The Problem

When a multicast publisher sends traffic:
1. Client sends UDP to multicast group (e.g., 233.84.178.0)
2. Traffic is encapsulated in GRE and sent to the device
3. Device receives GRE packet on eth1 (CYOA network interface)
4. **Traffic arrives at the device's tunnel interface (tu500)** - confirmed via tcpdump
5. **Traffic is NOT forwarded to the egress interface** - despite correct mroute entries

### Root Cause: Userspace Forwarding Agent

cEOS uses a **userspace forwarding agent** (Memory Forwarding Agent / Memory FIB) rather than the Linux kernel's native forwarding. The packet flow is:

```
┌─────────────────────────────────────────────────────────────────────┐
│                         cEOS Container                               │
│                                                                      │
│  ┌──────────┐    ┌─────────────────────┐    ┌──────────┐           │
│  │   eth1   │───▶│  EOS Userspace      │───▶│   tu500  │           │
│  │(GRE outer)│    │  Forwarding Agent   │    │(inner pkt)│           │
│  └──────────┘    │  (fwd0 interface)    │    └──────────┘           │
│                  └─────────────────────┘                            │
│                            │                                         │
│                            ▼                                         │
│                  Packet "injected" into                             │
│                  kernel network stack                                │
│                  (bypasses normal input path)                        │
│                            │                                         │
│                            ▼                                         │
│                  ┌─────────────────────┐                            │
│                  │  Kernel Multicast   │                            │
│                  │  Routing Lookup     │                            │
│                  │                     │                            │
│                  │  Result: iif = -1   │◀── Can't match to VIF      │
│                  │  (no input iface)   │                            │
│                  └─────────────────────┘                            │
│                            │                                         │
│                            ▼                                         │
│                     Packet dropped                                   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

The critical issue is that when the EOS userspace agent decapsulates the GRE tunnel and delivers the inner packet to the tu500 interface, it does so in a way that **bypasses the kernel's normal packet input path**. The kernel's multicast forwarding code cannot properly associate the packet with a Virtual Interface (VIF), resulting in `iif=-1` (no input interface) in the multicast forwarding cache.

### Evidence

#### 1. Traffic Arrives at Tunnel Interface
```bash
$ docker exec dz-local-device-dz2 tcpdump -i tu500 host 233.84.178.0
02:55:26.333211 tu500 In ifindex 30 ... 9.169.90.145 > 233.84.178.0: UDP
```
Packets are visible on tu500 with the correct interface index.

#### 2. Kernel Multicast Forwarding Shows iif=-1
```bash
$ docker exec dz-local-device-dz2 cat /proc/net/ip_mr_cache
Group    Origin   Iif     Pkts    Bytes    Wrong Oifs
00B254E9 00000000 0          0        0        0      # (*, G) entry - OK
00B254E9 915AA909 -1         0        0        0      # (S, G) entry - iif=-1!
```
The source-specific entry shows `Iif=-1`, meaning the kernel received the packet but couldn't determine which VIF it arrived on.

#### 3. Packets Go to INPUT, Not FORWARD
```bash
$ docker exec dz-local-device-dz2 iptables -L INPUT -n -v
Chain INPUT (policy DROP)
 pkts bytes target     prot opt in     out     source               destination
   33  2518 EOS_INPUT  all  --  *      *       0.0.0.0/0            0.0.0.0/0

$ docker exec dz-local-device-dz2 iptables -L FORWARD -n -v
Chain FORWARD (policy DROP)
 pkts bytes target     prot opt in     out     source               destination
    0     0 EOS_FORWARD  all  --  *      *       0.0.0.0/0            0.0.0.0/0
```
Multicast packets hit the INPUT chain (local delivery) not the FORWARD chain.

---

## Attempted Workarounds

### 1. `software-forwarding kernel` Configuration

**Attempt:** Configure EOS to use kernel-based multicast forwarding.

```
router multicast
   ipv4
      software-forwarding kernel
   ipv6
      software-forwarding kernel
```

**Result:** Configuration applies but has no effect. The `mc_forwarding` sysctl remains 0, indicating EOS doesn't open the kernel multicast routing socket.

```bash
$ cat /proc/sys/net/ipv4/conf/all/mc_forwarding
0
```

### 2. Manual Kernel Multicast Routing via Python

**Attempt:** Manually open the kernel multicast routing socket and configure VIFs/MFC entries.

```python
import socket, struct

# Open multicast routing socket (MRT_INIT)
s = socket.socket(socket.AF_INET, socket.SOCK_RAW, socket.IPPROTO_IGMP)
s.setsockopt(socket.IPPROTO_IP, 200, struct.pack("i", 1))  # MRT_INIT

# Add VIF for tu500 (ifindex 30)
vif = struct.pack("HBBI4s4s", 0, 8, 1, 0, struct.pack("I", 30), b"\x00"*4)
s.setsockopt(socket.IPPROTO_IP, 202, vif)  # MRT_ADD_VIF

# Add MFC entry for (*, 233.84.178.0)
mfc = struct.pack("4s4sHxx32sIIII", origin, group, 0, ttls, 0, 0, 0, 0)
s.setsockopt(socket.IPPROTO_IP, 204, mfc)  # MRT_ADD_MFC
```

**Result:** VIFs and MFC entries appear in `/proc/net/ip_mr_vif` and `/proc/net/ip_mr_cache`. `mc_forwarding` becomes 1. However, received packets still show `iif=-1` because they don't arrive through normal kernel input path.

```bash
$ cat /proc/net/ip_mr_vif
Interface      BytesIn  PktsIn  BytesOut PktsOut Flags Local    Remote
 0 tu500             0       0         0       0 00008 0000001E 00000000
 1 eth2              0       0         0       0 00008 0000000D 00000000
```
VIFs configured correctly, but BytesIn/PktsIn remain 0.

### 3. iptables Rule Modification

**Attempt:** Remove DROP rules in EOS_FORWARD chain that might block forwarding.

```bash
$ iptables -D EOS_FORWARD -i eth2 -j DROP
```

**Result:** No effect. Packets still go to INPUT chain, not FORWARD chain.

### 4. Enable ip_forward

**Attempt:** Ensure IP forwarding is enabled.

```bash
$ echo 1 > /proc/sys/net/ipv4/ip_forward
```

**Result:** No effect on multicast forwarding.

### 5. Add Multicast Routes

**Attempt:** Add explicit routes for multicast address range.

```bash
$ ip route add 224.0.0.0/4 dev eth2
```

**Result:** No effect. Multicast routing uses MFC, not the regular routing table.

### 6. Kernel GRE Tunnel

**Attempt:** Create a parallel kernel-native GRE tunnel to bypass EOS's userspace handling.

```bash
$ ip tunnel add kgre0 mode gre remote 9.169.90.110 local 9.169.90.16
```

**Result:** Failed - tunnel already exists (EOS's tu500 is a kernel tunnel, just managed by userspace).

---

## Why These Workarounds Failed

The fundamental issue is **how EOS delivers decapsulated packets to the kernel**:

1. **Normal kernel path:** Packet arrives on interface → netfilter PREROUTING → routing decision → FORWARD or INPUT
2. **EOS userspace path:** GRE packet arrives → userspace agent decapsulates → inner packet "injected" into kernel stack

When EOS injects the decapsulated packet, it appears on tu500 for tools like tcpdump and iptables, but the kernel's multicast routing code doesn't see it as "arriving on tu500" in the way needed for VIF matching.

The `iif=-1` in the MFC cache is the kernel's way of saying "I received this multicast packet but I don't know which VIF it came from." This happens because:
- The packet wasn't received through the normal `netif_receive_skb()` path for tu500
- The kernel's `ipmr_cache_find()` can't match the packet's input device to a registered VIF

---

## Alternatives Considered

### 1. vEOS (VM-based EOS)
- **Pros:** May have proper kernel dataplane integration
- **Cons:** Heavier weight, requires VM hypervisor, not confirmed to work

### 2. FRR (Free Range Routing)
- **Pros:** Uses native kernel routing, multicast forwarding works
- **Cons:** Different configuration format, would require significant devnet changes, doublezero-agent speaks EOS

### 3. Real Hardware
- **Pros:** Full functionality
- **Cons:** Cost, not available for local development

### 4. Accept Control-Plane-Only Testing
- **Pros:** No changes needed, can validate PIM/MSDP/mroute setup
- **Cons:** Can't verify actual traffic flow

---

## Recommendations

1. **For local development:** Use the devnet for control plane validation only. The test plan in `e2e/docs/MULTICAST_TEST_PLAN.md` covers what can be verified.

2. **For data plane testing:** Use staging/production environments with real hardware.

3. **Future investigation:** If multicast data plane testing in containers becomes critical, investigate:
   - vEOS compatibility
   - Hybrid approach with FRR for data plane testing only
   - Arista support channels for cEOS multicast forwarding roadmap

---

## References

- `e2e/docs/MULTICAST_TEST_PLAN.md` - Control plane test procedures
- `CLAUDE.md` - Devnet documentation and debugging commands
- `e2e/internal/devnet/device/startup-config.tmpl` - Device startup configuration template
- `controlplane/controller/internal/controller/templates/tunnel.tmpl` - Controller config template (includes ISIS)

---

## Appendix: Useful Debugging Commands

```bash
# Check kernel multicast state
docker exec dz-local-device-dz1 cat /proc/sys/net/ipv4/conf/all/mc_forwarding
docker exec dz-local-device-dz1 cat /proc/sys/net/ipv4/ip_forward
docker exec dz-local-device-dz1 cat /proc/net/ip_mr_vif
docker exec dz-local-device-dz1 cat /proc/net/ip_mr_cache

# Check EOS multicast state
docker exec dz-local-device-dz1 Cli -c "show ip mroute"
docker exec dz-local-device-dz1 Cli -c "show ip pim neighbor"
docker exec dz-local-device-dz1 Cli -c "show ip msdp peer"
docker exec dz-local-device-dz1 Cli -p 15 -c "show running-config section multicast"

# Trace packets
docker exec dz-local-device-dz1 tcpdump -i any -e host 233.84.178.0

# Check iptables flow
docker exec dz-local-device-dz1 iptables -t raw -L PREROUTING -n -v
docker exec dz-local-device-dz1 iptables -L FORWARD -n -v
docker exec dz-local-device-dz1 iptables -L INPUT -n -v

# Check interface details
docker exec dz-local-device-dz1 ip link show tu500
docker exec dz-local-device-dz1 cat /sys/class/net/tu500/ifindex
```
