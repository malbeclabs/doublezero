# Permission-account authorization for the feed oracle (serviceability)

Issue: malbeclabs/infra#1652 (reframed — see below). Parent: #1547 Generalized Seat Buying.
Target repo: malbeclabs/doublezero (this repo, serviceability program). Branch off `origin/main`.

## Why this reframes #1652

#1652 proposed giving the shred oracle a scoped `feed_authority` delete authority (mirroring the
`feed_authority` owns-it scoping on set/close). Two findings make that the wrong vehicle:

1. **`feed_authority`'s owns-only gate is incompatible with the oracle's validator-owned flow.**
   `set`/`close`/`add`/`remove` deny when `feed_authority == payer && accesspass.owner != payer` —
   and that check fires even for a caller who is *also* in `foundation_allowlist`. Validator-owned
   access passes are owned by the validator (the oracle skips `set_access_pass` on an existing pass
   precisely so it does not clobber the validator's settings), so an oracle holding `feed_authority`
   would be blocked on every validator-owned pass. This is exactly why the validator-owned rollout
   cleared `feed_authority` and put the oracle in `foundation_allowlist`.
2. **The `sentinel_authority_pk` slot is occupied.** It is wired as the billing sentinel
   (`processors/tenant/update_payment_status.rs`, "used by billing sentinel after deduction") and the
   suspend/circuit-breaker authority. It is a single-pubkey slot, so the oracle cannot co-occupy it,
   and folding the oracle into it would put billing + the network kill-switch on the hot oracle key.

The least-privilege path that avoids both problems is the program's **Permission-account** system:
grant the oracle a per-key `Permission` PDA carrying exactly the bits it needs, and make the handlers
it calls honor that Permission account. No single-role slot contention, no owns-only gate, far
narrower than `foundation_allowlist` (no governance / allowlist-edit / authority-rotation bits).

## Scope of this spec (Unit 1 of 3)

This spec covers ONLY the serviceability program change. It is self-contained and testable on its own
— the new Permission-account path is dormant until a caller actually passes a Permission account, so
nothing changes for existing callers. Two follow-up units are out of scope here:

- **Unit 2 (oracle wiring, repo `malbeclabs/doublezero-shreds`):** `dz_ledger.rs` appends the oracle's
  Permission PDA `AccountMeta` to the relevant instructions.
- **Unit 3 (operational rollout):** `CreatePermission` for the oracle's key with
  `ACCESS_PASS_ADMIN | USER_ADMIN`, then remove the oracle from `foundation_allowlist`.

## Goal / acceptance

A key that signs while passing its own `Permission` PDA (status `Activated`) bearing
`ACCESS_PASS_ADMIN | USER_ADMIN` can perform all seven serviceability operations the oracle uses,
**without** `foundation_allowlist` membership and **without** any single-role slot. Every existing
caller's authorization is unchanged (additive only).

## Design

### 1. New helper: `authorize_permission_account_only` (authorize.rs)

`authorize()` already supports a Permission-account path, but it bundles a *legacy fallback*
(`check_legacy_any`) whose composite differs from each handler's hand-written inline check (e.g.
`delete` is `foundation || user.owner`, but `USER_ADMIN` legacy is `foundation || activator`). Routing
existing callers through `authorize()` would therefore silently change their authorities. So we add a
**permission-account-only** helper with no legacy fallback, to be OR'd with each handler's existing
inline check:

```rust
/// Permission-account-only authorization (no legacy GlobalState fallback).
///
/// Reads the next account from `accounts_iter` as the payer's optional trailing
/// `Permission` PDA:
/// - No further account present  -> Ok(false)  (caller falls back to its own inline checks)
/// - Present, is the payer's Permission PDA, program-owned, status == Activated,
///   and `permissions & any_of_flags != 0`  -> Ok(true)
/// - Present, is the payer's Permission PDA, program-owned, Activated, but no
///   required bit set  -> Ok(false)
/// - Present but NOT the payer's Permission PDA, or not program-owned  -> Err
///   (a malformed/incorrect account was supplied)
///
/// Unlike `authorize`, this never consults `check_legacy_any`, so OR-ing it into a
/// handler's existing inline check cannot widen the legacy authorities.
pub fn authorize_permission_account_only<'a, 'b: 'a, I>(
    program_id: &Pubkey,
    accounts_iter: &mut I,
    payer_key: &Pubkey,
    any_of_flags: u128,
) -> Result<bool, ProgramError>
where
    I: Iterator<Item = &'a AccountInfo<'b>>;
```

It reuses the same validation `authorize()`'s new path performs (PDA == `get_permission_pda(program_id,
payer_key)`, `owner == program_id`, deserialize `Permission`, `status == Activated`). The only
behavioral differences from `authorize()`'s new path: a missing-bit result returns `Ok(false)` rather
than `Err` (so the handler's own inline OR decides the final verdict), and an absent account returns
`Ok(false)` rather than entering the legacy branch.

### 2. Integration pattern for each handler

The Permission account is an **optional trailing account** (appended after each instruction's current
last account), so today's callers — which pass no such account — are unaffected and keep hitting their
inline checks. Because the helper reads the *next* account from the iterator, the authorization
decision must be made **after all of the handler's required accounts have been read** (the iterator
must be positioned at the trailing slot) and **before any state mutation**. Per handler:

1. Compute `inline_ok` from the already-read accounts using the handler's *existing* inline check,
   verbatim (do not change it).
2. Finish reading the handler's required accounts (so the iterator reaches the trailing slot).
3. Before mutating any account: `let authorized = inline_ok || authorize_permission_account_only(program_id, accounts_iter, payer, BIT)?;` and reject with the handler's existing error if `!authorized`.

Handlers today reject early (right after reading `globalstate`); this moves the *final* rejection to
just after the required-account reads, before mutations. No account read is destructive, so deferring
the rejection is safe. The existing `feed_authority` owns-only restrictions stay exactly as they are —
they gate only the `feed_authority` path, never the Permission-account path (so the oracle's
Permission account is not owns-gated, which is required for validator-owned passes it does not own).

### 3. The seven handlers and their required bit

| Handler (file) | Existing inline authority (preserve verbatim) | Bit added via `perm_only` |
|---|---|---|
| `SetAccessPass` (`accesspass/set.rs`) | `foundation \|\| sentinel \|\| feed_authority \|\| tenant_admin \|\| accesspass.owner==payer` (+ feed owns-it) | `ACCESS_PASS_ADMIN` |
| `CloseAccessPass` (`accesspass/close.rs`) | `foundation \|\| feed_authority` (+ feed owns-it) | `ACCESS_PASS_ADMIN` |
| `AddMulticastGroupSubAllowlist` (`multicastgroup/allowlist/subscriber/add.rs`) | `mgroup.owner==payer \|\| sentinel \|\| feed_authority \|\| foundation` (+ feed owns-it) | `ACCESS_PASS_ADMIN` |
| `RemoveMulticastGroupSubAllowlist` (`multicastgroup/allowlist/subscriber/remove.rs`) | same as add | `ACCESS_PASS_ADMIN` |
| `UpdateMulticastGroupRoles` (`multicastgroup/subscribe.rs`) | `accesspass.user_payer==payer \|\| foundation` | `ACCESS_PASS_ADMIN` |
| `DeleteUser` (`user/delete.rs`) | `foundation \|\| user.owner==payer` | `USER_ADMIN` |
| `CreateSubscribeUser` owner_override (`user/create_core.rs`) | `foundation \|\| sentinel` (to set `owner != payer`) | `USER_ADMIN` |

The oracle's `Permission` bitmask is therefore `ACCESS_PASS_ADMIN | USER_ADMIN`.

> Verification item for the plan: confirm `CreateSubscribeUser` has no *other* foundation-only gate
> beyond the `owner_override` check (the oracle creates users with `owner = validator`). If a second
> gate exists, it needs the same `perm_only` OR.

### 4. Tests (`programs/doublezero-serviceability/tests/`, solana-program-test)

Reuse the `permission_test.rs` harness (`CreatePermission` + `get_permission_pda`) and the
per-handler test helpers. For each of the seven handlers:

- **Positive:** a signer who is NOT foundation/sentinel/feed/owner, but passes an `Activated`
  Permission PDA with the required bit, succeeds. For `delete`/`close`, use a pass/user owned by a
  *different* key to prove the Permission path has no owns-it restriction.
- **Negative (no account):** same signer, no Permission account passed → the handler's existing error
  (unchanged legacy behavior).
- **Negative (insufficient/suspended):** Permission PDA present but missing the bit, or `Suspended` →
  rejected.
- **Regression:** the existing foundation / `user.owner` / `user_payer` / feed-authority-owns-it paths
  still succeed/deny exactly as before (one representative assertion each).

Pin exact error variants (`NotAllowed` / `Unauthorized` as each handler currently returns; the new
helper's malformed-account case returns `InvalidArgument`/`InvalidAccountData`). Add a focused unit
test for `authorize_permission_account_only` covering each branch of its contract.

### 5. IDL / SDK / docs

- Each of the seven instructions gains one **optional trailing account** (the payer's Permission PDA).
  Update the program IDL accordingly. Existing instruction builders that omit it stay valid.
- Update `PERMISSION.md` to record that these seven handlers now honor a Permission account bearing
  `ACCESS_PASS_ADMIN` / `USER_ADMIN` (as listed above), in addition to their legacy authorities.
- No instruction *data* (args) change; no discriminator change.

## Out of scope

- Oracle wiring (Unit 2) and the operational rollout (Unit 3: create the oracle's Permission PDA;
  remove it from `foundation_allowlist`).
- Setting the `RequirePermissionAccounts` feature flag (the program-wide switch that disables the
  legacy path). This change is purely additive; the legacy paths remain for all existing callers.
- Converting these handlers to call `authorize()` wholesale (would change legacy composites). We add
  the narrow `perm_only` OR instead.
- Any change to `feed_authority`'s owns-only gates or the sentinel/billing wiring.
