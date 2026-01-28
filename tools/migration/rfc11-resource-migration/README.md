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

# Generate parallel migration script (default: 8 jobs)
./rfc11-resource-migration --output migration.sh

# Generate script with higher parallelism for faster execution
./rfc11-resource-migration --output migration.sh --parallel 16

# Generate sequential script (no parallelism, for debugging)
./rfc11-resource-migration --output migration.sh --parallel 0

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
| `--parallel <n>`    | Number of parallel jobs (default: 8, 0 for sequential)        |
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

The `--output` flag generates an executable bash script with **phased parallel execution**:

```bash
#!/bin/bash
set -euo pipefail

# ============================================
# RFC11 Resource Migration Script
# Generated: 2024-01-15T10:30:00Z
# Parallelism: 8 jobs
# ============================================

# Check for GNU parallel
if ! command -v parallel &> /dev/null; then
    echo "ERROR: GNU parallel is required but not installed."
    echo "Install with: brew install parallel (macOS) or apt install parallel (Linux)"
    exit 1
fi

# ============================================
# Phase 1: Create Global ResourceExtension Accounts
# These must complete before any allocations can happen
# ============================================
echo "[Phase 1/5] Creating Global ResourceExtension Accounts (5 commands)..."
run_cmd 'doublezero resource create --resource-type device-tunnel-block'
...

# ============================================
# Phase 2: Create Per-Device ResourceExtension Accounts
# These can run in parallel as they don't depend on each other
# ============================================
echo "[Phase 2/5] Creating Per-Device ResourceExtension Accounts (24 commands)..."
parallel --halt soon,fail=10% --retries 3 -j 8 :::: <<'PARALLEL_EOF'
doublezero resource create --resource-type tunnel-ids --associated-pubkey 9xQeW... --index 0
doublezero resource create --resource-type dz-prefix-block --associated-pubkey 9xQeW... --index 0
...
PARALLEL_EOF

# ============================================
# Phase 3: Allocate Link Resources
# ============================================
echo "[Phase 3/5] Allocating Link Resources (20 commands)..."
parallel --halt soon,fail=10% --retries 3 -j 8 :::: <<'PARALLEL_EOF'
doublezero resource allocate --resource-type device-tunnel-block --requested-allocation 172.16.0.0/31
...
PARALLEL_EOF

# ============================================
# Phase 4: Allocate User Resources
# This is the largest phase - parallelism helps significantly here
# ============================================
echo "[Phase 4/5] Allocating User Resources (944 commands)..."
parallel --halt soon,fail=10% --retries 3 -j 8 :::: <<'PARALLEL_EOF'
doublezero resource allocate --resource-type user-tunnel-block --requested-allocation 169.254.0.0/31
...
PARALLEL_EOF

# ============================================
# Phase 5: Allocate MulticastGroup Resources
# ============================================
echo "[Phase 5/5] Allocating MulticastGroup Resources (8 commands)..."
...

echo "=== Migration Complete ==="
echo "Total commands: $TOTAL_COMMANDS"
echo "Successful: $SUCCESSFUL_COMMANDS"
```

### Execution Phases

The migration runs in 5 phases with dependency ordering:

| Phase | Commands                    | Execution  | Reason                                      |
| ----- | --------------------------- | ---------- | ------------------------------------------- |
| 1     | Global creates (5)          | Sequential | Must complete before any allocations        |
| 2     | Per-device creates          | Parallel   | Independent of each other                   |
| 3     | Link allocations            | Parallel   | Depends on Phase 1                          |
| 4     | User allocations            | Parallel   | Largest phase, benefits most from parallel  |
| 5     | Multicast allocations       | Parallel   | Depends on Phase 1                          |

### GNU Parallel Features

- `--retries 3`: Automatically retries failed commands up to 3 times
- `--halt soon,fail=10%`: Stops if more than 10% of commands fail
- `-j N`: Controls parallelism (adjust based on RPC rate limits)

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
5. **Start with low parallelism**: Test with `--parallel 4` before increasing
6. **Run on mainnet**: Execute the script with a foundation key
7. **Verify**: Use `doublezero resource get` to confirm allocations

## Performance

The parallel execution significantly speeds up migration for large networks:

| Network | Commands | Sequential (~1s/cmd) | Parallel -j8 | Parallel -j16 |
| ------- | -------- | -------------------- | ------------ | ------------- |
| Testnet | ~1001    | ~17-33 min           | ~2-4 min     | ~1-2 min      |
| Mainnet | TBD      | TBD                  | TBD          | TBD           |

### Tuning Parallelism

- **Start low**: Begin with `-j 4` or `-j 8` to avoid RPC rate limits
- **Monitor errors**: If you see rate limit errors, reduce parallelism
- **Increase carefully**: RPC providers vary; test before using high values
- **Sequential for debugging**: Use `--parallel 0` to debug failures

### Requirements

- **GNU parallel**: Required for parallel execution
  ```bash
  # macOS
  brew install parallel

  # Ubuntu/Debian
  apt install parallel
  ```
- **doublezero CLI**: Must be in PATH and configured with appropriate keys

## Notes

- Only processes activated Devices, Links, Users, and MulticastGroups
- SegmentRoutingIds is created but has no allocations (reserved for future use)
- The generated script uses `set -euo pipefail` for safety
- Each command includes comments explaining what it does
- Failed commands are tracked and reported at the end of execution
