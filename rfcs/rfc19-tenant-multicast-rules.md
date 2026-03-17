# RFC-19: Tenant Multicast Rules

## Summary

**Status: `Draft`**

Add a multicast rule list to the `Tenant` account that automatically grants all users of
that tenant access to specified multicast groups (as publisher, subscriber, or both). The
`doublezerod` daemon subscribes to onchain `Tenant` account updates, detects rule changes,
and connects or disconnects multicast groups without any manual per-user authorization.

Today, multicast group access requires per-user authorization via `AccessPass` allowlists.
Tenant multicast rules replace this with a declarative, tenant-scoped policy. The daemon
enforces it automatically as part of its existing reconciler loop — the same pattern
introduced by RFC-17 for IBRL provisioning.

## Motivation

Certain tenants require multicast group connectivity as a core part of their service
model — either to distribute information across their users (e.g., market data broadcast,
consensus messages) or to feed data flows into external services (e.g., analytics
pipelines, replication targets). For these tenants, multicast access is a prerequisite
for any user connection to be functional.

A tenant operator who wants all their users to publish or subscribe to a specific
multicast group must today issue one `AddMulticastGroupPubAllowlist` /
`AddMulticastGroupSubAllowlist` transaction per user, keyed by `client_ip + user_payer`.
There is no tenant-level shortcut. As tenant size grows, this becomes a scaling problem:

- Onboarding a new user requires a separate multicast authorization step after activation.
- Removing a user requires manual cleanup of the AccessPass entry.
- There is no single place to inspect or update the multicast policy for a tenant.

Tenant multicast rules solve this by expressing the policy once at the tenant level. The
daemon detects changes and applies them automatically — consistent with the self-healing,
onchain-reconciled model established by RFC-17.

## New Terminology

| Term | Definition |
|------|-----------|
| **Tenant multicast rule** | A tuple of `(multicast_group, role)` stored on a `Tenant` account, declaring that all users of that tenant should have the given multicast role for that group. |
| **Effective multicast set** | The union of `User.publishers` / `User.subscribers` (explicit per-user grants) and the groups derived from `tenant.multicast_rules`. This is what the daemon provisions. |

## Alternatives Considered

**Do nothing.** Operators continue authorizing users one by one via AccessPass. This
works but does not scale and requires out-of-band tooling to stay consistent.

**Activator-driven propagation.** The activator writes to `User.publishers` /
`User.subscribers` on activation and on rule changes. Rejected because it introduces
activator state for a concern that belongs to the client: the daemon already owns
multicast provisioning and has the right context (local tunnel state, incremental update
capability) to apply changes safely.

**Separate `TenantMulticastRule` PDA per rule.** A child account per rule avoids growing
the `Tenant` account. Rejected because the expected rule count per tenant is small
(single digits in practice), an inline Vec keeps the design to a single account fetch,
and separate PDAs add instruction complexity without meaningful benefit at this scale.

## Detailed Design

### Data Structures

Two new types added to the `doublezero-serviceability` program:

```rust
pub enum TenantMulticastRole {
    Publisher = 0,
    Subscriber = 1,
    PublisherAndSubscriber = 2,
}

pub struct TenantMulticastRule {
    pub multicast_group: Pubkey,   // 32 bytes
    pub role: TenantMulticastRole, // 1 byte
}
```

New field appended to `Tenant`:

```rust
pub multicast_rules: Vec<TenantMulticastRule>,  // 4 + (33 * len) bytes; max 32 entries
```

File: `smartcontract/programs/doublezero-serviceability/src/state/tenant.rs`

### Smart Contract Instructions

**`AddTenantMulticastRule`**
- Signers: the payer must be authorized to manage **both** accounts simultaneously:
  - **Tenant authority**: tenant `owner`, member of `administrators`, or foundation allowlist
  - **MulticastGroup authority**: multicast group `owner` or foundation allowlist
- Accounts: `Tenant`, `MulticastGroup`
- Effect: appends a `TenantMulticastRule`; rejects if `multicast_group` already present
  or if `multicast_rules.len() == 32`

**`UpdateTenantMulticastRule`**
- Signers: the payer must be authorized to manage **both** accounts simultaneously:
  - **Tenant authority**: tenant `owner`, member of `administrators`, or foundation allowlist
  - **MulticastGroup authority**: multicast group `owner` or foundation allowlist
- Accounts: `Tenant`, `MulticastGroup`
- Effect: updates the `role` of the entry matching `multicast_group`; rejects if not found

**`RemoveTenantMulticastRule`**
- Signers: tenant `owner`, member of `administrators`, or foundation allowlist
- Accounts: `Tenant`
- Effect: removes the entry matching `multicast_group`; no-op if not found

Files:
- `smartcontract/programs/doublezero-serviceability/src/processors/tenant/add_multicast_rule.rs`
- `smartcontract/programs/doublezero-serviceability/src/processors/tenant/update_multicast_rule.rs`
- `smartcontract/programs/doublezero-serviceability/src/processors/tenant/remove_multicast_rule.rs`

### Authorization Model

Multicast access for a user is valid if **either** condition holds:

1. **AccessPass path** (existing, unchanged): `AccessPass.mgroup_pub_allowlist` /
   `mgroup_sub_allowlist` contains the group.
2. **Tenant rule path** (new): `user.tenant_pk → tenant.multicast_rules` contains the
   group with a matching role.

This keeps full backward compatibility with existing AccessPass-authorized users.

### Daemon Changes (`doublezerod`)

The daemon's reconciler loop (RFC-17) already fetches all onchain program data on each
cycle. The change is to extend the reconciler to:

1. **Resolve the user's tenant**: using `user.tenant_pk`, fetch the `Tenant` account from
   the already-loaded program data.

2. **Compute the effective multicast set**: union of
   - `User.publishers` / `User.subscribers` (explicit per-user grants, existing)
   - Groups derived from `tenant.multicast_rules` filtered by role

3. **Detect changes**: diff the effective multicast set against the currently provisioned
   state. If only the multicast group list changed (tunnel endpoint, ASN, and DZ IP are
   unchanged), apply an incremental update — same logic already used when
   `User.publishers` / `User.subscribers` change (RFC-15).

4. **Detect `Tenant` account changes on the existing polling cycle**: the daemon already
   fetches all program accounts on each 10-second cycle. It reads the `Tenant` account
   linked via `user.tenant_pk` and diffs `multicast_rules` against the previously seen
   snapshot. A detected change triggers the same incremental update path used when
   `User.publishers` / `User.subscribers` change.

```
Reconciler cycle (every 10s) →
  Fetch all program accounts (existing) →
  Read Tenant account via user.tenant_pk →
  Recompute effective multicast set →
  Diff against current provisioned state →
  If groups changed: incremental multicast update
    (add/remove PIM groups, BGP routes — no tunnel restart)
  If infrastructure changed: full reprovision
```

Files:
- `client/doublezerod/internal/manager/` (reconciler)
- `client/doublezerod/internal/services/` (multicast service, incremental update path)

### CLI Changes

New subcommand group under `doublezero tenant`:

```bash
# Add a rule
doublezero tenant multicast-rule add --tenant <code> --group <code> --role publisher
doublezero tenant multicast-rule add --tenant <code> --group <code> --role subscriber
doublezero tenant multicast-rule add --tenant <code> --group <code> --role both

# Remove a rule
doublezero tenant multicast-rule remove --tenant <code> --group <code>

# List rules
doublezero tenant multicast-rule list --tenant <code>
doublezero tenant multicast-rule list --tenant <code> --json
```

File: `client/doublezero/src/` (CLI crate, new subcommand handlers)

### SDK Changes

The Go, Python, and TypeScript SDKs update `Tenant` deserialization to include
`multicast_rules`. Because the field is appended at the end of the Borsh-serialized
struct, existing accounts deserialize to an empty `Vec` — no migration required.

## Impact

- **Codebase**: smart contract (new types + 2 instructions), daemon reconciler (tenant
  subscription + effective multicast set computation), CLI (new subcommand group), all
  three SDKs.
- **Activator**: no changes.
- **Operations**: tenant operators set rules once; users receive multicast connectivity
  automatically on next reconcile cycle after the rule takes effect onchain.
- **Performance**: rule changes trigger one incremental multicast update per connected
  user daemon. No new onchain transactions per user.

## Security Considerations

- Adding a rule requires dual authorization: the signer must have authority over both the
  `Tenant` (owner, administrator, or foundation allowlist) and the `MulticastGroup`
  (owner or foundation allowlist). This prevents a tenant administrator from unilaterally
  associating a multicast group they do not control.
- Removing a rule requires only tenant authority (owner, administrator, or foundation
  allowlist) — the multicast group account is not needed because the operation reduces
  access rather than grants it.
- The daemon validates the `Tenant` account PDA derivation before applying rules to
  prevent a maliciously crafted account from being substituted.

## Backward Compatibility

- Existing `AccessPass`-based multicast authorization is unchanged.
- Tenants without `multicast_rules` (all existing tenants) have an empty Vec — no
  behavior change for currently-connected daemons.
- SDK deserialization is backward compatible (empty Vec for old accounts).
- No changes to the activator, the `doublezero connect multicast` CLI syntax, or the
  daemon provisioning API.

## Open Questions

1. **Rule removal**: when a rule is removed, the daemon incrementally removes those
   multicast groups from the active session. Confirm this is the expected behavior (vs.
   waiting for the user to reconnect).
2. **Rule cap**: 32 rules per tenant is proposed. Confirm this covers expected use cases.
