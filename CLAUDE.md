# Claude Code Instructions

## Git Commits

- Do not add "Co-Authored-By" lines to commit messages
- Use the format `component: short description` (e.g., `lake/indexer: fix flaky staging test`, `telemetry: use CLICKHOUSE_PASS env var`)
- Keep the description lowercase (except proper nouns) and concise

## Local Devnet / E2E Environment

The local devnet runs in Docker containers with the naming convention `dz-local-*`.

### Container Types

- **Devices**: `dz-local-device-dz1`, `dz-local-device-dz2` - Arista cEOS containers
- **Clients**: `dz-local-client-{pubkey}` - Client containers running doublezerod
- **Manager**: `dz-local-manager` - Runs the doublezero CLI for admin operations
- **Controller**: `dz-local-controller` - Pushes configs to devices

### Arista Device Interaction

```bash
# Basic CLI command
docker exec dz-local-device-dz1 Cli -c "show ip bgp summary"

# Privileged mode (needed for show running-config, configure, etc.)
docker exec dz-local-device-dz1 Cli -p 15 -c "show running-config section Tunnel500"

# Multi-line config via heredoc
docker exec dz-local-device-dz1 bash -c 'Cli -p 15 << EOF
configure
daemon doublezero-agent
shutdown
no shutdown
end
EOF
'
```

### Useful Device Commands

- `show ip bgp summary` / `show ip bgp summary vrf vrf1` - BGP neighbor status
- `show interfaces Tunnel500` - Tunnel interface status
- `show ip pim neighbor` - PIM neighbors
- `show ip mroute` - Multicast routes
- `show vrf` - VRF info and interface assignments

### Agent Logs on Devices

- Logs are in `/var/log/agents/` with symlinks in `/var/log/agents-latest/`
- doublezero-agent log: `/var/log/agents-latest/doublezero-agent`
- Launcher log shows daemon start/stop events: `/var/log/agents/Launcher-*`

### Common Device Issues

1. **Config commits failing with "internal error"**: Check `/var/tmp` disk space - core dumps can fill it up
   ```bash
   docker exec dz-local-device-dz1 df -h /var/tmp
   docker exec dz-local-device-dz1 bash -c 'rm -f /var/tmp/agents/core.*'
   ```

2. **Tunnel config in running-config but interface doesn't exist**: The Tunnel agent may not have created the kernel interface. Restart the container:
   ```bash
   docker restart dz-local-device-dz2
   ```

3. **doublezero-agent not applying configs**: Check if agent is running and logs for errors
   ```bash
   docker exec dz-local-device-dz1 ps aux | grep doublezero-agent
   docker exec dz-local-device-dz1 tail -30 /var/log/agents-latest/doublezero-agent
   ```

### Client Interaction

```bash
# Check client tunnel status
docker exec dz-local-client-{pubkey} doublezero status

# Check routes on client
docker exec dz-local-client-{pubkey} ip route show

# Check tunnel interface
docker exec dz-local-client-{pubkey} ip addr show doublezero1
```

### Manager Commands

```bash
# List users and their multicast group subscriptions
docker exec dz-local-manager doublezero user list

# List devices
docker exec dz-local-manager doublezero device list

# List multicast groups
docker exec dz-local-manager doublezero multicast group list
```

### cEOS Interface Mapping

cEOS maps Arista interface names to Linux kernel interfaces:
- `Ethernet1` → `eth1` (CYOA network - client tunnels)
- `Ethernet2` → `eth2` (inter-device WAN link)
- `Management0` → `eth0` (management network)
- `Tunnel500` → `tu500` (user GRE tunnels)

To find interface indices:
```bash
docker exec dz-local-device-dz1 cat /sys/class/net/tu500/ifindex
docker exec dz-local-device-dz1 cat /sys/class/net/eth2/ifindex
```

### Restarting Parts of the Devnet

You don't always need to `dev/dzctl destroy` and rebuild everything. If only specific containers need restarting:

- **Devices or clients**: Remove just those containers, rebuild, and re-add them:
  ```bash
  docker rm -f dz-local-device-dz1
  dev/dzctl build
  dev/dzctl add-device --code dz1 --exchange xams --location ams --cyoa-network-host-id 8 --additional-networks dz1:dz2
  ```
- **Clients**:
  ```bash
  docker rm -f dz-local-client-FposHWrkvPP3VErBAWCd4ELWGuh2mgx2Wx6cuNEA4X2S
  dev/dzctl build
  dev/dzctl add-client --cyoa-network-host-id 100 --keypair-path dev/.deploy/dz-local/client-FposHWrkvPP3VErBAWCd4ELWGuh2mgx2Wx6cuNEA4X2S/keypair.json
  ```
- **Core services** (manager, controller, etc.): These are lighter and can be restarted with `docker restart dz-local-controller`.

Only use `dev/dzctl destroy -y` when you need a completely clean slate (e.g., ledger state is corrupted or you want to reset onchain state).

### Running E2E Tests

**Important:** E2E tests are resource-intensive (each test spins up multiple Docker containers including cEOS devices). Always run specific tests rather than the full suite, as running all tests concurrently will exhaust memory on most machines.

```bash
# Run a specific test (preferred)
go test -tags e2e -run TestE2E_Multicast_Publisher -v -count=1 ./e2e/...
go test -tags e2e -run TestE2E_Multicast_Subscriber -v -count=1 ./e2e/...

# Run all tests (requires high-memory machine)
dev/e2e-test.sh
```

## E2E Documentation

Detailed documentation for E2E testing is in `e2e/docs/`:

### Multicast Test Plan
See `e2e/docs/MULTICAST_TEST_PLAN.md` for:
- Step-by-step devnet setup with multicast configuration
- Devnet lifecycle commands (destroy, build, start, add devices/clients)
- Onchain setup (access passes, links, multicast groups, allowlists)
- Control plane verification (tunnels, PIM, MSDP, mroutes)
- Troubleshooting guide

### cEOS Multicast Limitation
See `e2e/docs/CEOS_MULTICAST_LIMITATION.md` for:
- Technical analysis of why multicast data plane doesn't work in cEOS
- Root cause (userspace forwarding agent bypasses kernel multicast routing)
- All attempted workarounds and why they failed
- Recommendations for the team

**TL;DR:** cEOS control plane works (PIM, MSDP, mroutes), but data plane doesn't forward multicast traffic. This is a fundamental architectural limitation that cannot be worked around.
