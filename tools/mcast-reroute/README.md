# mcast-reroute

Attaches an eBPF (`BPF_CGROUP_UDP4_SENDMSG`) program to the root cgroup that rewrites the source IP of outgoing UDP multicast packets. This causes the kernel to re-evaluate the routing decision, sending traffic over an interface determined by the new source address (e.g. a GRE tunnel) instead of the interface that owns the original bound address.

The eBPF program is automatically detached on shutdown (SIGINT/SIGTERM) or if the process crashes (fd-based BPF link).

## Usage

```bash
go build -o mcast-reroute ./tools/mcast-reroute

# Rewrite source 137.174.145.145 → 147.51.126.1 for multicast to 233.84.178.0:7733
./mcast-reroute \
  -src 137.174.145.145 \
  -rewrite-src 147.51.126.1 \
  -dst 233.84.178.0:7733

# Multiple destinations
./mcast-reroute \
  -src 137.174.145.145 \
  -rewrite-src 147.51.126.1 \
  -dst 233.84.178.0:7733 \
  -dst 233.84.178.1:7733
```

## Flags

| Flag | Required | Description |
|------|----------|-------------|
| `-src` | yes | Source IP to match on outgoing packets |
| `-rewrite-src` | yes | New source IP to substitute |
| `-dst` | yes | Multicast destination `group:port` to match (repeatable) |

## Requirements

- Linux kernel with BPF support
- `CAP_BPF` and `CAP_NET_ADMIN` capabilities (or root)
- `CAP_SYS_RESOURCE` on kernels < 5.11 (for memlock rlimit)

When running as a non-root service, grant capabilities via systemd or setcap:

```bash
# systemd
AmbientCapabilities=CAP_BPF CAP_NET_ADMIN

# setcap
sudo setcap cap_bpf,cap_net_admin=ep ./mcast-reroute
```

## How it works

When a process binds a UDP socket to a specific source IP (e.g. `137.174.145.145` on `eth0`) and sends to a multicast group, the kernel routes the packet out the interface that owns that IP — ignoring any multicast route pointing elsewhere (e.g. a GRE tunnel).

This tool intercepts `sendmsg()` via eBPF before the routing decision and rewrites the source IP to one that belongs to the desired egress interface. The kernel then naturally routes the packet over that interface. Checksums are recalculated by the kernel after the rewrite.

Only packets matching all three criteria are rewritten: source IP, multicast destination range (224.0.0.0/4), and specific destination IP/port. All other traffic passes through unchanged.
