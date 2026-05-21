# RFC-21: Extending Tenants to Multicast Use Cases

## Summary

**Status: Draft**

Today `Tenant` accounts are a unicast-only construct: every tenant allocates a VRF ID from the global pool of 1024, and the rest of its fields exist to support unicast routing isolation. This RFC extends the tenant so that the same account can group the network resources of any product use case, regardless of whether it carries unicast traffic, multicast traffic, or both. The unicast behavior is preserved unchanged; multicast becomes an additive extension.

The extension is realized with three pieces: a small flag on `Tenant` declaring which traffic classes it participates in, an optional `tenant_pk` on `MulticastGroup` create and update, and a widened `set_access_pass` authorization so tenant administrators can manage their own access passes. Together, these turn the tenant into the unit of resource grouping per use case, owned and operated by the team running that use case.

## Motivation

The tenant is the natural place to group everything that belongs to a single customer or product use case: users, access passes, billing configuration, administrative authority. In practice, however, the tenant is tied to unicast routing today. It always allocates a VRF ID, and there is no mechanism to attach multicast resources to it.

As more use cases adopt the tenant as their grouping construct, two limitations surface:

1. **The VRF pool becomes the bottleneck.** The global pool of 1024 VRF IDs is consumed even by tenants that exist purely to group multicast publishers, subscribers, and per-tenant access permissions and never carry unicast traffic.
2. **Self-service operation is blocked.** Only foundation, sentinel, or feed authorities can create the access passes that grant users into a tenant. The team running the use case cannot onboard their own users without going through a foundation key.

This RFC extends the tenant from a unicast-only construct to a use-case-level grouping primitive: the tenant declares which traffic classes it participates in, owns the resources of those classes, and is administered by its own keys. Existing tenants continue to behave exactly as before; multicast and self-service administration are additive extensions.

## New Terminology

- **Use-case tenant**: a tenant whose purpose is to group all of the resources associated with a single product use case (for example, a validator fleet, a content distribution service, an exchange feed). It may participate in unicast traffic, multicast traffic, or both.
- **Tenant capability**: a boolean attribute of a tenant declaring support for one class of traffic. Two are defined initially: `UNICAST` (allocates a VRF ID) and `MULTICAST` (owns multicast groups). The set is open to future additions.
- **Unicast tenant**: a tenant with the `UNICAST` capability set. Allocates a VRF ID. Behaves identically to today's tenants.
- **Multicast tenant**: a tenant with the `MULTICAST` capability set. Holds no VRF ID. Multicast groups can declare it as their owner.
- **Dual tenant**: a tenant with both capabilities. Allocates a VRF ID and is eligible to own multicast groups.

## Alternatives Considered

1. **Do nothing.** Keep tenants unicast-only and introduce a separate grouping primitive for multicast use cases. Forces every new use case to invent its own model and defeats the goal of one consistent per-use-case construct.

2. **Two boolean fields (`unicast_enabled`, `multicast_enabled`).** Equivalent expressive power but less extensible: a third traffic class would require another schema change. The bitfield uses one byte today and has room for future flags without further migrations.

3. **An enum (`Unicast | Multicast | Both`).** Explicit but combinatorial: each new traffic class multiplies the number of variants. The bitfield composes naturally.

4. **A new account type per traffic class.** Treat multicast tenants and unicast tenants as different onchain accounts. Cleaner type system but doubles the surface area for instructions, indexers, CLI, and SDKs without a clear product benefit.

5. **Allocate VRF lazily on first unicast user.** Defers the decision until a user binds in. Saves VRF IDs but makes the tenant's intent implicit and harder to reason about for operators inspecting accounts.

The chosen approach extends the existing `Tenant` schema in place, using append-only field evolution and a sentinel value (`vrf_id = 0`) for the no-unicast case. It keeps the unicast story unchanged and lets one account type cover every use case grouping.

## Detailed Design

The extension is implemented by appending a single byte to the `Tenant` account that encodes which traffic classes the tenant participates in, by making VRF allocation conditional on that byte, by accepting an optional tenant pubkey on multicast group create and update, and by widening the access-pass authorization. The remaining subsections detail each of these pieces.

### Capability flags

Two flags are introduced, packed into a single byte:

| Bit | Name                          | Meaning                                       |
|-----|-------------------------------|-----------------------------------------------|
| 0   | `TENANT_CAPABILITY_UNICAST`   | Tenant carries unicast traffic; has a VRF ID. |
| 1   | `TENANT_CAPABILITY_MULTICAST` | Tenant groups multicast resources.            |

At least one bit must be set. Unknown bits are rejected on validation. The byte is appended to the existing `Tenant` struct after `include_topologies` (append-only schema evolution, no reordering).

### Invariants

- `capabilities != 0` and `capabilities & !KNOWN_MASK == 0`.
- `(capabilities & UNICAST != 0)` if and only if `vrf_id != 0`.
- Legacy tenants (written before this RFC) deserialize with `capabilities == 0`. On read, when `vrf_id != 0`, the program interprets capabilities as `UNICAST`. The next write persists the explicit byte.

### Lifecycle

- **Create.** Caller passes the desired `capabilities`. The VRF ID is allocated from the global pool only when `UNICAST` is set; otherwise `vrf_id` is `0` and the VRF resource extension is not mutated.
- **Update.** Capabilities can be added (additive only) via `add_capabilities`. Adding `UNICAST` allocates a VRF ID. Removing capabilities is not supported in this RFC; the operator must delete and recreate the tenant.
- **Delete.** VRF ID is deallocated only when `vrf_id != 0`.

### Tenant administrators and access passes

A tenant administrator (any pubkey in `Tenant.administrators`) is granted authority over access passes that touch only tenants they administer. The top-level authorization in `set_access_pass` accepts the call when either:

- the existing path holds (sentinel, feed, or foundation), or
- every tenant referenced by this call (added or removed) is a tenant the payer administers.

The existing per-tenant administrator check on the "add tenant" path is mirrored onto the "remove tenant" path. Modifications to `mgroup_pub_allowlist` and `mgroup_sub_allowlist` are not part of `set_access_pass` today and are not part of this RFC.

### Multicast group ownership (optional `tenant_pk`)

`MulticastGroup` already carries a `tenant_pk` field, but today it is set to `Pubkey::default()` on creation and is not exposed at the instruction level. With tenants now able to declare a `MULTICAST` capability, the create and update flows let callers attach a multicast group to a specific tenant.

- **Create.** `create_multicastgroup` accepts an optional `tenant_pk` argument. When provided, the program validates that the referenced account is a `Tenant` with the `MULTICAST` capability and stores the pubkey on the group. When omitted, behavior is unchanged: `tenant_pk` remains `Pubkey::default()` and the group is treated as un-tenanted.
- **Update.** `update_multicastgroup` accepts an optional `tenant_pk` argument. When provided, the same validation runs and the group's `tenant_pk` is updated. `Pubkey::default()` is reserved as an explicit "clear" value; any other pubkey must reference a `MULTICAST`-capable tenant. Updating `tenant_pk` is restricted to the multicast group owner or the foundation, sentinel, or feed authority.

This is intentionally optional in this RFC so existing tooling and groups keep working. A follow-up RFC will make `tenant_pk` mandatory on creation and enforce it at activation, subscribe, and publish time, gated by a transition window in which operators backfill `tenant_pk` on existing groups. The expectation is that within one or two release cycles, every multicast group will be owned by a tenant and the un-tenanted path will be removed.

### CLI

`doublezero tenant create` learns two flags:

```bash
# Unicast only (default if neither flag is given, for compatibility)
doublezero tenant create --code my-tenant --unicast

# Multicast only (no VRF allocated)
doublezero tenant create --code my-multicast-tenant --multicast

# Both
doublezero tenant create --code my-dual-tenant --unicast --multicast
```

`doublezero tenant update` learns an additive flag:

```bash
# Add multicast capability to an existing unicast tenant
doublezero tenant update --pubkey <pk> --add-capability multicast
```

`doublezero tenant list` and `doublezero tenant get` show `capabilities` (rendered as `unicast`, `multicast`, or `unicast,multicast`) and display `vrf_id` as `-` when zero.

`doublezero access-pass set` accepts the same arguments as today; the underlying instruction enforces the widened authorization automatically.

`doublezero multicast group create` and `doublezero multicast group update` gain an optional `--tenant <code-or-pubkey>` flag:

```bash
# Create a multicast group owned by a tenant
doublezero multicast group create --code stream-a --max-bandwidth 10Gbps --tenant my-multicast-tenant

# Attach (or move) an existing group to a tenant
doublezero multicast group update --pubkey <mgroup-pk> --tenant my-multicast-tenant
```

Omitting `--tenant` preserves today's behavior. `doublezero multicast group list` and `get` display the tenant code when present, or `-` otherwise.

## Impact

| Area              | Change                                                                                     |
|-------------------|--------------------------------------------------------------------------------------------|
| Onchain program   | New byte appended to `Tenant`; conditional VRF allocation in create/update/delete; widened authorization in `accesspass/set`; optional `tenant_pk` argument on `create_multicastgroup` and `update_multicastgroup`. |
| Rust SDK          | Tenant command modules mirror the new instruction signatures.                              |
| Go SDK            | New `Capabilities` field with a legacy-fallback decoder; updated JSON marshaling.          |
| TypeScript/Python | No public Tenant serializer in those SDKs today; fixtures regenerated for completeness.    |
| CLI               | New flags on `tenant create`, `tenant update`, `multicast group create`, `multicast group update`; new columns in the corresponding `list` and `get` commands. |
| Operator workflow | Multicast-only tenants no longer consume VRF IDs; tenant admins can manage their own access passes. |
| Foundation tooling| No change for foundation, sentinel, or feed callers; their existing flows are unaffected.  |

## Security Considerations

- **Authorization widening for `set_access_pass`.** Tenant administrators gain the ability to call `set_access_pass`, but only when every referenced tenant is one they administer. This is enforced both at the top-level gate (to accept the call) and at the per-tenant check (already present on add, mirrored to remove). Foundation-level callers retain their existing privileges.
- **VRF pool exhaustion.** Multicast-only tenants no longer consume VRF IDs, which is a net improvement for pool availability. The legacy fallback (treat `vrf_id != 0` as UNICAST on read) preserves the existing accounting for already-allocated IDs.
- **Schema invariants.** `validate()` rejects `capabilities == 0`, unknown bits, and any inconsistency between the UNICAST bit and `vrf_id`. Unit tests cover each case.
- **Direct `vrf_id` writes via `UpdateTenant`.** The existing `TenantUpdateArgs.vrf_id` field is preserved for compatibility; the validation layer enforces the UNICAST/`vrf_id` invariant, so an inconsistent direct write is rejected.

## Backward Compatibility

- The `Tenant` account layout is extended only at the end. Borsh deserialization of pre-RFC accounts returns `capabilities = 0`, which the program treats as `UNICAST` whenever `vrf_id != 0`.
- `TenantCreateArgs` and `TenantUpdateArgs` already derive `BorshDeserializeIncremental`. Older CLIs that do not pass `capabilities` get the default (treated as `UNICAST` on create), so existing tooling continues to work for unicast tenants.
- The widened access-pass authorization is purely additive: existing foundation, sentinel, and feed callers are unaffected.

No explicit migration is required. The first write to a legacy tenant after this change persists `capabilities = UNICAST` (or whichever explicit value the operator chooses via `update --add-capability`). Existing access passes and users are unaffected.

## Open Questions

1. **Mandatory multicast group ownership.** This RFC makes `tenant_pk` optional on multicast group create and update. A follow-up RFC will make it mandatory on creation and enforce it at activation, subscribe, and publish time, including the backfill and deprecation path for groups that today carry `Pubkey::default()`.
2. **Per-tenant resource limits.** This RFC does not introduce limits on the number of users, multicast groups, or access passes per tenant. A follow-up RFC will define limit fields and enforcement points.
3. **Capability removal.** Today an operator who wants to drop a capability must delete and recreate the tenant. A future RFC could define a safe teardown path (drain users or multicast groups, then clear the bit and deallocate the VRF ID).
4. **CLI default behavior.** `tenant create` without `--unicast` or `--multicast` currently defaults to UNICAST to preserve compatibility. Whether to require explicit selection (and break old scripts) is a small follow-up decision.
