# uping

Minimal Linux-only raw ICMP echo library and toolset for user-space liveness probing over doublezero interfaces, even when certain routes are not in the kernel routing table.

## Components

- **Receiver**: Listens on a specific interface and replies to ICMP echo requests with manually constructed IPv4 headers (`IP_HDRINCL`), ensuring replies always originate from the specified interface and address, bypassing kernel ICMP handling.
- **Sender**: Sends ICMP echo requests and measures round-trip times via raw sockets (`SO_BINDTODEVICE`, `IP_PKTINFO`), functioning even when no route to the target exists in the system routing table.

## Example

```bash
uping-recv --iface doublezero0 --ip 9.169.90.100
uping-send --iface doublezero0 --src 9.169.90.100 --dst 9.169.90.110
```

## Notes

- IPv4 only
- Requires `CAP_NET_RAW`
