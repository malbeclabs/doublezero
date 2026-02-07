# RFC-13: Per-Device Unicast and Multicast User Limits

## Summary

**Status: Draft**

This RFC adds separate per-device limits for unicast and multicast users, allowing operators to control each user type independently rather than relying on a combined `max_users` limit.

## Motivation

Multicast traffic can have significantly higher bandwidth utilization than unicast due to amplification—a single multicast stream is replicated to many subscribers. The existing `max_users` limit applies to all user types combined, giving operators no way to manage this difference. Separate limits allow operators to cap multicast users based on bandwidth capacity while allowing more unicast users on the same device.

## Alternatives Considered

1. **Single combined limit only** - Keep using `max_users` for all user types. Simple but doesn't address the bandwidth disparity between unicast and multicast traffic.

2. **Percentage-based limits** - Express multicast limit as a percentage of `max_users` (e.g., "max 20% multicast"). More complex to implement and reason about; absolute numbers are clearer for operators.

3. **Bandwidth-based limits** - Limit by estimated bandwidth rather than user count. Requires traffic estimation and is harder to enforce deterministically onchain.

4. **Separate tunnel ID pools** - Partition the tunnel ID range between user types. Adds complexity to the controller and reduces flexibility; a device with few multicast users couldn't reclaim those IDs for unicast.

The per-type count approach (option chosen) provides simple, deterministic limits while keeping the shared tunnel ID pool for maximum flexibility.

## Design

### Device Account Fields

Four new fields on the Device account:

```rust
pub struct Device {
    // ... existing fields ...
    pub users_count: u16,             // Total users (kept for backward compatibility)
    pub max_users: u16,               // Total max (kept for backward compatibility)

    // Per-type user counts and limits
    pub unicast_users_count: u16,     // Count of non-multicast users
    pub multicast_users_count: u16,   // Count of multicast users
    pub max_unicast_users: u16,       // Per-device unicast limit
    pub max_multicast_users: u16,     // Per-device multicast limit
}
```

### Limit Enforcement

When creating a user, the onchain program checks `max_users` first, then the per-type limit. The `max_users` check takes precedence—if it blocks, type-specific limits are not evaluated. Counters are incremented on user creation and decremented on user close.

New error codes:
- `MaxUnicastUsersExceeded` (73)
- `MaxMulticastUsersExceeded` (74)

The doublezerod client also checks limits before submitting a transaction, providing immediate feedback. The onchain program remains the source of truth.

### CLI

**View limits (device get/list):**
```
max_users: 255
users_count: 45
max_unicast_users: 80
unicast_users_count: 35
max_multicast_users: 48
multicast_users_count: 10
```

**Set limits (device update):**
```bash
doublezero device update --pubkey <device> --max-unicast-users 100 --max-multicast-users 28
```

## Code Impact

### Changed Components

| Component | Changes |
|-----------|---------|
| **Onchain program** | `user/create.rs`: limit checking + counter increment; `user/closeaccount.rs`: counter decrement; `device/update.rs`: set limits |
| **Device state** | New fields with Borsh deserialization defaults |
| **CLI** | Display new fields in `device get/list`; new flags for `device update` |
| **Client (doublezerod)** | Pre-flight capacity check before user creation |

### Unchanged Components

| Component | Reason |
|-----------|--------|
| **Controller** | Limits are enforced onchain; controller doesn't partition tunnel IDs |
| **Activator** | No changes needed for limit enforcement |
| **Tunnel ID pool** | All user types share the single pool (500-627) |

## Rollout

### Phase 1: Deploy (Current)
- Deploy code with 0 = unlimited semantics
- All existing devices default to 0 (unlimited) for new fields
- No operator action required

### Phase 2: Configure Limits
- Operators set limits per-device via `device update`
- Devices without explicit limits remain unlimited

### Phase 3: Enforce Zero Semantics
- Change 0 semantics to mean "no users of this type allowed"
- Requires all devices to have explicit limits configured first

## Migration

No explicit migration required:

1. **Borsh deserialization defaults** - Existing Device accounts get 0 for all new fields when read
2. **First write persists defaults** - Next update to a device serializes the new fields
3. **No ResourceExtension changes** - All user types share the existing tunnel ID pool

## Backward Compatibility

- Existing users and tunnel IDs unaffected
- Existing devices default to 0 (unlimited during rollout)
- `max_users` and `users_count` maintained for total tracking
- Old clients continue to work; onchain program handles enforcement

## Open Questions

1. **Phase 3 timing** - When should we change 0 semantics from "unlimited" to "disallowed"? This requires coordination to ensure all devices have explicit limits configured first.

2. **Global default limits** - Should there be a global config for default per-type limits applied to new devices? Currently each device must be configured individually.

3. **Limit change restrictions** - Should we prevent lowering limits below current user counts? Currently allowed (existing users remain, but no new ones can join).
