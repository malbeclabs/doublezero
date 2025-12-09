# RFC-11: Onchain Activation

**Status:** Draft

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

2. **Allocation**: Reserves network resources from shared pools before activation. This includes IP addresses for tunnels, tunnel IDs, and segment routing IDs. The activator maintains local bitmaps and allocators to track which resources are in use.

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

"Allocation" means reserving network resources from shared pools. Each resource type serves a specific networking purpose:

| Resource               | Scope      | Purpose                                                   | Example        |
| ---------------------- | ---------- | --------------------------------------------------------- | -------------- |
| **User tunnel_net**    | Global     | /31 block for GRE tunnel between client and DZD           | 169.254.0.2/31 |
| **Link tunnel_net**    | Global     | /31 block for device-to-device tunnels                    | 172.16.0.2/31  |
| **Tunnel ID**          | Per-device | Identifier for tunnel interfaces (Arista range: 500-4095) | 501            |
| **DZ IP**              | Per-device | Routable IP from device's announced prefix                | 10.0.0.5       |
| **Multicast IP**       | Global     | Public IP for multicast group traffic                     | 233.84.178.10  |
| **Segment Routing ID** | Per-device | Identifier for device interface routing                   | 1001           |

**Important distinction:** These allocated resources are separate from PDA seeds. For example, a User account's PDA is derived from the client's public IP (`client_ip`), but the `tunnel_net` and `dz_ip` allocated to that user come from entirely different IP pools. This is a common source of confusion—the PDA seed identifies the account, while allocated resources configure the network path.

### Entity Resource Requirements

Each entity type requires different resources from the shared pools:

| Entity             | tunnel_net |     tunnel_id     |   dz_ip    | segment_routing_id | multicast_ip | ResourceExtension Accounts |
| ------------------ | :--------: | :---------------: | :--------: | :----------------: | :----------: | -------------------------- |
| **User**           |   Global   |    Per-device     | Per-device |         -          |      -       | Global + Device            |
| **Link**           |   Global   | Per-device (both) |     -      |         -          |      -       | Global + Both Devices      |
| **Interface**      |     -      |         -         | Per-device |     Per-device     |      -       | Device only                |
| **MulticastGroup** |     -      |         -         |     -      |         -          |    Global    | Global only                |

This shows why multiple entity types must coordinate through shared allocation pools - a Link and a User could collide if they drew from the same `tunnel_net` pool without centralized tracking.

## Goals

1. **Stateless activator** - Move allocation state onchain via ResourceExtension accounts
2. **Restartable without reconciliation** - Activator can restart without rebuilding local state
3. **Zero breaking changes** - Same IP ranges, same allocation strategy
4. **Full resource recycling** - Reclaim IPs and IDs on deletion
5. **Simple rollback** - Phase 1 can be reverted by restoring local state in activator

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

### Phase 1: Stateless Activator (Backward Compatible Path)

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

### Phase 1: Stateless Activator (New Client Path)

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

## Phase 1: Stateless Activator

### What Changes

- New `ResourceExtension` account type with bitmap-based allocation
- Modified instructions with onchain allocation (symmetric create/delete behavior):
  - `CreateUser` - now does create -> allocate -> activate atomically (when ResourceExtension provided)
  - `ActivateUser` - now does allocate -> activate (for Pending users from old clients)
  - `DeleteUser` - now does delete -> deallocate -> close atomically (when ResourceExtension provided); otherwise sets status=Deleting for activator
  - `CloseAccountUser` - for backward compatibility with Deleting users from old clients
- New Foundation escape hatch instructions:
  - `CreateResourceExtension` - initialize ResourceExtension PDA
  - `AllocateResource` - manually allocate a specific slot
  - `ReleaseResource` - manually release a specific slot
  - `SetResourceState` - batch set bitmap (migration)
- Activator removes local state, uses onchain allocation instead
- Activator becomes restartable without state reconciliation

### Why Onchain Allocation Works

Moving allocation onchain is technically feasible because:

1. **Bitmaps are compact**: A 4KB bitmap can track 32,768 slots. The global ResourceExtension (~8KB) handles all user and link tunnel allocations. Per-device extensions (~1KB each) handle device-scoped resources.

2. **Allocation is atomic**: Allocation happens inside the instruction (CreateUser, ActivateUser). The program reads the bitmap, finds the next available slot, marks it allocated, and writes the resource value—all atomically. No race conditions possible.

3. **Bitmap scanning is fast**: With <3% utilization, finding an available slot requires scanning very few bits. Worst case is O(n) where n = 512 words (32K bits), but expected case at current utilization is O(1) - the first word typically has free slots.

4. **State is reconstructible**: If needed, bitmaps can be reconstructed from existing account data using `SetResourceState`. This provides a recovery path without complex reconciliation logic.

5. **No performance regression**: Each instruction does slightly more work (bitmap scan + update), but eliminates WebSocket reconnection issues and state reconciliation overhead. Net simplification.

### ResourceExtension Account

```rust
struct ResourceExtension {
    account_type: AccountType,
    associated_with: Pubkey,		    // Device PK or Pubkey::default() for global
    id_allocations: Vec<IdBitmap>,	    // TunnelId, SegmentRoutingId
    ip_allocations: Vec<IpBlockBitmap>,	    // UserTunnelNet, LinkTunnelNet, DzIp, Multicast
}
```

**Scope:**

| Scope      | PDA Seeds                             | Contains                                |
| ---------- | ------------------------------------- | --------------------------------------- |
| Global     | `["resource_ext", Pubkey::default()]` | UserTunnelNet, LinkTunnelNet, Multicast |
| Per-device | `["resource_ext", device_pk]`         | TunnelId, SegmentRoutingId, DzIp        |

### Instruction Changes

**Modified Instructions:**

| Instruction                | Flow                           | ResourceExtension              | Authorization                                      |
| -------------------------- | ------------------------------ | ------------------------------ | -------------------------------------------------- |
| CreateUser                 | create -> allocate -> activate | Optional (device+global)       | AccessPass owner                                   |
| ActivateUser               | allocate -> activate           | Required (device+global)       | `activator_authority_pk` OR `foundation_allowlist` |
| DeleteUser                 | delete -> deallocate -> close  | Optional (device+global)       | User owner OR `foundation_allowlist`               |
| CloseAccountUser           | deallocate -> close            | Required (device+global)       | `activator_authority_pk` OR `foundation_allowlist` |
| CreateLink                 | create -> allocate -> activate | Optional (global+both devices) | Contributor owner (either device)                  |
| ActivateLink               | allocate -> activate           | Required (global+both devices) | `activator_authority_pk` OR `foundation_allowlist` |
| DeleteLink                 | delete -> deallocate -> close  | Optional (global+both devices) | Contributor owner OR `foundation_allowlist`        |
| CloseAccountLink           | deallocate -> close            | Required (global+both devices) | `activator_authority_pk` OR `foundation_allowlist` |
| CreateDevice               | create (Pending)               | N/A (triggers creation)        | Contributor owner                                  |
| ActivateDevice             | activate                       | N/A (triggers creation)        | `activator_authority_pk` OR `foundation_allowlist` |
| DeleteDevice               | delete -> close                | Optional (device)              | Contributor owner OR `foundation_allowlist`        |
| CloseAccountDevice         | close                          | N/A                            | `activator_authority_pk` OR `foundation_allowlist` |
| CreateInterface            | create -> allocate -> activate | Optional (device)              | Contributor owner                                  |
| ActivateInterface          | allocate -> activate           | Required (device)              | `activator_authority_pk` OR `foundation_allowlist` |
| DeleteInterface            | delete -> deallocate -> close  | Optional (device)              | Contributor owner OR `foundation_allowlist`        |
| CloseAccountInterface      | deallocate -> close            | Required (device)              | `activator_authority_pk` OR `foundation_allowlist` |
| CreateMulticastGroup       | create -> allocate -> activate | Optional (global)              | Authority signer                                   |
| ActivateMulticastGroup     | allocate -> activate           | Required (global)              | `activator_authority_pk` OR `foundation_allowlist` |
| DeleteMulticastGroup       | delete -> deallocate -> close  | Optional (global)              | Authority signer                                   |
| CloseAccountMulticastGroup | deallocate -> close            | Required (global)              | `activator_authority_pk` OR `foundation_allowlist` |

**User Instructions:**

- **CreateUser**: With ResourceExtension accounts, atomically creates, allocates resources, and activates. Without ResourceExtension accounts (old clients), creates Pending user as before—activator handles activation via ActivateUser.
- **ActivateUser**: For backward compatibility with Pending users created before migration. Program allocates from bitmap and activates.
- **DeleteUser**: With ResourceExtension accounts, atomically releases resources (tunnel_net, tunnel_id, dz_ip) back to bitmaps and closes account. Without ResourceExtension accounts, sets `status=Deleting`—activator handles closure via CloseAccountUser.
- **CloseAccountUser**: For backward compatibility with Deleting users. Requires `status=Deleting`. Releases resources back to bitmaps and closes account.

**Link Instructions:**

- **CreateLink**: With ResourceExtension accounts (global + both devices), atomically creates link, allocates `tunnel_net` from global pool and `tunnel_id` from each device's bitmap, and activates. Without ResourceExtension accounts, creates Pending link for activator.
- **ActivateLink**: For backward compatibility with Pending links. Program allocates `tunnel_net` and both `tunnel_id` slots.
- **DeleteLink**: With ResourceExtension accounts, atomically releases `tunnel_net` and both `tunnel_id` slots back to bitmaps, resets interface statuses to Unlinked, and closes account. Without ResourceExtension accounts, sets `status=Deleting`—activator handles closure via CloseAccountLink.
- **CloseAccountLink**: For backward compatibility with Deleting links. Requires `status=Deleting`. Releases resources back to bitmaps, resets interface statuses, and closes account.

**Device Instructions:**

- **CreateDevice**: Creates device in Pending state. Does not allocate resources—Device itself is a container for per-device resources.
- **ActivateDevice**: Transitions device to Activated. Activator creates per-device ResourceExtension if it doesn't exist (see "New Device Onboarding").
- **DeleteDevice**: Requires all users, links, and interfaces on device to be deleted first. With ResourceExtension account, atomically decrements reference counts and closes account (also closes the per-device ResourceExtension). Without ResourceExtension, sets `status=Deleting`—activator handles closure via CloseAccountDevice.
- **CloseAccountDevice**: For backward compatibility with Deleting devices. Requires `status=Deleting`. Decrements reference counts on Contributor, Location, and Exchange, then closes account.

**Interface Instructions:**

> **Note:** Interfaces are embedded within Device accounts, not separate PDAs. The instructions below modify the interface data within the parent Device account.

- **CreateInterface**: With ResourceExtension account (device), atomically creates interface within Device. For loopback interfaces, allocates `segment_routing_id` from device bitmap and `dz_ip` if configured. Physical interfaces are set to unlinked status.
- **ActivateInterface**: For backward compatibility with Pending interfaces. Program allocates resources based on interface type.
- **DeleteInterface**: With ResourceExtension account, atomically releases `segment_routing_id` and `dz_ip` (if allocated) back to bitmaps and removes interface from Device account. Without ResourceExtension, sets interface `status=Deleting`—activator handles closure via CloseAccountInterface.
- **CloseAccountInterface**: For backward compatibility with Deleting interfaces. Requires interface `status=Deleting`. Releases resources back to bitmaps and removes interface from Device account.

**MulticastGroup Instructions:**

- **CreateMulticastGroup**: With ResourceExtension account (global), atomically creates group and allocates `multicast_ip` from global pool.
- **ActivateMulticastGroup**: For backward compatibility with Pending groups. Program allocates `multicast_ip`.
- **DeleteMulticastGroup**: With ResourceExtension account, atomically releases `multicast_ip` back to bitmap and closes account. Without ResourceExtension, sets `status=Deleting`—activator handles closure via CloseAccountMulticastGroup.
- **CloseAccountMulticastGroup**: For backward compatibility with Deleting groups. Requires `status=Deleting`. Releases `multicast_ip` back to bitmap and closes account.

**New Foundation Instructions:**

| Instruction             | Purpose                           | Authorization          |
| ----------------------- | --------------------------------- | ---------------------- |
| CreateResourceExtension | Initialize ResourceExtension PDA  | `foundation_allowlist` |
| AllocateResource        | Manually allocate a specific slot | `foundation_allowlist` |
| ReleaseResource         | Manually release a specific slot  | `foundation_allowlist` |
| SetResourceState        | Batch set bitmap (migration)      | `foundation_allowlist` |

These are escape hatches for migration, recovery, and edge cases. Normal operations use Create/Activate/Delete instructions.

### Device Selection

For per-device resources (TunnelId, DzIp, SegmentRoutingId), the program must know which device's ResourceExtension to allocate from. Device selection works as follows:

- **CreateUser/ActivateUser**: The client specifies a `device_pubkey` argument. The program derives the per-device ResourceExtension PDA from `["resource_ext", device_pubkey]` and allocates TunnelId and DzIp from that device's bitmaps.

- **Device validation**: The program verifies the specified device is valid (exists, is Activated) and has capacity in its ResourceExtension bitmaps.

- **SDK convenience**: The SDK can auto-select a device based on criteria like geographic proximity, available capacity, or load balancing. This is a client-side decision—the program just needs a valid device pubkey.

### Error Handling

**Allocation Errors:**

| Error                       | Cause                                      | Recovery                                        |
| --------------------------- | ------------------------------------------ | ----------------------------------------------- |
| `BitmapFull`                | All slots in bitmap are allocated          | Wait for deletions or use different device      |
| `ResourceExtensionNotFound` | Per-device ResourceExtension doesn't exist | Activator auto-creates it; retry after creation |
| `DeviceNotActivated`        | Specified device is not in Activated state | Use a different device                          |
| `InvalidResourceType`       | Resource type doesn't match account scope  | Programming error; fix client code              |

**Atomicity Guarantees:**

- If any allocation fails within CreateUser/ActivateUser, the entire instruction fails—no partial state changes
- Solana's transaction model ensures all-or-nothing execution
- On failure, client can retry with same or different parameters

**Monitoring Recommendations:**

- Alert when any bitmap exceeds 80% utilization
- Track allocation failures by error type
- Monitor per-device utilization to identify capacity imbalances

> **Note:** The activator's keypair is already on `foundation_allowlist` (required for existing reject operations like `RejectUser`, `RejectLink`). This means the activator can call `CreateResourceExtension` directly—no separate Foundation action needed for new device onboarding.

### State Verification

Before migrating to onchain allocation, the activator must verify that its local state matches the current onchain state. This ensures the bitmap initialization via `SetResourceState` is accurate.

**Verification process:**

1. Activator reads all existing User, Link, Interface, and MulticastGroup accounts from onchain
2. Activator compares derived allocations (tunnel_id, tunnel_net, dz_ip) against its local state
3. Any discrepancies are logged with account pubkey, expected value, and actual value
4. Discrepancy report is reviewed by maintainers before proceeding with migration
5. If discrepancies exist, investigate root cause before initializing bitmaps

**Discrepancy handling:**

- If onchain has allocations not in local state: local state was lost (e.g., restart without persistence)
- If local state has allocations not onchain: entities were deleted but local state not updated
- Resolution: Use onchain state as source of truth for bitmap initialization

This verification is a prerequisite for migration step 5 (Initialize bitmaps via `SetResourceState`).

### Migration Steps

1. Deploy program update with ResourceExtension and modified instructions
2. Create global ResourceExtension (UserTunnelNet, LinkTunnelNet, Multicast)
3. Create per-device ResourceExtensions for all existing devices (TunnelId, SegmentRoutingId, DzIp)
4. **Run state verification** - activator compares local state with onchain entities
5. Initialize bitmaps from existing allocations via `SetResourceState`
6. Deploy updated activator that passes ResourceExtension to ActivateUser/CloseAccountUser
7. Verify activator processes pending entities correctly
8. Release updated SDK requiring ResourceExtension for CreateUser

> **Note:** Steps 2-3 create ResourceExtensions for existing infrastructure. After migration, the activator auto-creates per-device ResourceExtensions for any new devices (see "New Device Onboarding").

> **SDK upgrade incentive:** After step 8, old clients still work but create Pending users requiring activator intervention. New SDK clients get atomic activation. Recommend SDK upgrade for better UX.

## Rollback Plan

### Phase 1 Rollback

1. **Immediate:** Deploy activator with local state restored (ignores ResourceExtension)
2. **Full rollback:** Continue running activator with local state indefinitely
3. **State recovery:** `SetResourceState` can reinitialize from onchain entity data

### SDK Client Considerations

After rollback, SDK clients behave as follows:

| SDK Version                      | CreateUser Behavior    | Impact                                                                                               |
| -------------------------------- | ---------------------- | ---------------------------------------------------------------------------------------------------- |
| Old SDK (no ResourceExtension)   | Creates Pending user   | Works normally; activator processes                                                                  |
| New SDK (with ResourceExtension) | Creates Activated user | Still works—ResourceExtension accounts are ignored by rolled-back activator but don't break anything |

**Key point:** Rollback is activator-only. The program still supports both paths (with and without ResourceExtension). New SDK clients continue to work—they just get atomic activation while the rolled-back activator ignores their ResourceExtension updates.

**If full program rollback needed:** Deploy previous program version. New SDK clients would need to downgrade or handle instruction failures gracefully.

### Decision Matrix

| Symptom                            | Action                             |
| ---------------------------------- | ---------------------------------- |
| ResourceExtension allocation fails | Deploy activator with local state  |
| Bitmap corruption suspected        | Reinitialize with SetResourceState |
| Onchain transaction latency        | Deploy activator with local state  |

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

| Resource        | Scope      | Capacity          | Source                       | Recycling |
| --------------- | ---------- | ----------------- | ---------------------------- | --------- |
| User tunnel_net | Global     | ~32K /31 blocks   | 169.254.0.0/16               | Yes       |
| Link tunnel_net | Global     | ~32K /31 blocks   | 172.16.0.0/16                | Yes       |
| Tunnel ID       | Per device | ~3,595 interfaces | u16, 500-4095 (Arista limit) | Yes       |
| DZ IP           | Per device | Depends on prefix | Device's dz_prefixes         | Yes       |
| Multicast       | Global     | ~256 addresses    | 233.84.178.0/24              | Yes       |

### Current Usage (December 2025)

| Metric           | Mainnet-Beta | Testnet | Devnet |
| ---------------- | ------------ | ------- | ------ |
| Devices          | 72           | 9       | 9      |
| Users            | 755          | 438     | 1      |
| Links            | 124          | 8       | 6      |
| Interfaces       | 410          | 36      | 27     |
| Multicast Groups | 4            | 7       | 23     |

All environments have abundant capacity (<3% utilization).

### Storage Costs

| Account                      | Size     | Rent-exempt (SOL) |
| ---------------------------- | -------- | ----------------- |
| Global ResourceExtension     | ~8.21 KB | ~0.06 SOL         |
| Per-device ResourceExtension | ~1.11 KB | ~0.008 SOL        |

**Total:** `~8.21 KB + (~1.11 KB × 72 devices) = ~88 KB`

## Summary

| Phase   | What Changes                            | Activator State     | Rollback            |
| ------- | --------------------------------------- | ------------------- | ------------------- |
| Current | -                                       | Stateful (local)    | -                   |
| Phase 1 | Add ResourceExtension, new instructions | Stateless (onchain) | Restore local state |

**Phase 1 benefits:** Stateless activator, no reconciliation on restart, allocation visible onchain

## Future Scope: Phase 2

Phase 2 (Permissionless Activation) is planned as future work. With the new design, Phase 2 becomes simpler:

- **CreateUser already does atomic activation** - clients just need authorization
- **Remove `activator_authority_pk` check** from ActivateUser (keep `foundation_allowlist` for manual operations)
- **ActivateUser becomes obsolete** for new clients (only needed for legacy Pending users)

The activator would only be needed for:

1. New device onboarding (CreateResourceExtension)
2. Legacy Pending entity cleanup (ActivateUser, ActivateLink, etc.)
3. Legacy Deleting entity cleanup (CloseAccountUser, CloseAccountLink, etc.)
4. Operational tasks (reject operations)

> **Note:** Items 2-3 are only needed for backward compatibility with old SDK clients. New SDK clients handle the full lifecycle (create+activate and delete+close) atomically without activator involvement.

This phase will be designed and documented separately once Phase 1 is stable in production.

## Appendix: Technical Details

### Bitmap Structures

```rust
struct IdBitmap {
    id_type: IdType,                 // TunnelId, SegmentRoutingId
    start_offset: u16,               // e.g., 500 for tunnel IDs
    bitmap: [u64; 64],               // 4,096 slots
    allocated_count: u16,
    first_free_index: u32,           // Index of first word with free bit (optimization)
}

struct IpBlockBitmap {
    block_type: IpBlockType,         // UserTunnelNet, LinkTunnelNet, DzIp, Multicast
    block: Ipv4Network,              // e.g., 169.254.0.0/16
    slot_size: u8,                   // 0=/32, 1=/31, 2=/30
    reserved_start: u8,              // Slots to skip at start
    reserved_end: u8,                // Slots to skip at end
    bitmap: Vec<u64>,                // 1 bit per slot
    allocated_count: u32,
    first_free_index: u32,           // Index of first word with free bit (optimization)
}
```

**`first_free_index` Maintenance:**

- **On allocate:** Start scan at `first_free_index` instead of 0. After allocation, if the word becomes full, increment `first_free_index` to the next word.
- **On deallocate:** If the freed bit is in a word before `first_free_index`, update `first_free_index` to that word's index.
- **Result:** O(1) allocation in the common case (sparse bitmaps), avoiding repeated scans of filled words.

### Slot-to-IP Conversion

```rust
fn slot_to_ip(slot: u32, block: Ipv4Network, slot_size: u8, reserved_start: u8) -> Ipv4Addr {
    let base: u32 = block.network().into();
    Ipv4Addr::from(base + reserved_start as u32 + (slot * (1 << slot_size)))
}

fn ip_to_slot(ip: Ipv4Addr, block: Ipv4Network, slot_size: u8, reserved_start: u8) -> u32 {
    let base: u32 = block.network().into();
    let ip_u32: u32 = ip.into();
    (ip_u32 - base - reserved_start as u32) / (1 << slot_size)
}
```

**Examples:**

| Block                          | slot_size | reserved_start | Slot 0         | Slot 1         |
| ------------------------------ | --------- | -------------- | -------------- | -------------- |
| 169.254.0.0/16 (UserTunnelNet) | 1 (/31)   | 2              | 169.254.0.2/31 | 169.254.0.4/31 |
| 172.16.0.0/16 (LinkTunnelNet)  | 1 (/31)   | 2              | 172.16.0.2/31  | 172.16.0.4/31  |
| 10.0.0.0/24 (DzIp)             | 0 (/32)   | 2              | 10.0.0.2       | 10.0.0.3       |

### Account Sizes

| ResourceExtension Scope | ID Bitmaps                                 | IP Blocks                                                    | Size     |
| ----------------------- | ------------------------------------------ | ------------------------------------------------------------ | -------- |
| Global                  | None                                       | UserTunnelNet (4 KB), LinkTunnelNet (4 KB), Multicast (32 B) | ~8.21 KB |
| Per-device              | TunnelId (512 B), SegmentRoutingId (512 B) | DzIp (32 B each)                                             | ~1.11 KB |

> **Note:** Each bitmap includes a `first_free_index: u32` field (4 bytes) for O(1) allocation optimization. Global ResourceExtension has 3 bitmaps (+12 bytes), per-device has 3 bitmaps (+12 bytes). This overhead is included in the sizes above.

### Code Examples

> **Note:** These examples show Rust SDK command wrappers, not raw Solana instructions. The SDK handles account derivation, PDA computation, and transaction building. Raw instruction usage would require manually specifying all account metas.

**User Creation - Before (Old Client):**

```rust
// Old client creates user, activator handles activation separately
CreateUserCommand {
    client_ip,
    user_type,
    cyoa_type,
    // ... other args
}.execute(client)?;
// User created with status=Pending
// Activator detects and activates later
```

**User Creation - After (New Client):**

```rust
// New client creates user with ResourceExtension accounts
// Program does create + allocate + activate atomically
CreateUserCommand {
    client_ip,
    user_type,
    cyoa_type,
    device_resource_ext,   // Required - program allocates TunnelId, DzIp
    global_resource_ext,   // Required - program allocates UserTunnelNet
    // ... other args
}.execute(client)?;
// User created with status=Activated
// tunnel_id, tunnel_net, dz_ip derived from bitmaps by program
```

**User Activation - Before (Activator with Local State):**

```rust
let mut user_tunnel_ips = IPBlockAllocator::new(config.user_tunnel_block);
let mut device_state = DeviceState::new(&device);

let tunnel_net = user_tunnel_ips.next_available_block(0, 2)?;
let tunnel_id = device_state.get_next_tunnel_id();
let dz_ip = device_state.get_next_dz_ip()?;
ActivateUserCommand { tunnel_id, tunnel_net, dz_ip }.execute(client)?;
```

**User Activation - After (Activator, Stateless):**

For Pending users created by old clients before migration:

```rust
// Activator passes ResourceExtension accounts
// Program allocates from bitmap and activates atomically
ActivateUserCommand {
    user: user_pubkey,
    device_resource_ext,   // Required - program allocates TunnelId, DzIp
    global_resource_ext,   // Required - program allocates UserTunnelNet
    // No tunnel_id, tunnel_net, dz_ip args - program derives from bitmap
}.execute(client)?;
// User transitioned from Pending to Activated
// Resource values written by program
```

> **Note:** The program reads ResourceExtension bitmaps, finds next available slots, marks them allocated, computes resource values, and writes to user account—all in a single atomic instruction. If bitmap is full, instruction fails.

**User Deletion - After (New Client, Atomic):**

```rust
// New client deletes user with ResourceExtension accounts
// Program does delete + deallocate + close atomically
DeleteUserCommand {
    user: user_pubkey,
    device_resource_ext,   // Optional - if provided, program releases TunnelId, DzIp
    global_resource_ext,   // Optional - if provided, program releases UserTunnelNet
}.execute(client)?;
// Slots released, account closed - no activator needed!
```

**User Deletion - Backward Compatible Path (Old Client):**

```rust
// Step 1: Old client marks user for deletion (sets status=Deleting)
DeleteUserCommand {
    user: user_pubkey,
    // No ResourceExtension accounts provided
}.execute(client)?;
// User status is now Deleting

// Step 2: Activator detects Deleting status, calls CloseAccount
CloseAccountUserCommand {
    user: user_pubkey,
    device_resource_ext,   // Required - program releases TunnelId, DzIp
    global_resource_ext,   // Required - program releases UserTunnelNet
}.execute(client)?;
// Slots released, account closed
```

**Foundation Escape Hatch (Manual Release):**

```rust
// For edge cases: manually release a specific slot
ReleaseResourceCommand {
    resource_ext: global_resource_ext,
    resource_type: ResourceType::UserTunnelNet,
    slot: 42,  // Specific slot to release
}.execute(client)?;  // Requires foundation_allowlist
```

### New Device Onboarding (Post-Migration)

**Current flow (pre-migration):**

1. Contributor or Foundation calls `CreateDevice` -> Device created with `status: Pending`
2. Activator detects Pending device, calls `ActivateDevice` -> Device `status: Activated`
3. Activator creates local `DeviceState` with per-device allocators (tunnel_ids, dz_ips)

**Post-migration flow:**

1. Contributor or Foundation calls `CreateDevice` -> Device created with `status: Pending`
2. Activator detects Pending device:
   - Checks if per-device `ResourceExtension` exists for this device
   - If not, calls `CreateResourceExtension` to create it (activator is on `foundation_allowlist`)
3. Activator calls `ActivateDevice` -> Device `status: Activated`
4. Activator can now process users/links for this device using onchain ResourceExtension

**Key difference:** The activator gains a new responsibility—creating per-device ResourceExtension accounts. This replaces the old responsibility of maintaining local `DeviceState` allocators. The activator's keypair is already on `foundation_allowlist` (required for existing reject operations), so no additional authorization is needed.

### Other Entity Types

All entity types follow the same instruction pattern with a two-step deletion flow:

| Entity         | Create Instruction   | Activate Instruction   | Delete Instruction   | CloseAccount Instruction   |
| -------------- | -------------------- | ---------------------- | -------------------- | -------------------------- |
| User           | CreateUser           | ActivateUser           | DeleteUser           | CloseAccountUser           |
| Link           | CreateLink           | ActivateLink           | DeleteLink           | CloseAccountLink           |
| Device         | CreateDevice         | ActivateDevice         | DeleteDevice         | CloseAccountDevice         |
| Interface      | CreateInterface      | ActivateInterface      | DeleteInterface      | CloseAccountInterface      |
| MulticastGroup | CreateMulticastGroup | ActivateMulticastGroup | DeleteMulticastGroup | CloseAccountMulticastGroup |

**Deletion flow (symmetric with creation):**

- **With ResourceExtension:** Owner calls Delete → Program deallocates resources and closes account atomically (no activator needed)
- **Without ResourceExtension:** Owner calls Delete (sets `status=Deleting`) → Activator detects → Activator calls CloseAccount

Each Create/Activate instruction allocates the resources specified in the Entity Resource Requirements table. Each Delete (with ResourceExtension) or CloseAccount instruction deallocates those same resources.

**User Account Fields (for resource tracking):**

```rust
struct User {
    // ... existing fields ...
    tunnel_net_slot: u32,    // Slot in UserTunnelNet (global ResourceExtension)
    tunnel_id_slot: u16,     // Slot in device's tunnel_id bitmap
    dz_ip_slot: u32,         // Slot in device's DzIp bitmap
}
```

**Link Account Fields (for resource tracking):**

```rust
struct Link {
    // ... existing fields ...
    tunnel_net_slot: u32,        // Slot in LinkTunnelNet (global ResourceExtension)
    tunnel_id_slot_a: u16,       // Slot in device_a's tunnel_id bitmap
    tunnel_id_slot_b: u16,       // Slot in device_b's tunnel_id bitmap
}
```

**Interface Account Fields (for resource tracking):**

```rust
struct Interface {
    // ... existing fields ...
    segment_routing_slot: Option<u16>,  // Slot in device's segment_routing bitmap (loopback only)
    dz_ip_slot: Option<u32>,            // Slot in device's DzIp bitmap (loopback only)
}
```

**MulticastGroup Account Fields (for resource tracking):**

```rust
struct MulticastGroup {
    // ... existing fields ...
    multicast_slot: u32,  // Slot in Multicast (global ResourceExtension)
}
```

## Appendix: Glossary

| Term                     | Definition                                                                                                                                                     |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Activator**            | Rust service that monitors onchain state, allocates resources, and submits activation transactions                                                             |
| **Allocation**           | Reserving a network resource (IP address, tunnel ID) from a shared pool for exclusive use by an entity                                                         |
| **Bitmap slot**          | A single position in a bitmap representing one allocatable resource unit                                                                                       |
| **CloseAccount**         | Instruction that finalizes entity deletion: verifies `status=Deleting`, releases allocated resources back to bitmaps, and closes the account                   |
| **DZ IP**                | A routable IP address from a device's announced prefix, assigned to users for network connectivity                                                             |
| **foundation_allowlist** | List of public keys authorized to perform privileged operations (CreateResourceExtension, SetResourceState, reject operations); includes the activator keypair |
| **PDA seed**             | Data used to derive a Program Derived Address; for User accounts, this is `client_ip`                                                                          |
| **ResourceExtension**    | Onchain account storing allocation bitmaps; can be global or per-device scoped                                                                                 |
| **tunnel_net**           | A /31 IP block used for GRE tunnel endpoints between client and DZD (users) or between devices (links)                                                         |
| **tunnel_id**            | Device-scoped identifier for tunnel interfaces, range 500-4095 on Arista devices                                                                               |
