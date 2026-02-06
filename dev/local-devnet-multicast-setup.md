# Local Devnet Setup with Multicast (BGP/PIM)

## 1. Build and Start Devnet

```bash
dev/dzctl destroy -y
dev/dzctl build
dev/dzctl start
```

## 2. Add Devices

### Add first device (Amsterdam)
```bash
dev/dzctl add-device --code dz1 --exchange xams --location ams --cyoa-network-host-id 8 --additional-networks dz1:dz2
```

### Create location and exchange for second device (London)
```bash
docker exec dz-local-manager doublezero location create --code lon --name "London" --country UK --lat 51.5 --lng -0.1
docker exec dz-local-manager doublezero exchange create --code xlon --name "London IX" --lat 51.5 --lng -0.1
```

### Add second device (London)
```bash
dev/dzctl add-device --code dz2 --exchange xlon --location lon --cyoa-network-host-id 16 --additional-networks dz1:dz2
```

## 3. Create Multicast Groups

```bash
docker exec dz-local-manager bash -c '
doublezero multicast group create --code mg01 --max-bandwidth 10Gbps --owner me -w
doublezero multicast group create --code mg02 --max-bandwidth 10Gbps --owner me -w
doublezero multicast group list
'
```

## 4. Add Clients

```bash
dev/dzctl add-client --cyoa-network-host-id 100
dev/dzctl add-client --cyoa-network-host-id 101
```

Note the pubkeys from the output (example values shown below - yours will differ):
- Client 100: `91bJ2d3o1AB7vYzMrfRjJB54AVAygWGWwxjXNKumxUQX`
- Client 101: `4tUtj2iUKaxXVc4sehi944NF31HdDNgzGF5gymBqFgHu`

## 5. Configure Access Passes and Allowlists

Replace the pubkeys below with the actual values from step 4:

```bash
docker exec dz-local-manager bash -c '
# Set access passes
doublezero access-pass set --accesspass-type prepaid --client-ip 9.169.90.100 --user-payer <CLIENT_100_PUBKEY>
doublezero access-pass set --accesspass-type prepaid --client-ip 9.169.90.101 --user-payer <CLIENT_101_PUBKEY>

# Add to allowlists
doublezero multicast group allowlist publisher add --code mg01 --user-payer <CLIENT_100_PUBKEY> --client-ip 9.169.90.100
doublezero multicast group allowlist subscriber add --code mg02 --user-payer <CLIENT_101_PUBKEY> --client-ip 9.169.90.101
'
```

## 6. Connect Clients

### Connect publisher to mg01
```bash
docker exec dz-local-client-<CLIENT_100_PUBKEY> doublezero connect multicast publisher mg01 --client-ip 9.169.90.100
```

### Connect subscriber to mg02
```bash
docker exec dz-local-client-<CLIENT_101_PUBKEY> doublezero connect multicast subscriber mg02 --client-ip 9.169.90.101
```

## 7. Verify Sessions

### Check client status
```bash
# Publisher
docker exec dz-local-client-<CLIENT_100_PUBKEY> doublezero status

# Subscriber
docker exec dz-local-client-<CLIENT_101_PUBKEY> doublezero status
```

Expected output shows `BGP Session Up` for both.

### Check BGP on device
```bash
docker exec dz-local-device-dz1 Cli -c "show ip bgp summary"
```

Expected: USER-500 and USER-501 in `Estab` state.

### Check PIM neighbors
```bash
docker exec dz-local-device-dz1 Cli -c "show ip pim neighbor"
```

Expected: PIM neighbor on Tunnel501 (subscriber).

### Check multicast routes
```bash
docker exec dz-local-device-dz1 Cli -c "show ip mroute"
```

Expected: Multicast group (e.g., 233.84.178.1) with outgoing interface to subscriber tunnel.

## Summary

| Component | Details |
|-----------|---------|
| Device dz1 | Amsterdam, 9.169.90.8 |
| Device dz2 | London, 9.169.90.16 |
| Publisher | 9.169.90.100, mg01, BGP Up ✅ |
| Subscriber | 9.169.90.101, mg02, BGP Up ✅, PIM Up ✅ |

## Troubleshooting

### BGP not establishing
Check agent logs on device:
```bash
docker exec dz-local-device-dz1 tail -30 /var/log/agents-latest/doublezero-agent
```

### Restart doublezero-agent on device
```bash
docker exec dz-local-device-dz1 bash -c 'Cli -p 15 << EOF
configure
daemon doublezero-agent
shutdown
no shutdown
end
EOF
'
```

### Check tunnel interface on device
```bash
docker exec dz-local-device-dz1 Cli -c "show interfaces Tunnel500"
docker exec dz-local-device-dz1 Cli -c "show interfaces Tunnel501"
```
