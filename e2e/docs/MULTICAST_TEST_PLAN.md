# Multicast E2E Test Plan

This document provides a step-by-step test plan for validating multicast functionality in the local devnet environment.

## Known Limitations

**IMPORTANT:** cEOS does not support multicast data plane forwarding. This test plan validates the **control plane only**:
- PIM neighbor formation
- MSDP peering and SA (Source Active) propagation
- Multicast route (mroute) table population
- Tunnel interface creation and BGP peering
- Onchain state (multicast groups, subscriptions)

Actual multicast traffic will NOT flow between publisher and subscriber in this environment. See `CLAUDE.md` for detailed technical explanation.

---

## Test Environment

| Component | Details |
|-----------|---------|
| Devices | dz1 (xams), dz2 (xlax) |
| Clients | Client 1 (9.169.90.100), Client 2 (9.169.90.110) |
| Multicast Groups | mcast-a (233.84.178.0), mcast-b (233.84.178.1) |
| RP Address | 10.0.0.0 (Anycast on both devices) |
| Inter-device Link | Ethernet2 (dz1) <-> Ethernet2 (dz2) |

**Note on client pubkeys:** The `add-client` command generates a random keypair when `--keypair-path` is not provided. Throughout this document, `$CLIENT1` and `$CLIENT2` refer to the generated pubkeys. After adding clients, discover them with:
```bash
export CLIENT1=$(docker ps --format '{{.Names}}' | grep dz-local-client | sort | head -1 | sed 's/dz-local-client-//')
export CLIENT2=$(docker ps --format '{{.Names}}' | grep dz-local-client | sort | tail -1 | sed 's/dz-local-client-//')
```

---

## Phase 1: Environment Setup

### 1.1 Tear Down Existing Environment
```bash
dev/dzctl destroy -y
```

**Verify:** No `dz-local-*` containers running
```bash
docker ps | grep dz-local
# Should return empty
```

### 1.2 Build Container Images
```bash
dev/dzctl build
```

**Verify:** Build completes without errors

### 1.3 Start Core Services
```bash
dev/dzctl start -v
```

**Verify:** Core containers running
```bash
docker ps --format "table {{.Names}}\t{{.Status}}" | grep dz-local
# Should show: ledger, manager, funder, controller, activator, influxdb, prometheus, device-health-oracle
```

### 1.4 Add Devices and Clients

Run these commands sequentially (parallel execution causes Solana race conditions):

```bash
dev/dzctl add-device --code dz1 --exchange xams --location ams --cyoa-network-host-id 8 --additional-networks dz1:dz2
```
```bash
dev/dzctl add-device --code dz2 --exchange xlax --location lax --cyoa-network-host-id 16 --additional-networks dz1:dz2
```
```bash
dev/dzctl add-client --cyoa-network-host-id 100
```
```bash
dev/dzctl add-client --cyoa-network-host-id 110
```

**Verify:** Device containers healthy and client containers running
```bash
docker ps --format "table {{.Names}}\t{{.Status}}" | grep -E "device|client"
# Should show dz-local-device-dz1 and dz-local-device-dz2 as "healthy"
# Should show both client containers
```

---

## Phase 2: Onchain Setup (Links and Access)

### 2.1 Set Access Passes
```bash
docker exec dz-local-manager doublezero access-pass set --accesspass-type prepaid --client-ip 9.169.90.100 --user-payer $CLIENT1

docker exec dz-local-manager doublezero access-pass set --accesspass-type prepaid --client-ip 9.169.90.110 --user-payer $CLIENT2
```

**Verify:** Each command returns a signature

### 2.2 Create WAN Link
```bash
docker exec dz-local-manager doublezero link create wan --code dz1:dz2 --contributor co01 --side-a dz1 --side-a-interface Ethernet2 --side-z dz2 --side-z-interface Ethernet2 --bandwidth 10Gbps --mtu 2048 --delay-ms 40 --jitter-ms 3
```

**Verify:** Returns signature

**Note:** The link will be in "provisioning" status initially. In production, the device-health-oracle activates links after health checks pass. In the local devnet, we must manually activate the link (see Phase 3.1).

### 2.3 Create Multicast Groups
```bash
docker exec dz-local-manager doublezero multicast group create --code mcast-a --max-bandwidth 1Gbps --owner me

docker exec dz-local-manager doublezero multicast group create --code mcast-b --max-bandwidth 1Gbps --owner me
```

**Verify:** Each returns signature

```bash
docker exec dz-local-manager doublezero multicast group list
```

**Expected:** Shows mcast-a and mcast-b with status "activated"

### 2.4 Add Clients to Allowlists

**Client 1 (subscriber):**
```bash
docker exec dz-local-manager doublezero multicast group allowlist subscriber add --code mcast-a --client-ip 9.169.90.100 --user-payer $CLIENT1

docker exec dz-local-manager doublezero multicast group allowlist subscriber add --code mcast-b --client-ip 9.169.90.100 --user-payer $CLIENT1
```

**Client 2 (publisher):**
```bash
docker exec dz-local-manager doublezero multicast group allowlist publisher add --code mcast-a --client-ip 9.169.90.110 --user-payer $CLIENT2

docker exec dz-local-manager doublezero multicast group allowlist publisher add --code mcast-b --client-ip 9.169.90.110 --user-payer $CLIENT2
```

---

## Phase 3: Device Underlay Verification

### 3.1 Activate the WAN Link

In the local devnet, the device-health-oracle does not automatically activate links. Manually activate the link to trigger ISIS configuration:

```bash
docker exec dz-local-manager doublezero link update --pubkey dz1:dz2 --status activated
```

**Verify:** Returns a signature

Wait 10-15 seconds for the controller to push ISIS configuration and adjacency to form.

### 3.2 Check ISIS Adjacency
```bash
docker exec dz-local-device-dz1 Cli -c "show isis neighbors"
```

**Expected:** L1/L2 adjacency with dz2 on Ethernet2

### 3.3 Check iBGP Peering
```bash
docker exec dz-local-device-dz1 Cli -c "show ip bgp summary"
```

**Expected:** iBGP session with 172.16.0.4 (dz2) in "Estab" state

### 3.4 Check MSDP Peering
```bash
docker exec dz-local-device-dz1 Cli -c "show ip msdp peer"
```

**Expected:** MSDP peer 172.16.0.4 in "Up" state (may take 30+ seconds after ISIS forms)

### 3.5 Check Loopback Connectivity
```bash
docker exec dz-local-device-dz1 Cli -p 15 -c "ping 172.16.0.3 repeat 2"
```

**Expected:** 2 packets transmitted, 2 received, 0% loss

---

## Phase 4: Client Connections

### 4.1 Connect Subscriber (Client 1 to dz1)
```bash
docker exec dz-local-client-$CLIENT1 doublezero connect multicast subscriber mcast-a mcast-b --device dz1 --verbose
```

**Verify:** Command completes without error

### 4.2 Connect Publisher (Client 2 to dz2)

Connect the publisher to both multicast groups at once:
```bash
docker exec dz-local-client-$CLIENT2 doublezero connect multicast publisher mcast-a mcast-b --device dz2 --verbose
```

**Verify:** Command completes without error

### 4.3 Wait for Tunnel Establishment
Wait 15-30 seconds for tunnels to come up.

---

## Phase 5: Control Plane Verification

### 5.1 Check Tunnel Interfaces on Devices

**dz1 (subscriber side):**
```bash
docker exec dz-local-device-dz1 Cli -c "show interfaces Tunnel500"
```

**Expected:**
- Tunnel500 is up, line protocol is up
- IP address 169.254.0.0/31
- Tunnel destination is client IP (9.169.90.100)

**dz2 (publisher side):**
```bash
docker exec dz-local-device-dz2 Cli -c "show interfaces Tunnel500"
```

**Expected:**
- Tunnel500 is up, line protocol is up
- IP address 169.254.0.2/31
- Tunnel destination is client IP (9.169.90.110)

### 5.2 Check BGP Peering on User Tunnels

**dz1:**
```bash
docker exec dz-local-device-dz1 Cli -c "show ip bgp summary"
```

**Expected:** BGP neighbor 169.254.0.1 (USER-500) in Estab state

**dz2:**
```bash
docker exec dz-local-device-dz2 Cli -c "show ip bgp summary"
```

**Expected:** BGP neighbor 169.254.0.3 (USER-500) in Estab state

### 5.3 Check PIM Neighbors

**dz1:**
```bash
docker exec dz-local-device-dz1 Cli -c "show ip pim neighbor"
```

**Expected:**
- PIM neighbor on Ethernet2 (inter-device)
- PIM neighbor on Tunnel500 (to subscriber) - may take time to form

**dz2:**
```bash
docker exec dz-local-device-dz2 Cli -c "show ip pim neighbor"
```

**Expected:**
- PIM neighbor on Ethernet2 (inter-device)
- PIM neighbor on Tunnel500 (to publisher) - may take time to form

### 5.4 Check Multicast Routes (mroutes)

**dz1 (should have mroutes for subscriber's groups):**
```bash
docker exec dz-local-device-dz1 Cli -c "show ip mroute"
```

**Expected:**
```
233.84.178.0
  0.0.0.0, RP 10.0.0.0, flags: W
    Incoming interface: Register
    Outgoing interface list:
      Tunnel500

233.84.178.1
  0.0.0.0, RP 10.0.0.0, flags: W
    Incoming interface: Register
    Outgoing interface list:
      Tunnel500
```

### 5.5 Check PIM Interface Configuration

```bash
docker exec dz-local-device-dz1 Cli -c "show ip pim interface"
docker exec dz-local-device-dz2 Cli -c "show ip pim interface"
```

**Expected:** Tunnel500 shows as PIM sparse-mode interface with border-router flag

### 5.6 Check Multicast Boundary ACLs

```bash
docker exec dz-local-device-dz1 Cli -p 15 -c "show running-config | section SEC-USER-MCAST-BOUNDARY"
```

**Expected:** ACL permits the multicast group addresses (233.84.178.0, 233.84.178.1)

---

## Phase 6: Client-Side Verification

### 6.1 Check Client Tunnel Interface

**Subscriber:**
```bash
docker exec dz-local-client-$CLIENT1 ip link show doublezero1
```

**Expected:** Interface exists, UP, LOWER_UP

**Publisher:**
```bash
docker exec dz-local-client-$CLIENT2 ip link show doublezero1
```

**Expected:** Interface exists, UP, LOWER_UP

### 6.2 Check Client Status

**Subscriber:**
```bash
docker exec dz-local-client-$CLIENT1 doublezero status
```

**Expected:** Shows connected status with tunnel info

**Publisher:**
```bash
docker exec dz-local-client-$CLIENT2 doublezero status
```

**Expected:** Shows connected status with tunnel info

### 6.3 Check Multicast Routes on Subscriber

```bash
docker exec dz-local-client-$CLIENT1 ip route show
```

**Expected:** Routes for 233.84.178.0 and 233.84.178.1 via doublezero1

---

## Phase 7: Onchain State Verification

### 7.1 Check User List
```bash
docker exec dz-local-manager doublezero user list
```

**Expected:** Shows both users with their multicast subscriptions

### 7.2 Check Multicast Group Details
```bash
docker exec dz-local-manager doublezero multicast group list
```

**Expected:**
- mcast-a: 1 publisher, 1 subscriber
- mcast-b: 1 publisher, 1 subscriber

### 7.3 Check Device Status
```bash
docker exec dz-local-manager doublezero device list
```

**Expected:** Both dz1 and dz2 shown with status

---

## Phase 8: Agent Configuration Verification

### 8.1 Check Agent Config on Device
```bash
docker exec dz-local-device-dz1 Cli -p 15 -c "show running-config section Tunnel500"
```

**Expected:** Full tunnel configuration including:
- IP address
- PIM sparse-mode
- Multicast boundary ACL
- Tunnel source/destination

### 8.2 Check Agent Logs (if issues)
```bash
docker exec dz-local-device-dz1 tail -50 /var/log/agents-latest/doublezero-agent
```

---

## Phase 9: Data Plane Test (Known to Fail)

This test documents the expected failure due to cEOS limitations.

### 9.1 Start Capture on Subscriber
```bash
docker exec dz-local-client-$CLIENT1 timeout 10 tcpdump -i doublezero1 host 233.84.178.0 -c 5
```

### 9.2 Send Multicast from Publisher
In another terminal:
```bash
docker exec dz-local-client-$CLIENT2 bash -c 'for i in 1 2 3 4 5; do echo "test$i" > /dev/udp/233.84.178.0/5000; done'
```

### 9.3 Verify Traffic Reaches Device
```bash
docker exec dz-local-device-dz2 timeout 5 tcpdump -i tu500 host 233.84.178.0 -c 5
```

**Expected:** Packets arrive at dz2's tu500 interface

### 9.4 Expected Result
- Subscriber tcpdump shows 0 packets (DATA PLANE DOES NOT WORK)
- This is the known cEOS limitation documented in CLAUDE.md

---

## Phase 10: Disconnect and Cleanup Verification

### 10.1 Disconnect Subscriber
```bash
docker exec dz-local-client-$CLIENT1 doublezero disconnect
```

### 10.2 Verify Tunnel Removed from Device
Wait 15-30 seconds, then:
```bash
docker exec dz-local-device-dz1 Cli -c "show interfaces Tunnel500"
```

**Expected:** Interface does not exist

### 10.3 Verify Client Tunnel Removed
```bash
docker exec dz-local-client-$CLIENT1 ip link show doublezero1
```

**Expected:** Device "doublezero1" does not exist

### 10.4 Verify Mroutes Cleaned Up
```bash
docker exec dz-local-device-dz1 Cli -c "show ip mroute"
```

**Expected:** No mroutes for 233.84.178.x with Tunnel500 in OIF list

---

## Quick Reference: Full Test Script

```bash
#!/bin/bash
set -e

echo "=== Phase 1: Environment Setup ==="
dev/dzctl destroy -y
dev/dzctl build
dev/dzctl start -v
dev/dzctl add-device --code dz1 --exchange xams --location ams --cyoa-network-host-id 8 --additional-networks dz1:dz2
dev/dzctl add-device --code dz2 --exchange xlax --location lax --cyoa-network-host-id 16 --additional-networks dz1:dz2
dev/dzctl add-client --cyoa-network-host-id 100
dev/dzctl add-client --cyoa-network-host-id 110

# Discover client pubkeys from container names
CLIENT1=$(docker ps --format '{{.Names}}' | grep dz-local-client | sort | head -1 | sed 's/dz-local-client-//')
CLIENT2=$(docker ps --format '{{.Names}}' | grep dz-local-client | sort | tail -1 | sed 's/dz-local-client-//')
echo "Client 1: $CLIENT1"
echo "Client 2: $CLIENT2"

echo "=== Phase 2: Onchain Setup ==="
docker exec dz-local-manager doublezero access-pass set --accesspass-type prepaid --client-ip 9.169.90.100 --user-payer $CLIENT1
docker exec dz-local-manager doublezero access-pass set --accesspass-type prepaid --client-ip 9.169.90.110 --user-payer $CLIENT2

docker exec dz-local-manager doublezero link create wan --code dz1:dz2 --contributor co01 --side-a dz1 --side-a-interface Ethernet2 --side-z dz2 --side-z-interface Ethernet2 --bandwidth 10Gbps --mtu 2048 --delay-ms 40 --jitter-ms 3

docker exec dz-local-manager doublezero multicast group create --code mcast-a --max-bandwidth 1Gbps --owner me
docker exec dz-local-manager doublezero multicast group create --code mcast-b --max-bandwidth 1Gbps --owner me

docker exec dz-local-manager doublezero multicast group allowlist subscriber add --code mcast-a --client-ip 9.169.90.100 --user-payer $CLIENT1
docker exec dz-local-manager doublezero multicast group allowlist subscriber add --code mcast-b --client-ip 9.169.90.100 --user-payer $CLIENT1
docker exec dz-local-manager doublezero multicast group allowlist publisher add --code mcast-a --client-ip 9.169.90.110 --user-payer $CLIENT2
docker exec dz-local-manager doublezero multicast group allowlist publisher add --code mcast-b --client-ip 9.169.90.110 --user-payer $CLIENT2

echo "=== Phase 3: Activate Link and Verify Underlay ==="
# Activate link to trigger ISIS configuration (in production, device-health-oracle does this)
docker exec dz-local-manager doublezero link update --pubkey dz1:dz2 --status activated
echo "Waiting for ISIS adjacency..."
sleep 15
echo "--- ISIS ---"
docker exec dz-local-device-dz1 Cli -c "show isis neighbors"
echo "Waiting for MSDP..."
sleep 20
echo "--- MSDP ---"
docker exec dz-local-device-dz1 Cli -c "show ip msdp peer"

echo "=== Phase 4: Connect Clients ==="
docker exec dz-local-client-$CLIENT1 doublezero connect multicast subscriber mcast-a mcast-b --device dz1 --verbose
docker exec dz-local-client-$CLIENT2 doublezero connect multicast publisher mcast-a mcast-b --device dz2 --verbose

echo "=== Waiting for tunnels ==="
sleep 30

echo "=== Phase 5: Control Plane Verification ==="
echo "--- Tunnels ---"
docker exec dz-local-device-dz1 Cli -c "show interfaces Tunnel500 | include line protocol"
docker exec dz-local-device-dz2 Cli -c "show interfaces Tunnel500 | include line protocol"
echo "--- PIM Neighbors ---"
docker exec dz-local-device-dz1 Cli -c "show ip pim neighbor"
echo "--- Mroutes ---"
docker exec dz-local-device-dz1 Cli -c "show ip mroute"
echo "--- Multicast Groups ---"
docker exec dz-local-manager doublezero multicast group list

echo "=== Test Complete ==="
```

---

## Troubleshooting

### Restarting Parts of the Devnet

You don't need to `dev/dzctl destroy` and rebuild everything when only specific containers need restarting. Remove just those containers, rebuild, and re-add them:

```bash
# Example: restart a device
docker rm -f dz-local-device-dz1
dev/dzctl build
dev/dzctl add-device --code dz1 --exchange xams --location ams --cyoa-network-host-id 8 --additional-networks dz1:dz2

# Example: restart a client
docker rm -f dz-local-client-$CLIENT1
dev/dzctl build
dev/dzctl add-client --cyoa-network-host-id 100
```

Core services (manager, controller, etc.) can be restarted with `docker restart dz-local-controller`. Only use `dev/dzctl destroy -y` when you need a completely clean slate (e.g., ledger state is corrupted).

### Tunnel Not Coming Up
1. Check agent logs: `docker exec dz-local-device-dz1 tail -30 /var/log/agents-latest/doublezero-agent`
2. Check disk space: `docker exec dz-local-device-dz1 df -h /var/tmp`
3. Restart container: `docker restart dz-local-device-dz1`

### ISIS Not Forming
1. Verify the link is activated: `docker exec dz-local-manager doublezero link list` should show status "activated"
2. If link is "provisioning", activate it: `docker exec dz-local-manager doublezero link update --pubkey dz1:dz2 --status activated`
3. Verify ISIS is enabled: `show running-config section Ethernet2` should show `isis enable 1`
4. Check ISIS process: `show isis summary`
5. Wait 10-15 seconds after link activation for adjacency to form

### MSDP Not Peering
1. Check loopback reachability: `ping 172.16.0.3`
2. Check BGP (MSDP depends on iBGP for next-hop): `show ip bgp summary`

### No Mroutes
1. Check PIM is enabled on tunnel: `show ip pim interface Tunnel500`
2. Check RP config: `show ip pim rp`
3. Check multicast boundary ACL: `show ip access-list SEC-USER-MCAST-BOUNDARY-*`
