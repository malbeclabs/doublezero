# Least-privilege feed oracle — change overview

**Status:** proposal, pending team consensus. No code written.
**Driver:** malbeclabs/infra#1652 (reframed below) · parent #1547 Generalized Seat Buying.
**Repos touched:** `malbeclabs/doublezero` (serviceability program) and `malbeclabs/doublezero-shreds` (oracle).
**Detailed Unit-1 spec:** `doublezero/docs/superpowers/specs/2026-06-17-serviceability-feed-oracle-permission-account-design.md`.

## TL;DR

The shred/feed oracle currently sits in `GlobalState.foundation_allowlist`, which is effectively
**near-superuser** (governance, allowlist edits, authority rotation, etc.). It only needs to manage
access passes, multicast subscriptions, and the users it provisions. We want to drop it to
**least privilege** by giving it a per-key **Permission PDA** carrying exactly
`ACCESS_PASS_ADMIN | USER_ADMIN`, and teaching the seven handlers it calls to honor that Permission
account. The change is **additive** — no existing caller's authorization changes. It splits into three
units (program → oracle wiring → operational rollout), sequenced carefully to avoid an unauthorized
window.

## Why not the two obvious options

The original #1652 idea was to give the oracle the scoped `feed_authority` role. Two findings killed
the simple paths:

- **`feed_authority` is incompatible with the oracle's validator-owned flow.** The access-pass
  handlers enforce an "owns-only" gate — `feed_authority == payer && accesspass.owner != payer → deny`
  — and that gate fires **even for foundation members**. Validator-owned passes are owned by the
  *validator* (the oracle deliberately does not re-`set` an existing pass, so as not to clobber the
  validator's settings). An oracle holding `feed_authority` would be blocked on every validator-owned
  pass. This is exactly why `feed_authority` was cleared during the validator-owned rollout.
- **The `sentinel` slot is taken.** `sentinel_authority_pk` is a single-pubkey slot already wired as
  the **billing sentinel** (writes tenant payment status after deduction) and the **suspend /
  circuit-breaker** authority. The oracle can't co-occupy it, and merging the network kill-switch onto
  the hot oracle key would be a poor separation of duties.

→ The **Permission-account (PDA) bitmask** system is the right vehicle: per-key, no slot contention,
no owns-only gate, and far narrower than foundation.

## The chosen approach (mechanism)

The program already has a `Permission` PDA per pubkey (`get_permission_pda(program_id, key)`) with a
`u128` bitmask and an `authorize()` helper. But `authorize()` bundles a **legacy fallback** whose
composite differs from each handler's hand-written inline check, so routing existing callers through
it would silently change their authorities. Instead:

1. Add a **new, no-legacy-fallback** helper, `authorize_permission_account_only(...)`, that validates
   *only* a passed Permission PDA (correct PDA for the signer, program-owned, `Activated`, required bit
   set).
2. In each of the seven handlers, **keep the existing inline check verbatim** and OR in
   `perm_only(BIT)`. Existing callers are untouched; a new Permission-account path is added.
3. The Permission account is an **optional trailing account** per instruction — today's callers omit
   it and keep working; the oracle passes it.

The oracle's Permission bitmask is `ACCESS_PASS_ADMIN | USER_ADMIN`.

## What changes, by unit

### Unit 1 — serviceability program (`malbeclabs/doublezero`)

The seven handlers the oracle uses, the authority each preserves, and the bit added:

| Handler | Preserved inline authority | Bit added |
|---|---|---|
| SetAccessPass | foundation / sentinel / feed (owns-it) / tenant-admin / pass-owner | `ACCESS_PASS_ADMIN` |
| CloseAccessPass | foundation / feed (owns-it) | `ACCESS_PASS_ADMIN` |
| AddMulticastGroupSubAllowlist | mgroup-owner / sentinel / feed (owns-it) / foundation | `ACCESS_PASS_ADMIN` |
| RemoveMulticastGroupSubAllowlist | same as add | `ACCESS_PASS_ADMIN` |
| UpdateMulticastGroupRoles (subscribe/unsubscribe) | pass `user_payer` / foundation | `ACCESS_PASS_ADMIN` |
| DeleteUser | foundation / `user.owner` | `USER_ADMIN` |
| CreateSubscribeUser (owner override) | foundation / sentinel | `USER_ADMIN` |

Plus: the `perm_only` helper, integration tests per handler (positive Permission path, negative
no-account/insufficient/suspended, regression on existing paths), an IDL bump (one optional trailing
account per instruction), and a `PERMISSION.md` update. **Self-contained and shippable on its own** —
the new path is dormant until a caller passes a Permission account.

One structural note for reviewers: because the Permission account is a *trailing* account, the final
authorization decision moves to just after each handler's required-account reads (before any state
mutation). Reads are non-destructive, so this is safe; it's a small restructure, not a logic change.

### Unit 2 — oracle wiring (`malbeclabs/doublezero-shreds`)

`dz_ledger.rs` appends the oracle's Permission PDA `AccountMeta` to the seven instructions it builds.
No behavior change until Unit 1 is deployed; the oracle continues to work via foundation in the
meantime.

### Unit 3 — operational rollout

`CreatePermission` for the oracle key with `ACCESS_PASS_ADMIN | USER_ADMIN`, then remove the oracle
from `foundation_allowlist`. **Sequencing matters** (we've had unauthorized-window incidents from
out-of-order authority changes before):

1. Deploy Unit 1 (handlers honor Permission accounts).
2. Deploy Unit 2 (oracle passes its Permission account).
3. `CreatePermission` for the oracle key; **verify** the oracle operates end-to-end via the Permission
   path while still in foundation.
4. Only then remove the oracle from `foundation_allowlist`.

Doing step 4 before 1–3 are confirmed leaves the oracle unauthorized.

## Security review framing

- **Additive / behavior-preserving:** every existing caller (foundation, `user.owner`, pass
  `user_payer`, `feed_authority` owns-it, sentinel, mgroup-owner) keeps its exact current path. The
  only new capability is "a key with a valid `Activated` Permission PDA bearing the required bit."
- **Net privilege reduction:** the oracle goes from foundation (≈ all admin bits, incl. allowlist
  edits and authority rotation) to exactly `ACCESS_PASS_ADMIN | USER_ADMIN`. Blast radius on oracle-key
  compromise shrinks accordingly.
- **No owns-only gate on the Permission path** — required so the oracle can manage both
  connection-ticket passes (which it owns) and validator-owned passes (which it does not).

## Open questions for the team

1. **Bit for `UpdateMulticastGroupRoles`:** proposed `ACCESS_PASS_ADMIN` (it's access-pass-gated). If
   the team prefers `MULTICAST_ADMIN`, the oracle's bitmask changes to add it.
2. **`USER_ADMIN` semantics:** our `delete`/`create` changes make a Permission-account `USER_ADMIN`
   holder able to delete/create users without an owns-it restriction (matching the oracle's
   cross-owner cleanup job). Confirm that's the intended meaning of the bit for permission holders.
3. **`RequirePermissionAccounts`:** out of scope here (we stay additive and leave legacy paths intact).
   Worth a separate decision on whether/when to flip the program-wide flag that retires legacy auth.
4. **`CreateSubscribeUser`:** plan-phase item — confirm there's no second foundation-only gate beyond
   the owner-override check.

## Issue / PR map

- **#1652** — to be reframed from "feed-authority delete" to this Permission-PDA approach.
- **#1547** (parent) — Generalized Seat Buying.
- **doublezero-shreds #501** (connection-ticket cap eviction + zero-connection close) — the feature
  that exercises the oracle's delete/unsubscribe/close; it currently relies on the oracle's foundation
  membership. This least-privilege work is its security follow-up; #501 does not depend on it (it ships
  on the foundation path in the interim).
