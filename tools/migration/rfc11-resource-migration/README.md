# RFC11 Resource Migration Script

This tool generates shell commands to migrate existing resource allocations to on-chain ResourceExtension accounts as part of RFC11 on-chain activation.

## Purpose

When migrating from the legacy off-chain resource allocation system to RFC11's on-chain allocation, existing Links, Users, and MulticastGroups have resources (tunnel IDs, tunnel networks, DZ IPs) that need to be recorded in the new ResourceExtension accounts. This script:

1. Queries on-chain data to find all existing allocations
2. Generates `doublezero resource create` commands for ResourceExtension accounts
3. Generates `doublezero resource allocate` commands to mark existing allocations

## Building

```bash
go build ./tools/migration/rfc11-resource-migration/
```

## Usage

```bash
# Dry-run on mainnet-beta (default)
./rfc11-resource-migration

# Dry-run on devnet
./rfc11-resource-migration --network devnet

# Verbose dry-run with details
./rfc11-resource-migration --verbose

# Generate executable script for mainnet
./rfc11-resource-migration --output migration.sh

# Generate script for devnet
./rfc11-resource-migration --network devnet --output migration-devnet.sh

# Override RPC URL if needed
./rfc11-resource-migration --network mainnet-beta --rpc https://custom-rpc.example.com
```

### Options

| Flag                | Description                                                   |
| ------------------- | ------------------------------------------------------------- |
| `--network <name>`  | Network: mainnet-beta (default), testnet, devnet, localnet    |
| `--rpc <URL>`       | Override RPC URL (optional)                                   |
| `--program-id <ID>` | Override program ID (optional)                                |
| `--output <file>`   | Output file for migration script (default: dry-run to stdout) |
| `--verbose`         | Show detailed information during dry-run                      |
| `--help`            | Show help message                                             |

### Known Networks

The script has built-in configuration for these networks (from `config/constants.go`):

| Network      | RPC URL                             | Program ID                                     |
| ------------ | ----------------------------------- | ---------------------------------------------- |
| mainnet-beta | doublezero-mainnet-beta.rpcpool.com | `ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv`  |
| testnet      | doublezerolocalnet.rpcpool.com      | `DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb` |
| devnet       | doublezerolocalnet.rpcpool.com      | `GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah` |
| localnet     | localhost:8899                      | `7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX` |

## Dry-Run Output

The dry-run shows what would be done without making any changes:

```
Network: mainnet-beta
RPC: https://doublezero-mainnet-beta.rpcpool.com/...
Program ID: ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv
Fetching program data...

=== RFC11 Resource Migration Dry-Run ===

--- Global ResourceExtension Accounts ---
[CREATE] device-tunnel-block
[CREATE] user-tunnel-block
[CREATE] multicast-group-block
[CREATE] link-ids
[CREATE] segment-routing-ids

--- Per-Device ResourceExtension Accounts ---
Device: la2-dz01 (9xQeW...)
  [CREATE] tunnel-ids index=0
  [CREATE] dz-prefix-block index=0

--- Link Allocations (2 links) ---
[ALLOCATE] device-tunnel-block: 172.16.0.0
[ALLOCATE] link-ids: 0

--- User Allocations (150 users) ---
[ALLOCATE] user-tunnel-block: 169.254.0.0
[ALLOCATE] tunnel-ids(la2-dz01, 0): 500
[ALLOCATE] dz-prefix-block(la2-dz01, 0): 45.33.100.10

--- MulticastGroup Allocations (3 groups) ---
[ALLOCATE] multicast-group-block: 233.84.178.1

=== Summary ===
ResourceExtension accounts to create: 12
Resources to allocate: 310
```

## Generated Script

The `--output` flag generates an executable bash script:

```bash
#!/bin/bash
set -euo pipefail

# ============================================
# RFC11 Resource Migration Script
# Generated: 2024-01-15T10:30:00Z
# ============================================

echo "=== Creating Global ResourceExtension Accounts ==="
# Global DeviceTunnelBlock for link tunnel_net (/31)
doublezero resource create --resource-type device-tunnel-block
...

echo "=== Creating Per-Device ResourceExtension Accounts ==="
# Device: 9xQeW...
# TunnelIds for device la2-dz01
doublezero resource create --resource-type tunnel-ids --associated-pubkey 9xQeW... --index 0
...

echo "=== Allocating Link Resources ==="
# Link la2-dz01:ny5-dz01 tunnel_net=172.16.0.0/31
doublezero resource allocate --resource-type device-tunnel-block --requested-allocation 172.16.0.0
...

echo "Migration complete!"
```

## ResourceTypes Migrated

### Global Resources (created once)

| ResourceType        | Purpose                    | Source                      |
| ------------------- | -------------------------- | --------------------------- |
| DeviceTunnelBlock   | Link tunnel networks (/31) | Link.tunnel_net             |
| UserTunnelBlock     | User tunnel networks (/31) | User.tunnel_net             |
| MulticastGroupBlock | Multicast IPs (/32)        | MulticastGroup.multicast_ip |
| LinkIds             | Link tunnel IDs (u16)      | Link.tunnel_id              |
| SegmentRoutingIds   | Reserved for future use    | -                           |

### Per-Device Resources (created for each activated device)

| ResourceType               | Purpose               | Source         |
| -------------------------- | --------------------- | -------------- |
| TunnelIds(device, 0)       | User tunnel IDs (u16) | User.tunnel_id |
| DzPrefixBlock(device, idx) | User DZ IPs (/32)     | User.dz_ip     |

## Verification Steps

1. **Dry run first**: Run without `--output` to see what would be done
2. **Review output**: Verify the summary matches expected allocations
3. **Generate script**: Run with `--output migration.sh`
4. **Test on devnet**: Run `--network devnet --output migration-devnet.sh` and test first
5. **Run on mainnet**: Execute the script with a foundation key
6. **Verify**: Use `doublezero resource get` to confirm allocations

## Notes

- Only processes activated Devices, Links, Users, and MulticastGroups
- SegmentRoutingIds is created but has no allocations (reserved for future use)
- The generated script uses `set -euo pipefail` for safety
- Each command includes comments explaining what it does
