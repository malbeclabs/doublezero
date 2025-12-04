# RFC-11: On-Chain Activation

**Status:** Draft

## Summary

Eliminate the activator service by moving allocation counters on-chain and providing atomic create+activate instructions. This RFC preserves the current allocation model (global pools, configurable ranges) while removing off-chain coordination.

## Motivation

The current activator service introduces operational complexity:

- Off-chain polling and WebSocket connections
- Race conditions between allocation and activation
- Single point of failure for resource allocation
- Complex state reconciliation between on-chain and off-chain
- Several instances of deadlocks reported via internal monitoring

This RFC moves allocation logic on-chain, eliminating these concerns while maintaining full compatibility with the existing network configuration.

## Goals

1. **Eliminate the activator** — No polling, no WebSocket, no off-chain allocation
2. **Zero breaking changes** — Same IP ranges, same allocation strategy
3. **Atomic operations** — Create+activate in single transaction
4. **Full resource recycling** — Reclaim IPs and IDs on deletion (users connect/disconnect frequently)
5. **No artificial limits** — No device caps or per-device partitioning
6. **Simple migration** — Initialize resource pools from current state

## Non-Goals

1. **Changing IP ranges** — Keep using GlobalConfig values (169.254.0.0/16 for users, 172.16.0.0/16 for links)
2. **Modifying existing on-chain accounts** — ResourceExtension is a new account type; existing Device, User, Link accounts remain unchanged

## Design

### Architecture Overview

**Current Flow (with Activator):**

```
┌─────────┐     1. CreateUser       ┌──────────┐
│  Client │ ───────────────────────▶│ On-Chain │
└─────────┘                         │ Program  │
     │                              └────┬─────┘
     │                                   │ User created
     │                                   │ status=Pending
     │                                   ▼
     │                              ┌──────────┐
     │  4. ActivateUser             │  User    │
     │◀─────────────────────────────│ Account  │
     │                              └────┬─────┘
     │                                   │
     │      2. Poll for pending          │
     │         accounts                  │
     │                              ┌────▼─────┐
     │                              │Activator │ Off-chain service
     │                              │ Service  │ (polling, WebSocket)
     │                              └────┬─────┘
     │                                   │
     │      3. Allocate IPs              │
     │         (tunnel_net, dz_ip)       │
     │                                   ▼
     │                              ┌──────────┐
     └─────────────────────────────▶│ On-Chain │
           Race condition risk!     │ Program  │
                                    └──────────┘
```

**New Flow (On-Chain Activation):**

```
┌─────────┐  CreateAndActivateUser  ┌──────────┐
│  Client │ ───────────────────────▶│ On-Chain │
└─────────┘                         │ Program  │
                                    └────┬─────┘
                                         │
           ┌─────────────────────────────┼─────────────────────────────┐
           │         ATOMIC TRANSACTION  │                             │
           │                             ▼                             │
           │  ┌────────────┐      ┌─────────────┐      ┌────────────┐  │
           │  │ Resource   │      │   Device    │      │ AccessPass │  │
           │  │ Extension  │      │   Account   │      │  Account   │  │
           │  │ (device)   │      │             │      │ users++    │  │
           │  │ tunnel_net │      │             │      │            │  │
           │  │ tunnel_id  │      │             │      │            │  │
           │  │ dz_ip      │      │             │      │            │  │
           │  └────────────┘      └─────────────┘      └────────────┘  │
           │                             │                             │
           │                             ▼                             │
           │                      ┌─────────────┐                      │
           │                      │    User     │                      │
           │                      │   Account   │                      │
           │                      │status=Active│                      │
           │                      └─────────────┘                      │
           │                                                           │
           └───────────────────────────────────────────────────────────┘
                                         │
                                         │
                                         ▼
                                    ┌──────────┐
                                    │Controller│ Polls on-chain state
                                    │          │ (unchanged behavior)
                                    └──────────┘
```

**Key Difference:** No off-chain allocation. All resource assignment happens atomically in a single transaction. The controller continues polling on-chain state as before—the only change is that accounts are now created in `Activated` state.

### Core Insight

The activator maintains counters and allocates sequentially from configured IP ranges. Users are created and deleted on every connection, requiring full resource recycling. We move allocation on-chain using bitmap-based ResourceExtension accounts that enable efficient allocation and deallocation with complete resource reuse.

### Account Modifications

#### Design Principle: No Changes to Existing Accounts

This RFC introduces a new `ResourceExtension` account type that manages resource allocation **without modifying existing on-chain accounts** (Device, User, Link, etc.). Each ResourceExtension is associated with either:

- A specific Device (per-device resources)
- `Pubkey::default()` (global resources)

This approach reduces migration risk and simplifies the upgrade path.

#### New Account: ResourceExtension

A ResourceExtension manages both ID allocations (tunnel IDs, segment routing IDs) and IP block allocations (tunnel_net, dz_ip) using bitmaps.

```rust
struct ResourceExtension {
    account_type: AccountType,       // Discriminator
    associated_with: Pubkey,         // Device PK or Pubkey::default() for global

    // ID bitmaps (fixed-size, inline)
    id_allocations: Vec<IdBitmap>,

    // IP block bitmaps (variable-size)
    ip_allocations: Vec<IpBlockBitmap>,
}

struct IdBitmap {
    id_type: IdType,                 // TunnelId, SegmentRoutingId
    start_offset: u16,               // e.g., 500 for tunnel IDs (Arista EOS naming)
    bitmap: [u64; 64],               // 4,096 slots
    allocated_count: u16,
}

struct IpBlockBitmap {
    block_type: IpBlockType,         // UserTunnelNet, LinkTunnelNet, DzIp, Multicast
    block: Ipv4Network,              // e.g., 169.254.0.0/16
    slot_size: u8,                   // Bits per slot (1 for /31 = 2 IPs, 0 for /32)
    reserved_start: u8,              // Slots to skip at start (network, gateway)
    reserved_end: u8,                // Slots to skip at end (broadcast)
    bitmap: Vec<u64>,                // 1 bit per slot (0 = free, 1 = allocated)
    allocated_count: u32,
}

enum IdType {
    TunnelId,           // Interface naming (Tunnel500-Tunnel4095)
    SegmentRoutingId,   // Segment routing labels
}

enum IpBlockType {
    UserTunnelNet,      // /31 blocks for user tunnels
    LinkTunnelNet,      // /31 blocks for device-to-device links
    DzIp,               // /32 addresses from device prefixes
    Multicast,          // /32 multicast group addresses
}
```

**Bitmap operations:**

- **Allocate:** Scan bitmap for first zero bit (u64 chunks), set it, return slot index. O(n/64) worst case; typically fast given low utilization.
- **Deallocate:** Clear bit at slot index. O(1).
- **Slot to IP:** `block.network() + reserved_start + (slot * (1 << slot_size))`

#### PDA Scheme

```
Global ResourceExtension:     ["resource_ext", Pubkey::default()]
Per-Device ResourceExtension: ["resource_ext", device_pk]
```

| Scope      | PDA Seeds                             | Contains                                        |
| ---------- | ------------------------------------- | ----------------------------------------------- |
| Global     | `["resource_ext", Pubkey::default()]` | LinkTunnelNet, Multicast                        |
| Per-device | `["resource_ext", device_pk]`         | TunnelId, SegmentRoutingId, DzIp, UserTunnelNet |

**Per-device UserTunnelNet:** Each device manages its own user tunnel allocation from the global `user_tunnel_block`. This ensures users connected to a device have tunnel IPs managed by that device's ResourceExtension.

#### Account Sizes

| ResourceExtension Scope | ID Bitmaps                                 | IP Blocks                              | Estimated Size |
| ----------------------- | ------------------------------------------ | -------------------------------------- | -------------- |
| Global                  | None                                       | LinkTunnelNet (4 KB), Multicast (32 B) | ~4.2 KB        |
| Per-device              | TunnelId (512 B), SegmentRoutingId (512 B) | UserTunnelNet (4 KB), DzIp (32 B each) | ~5.1 KB        |

**Tunnel ID constraint:** Interface naming (`TunnelXXX`) is limited to 4095 on Arista EOS. The bitmap supports slots 0-4095, with `start_offset` typically set to 500.

**Reserved IP handling:** `reserved_start` (typically 2: network .0, gateway .1) and `reserved_end` (typically 1: broadcast) are pre-marked during initialization.

#### Resource Relationships

```
GlobalConfig
    │
    └── Pubkey::default() ──────► ResourceExtension (Global)
                                      ├── LinkTunnelNet bitmap (device_tunnel_block)
                                      └── Multicast bitmap (multicast_group_block)

Device
    │
    └── device_pk ──────────────► ResourceExtension (Per-Device)
                                      ├── TunnelId bitmap
                                      ├── SegmentRoutingId bitmap
                                      ├── UserTunnelNet bitmap (user_tunnel_block)
                                      └── DzIp bitmap(s) (dz_prefixes)
```

#### Entity Account Additions

Existing entity accounts (User, Link, Interface, MulticastGroup) gain slot tracking fields for deallocation. These are **new fields only**, not modifications to existing data:

```rust
struct User {
    // ... existing fields unchanged ...
    tunnel_net_slot: u32,    // Slot in device's UserTunnelNet bitmap
    dz_ip_slot: u32,         // Slot in device's DzIp bitmap
    tunnel_id_slot: u16,     // Slot in device's TunnelId bitmap
}

struct Link {
    // ... existing fields unchanged ...
    tunnel_net_slot: u32,        // Slot in global LinkTunnelNet bitmap
    tunnel_id_slot_a: u16,       // Slot in device_a's TunnelId bitmap
    tunnel_id_slot_b: u16,       // Slot in device_b's TunnelId bitmap
}

struct Interface {
    // ... existing fields unchanged ...
    segment_routing_slot: Option<u16>,  // Slot in device's SegmentRoutingId bitmap
    dz_ip_slot: Option<u32>,            // Slot in device's DzIp bitmap
}

struct MulticastGroup {
    // ... existing fields unchanged ...
    multicast_slot: u32,  // Slot in global Multicast bitmap
}
```

### Authority Model

Admin operations use the existing `foundation_allowlist` in GlobalState:

```rust
// Existing field in GlobalState (not new)
pub foundation_allowlist: Vec<Pubkey>
```

**Authorization levels:**

| Operation                  | Required Authority                  |
| -------------------------- | ----------------------------------- |
| CreateResourceExtension    | foundation_allowlist member         |
| AllocateResource           | foundation_allowlist OR program CPI |
| ReleaseResource            | foundation_allowlist OR program CPI |
| SetResourceState           | foundation_allowlist member         |
| BanUser                    | foundation_allowlist member         |
| UpdateDeviceContributor    | foundation_allowlist member         |
| CreateAndActivateUser      | AccessPass owner                    |
| CreateAndActivateLink      | Contributor owner (either device)   |
| CreateAndActivateDevice    | Contributor owner                   |
| CreateAndActivateInterface | Contributor owner                   |
| DeleteUser                 | AccessPass owner                    |
| DeleteLink                 | Creator or Contributor owner        |
| DeleteDevice               | Contributor owner                   |
| DeleteInterface            | Contributor owner                   |
| SweepExpiredUsers          | Permissionless                      |

### PDA Seeds

| Entity                         | PDA Seeds                                               |
| ------------------------------ | ------------------------------------------------------- |
| User                           | `["doublezero", "user", client_ip, user_type]`          |
| Link                           | `["doublezero", "link", device_lo, device_hi]` (sorted) |
| Interface                      | `["doublezero", "interface", device, name]`             |
| MulticastGroup                 | `["doublezero", "multicast", group_address]`            |
| ResourceExtension (global)     | `["resource_ext", Pubkey::default()]`                   |
| ResourceExtension (per-device) | `["resource_ext", device_pk]`                           |

**Index-free PDA design:** All entity PDAs use content-addressable seeds rather than global indices. This eliminates race conditions in concurrent creation and simplifies account lookups. User PDA changes are being implemented in [PR#2332](https://github.com/malbeclabs/doublezero/pull/2332); Link, Interface, and MulticastGroup follow the same pattern.

### IP Address Computation

Addresses are computed from ResourceExtension bitmaps:

**User tunnel_net (per-device):**

```
ext = ResourceExtension(device_pk)          // device's resource extension
bitmap = ext.get_ip_bitmap(UserTunnelNet)   // manages portion of 169.254.0.0/16
slot = bitmap.allocate()                    // finds first free bit, sets it
tunnel_net = bitmap.block + (slot * 2)      // /31 block (2 IPs per user)
```

**Link tunnel_net (global):**

```
ext = ResourceExtension(Pubkey::default())  // global resource extension
bitmap = ext.get_ip_bitmap(LinkTunnelNet)   // manages 172.16.0.0/16
slot = bitmap.allocate()                    // finds first free bit, sets it
tunnel_net = bitmap.block + (slot * 2)      // /31 block (2 IPs per link)
```

**DZ IP (per-device):**

```
ext = ResourceExtension(device_pk)          // device's resource extension
bitmap = ext.get_ip_bitmap(DzIp)            // manages device's dz_prefixes
slot = bitmap.allocate()                    // finds first free bit
dz_ip = bitmap.block + slot                 // /32 address
```

Reserved IPs (.0 network, .1 gateway, broadcast) are pre-marked as allocated during ResourceExtension creation via `reserved_start` and `reserved_end`.

**Deallocation:**

```
bitmap.deallocate(slot)  // clears bit, slot available for reuse
```

### Logging

Transaction logs contain the executed instruction name. Values computed on-chain are logged for observability:

| Computed Value          | Logged In                                         |
| ----------------------- | ------------------------------------------------- |
| tunnel_net IP           | CreateAndActivateUser, CreateAndActivateLink      |
| dz_ip                   | CreateAndActivateUser, CreateAndActivateInterface |
| tunnel_id slot          | CreateAndActivateUser, CreateAndActivateLink      |
| segment_routing_id slot | CreateAndActivateInterface                        |
| multicast address       | CreateAndActivateMulticastGroup                   |
| deallocated slots       | Delete\* instructions                             |

This enables off-chain indexers to track allocations without parsing account data.

## New Instructions

### CreateResourceExtension

**Purpose:** Initialize a ResourceExtension account for managing resource allocation.

**Accounts:**

1. `resource_extension` (PDA to create, writable)
2. `globalconfig_account` (for IP block references)
3. `associated_account` (Device PK for per-device, or system program for global)
4. `authority` (signer, foundation_allowlist member)
5. `payer` (signer)

**Parameters:**

- `id_bitmaps: Vec<IdBitmapConfig>` — ID bitmap configurations to initialize
- `ip_blocks: Vec<IpBlockConfig>` — IP block configurations to initialize

**Steps:**

1. Derive PDA: `["resource_ext", associated_with]`
2. Validate authority is in foundation_allowlist
3. For each ID bitmap: initialize with zeroed bitmap, set start_offset
4. For each IP block: calculate bitmap size, pre-mark reserved slots
5. Create ResourceExtension account

### AllocateResource

**Purpose:** Allocate a resource slot from a ResourceExtension bitmap.

**Accounts:**

1. `resource_extension` (writable)
2. `authority` (signer, foundation_allowlist member OR program CPI)

**Parameters:**

- `resource_type: ResourceType` — Which bitmap to allocate from (TunnelId, SegmentRoutingId, UserTunnelNet, etc.)

**Returns:** Allocated slot index (logged and returned in instruction data)

**Steps:**

1. Validate authority (foundation_allowlist OR valid program CPI)
2. Find target bitmap by resource_type
3. Scan bitmap for first zero bit (O(n/64) with u64 chunks)
4. Set bit, increment allocated_count
5. Log and return slot index

**Usage:** Called by foundation manually for operational needs, or internally by CreateAndActivate\* instructions via CPI.

### ReleaseResource

**Purpose:** Release a previously allocated resource slot.

**Accounts:**

1. `resource_extension` (writable)
2. `authority` (signer, foundation_allowlist member OR program CPI)

**Parameters:**

- `resource_type: ResourceType` — Which bitmap to release from
- `slot: u32` — Slot index to release

**Steps:**

1. Validate authority (foundation_allowlist OR valid program CPI)
2. Find target bitmap by resource_type
3. Validate slot is currently allocated (bit is set)
4. Clear bit, decrement allocated_count
5. Log released slot

**Usage:** Called by foundation manually, or internally by Delete\* instructions via CPI.

### SetResourceState

**Purpose:** Batch set bitmap state for migration from current allocations.

**Accounts:**

1. `resource_extension` (writable)
2. `authority` (signer, foundation_allowlist member)

**Parameters:**

- `resource_type: ResourceType` — Which bitmap to update
- `slots: Vec<u32>` — Slot indices to mark as allocated

**Steps:**

1. Validate authority is in foundation_allowlist
2. Find target bitmap by resource_type
3. For each slot in slots:
   - Validate slot < total_slots
   - Set bit in bitmap
   - Increment allocated_count
4. Log slots marked as allocated

**Usage:** Called during migration to mark currently-allocated resources in the new bitmap system.

### CreateAndActivateDevice

**Purpose:** Atomically create a device and its associated ResourceExtension.

**Accounts:**

1. `device_account` (PDA to create, writable)
2. `resource_extension` (PDA to create, writable)
3. `globalconfig_account` (for IP block references)
4. `contributor_account`
5. `authority` (signer, contributor owner)
6. `payer` (signer)

**Steps:**

1. Validate contributor is valid
2. Create device account with standard fields
3. Create ResourceExtension with PDA `["resource_ext", device_pk]`:
   - Initialize TunnelId bitmap (start_offset=500)
   - Initialize SegmentRoutingId bitmap
   - Initialize UserTunnelNet bitmap from globalconfig.user_tunnel_block
   - Initialize DzIp bitmap(s) from device's dz_prefixes
4. Set device status to `Activated`
5. Log device and ResourceExtension creation

### CreateAndActivateUser

**Purpose:** Atomically creates and activates a user with allocated resources.

**PDA Derivation**

```
seeds = ["doublezero", "user", client_ip, user_type]
```

This ensures one user per (client_ip, user_type) tuple. Duplicate attempts fail with `AccountAlreadyExists`.

**Accounts:**

1. `user_account` (PDA to create, writable)
2. `device_account`
3. `device_resource_ext` (ResourceExtension for device, writable)
4. `accesspass_account` (writable)
5. `payer` (signer)

**Steps:**

1. Derive PDA and verify `user_account` matches expected address
2. Validate `user_account` does not already exist (data_is_empty)
3. Validate device is `Activated`
4. Validate AccessPass: caller is owner, not expired, under max_users limit
5. CPI to AllocateResource: allocate tunnel_id from device's TunnelId bitmap
6. CPI to AllocateResource: allocate tunnel_net from device's UserTunnelNet bitmap, compute IP
7. CPI to AllocateResource: allocate dz_ip from device's DzIp bitmap, compute IP
8. Store slot indices in user account (for deallocation on delete)
9. Create user account with `Activated` status
10. Increment `accesspass.active_user_count`
11. Log allocated tunnel_net, dz_ip, tunnel_id

**Error Cases:**

- `AccountAlreadyExists`: User PDA already exists for this (client_ip, user_type)
- `UserTunnelNetExhausted`: All slots allocated in device's UserTunnelNet bitmap
- `DzIpExhausted`: All slots allocated in device's DzIp bitmap
- `TunnelIdExhausted`: All 4096 tunnel_id slots allocated on device

### CreateAndActivateLink

**Purpose:** Atomically creates and activates a link between two devices.

**PDA Derivation:**

```
(device_lo, device_hi) = sort(device_a_pk, device_b_pk)
seeds = ["doublezero", "link", device_lo, device_hi]
```

Sorting device keys ensures (A→B) and (B→A) resolve to the same PDA, preventing duplicate links.

**Accounts:**

1. `link_account` (PDA to create, writable)
2. `device_a_account`, `device_b_account`
3. `device_a_resource_ext` (ResourceExtension for device_a, writable)
4. `device_b_resource_ext` (ResourceExtension for device_b, writable)
5. `global_resource_ext` (ResourceExtension for global, writable)
6. `contributor_a_account`, `contributor_b_account`
7. `payer` (signer)

**Steps:**

1. Derive link PDA with sorted device keys
2. Validate `link_account` matches expected PDA
3. Validate `link_account` does not already exist (data_is_empty)
4. Validate both devices are `Activated`
5. Validate contributors belong to their respective devices
6. Validate caller is contributor owner for either device
7. CPI to AllocateResource: allocate tunnel_id from device_a's TunnelId bitmap
8. CPI to AllocateResource: allocate tunnel_id from device_b's TunnelId bitmap
9. CPI to AllocateResource: allocate tunnel_net from global LinkTunnelNet bitmap, compute IP
10. Store slot indices in link account (for deallocation on delete)
11. Create link with `Activated` status
12. Log allocated tunnel_net, tunnel_id_a, tunnel_id_b

**Error Cases:**

- `LinkAlreadyExists`: Link PDA already exists between these devices
- `LinkTunnelNetExhausted`: All slots allocated in global LinkTunnelNet bitmap
- `TunnelIdExhausted`: One of the devices has all 4096 tunnel_id slots allocated

### CreateAndActivateInterface

**Purpose:** Creates interface on device with allocated resources.

**Accounts:**

1. `interface_account` (PDA to create, writable)
2. `device_account`
3. `device_resource_ext` (ResourceExtension for device, writable)
4. `authority` (signer, contributor owner)
5. `payer` (signer)

**Steps:**

1. Validate device is `Activated`
2. Validate caller is contributor owner
3. If loopback interface:
   - CPI to AllocateResource: allocate segment_routing_id from device's SegmentRoutingId bitmap
   - CPI to AllocateResource: allocate IP from device's DzIp bitmap if configured
4. If physical interface:
   - Set status to unlinked
5. Store slot indices in interface account (for deallocation on delete)
6. Create interface account with `Activated` status
7. Log allocated segment_routing_id, dz_ip (if applicable)

### CreateAndActivateMulticastGroup

**Purpose:** Creates multicast group with allocated address.

**Accounts:**

1. `multicast_account` (PDA to create, writable)
2. `global_resource_ext` (ResourceExtension for global, writable)
3. `authority` (signer, foundation_allowlist member)
4. `payer` (signer)

**Steps:**

1. Validate authority is in foundation_allowlist
2. CPI to AllocateResource: allocate slot from global Multicast bitmap
3. Compute address from bitmap block + slot
4. Store slot index in multicast account (for deallocation on delete)
5. Create in `Activated` state
6. Log allocated multicast address

### DeleteUser

**Purpose:** Closes user account and reclaims allocated resources.

**Accounts:**

1. `user_account` (writable, to close)
2. `device_account`
3. `device_resource_ext` (ResourceExtension for device, writable)
4. `accesspass_account` (writable)
5. `authority` (signer, accesspass owner)

**Steps:**

1. Validate user account exists
2. Validate caller is accesspass owner
3. CPI to ReleaseResource: release tunnel_id slot from device's TunnelId bitmap
4. CPI to ReleaseResource: release tunnel_net slot from device's UserTunnelNet bitmap
5. CPI to ReleaseResource: release dz_ip slot from device's DzIp bitmap
6. Decrement `accesspass.active_user_count`
7. Close account (return lamports to owner)
8. Log deallocated slots

Resources are immediately available for reuse by subsequent CreateAndActivateUser calls.

### DeleteLink

**Purpose:** Closes link account and reclaims allocated resources.

**Accounts:**

1. `link_account` (writable, to close)
2. `device_a_account`, `device_b_account`
3. `device_a_resource_ext` (ResourceExtension for device_a, writable)
4. `device_b_resource_ext` (ResourceExtension for device_b, writable)
5. `global_resource_ext` (ResourceExtension for global, writable)
6. `authority` (signer, creator or contributor owner)

**Authorization:** Creator OR contributor owner of either device.

**Steps:**

1. Validate link account exists
2. Validate caller is authorized (creator or contributor owner)
3. CPI to ReleaseResource: release tunnel_id_a from device_a's TunnelId bitmap
4. CPI to ReleaseResource: release tunnel_id_b from device_b's TunnelId bitmap
5. CPI to ReleaseResource: release tunnel_net from global LinkTunnelNet bitmap
6. Close account (return lamports to payer)
7. Log deallocated slots

### DeleteDevice

**Purpose:** Closes device account and its associated ResourceExtension.

**Accounts:**

1. `device_account` (writable, to close)
2. `device_resource_ext` (ResourceExtension for device, writable, to close)
3. `authority` (signer, contributor owner)

**Prerequisites:**

- All users on device must be deleted first
- All links involving device must be deleted first
- All interfaces on device must be deleted first

**Steps:**

1. Validate device has no active users (check or require explicit proof)
2. Validate device has no active links
3. Validate device has no active interfaces
4. Close device's ResourceExtension account (return lamports)
5. Close device account (return lamports)

### DeleteInterface

**Purpose:** Closes interface account and reclaims allocated resources.

**Accounts:**

1. `interface_account` (writable, to close)
2. `device_account`
3. `device_resource_ext` (ResourceExtension for device, writable)
4. `authority` (signer, contributor owner)

**Steps:**

1. Validate interface account exists
2. Validate caller is contributor owner
3. If loopback with segment_routing_id:
   - CPI to ReleaseResource: release segment_routing slot from device's SegmentRoutingId bitmap
4. If interface had dz_ip:
   - CPI to ReleaseResource: release dz_ip slot from device's DzIp bitmap
5. Close account (return lamports)
6. Log deallocated slots

### DeleteMulticastGroup

**Purpose:** Closes multicast group and reclaims allocated address.

**Accounts:**

1. `multicast_account` (writable, to close)
2. `global_resource_ext` (ResourceExtension for global, writable)
3. `authority` (signer, foundation_allowlist member)

**Steps:**

1. Validate multicast account exists
2. Validate caller is in foundation_allowlist
3. CPI to ReleaseResource: release multicast slot from global Multicast bitmap
4. Close account (return lamports)
5. Log deallocated slot

### BanUser

**Purpose:** Ban a user and reclaim their resources.

**Accounts:**

1. `user_account` (writable)
2. `device_account`
3. `device_resource_ext` (ResourceExtension for device, writable)
4. `accesspass_account` (writable)
5. `authority` (signer, foundation_allowlist member)

**Steps:**

1. Validate authority is in foundation_allowlist
2. CPI to ReleaseResource: release tunnel_id slot from device's TunnelId bitmap
3. CPI to ReleaseResource: release tunnel_net slot from device's UserTunnelNet bitmap
4. CPI to ReleaseResource: release dz_ip slot from device's DzIp bitmap
5. Decrement `accesspass.active_user_count`
6. Mark user status as `Banned`
7. Log deallocated slots

Resources are reclaimed but account remains for audit trail. Account can be closed separately if needed.

### SweepExpiredUsers

**Purpose:** Batch mark users as OutOfCredits when AccessPass expires.

**Authorization:** Permissionless. Anyone can call this to trigger cleanup of expired users.

**Accounts:**

1. `accesspass_account` (writable)
2. `user_accounts` (writable, up to 10)
3. `caller` (signer, pays for transaction)

**Steps:**

1. Check `accesspass.expiry_time < current_timestamp`
2. Mark accesspass status as `Expired` if not already
3. For each user account provided (up to 10):
   - Skip if user.accesspass_pk != accesspass_account.key
   - Skip if already OutOfCredits (idempotent)
   - Mark as OutOfCredits
4. Log count of users marked as OutOfCredits

**Permissionless rationale:** Expiration is a factual state (timestamp comparison). Restricting who can trigger cleanup adds operational burden with no security benefit. Anyone can verify an AccessPass is expired.

**Resources NOT released:** SweepExpiredUsers intentionally does NOT release allocated resources (tunnel_net, dz_ip, tunnel_id). Users marked as OutOfCredits may regain access if the AccessPass is renewed or credits are added. Resources are only released on explicit DeleteUser. This preserves the user's network identity during temporary expiration.

**Lazy Invalidation:** Users on expired AccessPasses are also blocked at CreateAndActivateUser (opportunistic check). Sweeps are for explicit cleanup; users can still be lazily invalidated when they attempt operations.

### UpdateDeviceContributor (Admin)

**Purpose:** Recovery path when `device.contributor_pk` becomes stale or incorrect.

**Authorization:** foundation_allowlist member only.

**Accounts:**

1. `device_account` (writable)
2. `foundation_allowlist_account`
3. `new_contributor_account` (must exist and be valid)
4. `authority` (signer, must be in foundation_allowlist)

**Steps:**

1. Validate authority is in foundation_allowlist
2. Validate new_contributor_account exists and is valid Contributor type
3. Update `device.contributor_pk` to new contributor

**Usage:** Operational recovery only. Use when original contributor is deleted, ownership transferred, or device was created with incorrect reference.

## AccessPass Expiration

Two mechanisms handle expiration:

1. **Opportunistic check:** CreateAndActivateUser validates expiry before creating users
2. **Batch sweep:** Anyone can call SweepExpiredUsers to mark existing users (permissionless)

## Controller Impact

**Current architecture:**

```
┌──────────┐                      ┌──────────────┐
│Activator │──── Writes ─────────>│  Blockchain  │
└──────────┘   (activate pending) │  (On-Chain)  │
                                  └──────┬───────┘
                                         │
┌──────────┐                             │
│Controller│<──── Polls ─────────────────┘
└──────────┘   (reads state, generates configs)
```

The activator and controller operate independently—both interact with on-chain state, but there is no direct communication between them. The controller polls on-chain accounts to generate device configurations.

**After this RFC:**

```
┌──────────────┐
│  Blockchain  │<──── Atomic create+activate
│  (On-Chain)  │
└──────┬───────┘
       │
┌──────┴──────┐
│  Controller │<──── Polls (unchanged)
└─────────────┘
```

The controller's behavior is unchanged. It continues polling the same on-chain accounts. The only difference is that accounts are now created in `Activated` state instead of transitioning from `Pending` to `Activated`.

## Capacity Analysis

| Resource        | Scope      | Capacity          | Source                           | Recycling |
| --------------- | ---------- | ----------------- | -------------------------------- | --------- |
| User tunnel_net | Global     | ~32K /31 blocks   | 169.254.0.0/16 (configurable)    | Yes       |
| Link tunnel_net | Global     | ~32K /31 blocks   | 172.16.0.0/16 (configurable)     | Yes       |
| Tunnel ID       | Per device | ~3,595 interfaces | u16, 500-4095 (Arista EOS limit) | Yes       |
| DZ IP           | Per device | Depends on prefix | Device's dz_prefixes             | Yes       |
| Multicast       | Global     | ~256 addresses    | 233.84.178.0/24 (configurable)   | Yes       |
| **Devices**     | **Global** | **Unlimited**     | No artificial cap                | N/A       |

### Current Usage (December 2025)

| Metric           | Mainnet-Beta | Testnet | Devnet |
| ---------------- | ------------ | ------- | ------ |
| Devices          | 72           | 9       | 9      |
| Users            | 755          | 438     | 1      |
| Links            | 124          | 8       | 6      |
| Interfaces       | 410          | 36      | 27     |
| Multicast Groups | 4            | 7       | 23     |

**Tunnel Net Utilization:**

| Resource        | Env     | Allocated | Capacity | Utilization |
| --------------- | ------- | --------- | -------- | ----------- |
| User tunnel_net | mainnet | 754       | 32,768   | 2.30%       |
|                 | testnet | 435       | 32,768   | 1.32%       |
|                 | devnet  | 1         | 32,768   | ~0%         |
| Link tunnel_net | mainnet | 123       | 32,768   | 0.37%       |
|                 | testnet | 8         | 32,768   | 0.02%       |
|                 | devnet  | 6         | 32,768   | 0.01%       |

All environments have abundant capacity. Even mainnet at peak usage is under 3% utilization for user tunnel_net.

### Concurrent vs Cumulative Capacity

With bitmap-based recycling, capacity is measured in **concurrent** allocations, not cumulative. When a user disconnects and is deleted, their resources are immediately available for reuse.

| Resource               | Concurrent Capacity | Connect/Disconnect Support |
| ---------------------- | ------------------- | -------------------------- |
| User tunnel_net        | ~32K simultaneous   | Unlimited over time        |
| Link tunnel_net        | ~32K simultaneous   | Unlimited over time        |
| Tunnel ID (per device) | ~3,595 per device   | Unlimited over time        |

### ResourceExtension Storage Costs

| Account                      | Size    | Rent-exempt (SOL) |
| ---------------------------- | ------- | ----------------- |
| Global ResourceExtension     | ~4.2 KB | ~0.03 SOL         |
| Per-device ResourceExtension | ~5.1 KB | ~0.04 SOL         |

**Total new storage:** ~4.2 KB global + ~5.1 KB per device

**Note:** No modifications to existing Device, User, Link, Interface, or MulticastGroup accounts. New slot tracking fields are added only to newly created entities.

**Pre-migration entity handling:** Entities created before migration lack slot tracking fields (`tunnel_net_slot`, `dz_ip_slot`, etc.). These entities cannot be deleted via the new Delete\* instructions since there are no slot indices to release. Pre-migration entities must be handled via the legacy activator flow until they naturally expire/disconnect, or via a separate migration instruction that backfills slot indices from their IP addresses.

## Migration

### Phase 1: Deploy Program Update

1. Add ResourceExtension account type to program
2. Add AllocateResource and ReleaseResource instructions
3. Add slot tracking fields to User, Link, Interface, MulticastGroup account types
4. No changes to existing on-chain account data

### Phase 2: Create ResourceExtensions

Create the global ResourceExtension:

```
CreateResourceExtension(
    associated_with: Pubkey::default(),
    ip_blocks: [LinkTunnelNet, Multicast]
)
```

For each existing device, create its ResourceExtension:

```
For each device:
  CreateResourceExtension(
      associated_with: device_pk,
      id_bitmaps: [TunnelId, SegmentRoutingId],
      ip_blocks: [UserTunnelNet, DzIp...]
  )
```

### Phase 3: Initialize Resource State

Use SetResourceState to mark currently-allocated resources:

```
# Query existing allocations
users = query_all_users()
links = query_all_links()
interfaces = query_all_interfaces()
multicast_groups = query_all_multicast_groups()

# Compute slots from existing IP addresses
user_tunnel_slots_by_device = group_by_device([ip_to_slot(u.tunnel_net) for u in users])
link_tunnel_slots = [ip_to_slot(l.tunnel_net, link_tunnel_block) for l in links]
# ... etc

# Set global ResourceExtension state
SetResourceState(global_resource_ext, LinkTunnelNet, link_tunnel_slots)
SetResourceState(global_resource_ext, Multicast, multicast_slots)

# Set per-device ResourceExtension state
For each device:
  SetResourceState(device_resource_ext, TunnelId, device_tunnel_id_slots)
  SetResourceState(device_resource_ext, SegmentRoutingId, device_sr_slots)
  SetResourceState(device_resource_ext, UserTunnelNet, user_tunnel_slots_by_device[device])
  SetResourceState(device_resource_ext, DzIp, dz_ip_slots)
```

### Phase 4: Backfill Pre-existing Entities (Optional)

For pre-existing entities to use the new Delete\* instructions, their slot indices must be backfilled:

```
BackfillEntitySlots {
    entity_account: Pubkey,          // User, Link, Interface, or MulticastGroup
    tunnel_net_slot: Option<u32>,    // Computed from entity's tunnel_net IP
    dz_ip_slot: Option<u32>,         // Computed from entity's dz_ip
    tunnel_id_slot: Option<u16>,     // Computed from entity's tunnel_id
    // ... other slots as applicable
}
```

**Authorization:** foundation_allowlist member only.

**Steps:**

1. Validate authority is in foundation_allowlist
2. Validate entity exists and lacks slot indices (all zero/unset)
3. Validate provided slots match entity's current IP addresses
4. Write slot indices to entity account

**Alternative:** Allow pre-existing entities to expire naturally. Users disconnect frequently, so most pre-migration users will be replaced by post-migration users over time.

### Phase 5: Deploy New Instructions

1. Add CreateAndActivate\* instructions
2. Add Delete\* instructions with resource reclaim
3. Add BackfillEntitySlots instruction (optional)
4. Update SDK to use new instructions
5. Mark old Create + Activate as deprecated

### Phase 6: Deprecate Activator

1. New entities use atomic instructions
2. Process remaining pending entities via activator
3. Shut down activator when queue empty

## Security Considerations

| Threat                    | Mitigation                                              |
| ------------------------- | ------------------------------------------------------- |
| Bitmap manipulation       | Only program can modify bitmaps; PDAs                   |
| Resource exhaustion       | Error when all slots allocated                          |
| Double allocation         | Bitmap bit already set check                            |
| Double deallocation       | Bitmap bit not set check                                |
| Front-running             | Atomic allocation; first valid tx wins                  |
| Unauthorized activation   | Same auth as current instructions                       |
| Unauthorized admin ops    | foundation_allowlist verification                       |
| Invalid slot deallocation | Slot index stored in entity account, verified on delete |

## Conclusion

This RFC eliminates the activator service with bitmap-based resource management:

- **~4.2 KB new global state** (Global ResourceExtension for LinkTunnelNet, Multicast)
- **~5.1 KB new state per Device** (Per-device ResourceExtension for TunnelId, SegmentRoutingId, UserTunnelNet, DzIp)
- **No modifications to existing accounts** — ResourceExtension is a new account type
- **Full resource recycling** via O(n/64) bitmap allocation and O(1) deallocation
- **Generic AllocateResource/ReleaseResource API** — usable by foundation manually or program via CPI
- **Zero breaking changes** to IP ranges or allocation strategy
- **No artificial device limits**
- **Per-device UserTunnelNet** — each device manages its own user tunnel allocations
- **Handles high connect/disconnect churn** (users created per-connection)

The design moves allocation on-chain while supporting the full lifecycle of network resources—creation, activation, and deletion with immediate resource reuse.
