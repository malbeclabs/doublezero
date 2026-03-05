# Permission System

The Permission system grants named capabilities to specific pubkeys via onchain `Permission`
accounts. It replaces the legacy `GlobalState` allowlist/authority model with a fine-grained,
auditable permission layer. Both models coexist during the transition period; the legacy model is
disabled by setting the `RequirePermissionAccounts` feature flag.

---

## Account Layout

`Permission` is a PDA owned by the serviceability program.

| Field          | Type               | Description                                               |
|----------------|--------------------|-----------------------------------------------------------|
| `account_type` | `u8`               | Discriminator — always `15` (`AccountType::Permission`)   |
| `owner`        | `Pubkey`           | The key that created this account (foundation member)     |
| `bump_seed`    | `u8`               | PDA bump                                                  |
| `status`       | `PermissionStatus` | `Activated` or `Suspended`                                |
| `user_payer`   | `Pubkey`           | The key being granted permissions                         |
| `permissions`  | `u128`             | Bitmask of `permission_flags::*`                          |

**PDA seeds:** `["doublezero", "permission", user_payer_bytes]`

---

## Permission Flags

Flags are a `u128` bitmask. Authorization uses **OR semantics**: a single matching bit is
sufficient.

### Tier 1 — System governance

| Constant             | Bit     | Description                                                         |
|----------------------|---------|---------------------------------------------------------------------|
| `FOUNDATION`         | `1<<0`  | Catch-all legacy flag (maps to `foundation_allowlist`)              |
| `PERMISSION_ADMIN`   | `1<<1`  | Manage Permission accounts (create/update/suspend/resume/delete)    |
| `GLOBALSTATE_ADMIN`  | `1<<13` | Manage GlobalState: feature flags, allowlists, authority keys       |
| `CONTRIBUTOR_ADMIN`  | `1<<14` | Manage Contributors: create, update, delete                         |

### Tier 2 — Infrastructure management

| Constant          | Bit    | Description                                                |
|-------------------|--------|------------------------------------------------------------|
| `INFRA_ADMIN`     | `1<<2` | Manage locations and exchanges                             |
| `NETWORK_ADMIN`   | `1<<3` | Manage devices and links                                   |
| `TENANT_ADMIN`    | `1<<4` | Manage tenants                                             |
| `MULTICAST_ADMIN` | `1<<5` | Manage multicast groups and their allowlists               |
| `RESERVATION`     | `1<<6` | Manage reservations                                        |

### Tier 3 — Operational roles

| Constant            | Bit     | Description                                          |
|---------------------|---------|------------------------------------------------------|
| `ACTIVATOR`         | `1<<7`  | Activate/reject network entities                     |
| `SENTINEL`          | `1<<8`  | Suspend network entities                             |
| `USER_ADMIN`        | `1<<9`  | Administer users (ban, delete, close account)        |
| `ACCESS_PASS_ADMIN` | `1<<10` | Create and modify access passes                      |

### Tier 4 — Technical/automated roles

| Constant        | Bit     | Description                    |
|-----------------|---------|--------------------------------|
| `HEALTH_ORACLE` | `1<<11` | Report device/link health      |
| `QA`            | `1<<12` | QA operations                  |

---

## Authorization Model

Authorization is resolved in `src/authorize.rs` via `authorize()`. Each instruction calls
`authorize()` after consuming its expected accounts. An optional trailing account — the caller's
`Permission` PDA — selects the path:

### New path (Permission account provided)

1. Validate the PDA matches `get_permission_pda(program_id, payer)`.
2. Verify the account is owned by the program and non-empty.
3. Check `permission.status == Activated`.
4. Check `permission.permissions & any_of_flags \!= 0`.

### Legacy path (no Permission account, `RequirePermissionAccounts` not set)

Falls back to `GlobalState` fields:

| Flag                | Legacy check                                                                       |
|---------------------|------------------------------------------------------------------------------------|
| `FOUNDATION`        | `foundation_allowlist.contains(payer)`                                             |
| `QA`                | `qa_allowlist.contains(payer)`                                                     |
| `ACTIVATOR`         | `activator_authority_pk == payer`                                                  |
| `SENTINEL`          | `sentinel_authority_pk == payer`                                                   |
| `HEALTH_ORACLE`     | `health_oracle_pk == payer`                                                        |
| `RESERVATION`       | `reservation_authority_pk == payer`                                                |
| `USER_ADMIN`        | `foundation_allowlist` OR `activator_authority_pk`                                 |
| `ACCESS_PASS_ADMIN` | `foundation_allowlist` OR `sentinel_authority_pk`                                  |
| `NETWORK_ADMIN`     | `foundation_allowlist` OR `activator_authority_pk`                                 |
| `TENANT_ADMIN`      | `foundation_allowlist` OR `sentinel_authority_pk`                                  |
| `MULTICAST_ADMIN`   | `foundation_allowlist` OR `activator_authority_pk` OR `sentinel_authority_pk`      |
| `PERMISSION_ADMIN`  | `foundation_allowlist`                                                             |
| `INFRA_ADMIN`       | `foundation_allowlist`                                                             |
| `GLOBALSTATE_ADMIN` | `foundation_allowlist`                                                             |
| `CONTRIBUTOR_ADMIN` | `foundation_allowlist`                                                             |

### Foundation bypass for `PERMISSION_ADMIN`

Even when `RequirePermissionAccounts` is set (legacy mode disabled), `foundation_allowlist` members
can still call Permission instructions without a Permission account. This prevents the foundation
from being locked out of the permission system when migrating to strict mode.

---

## Instructions

All Permission instructions require `PERMISSION_ADMIN` authorization.

### CreatePermission

Creates a Permission PDA granting `permissions` to `user_payer`.

**Args:** `PermissionCreateArgs { user_payer: Pubkey, permissions: u128 }`

**Accounts:**
```
[0] permission_pda   (writable) — PDA for user_payer
[1] globalstate      (readonly)
[2] payer            (signer)
[3] system_program
[4] payer_permission (optional) — payer's own Permission PDA for new-path authorization
```

**Guards:**
- `permissions \!= 0`
- Account must not already be initialized

### UpdatePermission

Replaces the `permissions` bitmask on an existing Permission account.

**Args:** `PermissionUpdateArgs { permissions: u128 }`

**Accounts:** same layout as Create (without system_program allocation)

**Guards:** `permissions \!= 0`

### SuspendPermission

Sets `status = Suspended`. While suspended, the holder cannot authorize any instruction.

**Args:** `PermissionSuspendArgs {}`

**Guards:** status must currently be `Activated`

### ResumePermission

Sets `status = Activated`.

**Args:** `PermissionResumeArgs {}`

**Guards:** status must currently be `Suspended`

### DeletePermission

Closes the account and refunds rent to the payer.

**Args:** `PermissionDeleteArgs {}`

---

## Adding a New Permission Flag

1. **Define the flag** in `src/state/permission.rs` inside `pub mod permission_flags`:

   ```rust
   /// Can manage Foo accounts: create, update, delete.
   pub const FOO_ADMIN: u128 = 1 << 15;  // next available bit
   ```

   Place it in the appropriate tier with a doc comment describing what it gates.

2. **Add the legacy mapping** in `src/authorize.rs` inside `check_legacy_any()`:

   ```rust
   // FOO_ADMIN in legacy = foundation (adjust to whichever legacy key applies).
   if any_of & permission_flags::FOO_ADMIN \!= 0
       && globalstate.foundation_allowlist.contains(payer)
   {
       return true;
   }
   ```

   Also update the doc comment on `authorize()` to list the new mapping.

3. **Guard the new instructions** by calling `authorize()` with the flag:

   ```rust
   authorize(
       program_id,
       accounts_iter,
       payer_account.key,
       &globalstate,
       permission_flags::FOO_ADMIN,
   )?;
   ```

4. **Update the SDKs:**

   - **Go** (`sdk/serviceability/go/state.go`):
     ```go
     PermissionFlagFooAdmin uint64 = 1 << 15
     ```

   - **TypeScript** (`sdk/serviceability/typescript/serviceability/state.ts`):
     ```ts
     export const PERMISSION_FLAG_FOO_ADMIN = 1n << 15n;
     ```

   - **Python** (`sdk/serviceability/python/serviceability/state.py`):
     ```python
     PERMISSION_FLAG_FOO_ADMIN = 1 << 15
     ```

5. **Add tests:**
   - A legacy-path unit test in `src/authorize.rs` (both allowed and denied cases).
   - An integration test in `tests/` verifying the instruction rejects unauthorized callers.

---

## Migration from Legacy to Strict Mode

1. Create `Permission` accounts for all operators currently using `GlobalState` keys.
2. Verify each operator can authorize instructions via their Permission account (use the new path
   by passing the Permission PDA as the trailing account on each transaction).
3. Set `FeatureFlag::RequirePermissionAccounts` via `SetFeatureFlags`.

Once the flag is set, all instructions require a Permission account. The only exception is
`PERMISSION_ADMIN`: `foundation_allowlist` members can always manage Permission accounts, even in
strict mode, to recover from misconfiguration.
