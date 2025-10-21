# uping

Minimal Linux-only raw ICMP echo library and toolset for user-space liveness probing over doublezero interfaces, even when certain routes are not in the kernel routing table.

## Components

- **Listener**: Responds to ICMP echo requests on a specific interface and IPv4 address, providing consistent user-space replies for local or unroutable peers.
- **Sender**: Sends ICMP echo requests and measures round-trip times per interface, operating reliably even without kernel routing. Handles retries, timeouts, and context cancellation.

## Example

```bash
uping-recv --iface doublezero0 --ip 9.169.90.100
uping-send --iface doublezero0 --src 9.169.90.100 --dst 9.169.90.110
```

## Notes

- IPv4 only
- Requires CAP_NET_RAW
- Socket egress/ingress is pinned to the selected interface
