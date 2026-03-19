# RFC-11: Onchain Activation

**Status:** Implemented

## Objective

- Make the activator service stateless by moving allocation state onchain
- Enable restartable activator without complex state reconciliation
- Zero breaking changes to IP ranges or allocation strategy

## Motivation

The current activator service introduces operational complexity:

- **Offchain polling and WebSocket connections** - The activator must maintain persistent connections to detect account changes. WebSocket disconnections cause missed events, requiring periodic full-state polling as a fallback.

- **Race conditions between allocation and activation** - Local state can diverge from onchain state between when a resource is allocated locally and when the activation transaction confirms. If the activator restarts or the transaction fails, allocated resources may be orphaned or double-allocated.

- **Single point of failure for resource allocation** - Only the activator can allocate resources. If it goes down, no new users, links, or interfaces can be activated until it recovers.

- **Complex state reconciliation on restart** - On restart, the activator must read all onchain accounts and infer which resources are allocated by examining `tunnel_net`, `dz_ip`, and `tunnel_id` values. This is error-prone and slow for large networks.

- **Deadlock incidents reported via internal monitoring** - Production incidents have occurred where the activator's local state became inconsistent, requiring manual intervention.

## Background: The Activator Service

The activator is a Rust service that bridges onchain DoubleZero Ledger state with network operations. Understanding its current role is essential to understanding why moving allocation onchain simplifies the system.

### What the Activator Does Today

The activator performs three core functions:

1. **Monitoring**: Watches for account changes via WebSocket subscriptions and periodic polling. When a client creates a User, Link, or other entity onchain with `status=Pending`, the activator detects this event.

2. **Allocation**: Reserves resources from shared pools before activation. This includes IP addresses for tunnels, tunnel IDs, and segment routing IDs. The activator maintains local bitmaps and allocators to track which resources are in use.

3. **Activation**: Submits transactions to transition entities from `Pending` to `Activated`, including the allocated resource values (tunnel_net, dz_ip, tunnel_id).

### Why Local State is Problematic

The activator currently maintains allocation state in memory:

```
┌──────────────────────────────────────────────────────┐
│                  Activator (in-memory)               │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │
│  │ IDAllocator │  │IPBlockAlloc │  │ DeviceState │   │
│  │ tunnel_ids  │  │ user_ips    │  │ per-device  │   │
│  │ segment_ids │  │ link_ips    │  │ allocators  │   │
│  └─────────────┘  └─────────────┘  └─────────────┘   │
└──────────────────────────────────────────────────────┘
```

On restart, the activator must reconstruct this state by reading all onchain accounts and inferring which resources are allocated. This reconciliation is error-prone and has caused production incidents.

### Resource Allocation Explained

"Allocation" means reserving a resource from a shared pool. Each resource type serves a specific networking purpose:

| Resource               | Scope      | Purpose                                                        | Example        |
| ---------------------- | ---------- | -------------------------------------------------------------- | -------------- |
| **User tunnel_net**    | Global     | /31 block for GRE tunnel between client and DZD                | 169.254.0.2/31 |
| **Device tunnel_net**  | Global     | /31 block for device-to-device tunnels                         | 172.16.0.2/31  |
| **Link tunnel_id**     | Global     | Identifier for link tunnel interfaces                          | 1              |
| **User tunnel_id**     | Per-device | Identifier for user tunnel interfaces (Arista range: 500-4095) | 501            |
| **DZ IP**              | Per-device | Routable IP from device's announced prefix                     | 10.0.0.5       |
| **Multicast IP**       | Global     | Public IP for multicast group traffic                          | 233.84.178.10  |
| **Segment Routing ID** | Global     | Identifier for device interface routing                        | 1001           |

**Important distinction:** These allocated resources are separate from PDA seeds. For example, a User account's PDA is derived from the client's public IP (`client_ip`), but the `tunnel_net` and `dz_ip` allocated to that user come from entirely different IP pools. This is a common source of confusion—the PDA seed identifies the account, while allocated resources configure the network path.

### Entity Resource Requirements

Each entity type requires different resources from the shared pools:

| Entity             | tunnel_net | tunnel_id  | dz_ip      | segment_routing_id | multicast_ip | ResourceExtension Accounts          |
| ------------------ | ---------- | ---------- | ---------- | ------------------ | ------------ | ----------------------------------- |
| **User**           | Global     | Per-device | Per-device | -                  | -            | Global (tunnel_net) + Device (rest) |
| **Link**           | Global     | Global     | -          | -                  | -            | Global only                         |
| **Interface**      | Global     | -          | -          | Global             | -            | Global only                         |
| **MulticastGroup** | -          | -          | -          | -                  | Global       | Global only                         |

This shows why multiple entity types must coordinate through shared allocation pools - a Link and a User could collide if they drew from the same `tunnel_net` pool without centralized tracking.

## Goals

1. **Stateless activator** - Move allocation state onchain via ResourceExtension accounts
2. **Restartable without reconciliation** - Activator can restart without rebuilding local state
3. **Zero breaking changes** - Same IP ranges, same allocation strategy
4. **Full resource recycling** - Reclaim IPs and IDs on deletion
5. **Simple rollback** - Can be reverted by disabling feature flag and bouncing activator

## Non-Goals

1. Changing existing IP range allocations
2. Eliminating the activator service (future scope)

## Architecture Overview

### Current Flow

```
     Client                    Activator                        Onchain
       │                       (stateful)                         │
       │                     ┌─────────────┐                      │
       │                     │ Local state │                      │
       │                     │ - bitmaps   │                      │
       │                     │ - allocators│                      │
       │                     └──────┬──────┘                      │
       │                            │                             │
       │  1. CreateUser ────────────│────────────────────────────>│
       │                            │                             │ User (Pending)
       │                            │<─── 2. Poll/subscribe ──────│
       │                            │                             │
       │                            │  3. Allocate locally        │
       │                            │     (update local bitmaps)  │
       │                            │        ↓                    │
       │                            │  ┌─────────────────────┐    │
       │                            │  │ Race condition risk │    │
       │                            │  │ Local state can     │    │
       │                            │  │ diverge from chain  │    │
       │                            │  └─────────────────────┘    │
       │                            │                             │
       │                            │  4. ActivateUser ──────────>│
       │                            │     (tunnel_net, dz_ip,     │ User (Activated)
       │                            │      tunnel_id)             │
```

**Problems:** Local state requires reconciliation on restart. Race conditions between steps 3-4.

### Stateless Activator (Backward Compatible Path)

For users created by old clients (without ResourceExtension), the activator handles activation:

```
     Client                    Activator                        Onchain
    (old SDK)                 (stateless)               ┌──────────────────┐
       │                           │                    │ Program          │
       │                           │                    │ ResourceExtension│
       │                           │                    │ User accounts    │
       │                           │                    └────────┬─────────┘
       │                           │                             │
       │  1. CreateUser ───────────│────────────────────────────>│
       │     (old instruction,     │                             │ User (Pending)
       │      no ResourceExt)      │                             │
       │                           │<─── 2. Poll/subscribe ──────│
       │                           │                             │
       │                           │  3. ActivateUser ──────────>│
       │                           │     (with ResourceExt)      │ bitmap updated
       │                           │                             │ User (Activated)
       │                           │     Program allocates       │
       │                           │     tunnel_net, dz_ip,      │
       │                           │     tunnel_id from bitmap   │
```

### Stateless Activator (New Client Path)

New clients pass ResourceExtension accounts to CreateUser for atomic create+allocate+activate:

```
     Client                                                 Onchain
    (new SDK)                                       ┌──────────────────┐
       │                                            │ Program          │
       │                                            │ ResourceExtension│
       │                                            │ User accounts    │
       │                                            └────────┬─────────┘
       │                                                     │
       │  1. CreateUser ────────────────────────────────────>│
       │     (with ResourceExt accounts)                     │
       │                                                     │ Program allocates
       │                                                     │ from bitmap
       │                                                     │
       │<──────────────────────────────────────── success ───│ User (Activated)
       │                                                     │
       │     No activator needed!                            │
       │     Atomic: create + allocate + activate            │
```

**Key change:** Allocation happens inside instructions, not as separate calls. The program reads ResourceExtension bitmaps, finds available slots, allocates, and writes resource values to the entity account—all atomically.

> **Note on PDA seeds vs allocated resources:** The `CreateUser` instruction uses `client_ip` (the user's public IP) as a PDA seed to derive the account address. This is unrelated to resource allocation. The allocated resources (`tunnel_net`, `dz_ip`, `tunnel_id`) come from separate shared pools stored in ResourceExtension accounts. Multiple entity types (User, Link, Interface, MulticastGroup) all draw from these shared pools, which is why centralized allocation tracking is necessary.

## Design

### What Changes

- New `ResourceExtension` account type with bitmap-based allocation (one allocator per account)
- Modified instructions with onchain allocation (symmetric create/delete behavior):
  - `CreateUser` - now does create -> allocate -> activate atomically (when ResourceExtension provided)
  - `ActivateUser` - now does allocate -> activate (for Pending users from old clients)
  - `DeleteUser` - now does delete -> deallocate -> close atomically (when ResourceExtension provided); otherwise sets status=Deleting for activator
  - `CloseAccountUser` - for backward compatibility with Deleting users from old clients
- New Foundation escape hatch instructions:
  - `CreateResourceExtension` - initialize ResourceExtension PDA
  - `AllocateResource` - manually allocate a specific slot
  - `ReleaseResource` - manually release a specific slot
  - `SetResourceState` - batch set bitmap (migration/recovery)
- New `doublezero resource` CLI commands for management and verification
- Activator removes local state, uses onchain allocation instead
- Activator becomes restartable without state reconciliation
- `FeatureFlag::OnChainAllocation` gates all onchain allocation paths

### Why Onchain Allocation Works

Moving allocation onchain is technically feasible because:

1. **Bitmaps are compact**: Each `ResourceExtension` account contains a single allocator with a bitmap sized to its resource type. For example, a `UserTunnelBlock` for a /16 network needs ~8 KB, while a per-device `TunnelIds` for range 500–4596 needs ~600 bytes. Total storage across all accounts is modest.

2. **Allocation is atomic**: Allocation happens inside the instruction (CreateUser, ActivateUser). The program reads the bitmap, finds the next available slot, marks it allocated, and writes the resource value—all atomically. No race conditions possible.

3. **Bitmap scanning is fast**: With <3% utilization, finding an available slot requires scanning very few bits. Each allocator maintains a `first_free_index` hint for O(1) amortized allocation.

4. **State is reconstructible**: If needed, bitmaps can be verified and corrected using `doublezero resource verify --fix`. This provides a recovery path without complex reconciliation logic.

5. **No performance regression**: Each instruction does slightly more work (bitmap scan + update), but eliminates WebSocket reconnection issues and state reconciliation overhead. Net simplification.

6. **Reduced contention**: Since each resource type has its own account, parallel transactions allocating different resource types don't lock the same account.

### ResourceExtension Account

Each `ResourceExtension` account contains a single allocator (IP or ID) and its bitmap:

```rust
enum Allocator {
    Ip(IpAllocator),
    Id(IdAllocator),
}

struct ResourceExtension {
    account_type: AccountType,
    owner: Pubkey,
    bump_seed: u8,
    associated_with: Pubkey,   // Device PK or Pubkey::default() for global
    allocator: Allocator,      // Single allocator per account
    storage: [u8],             // Bitmap bytes (remainder of account)
}
```

Each resource type gets its own `ResourceExtension` account, identified by a `ResourceType` enum used for PDA derivation:

| ResourceType | Scope | Allocator | Range | Allocation Size |
| --- | --- | --- | --- | --- |
| `UserTunnelBlock` | Global | IP | `GlobalConfig.user_tunnel_block` | 2 (/31) |
| `DeviceTunnelBlock` | Global | IP | `GlobalConfig.device_tunnel_block` | 2 (/31) |
| `MulticastGroupBlock` | Global | IP | `GlobalConfig.multicastgroup_block` | 1 (/32) |
| `MulticastPublisherBlock` | Global | IP | `GlobalConfig.multicast_publisher_block` | 1 (/32) |
| `LinkIds` | Global | ID | 0–65535 | 1 |
| `SegmentRoutingIds` | Global | ID | 1–65535 | 1 |
| `VrfIds` | Global | ID | 1–1024 | 1 |
| `DzPrefixBlock(device, idx)` | Per-device | IP | `device.dz_prefixes[idx]` | 1 (/32) |
| `TunnelIds(device, idx)` | Per-device | ID | 500–4596 | 1 |

Per-device resource types take a device pubkey and an index, supporting devices with multiple DZ prefixes.

**PDA derivation:**

| ResourceType | Seeds |
| --- | --- |
| `DeviceTunnelBlock` | `["doublezero", "devicetunnelblock"]` |
| `UserTunnelBlock` | `["doublezero", "usertunnelblock"]` |
| `MulticastGroupBlock` | `["doublezero", "multicastgroupblock"]` |
| `MulticastPublisherBlock` | `["doublezero", "multicastpublisherblock"]` |
| `LinkIds` | `["doublezero", "linkids"]` |
| `SegmentRoutingIds` | `["doublezero", "segmentroutingids"]` |
| `VrfIds` | `["doublezero", "vrfids"]` |
| `DzPrefixBlock(dev, idx)` | `["doublezero", "dzprefixblock", dev_pk_bytes, idx_le_bytes]` |
| `TunnelIds(dev, idx)` | `["doublezero", "tunnelids", dev_pk_bytes, idx_le_bytes]` |

### Feature Flag

Onchain allocation is gated by `FeatureFlag::OnChainAllocation` (bit 0 in `GlobalState.feature_flags`). When disabled, all instructions behave in legacy mode. The activator reads this flag at startup to choose between stateful and stateless code paths.

### Instruction Changes

#### Control Mechanism

Instructions use explicit flags in their arguments to determine whether to use onchain allocation:

- **User instructions**: `dz_prefix_count: u8` — when `> 0`, the instruction expects `3 + dz_prefix_count` ResourceExtension accounts and performs onchain allocation. When `0`, legacy behavior.
- **Link/Interface/MulticastGroup**: `use_onchain_allocation: bool` — same dual-mode pattern.

This makes account parsing unambiguous: the program knows exactly how many `ResourceExtension` accounts to expect from the instruction arguments.

#### Modified Instructions

| Instruction                | Flow                           | ResourceExtension                 | Authorization                                      |
| -------------------------- | ------------------------------ | --------------------------------- | -------------------------------------------------- |
| CreateUser                 | create -> allocate -> activate | Optional (global + device)        | AccessPass owner                                   |
| ActivateUser               | allocate -> activate           | Required (global + device)        | `activator_authority_pk` OR `foundation_allowlist` |
| DeleteUser                 | delete -> deallocate -> close  | Optional (global + device)        | User owner OR `foundation_allowlist`               |
| CloseAccountUser           | deallocate -> close            | Required (global + device)        | `activator_authority_pk` OR `foundation_allowlist` |
| CreateLink                 | create -> allocate -> activate | Optional (global only)            | Contributor owner (either device)                  |
| ActivateLink               | allocate -> activate           | Required (global only)            | `activator_authority_pk` OR `foundation_allowlist` |
| DeleteLink                 | delete -> deallocate -> close  | Optional (global only)            | Contributor owner OR `foundation_allowlist`        |
| CloseAccountLink           | deallocate -> close            | Required (global only)            | `activator_authority_pk` OR `foundation_allowlist` |
| CreateDevice               | create (+ resources if onchain)| Optional (creates per-device)     | Contributor owner                                  |
| ActivateDevice             | activate (+ resources if legacy)| Optional (creates per-device)    | `activator_authority_pk` OR `foundation_allowlist` |
| DeleteDevice               | delete -> close                | Optional (device)                 | Contributor owner OR `foundation_allowlist`        |
| CloseAccountDevice         | close                          | N/A                               | `activator_authority_pk` OR `foundation_allowlist` |
| CreateInterface            | create -> allocate -> activate | Optional (global)                 | Contributor owner                                  |
| ActivateInterface          | allocate -> activate           | Required (global)                 | `activator_authority_pk` OR `foundation_allowlist` |
| DeleteInterface            | delete -> deallocate -> close  | Optional (global)                 | Contributor owner OR `foundation_allowlist`        |
| CloseAccountInterface      | deallocate -> close            | Required (global)                 | `activator_authority_pk` OR `foundation_allowlist` |
| CreateMulticastGroup       | create -> allocate -> activate | Optional (global)                 | `foundation_allowlist`                             |
| ActivateMulticastGroup     | allocate -> activate           | Required (global)                 | `activator_authority_pk` OR `foundation_allowlist` |
| DeleteMulticastGroup       | delete -> deallocate -> close  | Optional (global)                 | Authority signer                                   |
| CloseAccountMulticastGroup | deallocate -> close            | Required (global)                 | `activator_authority_pk` OR `foundation_allowlist` |

#### User Instructions

- **CreateUser**: With ResourceExtension accounts (`dz_prefix_count > 0`), atomically creates, allocates resources, and activates. Without ResourceExtension accounts (old clients), creates Pending user as before—activator handles activation via ActivateUser.
- **ActivateUser**: For backward compatibility with Pending users created before migration. Program allocates from bitmap and activates.
- **DeleteUser**: With ResourceExtension accounts, atomically releases resources (tunnel_net, tunnel_id, dz_ip) back to bitmaps and closes account. Without ResourceExtension accounts, sets `status=Deleting`—activator handles closure via CloseAccountUser.
- **CloseAccountUser**: For backward compatibility with Deleting users. Requires `status=Deleting`. Releases resources back to bitmaps and closes account.

**User account layout when `dz_prefix_count > 0`:**

| Position | Account | Purpose |
| --- | --- | --- |
| Fixed | `user_tunnel_block` | Global — allocates `tunnel_net` (/31) |
| Fixed | `multicast_publisher_block` | Global — allocates `dz_ip` for multicast publishers |
| Fixed | `device_tunnel_ids` | Per-device — allocates `tunnel_id` |
| Variable (0..N) | `dz_prefix_block[0..N]` | Per-device — allocates `dz_ip` for EdgeFiltering/IBRLWithAllocatedIP |

**DZ IP allocation logic:**

The `dz_ip` source depends on user type:

| User Type | DZ IP Source |
| --- | --- |
| Multicast publisher | `MulticastPublisherBlock` |
| EdgeFiltering / IBRLWithAllocatedIP | First available `DzPrefixBlock` |
| IBRL (standard) | Uses `client_ip` directly (no allocation) |

**Idempotency:** Allocation is skipped if resources are already set (non-default values). This handles re-activation from `Updating` status safely.

#### Link Instructions

- **CreateLink**: With ResourceExtension accounts (`use_onchain_allocation = true`), atomically creates link, allocates `tunnel_net` and `tunnel_id` from global pools, and activates. Without ResourceExtension accounts, creates Pending link for activator.
- **ActivateLink**: For backward compatibility with Pending links. Program allocates `tunnel_net` and `tunnel_id` from global pools.
- **DeleteLink**: With ResourceExtension accounts, atomically releases `tunnel_net` and `tunnel_id` back to global bitmaps, resets interface statuses to Unlinked, and closes account. Without ResourceExtension accounts, sets `status=Deleting`—activator handles closure via CloseAccountLink.
- **CloseAccountLink**: For backward compatibility with Deleting links. Requires `status=Deleting`. Releases resources back to bitmaps, resets interface statuses, and closes account.

**Link account layout when `use_onchain_allocation = true`:**

| Account | Purpose |
| --- | --- |
| `device_tunnel_block` | Global — allocates `tunnel_net` (/31) |
| `link_ids` | Global — allocates `tunnel_id` |

#### Device Instructions

Per-device `ResourceExtension` accounts (`TunnelIds` and `DzPrefixBlock` for each prefix) are created as part of device lifecycle, but the creation path depends on the mode:

- **Legacy mode (onchain allocation disabled):** `CreateDevice` creates the device with `status=Pending`. When the activator calls `ActivateDevice`, the program creates the per-device `ResourceExtension` accounts and transitions the device to `Activated`.
- **Onchain mode (onchain allocation enabled):** `CreateDevice` is called with `resource_count > 0`. The program creates both the device account and its per-device `ResourceExtension` accounts atomically, setting `status=Activated` immediately. No activator involvement needed.

In both cases, `resource_count` equals `1 + len(dz_prefixes)`: one `TunnelIds` account plus one `DzPrefixBlock` per DZ prefix.

**Special behavior:** When a `DzPrefixBlock` is created, slot 0 is reserved (pre-allocated) for the device's loopback interface tunnel endpoint.

- **DeleteDevice**: Requires all users, links, and interfaces on device to be deleted first. With ResourceExtension account, atomically decrements reference counts and closes account. Without ResourceExtension, sets `status=Deleting`—activator handles closure via CloseAccountDevice.
- **CloseAccountDevice**: For backward compatibility with Deleting devices. Requires `status=Deleting`. Decrements reference counts on Contributor, Location, and Exchange, then closes account.

#### Interface Instructions

> **Note:** Interfaces are embedded within Device accounts, not separate PDAs. The instructions below modify the interface data within the parent Device account.

- **CreateInterface**: With ResourceExtension accounts (`use_onchain_allocation = true`), atomically creates interface within Device. For loopback interfaces, allocates IP from `DeviceTunnelBlock` and `segment_routing_id` from `SegmentRoutingIds` (Vpnv4 only). Physical interfaces are set to unlinked status.
- **ActivateInterface**: For backward compatibility with Pending interfaces. Program allocates resources based on interface type.
- **DeleteInterface**: With ResourceExtension account, atomically releases resources back to bitmaps and removes interface from Device account. Without ResourceExtension, sets interface `status=Deleting`—activator handles closure via CloseAccountInterface.
- **CloseAccountInterface**: For backward compatibility with Deleting interfaces. Requires interface `status=Deleting`. Releases resources back to bitmaps and removes interface from Device account.

**Interface account layout when `use_onchain_allocation = true`:**

| Account | Purpose |
| --- | --- |
| `device_tunnel_block` | Global — allocates loopback IP (/32) |
| `segment_routing_ids` | Global — allocates `segment_routing_id` (Vpnv4 only) |

#### MulticastGroup Instructions

- **CreateMulticastGroup**: With ResourceExtension account (`use_onchain_allocation = true`), atomically creates group and allocates `multicast_ip` from global pool.
- **ActivateMulticastGroup**: For backward compatibility with Pending groups. Program allocates `multicast_ip`.
- **DeleteMulticastGroup**: With ResourceExtension account, atomically releases `multicast_ip` back to bitmap and closes account. Without ResourceExtension, sets `status=Deleting`—activator handles closure via CloseAccountMulticastGroup.
- **CloseAccountMulticastGroup**: For backward compatibility with Deleting groups. Requires `status=Deleting`. Releases `multicast_ip` back to bitmap and closes account.

**MulticastGroup account layout when `use_onchain_allocation = true`:**

| Account | Purpose |
| --- | --- |
| `multicast_group_block` | Global — allocates `multicast_ip` (/32) |

#### New Foundation Instructions

| Instruction             | Purpose                            | Authorization          |
| ----------------------- | ---------------------------------- | ---------------------- |
| CreateResourceExtension | Initialize ResourceExtension PDA   | `foundation_allowlist` |
| AllocateResource        | Manually allocate a specific slot  | `foundation_allowlist` |
| ReleaseResource         | Manually release a specific slot   | `foundation_allowlist` |
| SetResourceState        | Batch set bitmap (migration)       | `foundation_allowlist` |

These are escape hatches for migration, recovery, and edge cases. Normal operations use Create/Activate/Delete instructions.

### Device Selection

For per-device resources (UserTunnelId, DzIp), the program must know which device's ResourceExtension to allocate from. Device selection works as follows:

- **CreateUser/ActivateUser**: The client specifies a `device_pubkey` argument. The program derives the per-device ResourceExtension PDAs from the device pubkey and allocates UserTunnelId and DzIp from that device's bitmaps.

- **Device validation**: The program verifies the specified device is valid (exists, is Activated) and has capacity in its ResourceExtension bitmaps.

- **SDK convenience**: The SDK can auto-select a device based on criteria like geographic proximity, available capacity, or load balancing. This is a client-side decision—the program just needs a valid device pubkey.

### Error Handling

**Allocation Errors:**

| Error                       | Cause                                      | Recovery                                        |
| --------------------------- | ------------------------------------------ | ----------------------------------------------- |
| `AllocationFailed`          | All slots in bitmap are allocated           | Wait for deletions or use different device      |
| `FeatureNotEnabled`         | `OnChainAllocation` flag not set            | Enable flag in `GlobalState`                    |
| `InvalidAccountType`        | Wrong account passed as ResourceExtension   | Programming error; fix client code              |
| `InvalidArgument`           | IP/ID type mismatch with allocator type     | Programming error; fix client code              |

**Atomicity Guarantees:**

- If any allocation fails within CreateUser/ActivateUser, the entire instruction fails—no partial state changes
- Solana's transaction model ensures all-or-nothing execution
- On failure, client can retry with same or different parameters

**Monitoring Recommendations:**

- Alert when any bitmap exceeds 80% utilization
- Track allocation failures by error type
- Monitor per-device utilization to identify capacity imbalances

> **Note:** The activator's keypair is already on `foundation_allowlist` (required for existing reject operations like `RejectUser`, `RejectLink`).

### Activator Changes

The activator reads `FeatureFlag::OnChainAllocation` at startup and branches into one of two code paths:

**Stateless mode (onchain allocation enabled):**

```rust
struct ProcessorStateless {
    devices: DeviceMapStateless,       // Cache only — no allocators
    multicastgroups: MulticastGroupMap, // Cache only
}
```

All activation commands pass `use_onchain_allocation: true` with zero-valued resource arguments. The program allocates from bitmaps atomically. No local rollback logic is needed.

**Stateful mode (legacy, onchain allocation disabled):**

Preserved for rollback. Maintains local `IDAllocator` and `IPBlockAllocator` instances for each resource type. Passes pre-allocated values to activation commands with `use_onchain_allocation: false`.

**Device onboarding:**

Per-device `ResourceExtension` accounts are created as part of device creation/activation (see "Device Instructions" above). In legacy mode, the activator calls `ActivateDevice` which creates the accounts. In onchain mode, `CreateDevice` creates them atomically.

## CLI: `doublezero resource` Commands

The CLI provides a full suite of commands for managing `ResourceExtension` accounts. These are essential for initial setup, ongoing operations, and debugging.

### `doublezero resource verify [--fix]`

The primary tool for migration and ongoing health checks. Scans all onchain accounts and cross-references allocated bitmap state against actual entity usage.

**What it checks (per resource type):**

| Resource Type | Entities Checked |
| --- | --- |
| `UserTunnelBlock` | Users' `tunnel_net` |
| `TunnelIds` (per device) | Users' `tunnel_id` |
| `DzPrefixBlock` (per device, per prefix) | Users' `dz_ip` |
| `DeviceTunnelBlock` | Device loopback interface IPs + Links' `tunnel_net` |
| `SegmentRoutingIds` | Device interface `node_segment_idx` |
| `LinkIds` | Links' `tunnel_id` |
| `MulticastGroupBlock` | MulticastGroups' `multicast_ip` |

**Discrepancy types detected:**

| Discrepancy | Meaning |
| --- | --- |
| `ExtensionNotFound` | `ResourceExtension` account doesn't exist for a resource type that should have one |
| `AllocatedButNotUsed` | Bitmap slot is marked allocated but no entity references it (orphaned) |
| `UsedButNotAllocated` | Entity references a resource value that isn't marked allocated in the bitmap |
| `DuplicateUsage` | Same resource value used by multiple entities (requires manual investigation) |

**With `--fix`:**

1. Creates any missing `ResourceExtension` accounts (`ExtensionNotFound`)
2. Re-verifies to get a fresh discrepancy list
3. Warns about duplicates (cannot auto-fix — requires manual investigation)
4. Prompts for confirmation, then:
   - Deallocates orphaned resources (`AllocatedButNotUsed`)
   - Allocates missing resources (`UsedButNotAllocated`)

### `doublezero resource create`

Creates a new `ResourceExtension` account for a given resource type.

```bash
# Global resource
doublezero resource create --resource-type DeviceTunnelBlock

# Per-device resource
doublezero resource create --resource-type DzPrefixBlock --associated-pubkey <DEVICE_PK> --index 0
```

### `doublezero resource get`

Lists all allocated values in a `ResourceExtension` account.

```bash
doublezero resource get --resource-type LinkIds
doublezero resource get --resource-type DzPrefixBlock --associated-pubkey <DEVICE_PK> --index 0 --json
```

### `doublezero resource allocate`

Manually allocates a resource. If `--requested-allocation` is omitted, the next available value is allocated.

```bash
# Next available
doublezero resource allocate --resource-type LinkIds

# Specific value
doublezero resource allocate --resource-type DeviceTunnelBlock --requested-allocation "172.16.0.4"
doublezero resource allocate --resource-type TunnelIds --associated-pubkey <DEVICE_PK> --index 0 --requested-allocation "501"
```

### `doublezero resource deallocate`

Releases a specific allocated resource back to the pool.

```bash
doublezero resource deallocate --resource-type LinkIds --value "5"
doublezero resource deallocate --resource-type DeviceTunnelBlock --value "172.16.0.4"
```

### `doublezero resource close`

Closes (deletes) a `ResourceExtension` account, reclaiming rent.

```bash
doublezero resource close --resource-type DeviceTunnelBlock
```

All per-device resource types (`DzPrefixBlock`, `TunnelIds`) require `--associated-pubkey` (device pubkey) and `--index`. All mutating commands (`create`, `allocate`, `deallocate`, `close`) require a Foundation identity and sufficient SOL balance.

## Migration Steps

1. Deploy program update with ResourceExtension support and modified instructions
2. Run `doublezero resource verify --fix` to create all `ResourceExtension` accounts (global and per-device) and initialize bitmaps from existing entity state. Review the proposed fixes before confirming.
3. Re-run `doublezero resource verify` (without `--fix`) to confirm zero discrepancies
4. Enable `FeatureFlag::OnChainAllocation` in `GlobalState`
5. Bounce the activator — it reads the feature flag at startup and switches to stateless mode
6. Verify activator processes pending entities correctly
7. Release updated SDK with onchain allocation support for client-side atomic operations

> **SDK upgrade incentive:** After step 7, old clients still work but create Pending users requiring activator intervention. New SDK clients get atomic activation. Recommend SDK upgrade for better UX.

## Rollback Plan

1. Disable `FeatureFlag::OnChainAllocation` in `GlobalState`
2. Bounce the activator — it restarts in stateful mode with local allocators

The program continues to support both paths. When the feature flag is disabled, new SDK clients fall back to the legacy instruction path without ResourceExtension accounts — they create Pending entities that the activator processes, just like old clients. The activator uses its local state for allocation.

For bitmap recovery after re-enabling: `doublezero resource verify --fix` can reinitialize bitmaps from existing entity state.

| Symptom                            | Action                                       |
| ---------------------------------- | -------------------------------------------- |
| ResourceExtension allocation fails | Disable feature flag, bounce activator        |
| Bitmap corruption suspected        | Run `doublezero resource verify --fix`        |
| Onchain transaction latency        | Disable feature flag, bounce activator        |

## Security Considerations

| Threat                  | Mitigation                                                          |
| ----------------------- | ------------------------------------------------------------------- |
| Bitmap manipulation     | Only program can modify; PDAs enforce ownership                     |
| Resource exhaustion     | Error when all slots allocated; monitoring alerts                   |
| Double allocation       | Bitmap bit already set check (inside instruction)                   |
| Double deallocation     | Bitmap bit not set check (inside instruction)                       |
| Race conditions         | Allocation inside instruction—no window between allocate and use    |
| Unauthorized activation | `activator_authority_pk` OR `foundation_allowlist` for ActivateUser |
| Unauthorized creation   | AccessPass owner required for CreateUser                            |

## Capacity Analysis

| Resource           | Scope      | Capacity          | Source                       | Recycling |
| ------------------ | ---------- | ----------------- | ---------------------------- | --------- |
| User tunnel_net    | Global     | ~32K /31 blocks   | 169.254.0.0/16               | Yes       |
| Device tunnel_net  | Global     | ~32K /31 blocks   | 172.16.0.0/16                | Yes       |
| Link tunnel_id     | Global     | ~65K IDs          | u16, 0–65535                 | Yes       |
| User tunnel_id     | Per device | ~4,096 IDs        | u16, 500–4596                | Yes       |
| DZ IP              | Per device | Depends on prefix | Device's dz_prefixes         | Yes       |
| Multicast group    | Global     | Depends on config | GlobalConfig.multicastgroup_block | Yes  |
| Multicast publisher| Global     | Depends on config | GlobalConfig.multicast_publisher_block | Yes |
| Segment Routing ID | Global     | ~65K IDs          | u16, 1–65535                 | Yes       |
| VRF ID             | Global     | ~1K IDs           | u16, 1–1024                  | Yes       |

### Storage Costs

Each `ResourceExtension` account is sized to its resource type:

| Account | Size | Rent-exempt (approx) |
| --- | --- | --- |
| `UserTunnelBlock` (/16, /31 alloc) | ~8 KB | ~0.06 SOL |
| `DeviceTunnelBlock` (/16, /31 alloc) | ~8 KB | ~0.06 SOL |
| `LinkIds` (0–65535) | ~8 KB | ~0.06 SOL |
| `SegmentRoutingIds` (1–65535) | ~8 KB | ~0.06 SOL |
| `MulticastGroupBlock` | Varies by config | < 0.01 SOL |
| `MulticastPublisherBlock` | Varies by config | < 0.01 SOL |
| `VrfIds` (1–1024) | ~216 B | < 0.01 SOL |
| Per-device `TunnelIds` (500–4596) | ~600 B | < 0.01 SOL |
| Per-device `DzPrefixBlock` | Varies by prefix | < 0.01 SOL |

**Total per device:** ~1–2 accounts (1 TunnelIds + 1 DzPrefixBlock per prefix), roughly 1–2 KB.

## Future Scope: Phase 2

Phase 2 (Activator End-of-Life) is planned as future work. With onchain allocation in place, the activator is only needed for backward compatibility with old SDK clients:

- **New SDK clients handle the full lifecycle atomically** — CreateUser does create+allocate+activate, DeleteUser does delete+deallocate+close. No activator involvement.
- **Old SDK clients** still create Pending users and Deleting users that require the activator to process via ActivateUser/CloseAccountUser.

Once all clients have upgraded to the new SDK, the activator can be decommissioned.

This phase will be designed and documented separately once all clients have migrated.

## Appendix: Technical Details

### Allocator Internals

Both allocators use bitmap-based allocation with a `first_free_index` hint for O(1) amortized allocation.

**IpAllocator:**

```rust
struct IpAllocator {
    base_net: NetworkV4,       // e.g., 169.254.0.0/16
    first_free_index: usize,   // Word index hint
}
```

One bit per /32 address in the base network. Multi-address allocations (e.g., /31 = 2 addresses) must be power-of-two aligned. Bitmap size: `2^(32 - prefix_len) / 8` bytes, rounded up to 8-byte alignment.

**IdAllocator:**

```rust
struct IdAllocator {
    range: (u16, u16),         // [start, end) e.g., (500, 4596)
    first_free_index: usize,   // Word index hint
}
```

One bit per ID in the range. Bitmap size: `(end - start) / 8` bytes, rounded up to 8-byte alignment.

Both allocators use `bytemuck::cast_slice_mut` for efficient 64-bit word scanning.

**`first_free_index` Maintenance:**

- **On allocate:** Start scan at `first_free_index` instead of 0. After allocation, if the word becomes full, increment `first_free_index` to the next word.
- **On deallocate:** If the freed bit is in a word before `first_free_index`, update `first_free_index` to that word's index.
- **Result:** O(1) allocation in the common case (sparse bitmaps), avoiding repeated scans of filled words.

### Account Layout

```
[header: 88 bytes (aligned to 8)] [bitmap: variable length]
```

Header fields: `account_type` (1) + `owner` (32) + `bump_seed` (1) + `associated_with` (32) + `allocator` (variable, up to 18 bytes for IpAllocator). Header is padded to 88 bytes (next 8-byte boundary after max header size of 84 bytes).

### Entity Account Fields

Entity accounts store actual allocated values, not slot indices. Slot indices are recomputed from values when needed for deallocation:

**User:**

```rust
struct User {
    // ... existing fields ...
    tunnel_net: NetworkV4,   // e.g., 169.254.0.4/31
    tunnel_id: u16,          // e.g., 501
    dz_ip: Ipv4Addr,         // e.g., 10.0.0.5
}
```

**Link:**

```rust
struct Link {
    // ... existing fields ...
    tunnel_net: NetworkV4,   // e.g., 172.16.0.4/31
    tunnel_id: u16,          // e.g., 7
}
```

**MulticastGroup:**

```rust
struct MulticastGroup {
    // ... existing fields ...
    multicast_ip: Ipv4Addr,  // e.g., 233.84.178.10
}
```

### Code Examples

> **Note:** These examples show Rust SDK command wrappers, not raw Solana instructions. The SDK handles account derivation, PDA computation, and transaction building.

**User Creation — Legacy (Old Client):**

```rust
// Old client creates user, activator handles activation separately
CreateUserCommand {
    client_ip,
    user_type,
    cyoa_type,
    device_pk,
    tunnel_endpoint,
    tenant_pk: None,
}.execute(client)?;
// User created with status=Pending
// Activator detects and activates later
```

**User Creation — Onchain (New Client):**

```rust
// New client creates user — SDK detects onchain allocation is enabled
// and includes ResourceExtension accounts automatically
CreateUserCommand {
    client_ip,
    user_type,
    cyoa_type,
    device_pk,
    tunnel_endpoint,
    tenant_pk: None,
}.execute(client)?;
// User created with status=Activated
// tunnel_id, tunnel_net, dz_ip allocated from bitmaps by program
```

The SDK determines whether to include ResourceExtension accounts based on the `FeatureFlag::OnChainAllocation` flag. The command struct is the same — the difference is in the accounts the SDK appends to the transaction.

**User Activation — Legacy (Activator with Local State):**

```rust
let mut user_tunnel_ips = IPBlockAllocator::new(config.user_tunnel_block);
let mut device_state = DeviceState::new(&device);

let tunnel_net = user_tunnel_ips.next_available_block(0, 2)?;
let tunnel_id = device_state.get_next_tunnel_id();
let dz_ip = device_state.get_next_dz_ip()?;

ActivateUserCommand {
    user_pubkey,
    tunnel_id,
    tunnel_net,
    dz_ip,
    use_onchain_allocation: false,
    tunnel_endpoint,
}.execute(client)?;
```

**User Activation — Onchain (Activator, Stateless):**

```rust
// Activator passes zero values — program allocates from bitmaps atomically
ActivateUserCommand {
    user_pubkey,
    tunnel_id: 0,
    tunnel_net: NetworkV4::default(),
    dz_ip: Ipv4Addr::UNSPECIFIED,
    use_onchain_allocation: true,
    tunnel_endpoint,
}.execute(client)?;
// User transitioned from Pending to Activated
// Resource values written by program
```

**User Deletion — Onchain (New Client, Atomic):**

```rust
// SDK includes ResourceExtension accounts when feature flag is enabled
DeleteUserCommand { pubkey: user_pubkey }.execute(client)?;
// Slots released, account closed — no activator needed
```

**User Deletion — Legacy (Old Client):**

```rust
// Step 1: Old client marks user for deletion (sets status=Deleting)
DeleteUserCommand { pubkey: user_pubkey }.execute(client)?;
// User status is now Deleting

// Step 2: Activator detects Deleting status, calls CloseAccount
CloseAccountUserCommand {
    pubkey: user_pubkey,
    use_onchain_deallocation: true,
}.execute(client)?;
// Slots released, account closed
```

**Foundation Escape Hatch (Manual Release):**

```bash
# For edge cases: manually release a specific slot
doublezero resource deallocate --resource-type UserTunnelBlock --value "169.254.0.4"
```

## Appendix: Glossary

| Term                     | Definition                                                                                                                                                     |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Activator**            | Rust service that monitors onchain state, allocates resources, and submits activation transactions                                                             |
| **Allocation**           | Reserving a resource (IP address, tunnel ID, segment routing ID) from a shared pool for exclusive use by an entity                                             |
| **Bitmap slot**          | A single position in a bitmap representing one allocatable resource unit                                                                                       |
| **CloseAccount**         | Instruction that finalizes entity deletion: verifies `status=Deleting`, releases allocated resources back to bitmaps, and closes the account                   |
| **DZ IP**                | A routable IP address from a device's announced prefix, assigned to users for network connectivity                                                             |
| **foundation_allowlist** | List of public keys authorized to perform privileged operations (CreateResourceExtension, SetResourceState, reject operations); includes the activator keypair |
| **PDA seed**             | Data used to derive a Program Derived Address; for User accounts, this is `client_ip`                                                                          |
| **ResourceExtension**    | Onchain account storing a single allocation bitmap; scoped to a specific resource type. Can be global or per-device.                                           |
| **ResourceType**         | Enum identifying which resource a `ResourceExtension` manages (e.g., `UserTunnelBlock`, `TunnelIds`). Encoded in PDA seeds.                                   |
| **tunnel_net**           | A /31 IP block used for GRE tunnel endpoints between client and DZD (users) or between devices (links)                                                         |
| **tunnel_id**            | Identifier for tunnel interfaces. Link tunnel_id is global (0–65535). User tunnel_id is per-device (500–4596).                                                 |
