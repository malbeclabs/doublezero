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
4. **No artificial limits** — No device caps or per-device partitioning
5. **Simple migration** — Set counters to `max(existing) + 1`

## Non-Goals

1. **Changing IP ranges** — Keep using GlobalConfig values (169.254.0.0/16 for users, 172.16.0.0/16 for links)
2. **Per-device allocation** — Keep global pool allocation (matches current activator)
3. **Resource recycling** — Can be added later if needed; current system doesn't have it

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
           │  │ tunnel_id++│      │ dz_ip++     │      │ users++    │  │
           │  │ tunnel_net │      │             │      │            │  │
           │  │   offset++ │      │             │      │            │  │
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
                                         │ Emit UserActivated event
                                         ▼
                                    ┌──────────┐
                                    │Controller│ Polls on-chain state
                                    │          │ (unchanged behavior)
                                    └──────────┘
```

**Key Difference:** No off-chain allocation. All resource assignment happens atomically in a single transaction. The controller continues polling on-chain state as before—the only change is that accounts are now created in `Activated` state.

### Core Insight

The activator maintains simple counters and allocates sequentially from configured IP ranges. We can move these counters on-chain and compute allocations atomically in instructions.

### Account Modifications

#### GlobalState Account (add ~72 bytes)

| Field                     | Type   | Purpose                                            |
| ------------------------- | ------ | -------------------------------------------------- |
| `admin_authority`         | Pubkey | Admin for privileged operations (sweeps, recovery) |
| `next_user_tunnel_offset` | u32    | Next offset in user_tunnel_block                   |
| `next_link_tunnel_offset` | u32    | Next offset in device_tunnel_block                 |
| `next_user_tunnel_id`     | u32    | Next tunnel ID for users (global)                  |
| `next_link_tunnel_id`     | u32    | Next tunnel ID for links                           |
| `next_multicast_offset`   | u32    | Next offset in multicast_group_block               |

**Note:** `admin_authority` is set at program initialization and can be updated via governance. Used for SweepExpiredUsers and UpdateDeviceContributor.

#### Device Account (add ~12 bytes)

| Field                     | Type | Purpose                                                 |
| ------------------------- | ---- | ------------------------------------------------------- |
| `next_dz_ip_offset`       | u32  | Next offset within device's dz_prefix                   |
| `next_segment_routing_id` | u16  | Next segment routing ID for loopback interfaces         |
| `dz_ip_reserved_count`    | u8   | Number of reserved IPs to skip (network, gateway, etc.) |

**Reserved IP handling:** DZ IP allocation skips the first `dz_ip_reserved_count` offsets (typically 2: network address .0 and gateway .1) and the last offset (broadcast). Set during device activation based on prefix size.

### IP Address Computation

Addresses are computed from GlobalConfig ranges (no hardcoded values):

**User tunnel_net:**

```
base = GlobalConfig.user_tunnel_block  // e.g., 169.254.0.0/16
offset = GlobalState.next_user_tunnel_offset
tunnel_net = base + (offset * 2)       // /31 block (2 IPs per user)
```

**Link tunnel_net:**

```
base = GlobalConfig.device_tunnel_block  // e.g., 172.16.0.0/16
offset = GlobalState.next_link_tunnel_offset
tunnel_net = base + (offset * 2)         // /31 block (2 IPs per link)
```

**DZ IP (per-device):**

```
base = Device.dz_prefixes[prefix_index]  // e.g., 10.0.0.0/24
reserved = Device.dz_ip_reserved_count   // e.g., 2 (skip .0 and .1)
offset = Device.next_dz_ip_offset + reserved
dz_ip = base + offset                    // /32 address (starts at .2)
```

Allocation also validates `offset < prefix_size - 1` to avoid broadcast address.

## New Instructions

### CreateAndActivateUser

**Purpose:** Atomically creates and activates a user.

**PDA Derivation:**

```
seeds = ["user", device_pk, client_ip, accesspass_pk]
```

This ensures one user per (device, client_ip, accesspass) tuple. Duplicate attempts fail with `AccountAlreadyExists`.

**Accounts:**

1. `user_account` (PDA to create, writable)
2. `device_account` (writable)
3. `accesspass_account` (writable)
4. `globalstate_account` (writable)
5. `globalconfig_account`
6. `payer` (signer)

**Steps:**

1. Derive PDA and verify `user_account` matches expected address
2. Validate `user_account` does not already exist (data_is_empty)
3. Validate device is `Activated`
4. Validate AccessPass: caller is owner, not expired, under max_users limit
5. Validate prefix_index within device.dz_prefixes bounds
6. Validate range capacity: `globalstate.next_user_tunnel_offset * 2 < globalconfig.user_tunnel_block.host_count()`
7. Validate DZ IP capacity: `device.next_dz_ip_offset + reserved < prefix.host_count() - 1`
8. Allocate tunnel_id: `globalstate.next_user_tunnel_id++`
9. Allocate tunnel_net: compute from `globalconfig.user_tunnel_block + (globalstate.next_user_tunnel_offset++ * 2)`
10. Allocate dz_ip: compute from `device.dz_prefixes[prefix_index] + (device.next_dz_ip_offset++ + reserved)`
11. Create user account with `Activated` status
12. Increment `accesspass.active_user_count`
13. Emit `UserActivated` event

**Error Cases:**

- `AccountAlreadyExists`: User PDA already exists for this (device, client_ip, accesspass)
- `UserTunnelNetExhausted`: Global user_tunnel_block range is full
- `DzIpExhausted`: Device's dz_prefix for this index is full

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
2. `device_a_account`, `device_b_account`
3. `contributor_a_account`, `contributor_b_account`
4. `globalstate_account` (writable)
5. `globalconfig_account`
6. `payer` (signer)

**Steps:**

1. Derive link PDA with sorted device keys
2. Validate `link_account` matches expected PDA
3. Validate `link_account` does not already exist (data_is_empty)
4. Validate both devices are `Activated`
5. Validate contributors belong to their respective devices
6. Validate caller is contributor owner for at least one device
7. Validate range capacity: `globalstate.next_link_tunnel_offset * 2 < globalconfig.device_tunnel_block.host_count()`
8. Allocate tunnel_id: `globalstate.next_link_tunnel_id++`
9. Allocate tunnel_net: compute from `globalconfig.device_tunnel_block + (globalstate.next_link_tunnel_offset++ * 2)`
10. Create link with `Activated` status
11. Emit `LinkActivated` event

**Error Cases:**

- `LinkAlreadyExists`: Link PDA already exists between these devices
- `LinkTunnelNetExhausted`: Global device_tunnel_block range is full

### CreateAndActivateInterface

**Purpose:** Creates interface on device with segment routing ID.

**Steps:**

1. Validate device is `Activated`
2. If loopback: allocate `device.next_segment_routing_id++`
3. If physical: set status to unlinked
4. Add interface to device
5. Emit event

### CreateAndActivateMulticastGroup

**Purpose:** Creates multicast group.

**Steps:**

1. Allocate offset: `globalstate.next_multicast_offset++`
2. Compute address from `globalconfig.multicast_group_block + offset`
3. Create in `Activated` state
4. Emit event

### DeleteUser

**Purpose:** Closes user account.

**Steps:**

1. Validate account exists
2. Validate caller is owner
3. Decrement `accesspass.active_user_count`
4. Close account (return lamports to owner)
5. Emit `UserDeleted` event

**Note:** Resources (tunnel_id, tunnel_net, dz_ip) are not recycled. Counters continue monotonically. This matches current activator behavior.

### DeleteLink

**Purpose:** Closes link account.

**Authorization:** Creator OR contributor of either device.

**Steps:**

1. Validate link account exists
2. Validate caller is authorized (creator or contributor owner)
3. Close account
4. Emit `LinkDeleted` event

### SweepExpiredUsers

**Purpose:** Batch mark users as OutOfCredits when AccessPass expires.

**Authorization:** AccessPass owner OR `globalstate.admin_authority` only. This prevents griefing where arbitrary callers mass-expire users.

**Accounts:**

1. `accesspass_account` (writable)
2. `globalstate_account` (for admin_authority check)
3. `user_accounts` (writable, up to 10)
4. `caller` (signer)

**Steps:**

1. Validate caller is `accesspass.owner` OR `globalstate.admin_authority`
2. Check `accesspass.expiry_time < current_timestamp`
3. Mark accesspass status as `Expired` if not already
4. For each user account provided (up to 10):
   - Skip if user.accesspass_pk != accesspass_account.key
   - Skip if already OutOfCredits (idempotent)
   - Mark as OutOfCredits
   - Emit `UserExpired` event
5. Emit `SweepCompleted` event with count

**Lazy Invalidation:** Users on expired AccessPasses are also blocked at CreateAndActivateUser (opportunistic check). Sweeps are for explicit cleanup; users can still be lazily invalidated when they attempt operations.

### UpdateDeviceContributor (Admin)

**Purpose:** Recovery path when `device.contributor_pk` becomes stale or incorrect.

**Authorization:** `globalstate.admin_authority` only.

**Accounts:**

1. `device_account` (writable)
2. `globalstate_account`
3. `new_contributor_account` (must exist and be valid)
4. `admin` (signer, must match globalstate.admin_authority)

**Steps:**

1. Validate caller is `globalstate.admin_authority`
2. Validate new_contributor_account exists and is valid Contributor type
3. Update `device.contributor_pk` to new contributor
4. Emit `DeviceContributorUpdated` event

**Usage:** Operational recovery only. Use when original contributor is deleted, ownership transferred, or device was created with incorrect reference.

## AccessPass Expiration

Two mechanisms handle expiration:

1. **Opportunistic check:** CreateAndActivateUser validates expiry before creating users
2. **Batch sweep:** Owner/admin calls SweepExpiredUsers to mark existing users

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

| Resource        | Scope      | Capacity          | Source                         |
| --------------- | ---------- | ----------------- | ------------------------------ |
| User tunnel_net | Global     | ~32K /31 blocks   | 169.254.0.0/16 (configurable)  |
| Link tunnel_net | Global     | ~32K /31 blocks   | 172.16.0.0/16 (configurable)   |
| User tunnel_id  | Global     | 4 billion         | u32 counter                    |
| Link tunnel_id  | Global     | 4 billion         | u32 counter                    |
| DZ IP           | Per device | Depends on prefix | Device's dz_prefixes           |
| Multicast       | Global     | ~256 addresses    | 233.84.178.0/24 (configurable) |
| **Devices**     | **Global** | **Unlimited**     | No artificial cap              |

**Current mainnet usage:**

- 717 users using ~1,434 IPs from 169.254.0.0/16 (2% of 65K capacity)
- 121 links using ~242 IPs from 172.16.0.0/16 (<1% of 65K capacity)
- Plenty of headroom for growth

### Exhaustion Analysis

**User tunnel_net (primary constraint):**

| Scenario   | Users  | Capacity Used | Runway    |
| ---------- | ------ | ------------- | --------- |
| Current    | 717    | 2.2%          | -         |
| 10x growth | 7,170  | 22%           | OK        |
| 50x growth | 35,850 | 109%          | Exhausted |

At current 169.254.0.0/16 (/31 allocation), capacity supports ~32K concurrent users. With no recycling, churn consumes capacity permanently.

**Exhaustion response:**

1. **Monitoring:** Emit warning events when capacity reaches 50%, 75%, 90%
2. **Expand range:** Update GlobalConfig.user_tunnel_block to larger range (e.g., add 10.254.0.0/16)
3. **Add recycling:** Implement free lists (see Future Enhancements) to reclaim deleted resources

**Why no recycling initially:**

- Current activator doesn't recycle either (same behavior)
- Adds complexity (~100 lines for free lists)
- Current 2% utilization provides years of runway at projected growth
- Can be added incrementally without breaking changes

**Migration safety:** The migration script validates `max_existing_offset < block_capacity` before setting counters, preventing silent overflow from misconfigured ranges.

## Migration

### Phase 1: Deploy Program Update

Add new fields to GlobalState and Device accounts. Program upgrade handles account resizing.

### Phase 2: Initialize Counters

Run one-time migration script:

1. Query GlobalConfig for current IP ranges
2. Query all activated users, compute max tunnel_net offset from user_tunnel_block
3. Query all activated links, compute max tunnel_net offset from device_tunnel_block
4. Query all devices, compute max dz_ip offset per device per prefix
5. **Validate:** Ensure all computed offsets fit within configured ranges
6. Set counters to max + 1

```
GlobalState:
  admin_authority = <configured admin pubkey>
  next_user_tunnel_offset = max(user.tunnel_net offsets) + 1
  next_link_tunnel_offset = max(link.tunnel_net offsets) + 1
  next_user_tunnel_id = max(user.tunnel_id) + 1
  next_link_tunnel_id = max(link.tunnel_id) + 1

Per Device:
  next_dz_ip_offset = max(user.dz_ip offset for this device) + 1
  dz_ip_reserved_count = 2  // Skip .0 (network) and .1 (gateway)
```

**Validation checks:**

- `next_user_tunnel_offset * 2 < globalconfig.user_tunnel_block.host_count()`
- `next_link_tunnel_offset * 2 < globalconfig.device_tunnel_block.host_count()`
- Per device: `next_dz_ip_offset + reserved < prefix.host_count() - 1`

Migration fails with clear error if any validation fails, preventing silent overflow.

### Phase 3: Deploy New Instructions

1. Add CreateAndActivate\* instructions
2. Add SweepExpiredUsers
3. Update SDK to use new instructions
4. Mark old Create + Activate as deprecated

### Phase 4: Deprecate Activator

1. New entities use atomic instructions
2. Process remaining pending entities via activator
3. Shut down activator when queue empty

## Security Considerations

| Threat                  | Mitigation                                |
| ----------------------- | ----------------------------------------- |
| Counter manipulation    | Only program can increment; PDAs          |
| Resource exhaustion     | Error when counter exceeds range capacity |
| Front-running           | Atomic allocation; first valid tx wins    |
| Unauthorized activation | Same auth as current instructions         |
| Double-delete           | Account existence check                   |

## Future Enhancements (Not in Scope)

If resource recycling becomes necessary due to high churn:

1. **Free lists** — Add small LIFO arrays to recycle recently-deleted resources
2. **Bitmap tracking** — Add PDA accounts with bitmaps for full recycling

These can be added incrementally without breaking changes.

## Conclusion

This RFC eliminates the activator service with minimal changes:

- **~72 bytes new state** on GlobalState
- **~12 bytes new state** per Device
- **~300 lines of new code**
- **Zero breaking changes** to IP ranges or allocation strategy
- **No artificial device limits**
- **Simple migration** (set counters to max + 1)

The design preserves the existing allocation model while moving coordination on-chain, achieving the core goal of eliminating off-chain infrastructure.
