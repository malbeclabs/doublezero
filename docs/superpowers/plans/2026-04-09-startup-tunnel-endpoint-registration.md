# Fix Startup Tunnel Endpoint Registration

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent duplicate GRE tunnel pairs by registering implicit tunnel endpoints (device `public_ip`) during activator startup reconstruction.

**Architecture:** The activator's `reserve_user_allocations` rebuilds in-memory state from existing onchain users at startup. It currently only registers explicit `tunnel_endpoint` values, missing users that fall back to the device's `public_ip`. This causes `get_available_tunnel_endpoint` to hand out already-in-use endpoints when a second user with the same `client_ip` arrives on the same device.

**Tech Stack:** Rust (activator crate)

---

### Task 1: Add regression test for startup tunnel endpoint registration

**Files:**
- Modify: `activator/src/processor.rs:534-629` (test module, after existing `test_updating_user_allocations_must_be_reserved_at_startup`)

- [ ] **Step 1: Write failing test**

Add a test that creates two users with the same `client_ip` on the same device — the first with `tunnel_endpoint = UNSPECIFIED` (simulating an existing activated user that uses the device's `public_ip` implicitly). After `reserve_user_allocations`, call `get_available_tunnel_endpoint` for that `client_ip` and assert it does NOT return the device's `public_ip` (since it's already in use).

```rust
/// Regression test: reserve_user_allocations must register implicit tunnel endpoints
/// (device public_ip) for users with unspecified tunnel_endpoint. Otherwise a second
/// user from the same client_ip gets the same endpoint, creating a duplicate GRE tunnel pair.
#[test]
fn test_implicit_tunnel_endpoint_reserved_at_startup() {
    use crate::{ipblockallocator::IPBlockAllocator, states::devicestate::DeviceState};
    use doublezero_sdk::{
        AccountType, Device, DeviceStatus, DeviceType, User, UserStatus, UserType,
    };
    use doublezero_serviceability::state::user::UserCYOA;
    use std::net::Ipv4Addr;

    let device_pubkey = Pubkey::new_unique();
    let device = Device {
        account_type: AccountType::Device,
        owner: Pubkey::new_unique(),
        index: 0,
        reference_count: 0,
        bump_seed: 0,
        contributor_pk: Pubkey::new_unique(),
        location_pk: Pubkey::new_unique(),
        exchange_pk: Pubkey::new_unique(),
        device_type: DeviceType::Hybrid,
        public_ip: [88, 216, 220, 195].into(),
        status: DeviceStatus::Activated,
        metrics_publisher_pk: Pubkey::default(),
        code: "TestDevice".to_string(),
        dz_prefixes: "10.0.0.0/24".parse().unwrap(),
        mgmt_vrf: "default".to_string(),
        interfaces: vec![],
        max_users: 255,
        users_count: 0,
        device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
        desired_status:
            doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        unicast_users_count: 0,
        multicast_subscribers_count: 0,
        max_unicast_users: 0,
        max_multicast_subscribers: 0,
        reserved_seats: 0,
        multicast_publishers_count: 0,
        max_multicast_publishers: 0,
    };

    let client_ip: Ipv4Addr = [64, 130, 32, 201].into();

    let mut device_map: DeviceMap = DeviceMap::new();
    device_map.insert(device_pubkey, DeviceState::new(&device));

    // Existing activated user with unspecified tunnel_endpoint (uses device public_ip implicitly)
    let existing_user = User {
        account_type: AccountType::User,
        owner: Pubkey::new_unique(),
        index: 0,
        bump_seed: 0,
        user_type: UserType::IBRL,
        tenant_pk: Pubkey::new_unique(),
        device_pk: device_pubkey,
        cyoa_type: UserCYOA::GREOverDIA,
        client_ip,
        dz_ip: [10, 0, 0, 1].into(),
        tunnel_id: 509,
        tunnel_net: "169.254.2.14/31".parse().unwrap(),
        status: UserStatus::Activated,
        publishers: vec![],
        subscribers: vec![],
        validator_pubkey: Pubkey::default(),
        tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        tunnel_flags: 0,
        bgp_status: Default::default(),
        last_bgp_up_at: 0,
        last_bgp_reported_at: 0,
    };

    let mut users: HashMap<Pubkey, User> = HashMap::new();
    users.insert(Pubkey::new_unique(), existing_user);

    let mut user_tunnel_ips = IPBlockAllocator::new("169.254.0.0/16".parse().unwrap());
    let mut publisher_dz_ips = IPBlockAllocator::new("148.51.120.0/21".parse().unwrap());

    reserve_user_allocations(&users, &mut device_map, &mut user_tunnel_ips, &mut publisher_dz_ips)
        .expect("reserve_user_allocations should succeed");

    // A second user from the same client_ip should NOT get device public_ip
    // because the first user is already using it
    let device_state = device_map.get(&device_pubkey).unwrap();
    let next_endpoint = device_state.get_available_tunnel_endpoint(client_ip);
    assert_eq!(
        next_endpoint, None,
        "BUG: get_available_tunnel_endpoint returned device public_ip which is already \
         in use by existing user with unspecified tunnel_endpoint — this creates a \
         duplicate GRE tunnel pair"
    );
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cargo test -p doublezero-activator test_implicit_tunnel_endpoint_reserved_at_startup`

Expected: FAIL — the assertion fails because `get_available_tunnel_endpoint` returns `Some(88.216.220.195)` (the device's `public_ip`), since `reserve_user_allocations` didn't register it.

### Task 2: Fix startup registration to include implicit endpoints

**Files:**
- Modify: `activator/src/processor.rs:146-149`

- [ ] **Step 3: Implement the fix**

In `reserve_user_allocations`, change the tunnel endpoint registration to always register the effective endpoint — either the explicit `tunnel_endpoint` or the device's `public_ip`:

```rust
                // Register tunnel endpoint (explicit or implicit via device public_ip)
                // so that get_available_tunnel_endpoint knows what's already in use.
                let effective_endpoint = if user.has_tunnel_endpoint() {
                    user.tunnel_endpoint
                } else {
                    device_state.device.public_ip
                };
                device_state.register_tunnel_endpoint(user.client_ip, effective_endpoint);
```

This replaces the current code at lines 146-149:
```rust
                // Register tunnel endpoint if set
                if user.has_tunnel_endpoint() {
                    device_state.register_tunnel_endpoint(user.client_ip, user.tunnel_endpoint);
                }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cargo test -p doublezero-activator test_implicit_tunnel_endpoint_reserved_at_startup`

Expected: PASS

- [ ] **Step 5: Run full activator test suite**

Run: `cargo test -p doublezero-activator`

Expected: All tests pass.

- [ ] **Step 6: Run formatting and lint**

Run: `make rust-fmt && make rust-lint`

Expected: Clean.

- [ ] **Step 7: Commit**

```bash
git add activator/src/processor.rs
git commit -m "activator: register implicit tunnel endpoints at startup

When rebuilding in-memory state from existing onchain users,
register the device's public_ip as the effective tunnel endpoint
for users with unspecified tunnel_endpoint. Previously only
explicit tunnel endpoints were registered, allowing
get_available_tunnel_endpoint to hand out already-in-use
endpoints when a second user with the same client_ip arrived
on the same device — creating duplicate GRE tunnel pairs that
the controller had to deduplicate by dropping one tunnel."
```
