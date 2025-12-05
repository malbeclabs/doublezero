# RFC-11: On-Chain Activation

**Status:** Draft

## Objective

- Eliminate the activator service by moving its allocation state on-chain
- Two-phase approach: (1) stateless activator, (2) no activator
- Each phase is independently deployable with clear rollback paths
- Zero breaking changes to IP ranges or allocation strategy

## Motivation

The current activator service introduces operational complexity:

- Off-chain polling and WebSocket connections
- Race conditions between allocation and activation
- Single point of failure for resource allocation
- Complex state reconciliation on restart
- Deadlock incidents reported via internal monitoring

## Goals

1. **Eliminate the activator** — No polling, no WebSocket, no off-chain allocation
2. **Incremental migration** — Two independent phases with clear rollback paths
3. **Zero breaking changes** — Same IP ranges, same allocation strategy
4. **Full resource recycling** — Reclaim IPs and IDs on deletion
5. **Simple rollback** — Each phase can be reverted independently

## Non-Goals

1. Changing IP ranges (keep 169.254.0.0/16 for users, 172.16.0.0/16 for links)
2. New combined instructions (reuse existing Create + Activate composed in transactions)

---

## Architecture Overview

### Current Flow

```
┌─────────┐     1. CreateUser       ┌──────────┐
│  Client │ ───────────────────────>│ On-Chain │
└─────────┘                         │ Program  │
                                    └────┬─────┘
                                         │ User created (status=Pending)
                                         v
          2. Poll for pending       ┌──────────┐
             accounts               │Activator │ Off-chain service
                                    │ Service  │ - Local state
          3. Allocate locally       └────┬─────┘ - State reconciliation on restart
             (tunnel_net, dz_ip)         │
                                         │
          4. ActivateUser ───────────────┘
                                    Race condition risk!
```

### Phase 1: Stateless Activator

```
┌─────────┐     1. CreateUser       ┌──────────┐
│  Client │ ───────────────────────>│ On-Chain │
└─────────┘                         │ Program  │
                                    └────┬─────┘
                                         │ User created (status=Pending)
                                         v
          2. Poll for pending       ┌──────────┐
             accounts               │Activator │ Stateless service
                                    │ Service  │ - No local state
          3. AllocateResource       └────┬─────┘ - Restartable without reconciliation
             (on-chain)                  │
                                    ┌────v─────────────┐
          4. Returns slot           │ResourceExtension │ On-chain bitmaps
                                    └──────────────────┘
          5. ActivateUser ───────────────┘
```

**Key change:** Allocation state moves on-chain. Activator becomes stateless.

### Phase 2: No Activator

```
┌─────────┐                         ┌──────────┐
│  Client │                         │ On-Chain │
└────┬────┘                         │ Program  │
     │                              └──────────┘
     │  TX 1: AllocateResource (TunnelId, UserTunnelNet, DzIp)
     │        → Returns allocated slots
     │
     │  TX 2: CreateUser + ActivateUser (atomic)
     │        → User never visible as Pending
     │
     └────────────────────────────────────────────┘
```

**Key change:** Remove `activator_authority_pk` check. Client does what activator did.

---

## Phase 1: Stateless Activator

### What Changes

- New `ResourceExtension` account type with bitmap-based allocation
- New instructions: `CreateResourceExtension`, `AllocateResource`, `ReleaseResource`, `SetResourceState`
- Activator removes local state, uses on-chain allocation instead
- Activator becomes restartable without state reconciliation

### ResourceExtension Account

```rust
struct ResourceExtension {
    account_type: AccountType,
    associated_with: Pubkey,         // Device PK or Pubkey::default() for global
    id_allocations: Vec<IdBitmap>,   // TunnelId, SegmentRoutingId
    ip_allocations: Vec<IpBlockBitmap>, // UserTunnelNet, LinkTunnelNet, DzIp, Multicast
}
```

**Scope:**

| Scope      | PDA Seeds                             | Contains                                |
| ---------- | ------------------------------------- | --------------------------------------- |
| Global     | `["resource_ext", Pubkey::default()]` | UserTunnelNet, LinkTunnelNet, Multicast |
| Per-device | `["resource_ext", device_pk]`         | TunnelId, SegmentRoutingId, DzIp        |

### New Instructions

| Instruction             | Purpose                          | Authorization                               |
| ----------------------- | -------------------------------- | ------------------------------------------- |
| CreateResourceExtension | Initialize ResourceExtension PDA | foundation_allowlist                        |
| AllocateResource        | Allocate slot from bitmap        | activator_authority OR foundation_allowlist |
| ReleaseResource         | Release slot back to bitmap      | activator_authority OR foundation_allowlist |
| SetResourceState        | Batch set bitmap (migration)     | foundation_allowlist only                   |

### Migration Steps

1. Deploy program update with ResourceExtension and new instructions
2. Create global ResourceExtension (UserTunnelNet, LinkTunnelNet, Multicast)
3. Create per-device ResourceExtensions (TunnelId, SegmentRoutingId, DzIp)
4. Initialize bitmaps from existing allocations via `SetResourceState`
5. Deploy updated activator using on-chain allocation
6. Verify activator processes pending entities correctly

---

## Phase 2: Permissionless Activation

### What Changes

Remove this check from activate/close instructions:

```rust
if globalstate.activator_authority_pk != *payer_account.key {
    return Err(DoubleZeroError::NotAllowed.into());
}
```

**That's it.** No signature changes, no CPI, no new account fields.

### Files to Modify

- `process_activate_user` (activate.rs:81-83)
- `process_activate_link` (activate.rs:81-83)
- `process_activate_device` (activate.rs:46-48)
- `process_activate_device_interface` (activate.rs:64-66)
- `process_activate_multicastgroup` (activate.rs:60-62)
- `process_closeaccount_*` and `process_ban_user`

### Authorization After Removal

| Instruction            | Existing Validation                                          |
| ---------------------- | ------------------------------------------------------------ |
| ActivateUser           | AccessPass owner (via `accesspass.user_payer == user.owner`) |
| ActivateLink           | Devices must be Activated, interfaces must be Unlinked       |
| ActivateDevice         | Device must be Pending                                       |
| ActivateInterface      | Device must be Activated                                     |
| ActivateMulticastGroup | (needs foundation_allowlist check added)                     |
| CloseAccount\*         | (needs owner/contributor checks added)                       |

### Client Flow

```rust
// TX 1: Allocate resources
let alloc_tx = Transaction::new([
    AllocateResource { resource_ext: device_ext, resource_type: TunnelId },
    AllocateResource { resource_ext: global_ext, resource_type: UserTunnelNet },
    AllocateResource { resource_ext: device_ext, resource_type: DzIp },
]);
let result = client.send_transaction(alloc_tx)?;

// Parse allocated slots from transaction logs (return data only preserves last instruction)
let (tunnel_id_slot, tunnel_net_slot, dz_ip_slot) = parse_allocation_logs(&result)?;

// TX 2: Create + Activate (atomic)
let activate_tx = Transaction::new([
    CreateUser { user_type, cyoa_type, client_ip },
    ActivateUser {
        tunnel_id: tunnel_id_slot + 500,
        tunnel_net: slot_to_ip(tunnel_net_slot, ...),
        dz_ip: slot_to_ip(dz_ip_slot, ...),
    },
]);
client.send_transaction(activate_tx)?;
```

**Note:** Solana return data only preserves the last instruction's output. Clients should parse transaction logs to retrieve all allocated slots, or submit separate allocation transactions.

### Client Deletion Flow

```rust
// TX 1: Release resources (order doesn't matter)
let release_tx = Transaction::new([
    ReleaseResource { resource_ext: global_ext, resource_type: UserTunnelNet, slot },
    ReleaseResource { resource_ext: device_ext, resource_type: DzIp, slot },
    ReleaseResource { resource_ext: device_ext, resource_type: TunnelId, slot },
]);
client.send_transaction(release_tx)?;

// TX 2: Close account
CloseAccountUser { ... }.execute(client)?;
```

### AllocateResource Authorization Update

**Phase 1:** activator_authority OR foundation_allowlist
**Phase 2:** + AccessPass owner (user resources) OR Contributor owner (device resources)

**Account changes for Phase 2:** Add optional `accesspass_account` to AllocateResource/ReleaseResource to validate user resource ownership. For device resources, add optional `contributor_account`.

### Migration Steps

1. Deploy program update removing authority checks
2. Update AllocateResource/ReleaseResource to allow AccessPass/Contributor owners
3. Update SDK/CLI to implement client-side allocation flow
4. Keep activator running for old clients during transition
5. Monitor activator queue - should drain as clients upgrade
6. Shut down activator when Pending count reaches zero

---

## Rollback Plan

### Phase 1 Rollback

1. **Immediate:** Deploy activator with local state restored (ignores ResourceExtension)
2. **Full rollback:** Continue running activator with local state indefinitely
3. **State recovery:** `SetResourceState` can reinitialize from on-chain entity data

### Phase 2 Rollback

1. Deploy program re-enabling `activator_authority_pk` checks
2. Set `activator_authority_pk` in GlobalState
3. Restart activator to resume processing Pending entities

### Decision Matrix

| Symptom                            | Phase | Action                             |
| ---------------------------------- | ----- | ---------------------------------- |
| ResourceExtension allocation fails | 1     | Deploy activator with local state  |
| Bitmap corruption suspected        | 1     | Reinitialize with SetResourceState |
| Permissionless activation abuse    | 2     | Re-enable authority checks         |
| Client transaction failures        | 2     | Downgrade SDK, re-enable activator |

---

## Security Considerations

| Threat                  | Mitigation                                                   |
| ----------------------- | ------------------------------------------------------------ |
| Bitmap manipulation     | Only program can modify; PDAs enforce ownership              |
| Resource exhaustion     | Error when all slots allocated; monitoring alerts            |
| Double allocation       | Bitmap bit already set check                                 |
| Double deallocation     | Bitmap bit not set check                                     |
| Front-running           | Atomic allocation; first valid tx wins                       |
| Unauthorized activation | Phase 1: activator_authority; Phase 2: existing checks apply |
| Griefing via allocation | Phase 2: Only AccessPass/Contributor owners can allocate     |
| Allocated but not used  | TX1 succeeds, TX2 fails → slots wasted until released        |

---

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

| Account                      | Size    | Rent-exempt (SOL) |
| ---------------------------- | ------- | ----------------- |
| Global ResourceExtension     | ~8.2 KB | ~0.06 SOL         |
| Per-device ResourceExtension | ~1.1 KB | ~0.008 SOL        |

**Total:** ~8.2 KB + (~1.1 KB × 72 devices) = ~87 KB

---

## Summary

| Phase   | What Changes                            | Activator State      | Rollback            |
| ------- | --------------------------------------- | -------------------- | ------------------- |
| Current | —                                       | Stateful (local)     | —                   |
| Phase 1 | Add ResourceExtension, new instructions | Stateless (on-chain) | Restore local state |
| Phase 2 | Remove activator_authority_pk checks    | Eliminated           | Re-enable checks    |

**Phase 1 benefits:** Stateless activator, no reconciliation on restart, allocation visible on-chain

**Phase 2 benefits:** No activator to operate, atomic create+activate, lower latency, minimal code changes

---

## Appendix: Technical Details

### Bitmap Structures

```rust
struct IdBitmap {
    id_type: IdType,                 // TunnelId, SegmentRoutingId
    start_offset: u16,               // e.g., 500 for tunnel IDs
    bitmap: [u64; 64],               // 4,096 slots
    allocated_count: u16,
}

struct IpBlockBitmap {
    block_type: IpBlockType,         // UserTunnelNet, LinkTunnelNet, DzIp, Multicast
    block: Ipv4Network,              // e.g., 169.254.0.0/16
    slot_size: u8,                   // 0=/32, 1=/31, 2=/30
    reserved_start: u8,              // Slots to skip at start
    reserved_end: u8,                // Slots to skip at end
    bitmap: Vec<u64>,                // 1 bit per slot
    allocated_count: u32,
}
```

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

| ResourceExtension Scope | ID Bitmaps                                 | IP Blocks                                                    | Size    |
| ----------------------- | ------------------------------------------ | ------------------------------------------------------------ | ------- |
| Global                  | None                                       | UserTunnelNet (4 KB), LinkTunnelNet (4 KB), Multicast (32 B) | ~8.2 KB |
| Per-device              | TunnelId (512 B), SegmentRoutingId (512 B) | DzIp (32 B each)                                             | ~1.1 KB |

### Activator Code Changes (Phase 1)

**User Activation - Before:**

```rust
let mut user_tunnel_ips = IPBlockAllocator::new(config.user_tunnel_block);
let mut device_state = DeviceState::new(&device);

let tunnel_net = user_tunnel_ips.next_available_block(0, 2)?;
let tunnel_id = device_state.get_next_tunnel_id();
let dz_ip = device_state.get_next_dz_ip()?;
ActivateUserCommand { tunnel_id, tunnel_net, dz_ip }.execute(client)?;
```

**User Activation - After:**

```rust
let tunnel_id_slot = AllocateResourceCommand {
    resource_ext: device_resource_ext,
    resource_type: ResourceType::TunnelId,
}.execute(client)?;
let tunnel_id = tunnel_id_slot + 500;

let tunnel_net_slot = AllocateResourceCommand {
    resource_ext: global_resource_ext,
    resource_type: ResourceType::UserTunnelNet,
}.execute(client)?;
let tunnel_net = slot_to_ip(tunnel_net_slot, user_tunnel_block, 1, 2);

let dz_ip_slot = AllocateResourceCommand {
    resource_ext: device_resource_ext,
    resource_type: ResourceType::DzIp,
}.execute(client)?;
let dz_ip = slot_to_ip(dz_ip_slot, device.dz_prefixes[0], 0, 2);

ActivateUserCommand { tunnel_id, tunnel_net, dz_ip }.execute(client)?;
```

**User Deletion - After:**

```rust
let tunnel_net_slot = ip_to_slot(user.tunnel_net.ip(), user_tunnel_block, 1, 2);
let dz_ip_slot = ip_to_slot(user.dz_ip, device.dz_prefixes[0], 0, 2);
let tunnel_id_slot = user.tunnel_id - 500;

ReleaseResourceCommand {
    resource_ext: global_resource_ext,
    resource_type: ResourceType::UserTunnelNet,
    slot: tunnel_net_slot,
}.execute(client)?;

ReleaseResourceCommand {
    resource_ext: device_resource_ext,
    resource_type: ResourceType::DzIp,
    slot: dz_ip_slot,
}.execute(client)?;

ReleaseResourceCommand {
    resource_ext: device_resource_ext,
    resource_type: ResourceType::TunnelId,
    slot: tunnel_id_slot,
}.execute(client)?;

CloseAccountUserCommand { pubkey }.execute(client)?;
```

### New Device Onboarding (Post-Migration)

1. Foundation calls `CreateDevice`
2. Foundation calls `CreateResourceExtension` for the new device
3. Activator begins processing users/links for the new device

Link, Interface, MulticastGroup flows follow similar patterns.
