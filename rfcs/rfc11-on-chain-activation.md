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
2. **Per-device IP pool partitioning** — Keep global pool allocation for tunnel_net (matches current activator)

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
           │  │GlobalState │      │   Device    │      │ AccessPass │  │
           │  │            │      │   Account   │      │  Account   │  │
           │  │ tunnel_net │      │ dz_ip++     │      │ users++    │  │
           │  │   offset++ │      │ tunnel_id++ │      │            │  │
           │  │            │      │             │      │            │  │
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

The activator maintains counters and allocates sequentially from configured IP ranges. Users are created and deleted on every connection, requiring full resource recycling. We move allocation on-chain using bitmap-based ResourceAccounts that enable O(1) allocation and deallocation with complete resource reuse.

### Account Modifications

#### New Account: ResourceAccount

A ResourceAccount manages allocation from an IP block using a bitmap for O(1) allocation/deallocation with full recycling.

```rust
struct ResourceAccount {
    account_type: AccountType,      // Discriminator
    resource_type: ResourceType,    // UserTunnelNet, LinkTunnelNet, Multicast, DzIp
    block: Ipv4Network,             // e.g., 169.254.0.0/16
    slot_size: u8,                  // Bits per slot (e.g., 1 for /31 = 2 IPs)
    total_slots: u32,               // Total allocatable slots
    allocated_count: u32,           // Current allocation count
    bitmap: Vec<u64>,               // 1 bit per slot (0 = free, 1 = allocated)
}

enum ResourceType {
    UserTunnelNet,    // /31 blocks from user_tunnel_block
    LinkTunnelNet,    // /31 blocks from device_tunnel_block
    Multicast,        // /32 from multicast_group_block
    DzIp,             // /32 from device dz_prefixes
}
```

**Bitmap operations:**

- **Allocate:** Find first zero bit, set it, return slot index. O(1) average case.
- **Deallocate:** Clear bit at slot index. O(1).
- **Slot to IP:** `block.network() + (slot * (1 << slot_size))`

**Account sizes:**

| Resource          | Block Size | Slots  | Bitmap Size | Total Account |
| ----------------- | ---------- | ------ | ----------- | ------------- |
| UserTunnelNet     | /16        | 32,768 | 4 KB        | ~4.1 KB       |
| LinkTunnelNet     | /16        | 32,768 | 4 KB        | ~4.1 KB       |
| Multicast         | /24        | 256    | 32 bytes    | ~100 bytes    |
| DzIp (per device) | /24        | 256    | 32 bytes    | ~100 bytes    |

**PDA seeds:**

```
ResourceAccount: ["resource", resource_type, block_network_address]
DzIp:            ["resource", "dz_ip", device_pk, prefix_index]
```

#### Device Account (add ~1 KB)

| Field                    | Type      | Purpose                                           |
| ------------------------ | --------- | ------------------------------------------------- |
| `tunnel_id_bitmap`       | [u64; 64] | 4,096 slots for tunnel interface IDs              |
| `segment_routing_bitmap` | [u64; 64] | 4,096 slots for segment routing IDs               |
| `dz_ip_reserved_count`   | u8        | Number of reserved IPs to skip (network, gateway) |

**Inline bitmaps:** TunnelId and SegmentRoutingId are stored inline in the Device account rather than as separate ResourceAccounts. With only 100-200 devices expected, the ~1 KB overhead per device is acceptable and simplifies the design.

**Tunnel ID constraint:** Interface naming (`TunnelXXX`) is limited to 4095 on Arista EOS. The bitmap supports slots 0-4095, with allocation typically starting at 500.

**Reserved IP handling:** DZ IP allocation skips the first `dz_ip_reserved_count` offsets (typically 2: network address .0 and gateway .1) and the last offset (broadcast).

#### Resource Relationships

```
GlobalConfig
    │
    ├── user_tunnel_block ──────► ResourceAccount (UserTunnelNet)
    ├── device_tunnel_block ────► ResourceAccount (LinkTunnelNet)
    └── multicast_group_block ──► ResourceAccount (Multicast)

Device
    │
    ├── dz_prefixes[0] ─────────► ResourceAccount (DzIp)
    ├── dz_prefixes[1] ─────────► ResourceAccount (DzIp)
    ├── tunnel_id_bitmap ───────► [inline, 512 bytes]
    └── segment_routing_bitmap ─► [inline, 512 bytes]
```

### Authority Model

Admin operations use the existing `foundation_allowlist` in GlobalState:

```rust
// Existing field in GlobalState (not new)
pub foundation_allowlist: Vec<Pubkey>
```

**Authorization levels:**

| Operation                  | Required Authority                |
| -------------------------- | --------------------------------- |
| CreateResourceAccount      | foundation_allowlist member       |
| SetResourceState           | foundation_allowlist member       |
| BanUser                    | foundation_allowlist member       |
| UpdateDeviceContributor    | foundation_allowlist member       |
| CreateAndActivateUser      | AccessPass owner                  |
| CreateAndActivateLink      | Contributor owner (either device) |
| CreateAndActivateDevice    | Contributor owner                 |
| CreateAndActivateInterface | Contributor owner                 |
| DeleteUser                 | AccessPass owner                  |
| DeleteLink                 | Creator or Contributor owner      |
| DeleteDevice               | Contributor owner                 |
| DeleteInterface            | Contributor owner                 |
| SweepExpiredUsers          | Permissionless                    |

### PDA Seeds

Entity PDAs (aligned with PR #1977 where applicable):

| Entity               | PDA Seeds                                                      |
| -------------------- | -------------------------------------------------------------- |
| User                 | `["doublezero", "user", client_ip, user_type]` (PR #1977)      |
| Link                 | `["doublezero", "link", index]` (existing)                     |
| Interface            | `["doublezero", "interface", index]` (existing)                |
| MulticastGroup       | `["doublezero", "multicast", index]` (existing)                |
| ResourceAccount      | `["doublezero", "resource", resource_type, block_network]`     |
| DzIp ResourceAccount | `["doublezero", "resource", "dz_ip", device_pk, prefix_index]` |

**User PDA change (PR #1977):** The new User PDA uses `client_ip` and `user_type` instead of a global index. This prevents duplicate users for the same client IP and ensures deterministic account generation without race conditions.

### IP Address Computation

Addresses are computed from ResourceAccount bitmaps:

**User tunnel_net:**

```
resource = ResourceAccount(UserTunnelNet)   // manages 169.254.0.0/16
slot = resource.allocate()                  // finds first free bit, sets it
tunnel_net = resource.block + (slot * 2)    // /31 block (2 IPs per user)
```

**Link tunnel_net:**

```
resource = ResourceAccount(LinkTunnelNet)   // manages 172.16.0.0/16
slot = resource.allocate()                  // finds first free bit, sets it
tunnel_net = resource.block + (slot * 2)    // /31 block (2 IPs per link)
```

**DZ IP (per-device):**

```
resource = ResourceAccount(DzIp, device, prefix_index)   // manages device's prefix
slot = resource.allocate()                               // finds first free bit
dz_ip = resource.block + slot                            // /32 address
```

Reserved IPs (.0 network, .1 gateway, broadcast) are pre-marked as allocated during ResourceAccount creation.

**Deallocation:**

```
resource.deallocate(slot)  // clears bit, slot available for reuse
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

### CreateResourceAccount

**Purpose:** Initialize a ResourceAccount for managing an IP block.

**Accounts:**

1. `resource_account` (PDA to create, writable)
2. `globalconfig_account` (for block reference)
3. `device_account` (optional, for DzIp type)
4. `authority` (signer, foundation_allowlist member)
5. `payer` (signer)

**Steps:**

1. Derive PDA from resource_type and block
2. Validate authority is in foundation_allowlist
3. Calculate bitmap size from block prefix length and slot_size
4. Initialize bitmap with reserved slots pre-marked (e.g., .0, .1, broadcast)
5. Create ResourceAccount with allocated_count reflecting reserved slots

### SetResourceState

**Purpose:** Set bitmap state for migration from current allocations.

**Accounts:**

1. `resource_account` (writable) OR `device_account` (writable, for inline bitmaps)
2. `authority` (signer, foundation_allowlist member)

**Parameters:**

- `slots: Vec<u32>` — Slot indices to mark as allocated
- `target: ResourceTarget` — Which bitmap to update (ResourceAccount, TunnelIdBitmap, SegmentRoutingBitmap)

**Steps:**

1. Validate authority is in foundation_allowlist
2. For each slot in slots:
   - Validate slot < total_slots
   - Set bit in bitmap
   - Increment allocated_count
3. Log slots marked as allocated

**Usage:** Called during migration to mark currently-allocated resources in the new bitmap system.

### CreateAndActivateDevice

**Purpose:** Atomically create a device with initialized resource bitmaps.

**Accounts:**

1. `device_account` (PDA to create, writable)
2. `contributor_account`
3. `authority` (signer, contributor owner)
4. `payer` (signer)

**Steps:**

1. Validate contributor is valid
2. Initialize device with zeroed tunnel_id_bitmap and segment_routing_bitmap
3. Set dz_ip_reserved_count based on prefix sizes
4. Create DzIp ResourceAccounts for each prefix in dz_prefixes
5. Set device status to `Activated`

### CreateAndActivateUser

**Purpose:** Atomically creates and activates a user with allocated resources.

**PDA Derivation (aligned with PR #1977):**

```
seeds = ["doublezero", "user", client_ip, user_type]
```

This ensures one user per (client_ip, user_type) tuple. Duplicate attempts fail with `AccountAlreadyExists`.

**Accounts:**

1. `user_account` (PDA to create, writable)
2. `device_account` (writable, for tunnel_id_bitmap)
3. `accesspass_account` (writable)
4. `user_tunnel_resource` (ResourceAccount, writable)
5. `dz_ip_resource` (ResourceAccount for device prefix, writable)
6. `payer` (signer)

**Steps:**

1. Derive PDA and verify `user_account` matches expected address
2. Validate `user_account` does not already exist (data_is_empty)
3. Validate device is `Activated`
4. Validate AccessPass: caller is owner, not expired, under max_users limit
5. Allocate tunnel_id from `device.tunnel_id_bitmap`
6. Allocate tunnel_net slot from `user_tunnel_resource`, compute IP
7. Allocate dz_ip slot from `dz_ip_resource`, compute IP
8. Store slot indices in user account (for deallocation on delete)
9. Create user account with `Activated` status
10. Increment `accesspass.active_user_count`
11. Log allocated tunnel_net, dz_ip, tunnel_id

**User Account Fields (for resource tracking):**

```rust
struct User {
    // ... existing fields ...
    tunnel_net_slot: u32,    // Slot in UserTunnelNet ResourceAccount
    dz_ip_slot: u32,         // Slot in DzIp ResourceAccount
    tunnel_id_slot: u16,     // Slot in Device.tunnel_id_bitmap
}
```

**Error Cases:**

- `AccountAlreadyExists`: User PDA already exists for this (device, client_ip, accesspass)
- `UserTunnelNetExhausted`: All slots allocated in UserTunnelNet ResourceAccount
- `DzIpExhausted`: All slots allocated in device's DzIp ResourceAccount
- `TunnelIdExhausted`: All 4096 tunnel_id slots allocated on device

### CreateAndActivateLink

**Purpose:** Atomically creates and activates a link between two devices.

**PDA Derivation:**

```
(device_lo, device_hi) = sort(device_a_pk, device_b_pk)
seeds = ["link", device_lo, device_hi]
```

Sorting device keys ensures (A→B) and (B→A) resolve to the same PDA, preventing duplicate links.

**Accounts:**

1. `link_account` (PDA to create, writable)
2. `device_a_account` (writable, for tunnel_id_bitmap)
3. `device_b_account` (writable, for tunnel_id_bitmap)
4. `contributor_a_account`, `contributor_b_account`
5. `link_tunnel_resource` (ResourceAccount, writable)
6. `payer` (signer)

**Steps:**

1. Derive link PDA with sorted device keys
2. Validate `link_account` matches expected PDA
3. Validate `link_account` does not already exist (data_is_empty)
4. Validate both devices are `Activated`
5. Validate contributors belong to their respective devices
6. Validate caller is contributor owner for at least one device
7. Allocate tunnel_id from `device_a.tunnel_id_bitmap`
8. Allocate tunnel_id from `device_b.tunnel_id_bitmap`
9. Allocate tunnel_net slot from `link_tunnel_resource`, compute IP
10. Store slot indices in link account (for deallocation on delete)
11. Create link with `Activated` status
12. Log allocated tunnel_net, tunnel_id_a, tunnel_id_b

**Link Account Fields (for resource tracking):**

```rust
struct Link {
    // ... existing fields ...
    tunnel_net_slot: u32,        // Slot in LinkTunnelNet ResourceAccount
    tunnel_id_slot_a: u16,       // Slot in device_a.tunnel_id_bitmap
    tunnel_id_slot_b: u16,       // Slot in device_b.tunnel_id_bitmap
}
```

**Error Cases:**

- `LinkAlreadyExists`: Link PDA already exists between these devices
- `LinkTunnelNetExhausted`: All slots allocated in LinkTunnelNet ResourceAccount
- `TunnelIdExhausted`: One of the devices has all 4096 tunnel_id slots allocated

### CreateAndActivateInterface

**Purpose:** Creates interface on device with allocated resources.

**Accounts:**

1. `interface_account` (PDA to create, writable)
2. `device_account` (writable, for segment_routing_bitmap)
3. `dz_ip_resource` (ResourceAccount, writable, if loopback needs IP)
4. `authority` (signer, contributor owner)
5. `payer` (signer)

**Steps:**

1. Validate device is `Activated`
2. Validate caller is contributor owner
3. If loopback interface:
   - Allocate segment_routing_id from `device.segment_routing_bitmap`
   - Allocate IP from `dz_ip_resource` if configured
4. If physical interface:
   - Set status to unlinked
5. Store slot indices in interface account (for deallocation on delete)
6. Create interface account with `Activated` status
7. Log allocated segment_routing_id, dz_ip (if applicable)

**Interface Account Fields (for resource tracking):**

```rust
struct Interface {
    // ... existing fields ...
    segment_routing_slot: Option<u16>,  // Slot in Device.segment_routing_bitmap
    dz_ip_slot: Option<u32>,            // Slot in DzIp ResourceAccount
}
```

### CreateAndActivateMulticastGroup

**Purpose:** Creates multicast group with allocated address.

**Accounts:**

1. `multicast_account` (PDA to create, writable)
2. `multicast_resource` (ResourceAccount, writable)
3. `authority` (signer)
4. `payer` (signer)

**Steps:**

1. Allocate slot from `multicast_resource`
2. Compute address from `multicast_resource.block + slot`
3. Store slot index in multicast account (for deallocation on delete)
4. Create in `Activated` state
5. Log allocated multicast address

**MulticastGroup Account Fields (for resource tracking):**

```rust
struct MulticastGroup {
    // ... existing fields ...
    multicast_slot: u32,  // Slot in Multicast ResourceAccount
}
```

### DeleteUser

**Purpose:** Closes user account and reclaims allocated resources.

**Accounts:**

1. `user_account` (writable, to close)
2. `device_account` (writable, for tunnel_id_bitmap)
3. `accesspass_account` (writable)
4. `user_tunnel_resource` (ResourceAccount, writable)
5. `dz_ip_resource` (ResourceAccount, writable)
6. `authority` (signer, accesspass owner)

**Steps:**

1. Validate user account exists
2. Validate caller is accesspass owner
3. Deallocate tunnel_id: clear bit at `user.tunnel_id_slot` in `device.tunnel_id_bitmap`
4. Deallocate tunnel_net: clear bit at `user.tunnel_net_slot` in `user_tunnel_resource`
5. Deallocate dz_ip: clear bit at `user.dz_ip_slot` in `dz_ip_resource`
6. Decrement `accesspass.active_user_count`
7. Close account (return lamports to owner)
8. Log deallocated slots

Resources are immediately available for reuse by subsequent CreateAndActivateUser calls.

### DeleteLink

**Purpose:** Closes link account and reclaims allocated resources.

**Accounts:**

1. `link_account` (writable, to close)
2. `device_a_account` (writable, for tunnel_id_bitmap)
3. `device_b_account` (writable, for tunnel_id_bitmap)
4. `link_tunnel_resource` (ResourceAccount, writable)
5. `authority` (signer, creator or contributor owner)

**Authorization:** Creator OR contributor of either device.

**Steps:**

1. Validate link account exists
2. Validate caller is authorized (creator or contributor owner)
3. Deallocate tunnel_id_a: clear bit at `link.tunnel_id_slot_a` in `device_a.tunnel_id_bitmap`
4. Deallocate tunnel_id_b: clear bit at `link.tunnel_id_slot_b` in `device_b.tunnel_id_bitmap`
5. Deallocate tunnel_net: clear bit at `link.tunnel_net_slot` in `link_tunnel_resource`
6. Close account (return lamports to payer)
7. Log deallocated slots

### DeleteDevice

**Purpose:** Closes device account and all associated resources.

**Accounts:**

1. `device_account` (writable, to close)
2. `dz_ip_resources[]` (ResourceAccounts, writable, to close)
3. `authority` (signer, contributor owner)

**Prerequisites:**

- All users on device must be deleted first
- All links involving device must be deleted first
- All interfaces on device must be deleted first

**Steps:**

1. Validate device has no active users (check or require explicit proof)
2. Validate device has no active links
3. Validate device has no active interfaces
4. Close all DzIp ResourceAccounts for this device
5. Close device account (return lamports)

### DeleteInterface

**Purpose:** Closes interface account and reclaims allocated resources.

**Accounts:**

1. `interface_account` (writable, to close)
2. `device_account` (writable, for segment_routing_bitmap)
3. `dz_ip_resource` (ResourceAccount, writable, if interface had IP)
4. `authority` (signer, contributor owner)

**Steps:**

1. Validate interface account exists
2. Validate caller is contributor owner
3. If loopback with segment_routing_id:
   - Deallocate: clear bit at `interface.segment_routing_slot` in `device.segment_routing_bitmap`
4. If interface had dz_ip:
   - Deallocate: clear bit at `interface.dz_ip_slot` in `dz_ip_resource`
5. Close account (return lamports)
6. Log deallocated slots

### DeleteMulticastGroup

**Purpose:** Closes multicast group and reclaims allocated address.

**Accounts:**

1. `multicast_account` (writable, to close)
2. `multicast_resource` (ResourceAccount, writable)
3. `authority` (signer)

**Steps:**

1. Validate multicast account exists
2. Validate caller is authorized
3. Deallocate: clear bit at `multicast.multicast_slot` in `multicast_resource`
4. Close account (return lamports)
5. Log deallocated slot

### BanUser

**Purpose:** Ban a user and reclaim their resources.

**Accounts:**

1. `user_account` (writable)
2. `device_account` (writable, for tunnel_id_bitmap)
3. `user_tunnel_resource` (ResourceAccount, writable)
4. `dz_ip_resource` (ResourceAccount, writable)
5. `authority` (signer, foundation_allowlist member)

**Steps:**

1. Validate authority is in foundation_allowlist
2. Deallocate all resources (same as DeleteUser steps 3-5)
3. Mark user status as `Banned`
4. Log deallocated slots

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

**Current mainnet usage:**

- 717 users using ~1,434 IPs from 169.254.0.0/16 (2% of 65K capacity)
- 121 links using ~242 IPs from 172.16.0.0/16 (<1% of 65K capacity)
- Plenty of headroom for growth

### Concurrent vs Cumulative Capacity

With bitmap-based recycling, capacity is measured in **concurrent** allocations, not cumulative. When a user disconnects and is deleted, their resources are immediately available for reuse.

| Resource               | Concurrent Capacity | Connect/Disconnect Support |
| ---------------------- | ------------------- | -------------------------- |
| User tunnel_net        | ~32K simultaneous   | Unlimited over time        |
| Link tunnel_net        | ~32K simultaneous   | Unlimited over time        |
| Tunnel ID (per device) | ~3,595 per device   | Unlimited over time        |

### ResourceAccount Storage Costs

| Account                | Size       | Rent-exempt (SOL)  |
| ---------------------- | ---------- | ------------------ |
| UserTunnelNet          | ~4.1 KB    | ~0.03 SOL          |
| LinkTunnelNet          | ~4.1 KB    | ~0.03 SOL          |
| Multicast              | ~100 bytes | ~0.001 SOL         |
| DzIp (per device)      | ~100 bytes | ~0.001 SOL         |
| Device bitmap overhead | ~1 KB      | Included in Device |

**Total new storage:** ~8.5 KB global + ~1.1 KB per device

## Migration

### Phase 1: Deploy Program Update

1. Add ResourceAccount type to program
2. Add bitmap fields to Device account
3. Add slot tracking fields to User, Link, Interface, MulticastGroup accounts
4. Program upgrade handles account resizing

### Phase 2: Create ResourceAccounts

Create the global ResourceAccounts:

```
CreateResourceAccount(UserTunnelNet, globalconfig.user_tunnel_block)
CreateResourceAccount(LinkTunnelNet, globalconfig.device_tunnel_block)
CreateResourceAccount(Multicast, globalconfig.multicast_group_block)
```

For each device, create DzIp ResourceAccounts:

```
For each device:
  For each prefix in device.dz_prefixes:
    CreateResourceAccount(DzIp, device, prefix_index)
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
user_tunnel_slots = [ip_to_slot(u.tunnel_net, user_tunnel_block) for u in users]
link_tunnel_slots = [ip_to_slot(l.tunnel_net, link_tunnel_block) for l in links]
# ... etc

# Set bitmap state
SetResourceState(user_tunnel_resource, user_tunnel_slots)
SetResourceState(link_tunnel_resource, link_tunnel_slots)
SetResourceState(multicast_resource, multicast_slots)

For each device:
  SetResourceState(device, TunnelIdBitmap, device_tunnel_id_slots)
  SetResourceState(device, SegmentRoutingBitmap, device_sr_slots)
  SetResourceState(dz_ip_resource, dz_ip_slots)
```

### Phase 4: Deploy New Instructions

1. Add CreateAndActivate\* instructions
2. Add Delete\* instructions with resource reclaim
3. Update SDK to use new instructions
4. Mark old Create + Activate as deprecated

### Phase 5: Deprecate Activator

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

- **~8.5 KB new global state** (ResourceAccounts for UserTunnelNet, LinkTunnelNet, Multicast)
- **~1.1 KB new state per Device** (inline bitmaps + DzIp ResourceAccounts)
- **Full resource recycling** via O(1) bitmap allocation/deallocation
- **Zero breaking changes** to IP ranges or allocation strategy
- **No artificial device limits**
- **Handles high connect/disconnect churn** (users created per-connection)

The design moves allocation on-chain while supporting the full lifecycle of network resources—creation, activation, and deletion with immediate resource reuse.
