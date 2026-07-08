use crate::{
    error::DoubleZeroError,
    pda::get_permission_pda,
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        permission::{permission_flags, Permission, PermissionStatus},
    },
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

/// Authorize `payer_key` for an instruction, using a Permission account (new path) or the
/// legacy GlobalState allowlists/authority keys (legacy path).
///
/// Call this after all expected accounts have been consumed from `accounts_iter`.
/// If an additional account is present in the iterator, it is treated as the Permission account.
///
/// `any_of_flags` uses OR semantics: the payer is authorized if their Permission account has
/// at least one of the specified `permission_flags::*` bits set.
///
/// Legacy fallback mapping (used whenever `FeatureFlag::RequirePermissionAccounts`
/// is not set — both when no Permission account is provided and when the provided
/// Permission account exists but does not grant the requested flag, so the SDK
/// auto-injecting the payer's Permission PDA can never lock out a legacy key):
///   FOUNDATION        → foundation_allowlist
///   QA                → qa_allowlist
///   ACTIVATOR         → activator_authority_pk
///   SENTINEL          → sentinel_authority_pk
///   HEALTH_ORACLE     → health_oracle_pk
///   FEED_AUTHORITY     → feed_authority_pk
///   USER_ADMIN        → foundation_allowlist
///   ACCESS_PASS_ADMIN → foundation_allowlist OR sentinel_authority_pk OR feed_authority_pk
///   NETWORK_ADMIN     → foundation_allowlist
///   TENANT_ADMIN      → foundation_allowlist OR sentinel_authority_pk
///   MULTICAST_ADMIN   → foundation_allowlist OR sentinel_authority_pk
///   PERMISSION_ADMIN  → foundation_allowlist (always allowed for foundation, even under
///                       RequirePermissionAccounts or when the payer's own Permission account
///                       is missing/suspended/under-privileged — see foundation_permission_recovery)
///   INFRA_ADMIN       → foundation_allowlist
///   GLOBALSTATE_ADMIN → foundation_allowlist
///   CONTRIBUTOR_ADMIN → foundation_allowlist
///   TOPOLOGY_ADMIN    → foundation_allowlist
///   RESOURCE_ADMIN    → foundation_allowlist
///   INDEX_ADMIN       → foundation_allowlist
pub fn authorize<'a, 'b: 'a, I>(
    program_id: &Pubkey,
    accounts_iter: &mut I,
    payer_key: &Pubkey,
    globalstate: &GlobalState,
    any_of_flags: u128,
) -> ProgramResult
where
    I: Iterator<Item = &'a AccountInfo<'b>>,
{
    match next_account_info(accounts_iter).ok() {
        Some(permission_account) => {
            // New path: validate Permission PDA and bitmask.
            let (expected_pda, _) = get_permission_pda(program_id, payer_key);
            if permission_account.key != &expected_pda {
                return Err(ProgramError::InvalidArgument);
            }
            // Verify program ownership immediately after the PDA-address check, before
            // inspecting the account's contents.
            if permission_account.owner != program_id {
                return Err(ProgramError::InvalidAccountData);
            }
            // Whether the supplied Permission account itself grants one of the flags.
            let granted = !permission_account.data_is_empty()
                && Permission::try_from(permission_account)
                    .map(|p| {
                        p.status == PermissionStatus::Activated && p.permissions & any_of_flags != 0
                    })
                    .unwrap_or(false);
            if !granted {
                // The SDK auto-appends the payer's Permission PDA whenever it exists
                // on-chain. Without the recovery below, a foundation member whose own
                // Permission account is suspended, under-privileged, or uninitialized
                // would be routed through this branch and denied the very PERMISSION_ADMIN
                // instruction needed to repair it — re-introducing the lockout the
                // None-branch fallback exists to prevent.
                if foundation_permission_recovery(globalstate, payer_key, any_of_flags) {
                    return Ok(());
                }
                // While strict mode is off, the legacy allowlists/authorities remain
                // authoritative — exactly as in the None branch. Because the SDK
                // auto-appends the payer's Permission PDA whenever one exists on-chain,
                // a present-but-insufficient Permission account must not lock out a key
                // that legacy authorization would still accept (e.g. a foundation member
                // who also holds an unrelated, under-privileged Permission account).
                // Once RequirePermissionAccounts is enabled, only the Permission account
                // (or the foundation PERMISSION_ADMIN recovery above) authorizes.
                if !is_feature_enabled(
                    globalstate.feature_flags,
                    FeatureFlag::RequirePermissionAccounts,
                ) && check_legacy_any(payer_key, globalstate, any_of_flags)
                {
                    return Ok(());
                }
                return Err(DoubleZeroError::NotAllowed.into());
            }
        }
        None => {
            // Legacy path: check GlobalState allowlists / authority keys.
            if is_feature_enabled(
                globalstate.feature_flags,
                FeatureFlag::RequirePermissionAccounts,
            ) {
                // Even in strict mode, foundation members can manage permissions to
                // prevent being locked out of the permission system.
                if foundation_permission_recovery(globalstate, payer_key, any_of_flags) {
                    return Ok(());
                }
                return Err(DoubleZeroError::NotAllowed.into());
            }
            if !check_legacy_any(payer_key, globalstate, any_of_flags) {
                return Err(DoubleZeroError::NotAllowed.into());
            }
        }
    }
    Ok(())
}

/// Foundation lockout recovery: a foundation member may always exercise
/// `PERMISSION_ADMIN`, even in strict mode or when their own Permission account is
/// missing, suspended, or lacks the flag. This guarantees the permission system can
/// never lock foundation out of managing permissions.
fn foundation_permission_recovery(
    globalstate: &GlobalState,
    payer_key: &Pubkey,
    any_of_flags: u128,
) -> bool {
    any_of_flags & permission_flags::PERMISSION_ADMIN != 0
        && globalstate.foundation_allowlist.contains(payer_key)
}

/// Whether `payer_key` may GRANT the `FOUNDATION` flag on a Permission account.
///
/// Granting `FOUNDATION` directly is gated beyond ordinary `PERMISSION_ADMIN`: only a
/// `foundation_allowlist` member, or an existing holder of the `FOUNDATION` flag via
/// their own Activated Permission account, may do so. This is independent of
/// `RequirePermissionAccounts` — foundation members remain authoritative in both modes.
///
/// NOTE: this blocks only the *direct* grant. It is not a hard privilege boundary: a
/// plain `PERMISSION_ADMIN` can grant itself `GLOBALSTATE_ADMIN` and then add itself to
/// `foundation_allowlist`. That is accepted — `FOUNDATION` is transitional and slated
/// for deprecation in favor of the granular per-flag permissions.
///
/// `permission_account` is the caller's own trailing Permission account (the one the
/// SDK auto-appends), if present. It is bound to `payer_key` by re-deriving the PDA,
/// so a caller cannot pass someone else's FOUNDATION permission.
pub fn can_grant_foundation(
    program_id: &Pubkey,
    permission_account: Option<&AccountInfo>,
    payer_key: &Pubkey,
    globalstate: &GlobalState,
) -> bool {
    if globalstate.foundation_allowlist.contains(payer_key) {
        return true;
    }
    if let Some(acc) = permission_account {
        let (expected_pda, _) = get_permission_pda(program_id, payer_key);
        if acc.key == &expected_pda && acc.owner == program_id && !acc.data_is_empty() {
            if let Ok(p) = Permission::try_from(acc) {
                return p.status == PermissionStatus::Activated
                    && p.permissions & permission_flags::FOUNDATION != 0;
            }
        }
    }
    false
}

/// Returns true if `payer` satisfies at least one of the requested flags using legacy
/// GlobalState fields.
fn check_legacy_any(payer: &Pubkey, globalstate: &GlobalState, any_of: u128) -> bool {
    if any_of & permission_flags::FOUNDATION != 0
        && globalstate.foundation_allowlist.contains(payer)
    {
        return true;
    }
    if any_of & permission_flags::QA != 0 && globalstate.qa_allowlist.contains(payer) {
        return true;
    }
    if any_of & permission_flags::ACTIVATOR != 0 && globalstate.activator_authority_pk == *payer {
        return true;
    }
    if any_of & permission_flags::SENTINEL != 0 && globalstate.sentinel_authority_pk == *payer {
        return true;
    }
    if any_of & permission_flags::HEALTH_ORACLE != 0 && globalstate.health_oracle_pk == *payer {
        return true;
    }
    if any_of & permission_flags::FEED_AUTHORITY != 0 && globalstate.feed_authority_pk == *payer {
        return true;
    }
    // USER_ADMIN in legacy = foundation only. (The activator authority has been
    // retired from the system, so it no longer grants user-management rights.)
    if any_of & permission_flags::USER_ADMIN != 0
        && globalstate.foundation_allowlist.contains(payer)
    {
        return true;
    }
    // ACCESS_PASS_ADMIN in legacy = foundation, sentinel, or feed authority. This
    // mirrors the historical accesspass/set authority and is applied uniformly to
    // all ACCESS_PASS_ADMIN instructions (so accesspass/close, previously
    // foundation+feed only, now also accepts the sentinel authority).
    if any_of & permission_flags::ACCESS_PASS_ADMIN != 0
        && (globalstate.foundation_allowlist.contains(payer)
            || globalstate.sentinel_authority_pk == *payer
            || globalstate.feed_authority_pk == *payer)
    {
        return true;
    }
    // NETWORK_ADMIN in legacy = foundation only. (The activator authority has
    // been retired from the system.)
    if any_of & permission_flags::NETWORK_ADMIN != 0
        && globalstate.foundation_allowlist.contains(payer)
    {
        return true;
    }
    // TENANT_ADMIN in legacy = foundation or sentinel.
    if any_of & permission_flags::TENANT_ADMIN != 0
        && (globalstate.foundation_allowlist.contains(payer)
            || globalstate.sentinel_authority_pk == *payer)
    {
        return true;
    }
    // MULTICAST_ADMIN in legacy = foundation or sentinel. (The activator authority
    // has been retired from the system.)
    if any_of & permission_flags::MULTICAST_ADMIN != 0
        && (globalstate.foundation_allowlist.contains(payer)
            || globalstate.sentinel_authority_pk == *payer)
    {
        return true;
    }
    // PERMISSION_ADMIN in legacy = foundation (only foundation can manage permissions).
    if any_of & permission_flags::PERMISSION_ADMIN != 0
        && globalstate.foundation_allowlist.contains(payer)
    {
        return true;
    }
    // INFRA_ADMIN in legacy = foundation.
    if any_of & permission_flags::INFRA_ADMIN != 0
        && globalstate.foundation_allowlist.contains(payer)
    {
        return true;
    }
    // GLOBALSTATE_ADMIN in legacy = foundation.
    if any_of & permission_flags::GLOBALSTATE_ADMIN != 0
        && globalstate.foundation_allowlist.contains(payer)
    {
        return true;
    }
    // CONTRIBUTOR_ADMIN in legacy = foundation.
    if any_of & permission_flags::CONTRIBUTOR_ADMIN != 0
        && globalstate.foundation_allowlist.contains(payer)
    {
        return true;
    }
    // TOPOLOGY_ADMIN in legacy = foundation.
    if any_of & permission_flags::TOPOLOGY_ADMIN != 0
        && globalstate.foundation_allowlist.contains(payer)
    {
        return true;
    }
    // RESOURCE_ADMIN in legacy = foundation.
    if any_of & permission_flags::RESOURCE_ADMIN != 0
        && globalstate.foundation_allowlist.contains(payer)
    {
        return true;
    }
    // INDEX_ADMIN in legacy = foundation.
    if any_of & permission_flags::INDEX_ADMIN != 0
        && globalstate.foundation_allowlist.contains(payer)
    {
        return true;
    }
    false
}

/// Every permission flag that some processor passes to [`authorize`], i.e. the flags
/// whose instructions honor Permission accounts. Enabling
/// `FeatureFlag::RequirePermissionAccounts` changes authorization ONLY for these
/// flags; instructions that still gate on `GlobalState` directly (e.g. the
/// user-create owner override) are unaffected.
///
/// This is the canonical list the `doublezero permission audit` command consumes to
/// detect coverage gaps before strict mode is enabled. A flag omitted here is a silent
/// lockout hazard, so this MUST list every flag any processor hands to [`authorize`].
/// When a new instruction is migrated to [`authorize`], add its flag here.
pub const AUTHORIZE_GATED_FLAGS: &[u128] = &[
    permission_flags::PERMISSION_ADMIN,
    permission_flags::ACCESS_PASS_ADMIN,
    permission_flags::USER_ADMIN,
    permission_flags::NETWORK_ADMIN,
    permission_flags::INFRA_ADMIN,
    permission_flags::TENANT_ADMIN,
    permission_flags::MULTICAST_ADMIN,
    permission_flags::CONTRIBUTOR_ADMIN,
    permission_flags::GLOBALSTATE_ADMIN,
    permission_flags::TOPOLOGY_ADMIN,
    permission_flags::RESOURCE_ADMIN,
    permission_flags::INDEX_ADMIN,
    permission_flags::ACTIVATOR,
    permission_flags::HEALTH_ORACLE,
];

/// Enumerates the legacy `GlobalState` keys that authorize `any_of_flags` today, each
/// paired with a static label naming its source. This is the enumerating inverse of
/// [`check_legacy_any`]: that function answers "is this payer authorized?", this one
/// answers "which keys are authorized?".
///
/// The two MUST agree; a `#[cfg(test)]` equivalence test keeps them in sync. This
/// function exists so off-chain tooling (`doublezero permission audit`) can share a
/// single source of truth with the on-chain authorization instead of hand-mirroring
/// the mapping. It is not used on the [`authorize`] hot path (which stays
/// allocation-free via [`check_legacy_any`]).
///
/// Default (unset, all-zero) authority keys are omitted: an unset authority authorizes
/// no one, and a signer can never be the default pubkey.
pub fn legacy_keys_for_flags(
    globalstate: &GlobalState,
    any_of_flags: u128,
) -> Vec<(Pubkey, &'static str)> {
    let mut keys: Vec<(Pubkey, &'static str)> = Vec::new();

    // Flags that fall back to the foundation allowlist in legacy mode.
    const FOUNDATION_FLAGS: u128 = permission_flags::FOUNDATION
        | permission_flags::USER_ADMIN
        | permission_flags::ACCESS_PASS_ADMIN
        | permission_flags::NETWORK_ADMIN
        | permission_flags::TENANT_ADMIN
        | permission_flags::MULTICAST_ADMIN
        | permission_flags::PERMISSION_ADMIN
        | permission_flags::INFRA_ADMIN
        | permission_flags::GLOBALSTATE_ADMIN
        | permission_flags::CONTRIBUTOR_ADMIN
        | permission_flags::TOPOLOGY_ADMIN
        | permission_flags::RESOURCE_ADMIN
        | permission_flags::INDEX_ADMIN;
    if any_of_flags & FOUNDATION_FLAGS != 0 {
        for pk in &globalstate.foundation_allowlist {
            if *pk != Pubkey::default() {
                keys.push((*pk, "foundation-allowlist"));
            }
        }
    }

    if any_of_flags & permission_flags::QA != 0 {
        for pk in &globalstate.qa_allowlist {
            if *pk != Pubkey::default() {
                keys.push((*pk, "qa-allowlist"));
            }
        }
    }

    // Sentinel authorizes ACCESS_PASS_ADMIN, TENANT_ADMIN, MULTICAST_ADMIN (and SENTINEL).
    const SENTINEL_FLAGS: u128 = permission_flags::SENTINEL
        | permission_flags::ACCESS_PASS_ADMIN
        | permission_flags::TENANT_ADMIN
        | permission_flags::MULTICAST_ADMIN;
    if any_of_flags & SENTINEL_FLAGS != 0 && globalstate.sentinel_authority_pk != Pubkey::default()
    {
        keys.push((globalstate.sentinel_authority_pk, "sentinel-authority"));
    }

    // Feed authority authorizes ACCESS_PASS_ADMIN (and FEED_AUTHORITY).
    const FEED_FLAGS: u128 = permission_flags::FEED_AUTHORITY | permission_flags::ACCESS_PASS_ADMIN;
    if any_of_flags & FEED_FLAGS != 0 && globalstate.feed_authority_pk != Pubkey::default() {
        keys.push((globalstate.feed_authority_pk, "feed-authority"));
    }

    if any_of_flags & permission_flags::ACTIVATOR != 0
        && globalstate.activator_authority_pk != Pubkey::default()
    {
        keys.push((globalstate.activator_authority_pk, "activator-authority"));
    }

    if any_of_flags & permission_flags::HEALTH_ORACLE != 0
        && globalstate.health_oracle_pk != Pubkey::default()
    {
        keys.push((globalstate.health_oracle_pk, "health-oracle"));
    }

    keys
}

/// Splits the trailing accounts of a variable-length instruction into its
/// `(payer, system_program, leading, permission)` parts.
///
/// Variable-length instructions (e.g. topology clear / assign-node-segments)
/// place a caller-controlled list of accounts first; the SDK client then
/// appends `payer`, `system_program`, and — when one exists on-chain — the
/// payer's Permission PDA. `remaining` is everything left after the
/// instruction's own fixed leading accounts have been consumed.
///
/// With a Permission account present the layout is `[leading.., payer, system,
/// permission]`, so the payer sits at `n - 3` and the last account is the
/// Permission account iff it matches that payer's PDA. The returned
/// `permission` is ready to hand to [`authorize`] (via a single-element
/// iterator); `leading` is the caller's variable-length list.
///
/// Errors when the two mandatory `payer`/`system_program` accounts are absent.
#[allow(clippy::type_complexity)]
pub fn split_trailing_permission<'a, 'r, 'info>(
    program_id: &Pubkey,
    remaining: &'a [&'r AccountInfo<'info>],
) -> Result<
    (
        &'r AccountInfo<'info>,
        &'r AccountInfo<'info>,
        &'a [&'r AccountInfo<'info>],
        Option<&'r AccountInfo<'info>>,
    ),
    ProgramError,
> {
    let n = remaining.len();
    if n < 2 {
        msg!("expected at least payer and system_program accounts");
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    let permission = if n >= 3 {
        let candidate_payer = remaining[n - 3];
        let (perm_pda, _) = get_permission_pda(program_id, candidate_payer.key);
        (remaining[n - 1].key == &perm_pda).then_some(remaining[n - 1])
    } else {
        None
    };
    Ok(if permission.is_some() {
        (
            remaining[n - 3],
            remaining[n - 2],
            &remaining[..n - 3],
            permission,
        )
    } else {
        (
            remaining[n - 2],
            remaining[n - 1],
            &remaining[..n - 2],
            None,
        )
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        pda::get_permission_pda,
        state::{
            accounttype::AccountType,
            feature_flags::FeatureFlag,
            permission::{Permission, PermissionStatus},
        },
    };
    use solana_program::{account_info::AccountInfo, pubkey::Pubkey};

    // ── Helpers ──────────────────────────────────────────────────────────────

    fn gs_with_foundation(member: &Pubkey) -> GlobalState {
        GlobalState {
            foundation_allowlist: vec![*member],
            ..GlobalState::default()
        }
    }

    fn gs_with_qa(member: &Pubkey) -> GlobalState {
        GlobalState {
            qa_allowlist: vec![*member],
            ..GlobalState::default()
        }
    }

    fn gs_with_activator(activator: &Pubkey) -> GlobalState {
        GlobalState {
            activator_authority_pk: *activator,
            ..GlobalState::default()
        }
    }

    fn gs_with_sentinel(sentinel: &Pubkey) -> GlobalState {
        GlobalState {
            sentinel_authority_pk: *sentinel,
            ..GlobalState::default()
        }
    }

    fn gs_with_health_oracle(oracle: &Pubkey) -> GlobalState {
        GlobalState {
            health_oracle_pk: *oracle,
            ..GlobalState::default()
        }
    }

    fn gs_with_feed(authority: &Pubkey) -> GlobalState {
        GlobalState {
            feed_authority_pk: *authority,
            ..GlobalState::default()
        }
    }

    /// `legacy_keys_for_flags` MUST enumerate exactly the keys that
    /// `check_legacy_any` accepts, or `permission audit` (which relies on the former)
    /// would understate lockout risk relative to the on-chain check (the latter). This
    /// guards the two from drifting apart.
    #[test]
    fn test_legacy_keys_for_flags_matches_check_legacy_any() {
        // A GlobalState with every authority/allowlist populated by a distinct key.
        let foundation = Pubkey::new_unique();
        let qa = Pubkey::new_unique();
        let activator = Pubkey::new_unique();
        let sentinel = Pubkey::new_unique();
        let health_oracle = Pubkey::new_unique();
        let feed = Pubkey::new_unique();
        let outsider = Pubkey::new_unique();
        let gs = GlobalState {
            foundation_allowlist: vec![foundation],
            qa_allowlist: vec![qa],
            activator_authority_pk: activator,
            sentinel_authority_pk: sentinel,
            health_oracle_pk: health_oracle,
            feed_authority_pk: feed,
            ..GlobalState::default()
        };

        let all_flags = [
            permission_flags::FOUNDATION,
            permission_flags::PERMISSION_ADMIN,
            permission_flags::GLOBALSTATE_ADMIN,
            permission_flags::CONTRIBUTOR_ADMIN,
            permission_flags::INDEX_ADMIN,
            permission_flags::INFRA_ADMIN,
            permission_flags::NETWORK_ADMIN,
            permission_flags::TOPOLOGY_ADMIN,
            permission_flags::RESOURCE_ADMIN,
            permission_flags::TENANT_ADMIN,
            permission_flags::MULTICAST_ADMIN,
            permission_flags::FEED_AUTHORITY,
            permission_flags::ACTIVATOR,
            permission_flags::SENTINEL,
            permission_flags::USER_ADMIN,
            permission_flags::ACCESS_PASS_ADMIN,
            permission_flags::HEALTH_ORACLE,
            permission_flags::QA,
        ];
        let candidates = [
            foundation,
            qa,
            activator,
            sentinel,
            health_oracle,
            feed,
            outsider,
        ];

        for &flag in &all_flags {
            let enumerated: Vec<Pubkey> = legacy_keys_for_flags(&gs, flag)
                .into_iter()
                .map(|(k, _)| k)
                .collect();
            for &key in &candidates {
                assert_eq!(
                    enumerated.contains(&key),
                    check_legacy_any(&key, &gs, flag),
                    "mismatch for flag {flag:#x} key {key}"
                );
            }
        }

        // Every authorize-gated flag must resolve to at least one legacy key with this
        // fully-populated GlobalState — otherwise the audit could never surface a gap
        // for it.
        for &flag in AUTHORIZE_GATED_FLAGS {
            assert!(
                !legacy_keys_for_flags(&gs, flag).is_empty(),
                "no legacy keys enumerated for gated flag {flag:#x}"
            );
        }
    }

    /// Call `authorize` with NO trailing account (forces the legacy path).
    fn authorize_legacy(
        program_id: &Pubkey,
        payer: &Pubkey,
        globalstate: &GlobalState,
        flags: u128,
    ) -> ProgramResult {
        let accounts: Vec<AccountInfo> = vec![];
        let mut iter = accounts.iter();
        authorize(program_id, &mut iter, payer, globalstate, flags)
    }

    // ── Legacy path: individual flags ────────────────────────────────────────

    #[test]
    fn test_legacy_foundation_allowed() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::FOUNDATION).is_ok());
    }

    #[test]
    fn test_legacy_foundation_not_member_denied() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&Pubkey::new_unique()); // different pubkey
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::FOUNDATION).is_err());
    }

    #[test]
    fn test_legacy_qa_allowed() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_qa(&payer);
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::QA).is_ok());
    }

    #[test]
    fn test_legacy_qa_denied() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = GlobalState::default();
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::QA).is_err());
    }

    #[test]
    fn test_legacy_activator_allowed() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer);
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::ACTIVATOR).is_ok());
    }

    #[test]
    fn test_legacy_activator_denied() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&Pubkey::new_unique());
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::ACTIVATOR).is_err());
    }

    #[test]
    fn test_legacy_sentinel_allowed() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_sentinel(&payer);
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::SENTINEL).is_ok());
    }

    #[test]
    fn test_legacy_sentinel_denied() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_sentinel(&Pubkey::new_unique());
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::SENTINEL).is_err());
    }

    #[test]
    fn test_legacy_health_oracle_allowed() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_health_oracle(&payer);
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::HEALTH_ORACLE).is_ok()
        );
    }

    #[test]
    fn test_legacy_feed_allowed() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_feed(&payer);
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::FEED_AUTHORITY).is_ok()
        );
    }

    // ── Legacy path: composite flags ─────────────────────────────────────────

    #[test]
    fn test_legacy_user_admin_via_foundation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::USER_ADMIN).is_ok());
    }

    #[test]
    fn test_legacy_user_admin_activator_denied() {
        // The activator authority has been retired and no longer grants USER_ADMIN.
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer);
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::USER_ADMIN).is_err());
    }

    #[test]
    fn test_legacy_user_admin_unauthorized() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = GlobalState::default();
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::USER_ADMIN).is_err());
    }

    #[test]
    fn test_legacy_access_pass_admin_via_foundation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(authorize_legacy(
            &program_id,
            &payer,
            &gs,
            permission_flags::ACCESS_PASS_ADMIN
        )
        .is_ok());
    }

    #[test]
    fn test_legacy_access_pass_admin_via_sentinel() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_sentinel(&payer);
        assert!(authorize_legacy(
            &program_id,
            &payer,
            &gs,
            permission_flags::ACCESS_PASS_ADMIN
        )
        .is_ok());
    }

    #[test]
    fn test_legacy_access_pass_admin_via_feed() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_feed(&payer);
        assert!(authorize_legacy(
            &program_id,
            &payer,
            &gs,
            permission_flags::ACCESS_PASS_ADMIN
        )
        .is_ok());
    }

    #[test]
    fn test_legacy_access_pass_admin_unauthorized() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = GlobalState::default();
        assert!(authorize_legacy(
            &program_id,
            &payer,
            &gs,
            permission_flags::ACCESS_PASS_ADMIN
        )
        .is_err());
    }

    #[test]
    fn test_legacy_network_admin_via_foundation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::NETWORK_ADMIN).is_ok()
        );
    }

    #[test]
    fn test_legacy_network_admin_activator_denied() {
        // The activator authority has been retired and no longer grants NETWORK_ADMIN.
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer);
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::NETWORK_ADMIN).is_err()
        );
    }

    #[test]
    fn test_legacy_network_admin_unauthorized() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_sentinel(&payer); // sentinel does NOT grant NETWORK_ADMIN
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::NETWORK_ADMIN).is_err()
        );
    }

    #[test]
    fn test_legacy_tenant_admin_via_foundation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::TENANT_ADMIN).is_ok());
    }

    #[test]
    fn test_legacy_tenant_admin_via_sentinel() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_sentinel(&payer);
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::TENANT_ADMIN).is_ok());
    }

    #[test]
    fn test_legacy_tenant_admin_unauthorized() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer); // activator does NOT grant TENANT_ADMIN
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::TENANT_ADMIN).is_err()
        );
    }

    #[test]
    fn test_legacy_multicast_admin_via_foundation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::MULTICAST_ADMIN).is_ok()
        );
    }

    #[test]
    fn test_legacy_multicast_admin_activator_denied() {
        // The activator authority has been retired and no longer grants MULTICAST_ADMIN.
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer);
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::MULTICAST_ADMIN).is_err()
        );
    }

    #[test]
    fn test_legacy_multicast_admin_via_sentinel() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_sentinel(&payer);
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::MULTICAST_ADMIN).is_ok()
        );
    }

    #[test]
    fn test_legacy_multicast_admin_unauthorized() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_qa(&payer); // QA does NOT grant MULTICAST_ADMIN
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::MULTICAST_ADMIN).is_err()
        );
    }

    #[test]
    fn test_legacy_infra_admin_via_foundation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::INFRA_ADMIN).is_ok());
    }

    #[test]
    fn test_legacy_infra_admin_unauthorized() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer); // activator does NOT grant INFRA_ADMIN
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::INFRA_ADMIN).is_err());
    }

    #[test]
    fn test_legacy_globalstate_admin_via_foundation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(authorize_legacy(
            &program_id,
            &payer,
            &gs,
            permission_flags::GLOBALSTATE_ADMIN
        )
        .is_ok());
    }

    #[test]
    fn test_legacy_globalstate_admin_unauthorized() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer); // activator does NOT grant GLOBALSTATE_ADMIN
        assert!(authorize_legacy(
            &program_id,
            &payer,
            &gs,
            permission_flags::GLOBALSTATE_ADMIN
        )
        .is_err());
    }

    #[test]
    fn test_legacy_contributor_admin_via_foundation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(authorize_legacy(
            &program_id,
            &payer,
            &gs,
            permission_flags::CONTRIBUTOR_ADMIN
        )
        .is_ok());
    }

    #[test]
    fn test_legacy_contributor_admin_unauthorized() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer); // activator does NOT grant CONTRIBUTOR_ADMIN
        assert!(authorize_legacy(
            &program_id,
            &payer,
            &gs,
            permission_flags::CONTRIBUTOR_ADMIN
        )
        .is_err());
    }

    #[test]
    fn test_legacy_permission_admin_via_foundation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::PERMISSION_ADMIN).is_ok()
        );
    }

    #[test]
    fn test_legacy_permission_admin_unauthorized() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer); // activator does NOT grant PERMISSION_ADMIN
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::PERMISSION_ADMIN).is_err()
        );
    }

    #[test]
    fn test_legacy_topology_admin_via_foundation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::TOPOLOGY_ADMIN).is_ok()
        );
    }

    #[test]
    fn test_legacy_topology_admin_unauthorized() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer); // activator does NOT grant TOPOLOGY_ADMIN
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::TOPOLOGY_ADMIN).is_err()
        );
    }

    #[test]
    fn test_legacy_resource_admin_via_foundation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::RESOURCE_ADMIN).is_ok()
        );
    }

    #[test]
    fn test_legacy_resource_admin_unauthorized() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_sentinel(&payer); // sentinel does NOT grant RESOURCE_ADMIN
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::RESOURCE_ADMIN).is_err()
        );
    }

    #[test]
    fn test_legacy_index_admin_via_foundation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::INDEX_ADMIN).is_ok());
    }

    #[test]
    fn test_legacy_index_admin_unauthorized() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_qa(&payer); // QA does NOT grant INDEX_ADMIN
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::INDEX_ADMIN).is_err());
    }

    // ── RequirePermissionAccounts feature flag ────────────────────────────────

    #[test]
    fn test_feature_flag_require_permission_accounts_blocks_legacy() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let mut gs = gs_with_foundation(&payer);
        gs.feature_flags = FeatureFlag::RequirePermissionAccounts.to_mask();
        // payer IS in foundation_allowlist but feature flag blocks the legacy path
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::FOUNDATION).is_err());
    }

    #[test]
    fn test_feature_flag_without_require_does_not_block_legacy() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let mut gs = gs_with_foundation(&payer);
        gs.feature_flags = FeatureFlag::OnChainAllocationDeprecated.to_mask(); // unrelated flag
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::FOUNDATION).is_ok());
    }

    #[test]
    fn test_feature_flag_require_permission_accounts_foundation_can_still_manage_permissions() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let mut gs = gs_with_foundation(&payer);
        gs.feature_flags = FeatureFlag::RequirePermissionAccounts.to_mask();
        // Foundation member can still manage permissions even in strict mode
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::PERMISSION_ADMIN).is_ok()
        );
        // But non-foundation cannot
        let other = Pubkey::new_unique();
        assert!(
            authorize_legacy(&program_id, &other, &gs, permission_flags::PERMISSION_ADMIN).is_err()
        );
    }

    // ── Permission account path (new) ─────────────────────────────────────────

    fn make_permission_data(
        program_id: &Pubkey,
        payer: &Pubkey,
        status: PermissionStatus,
        permissions: u128,
    ) -> (Pubkey, u8, Vec<u8>) {
        let (pda, bump) = get_permission_pda(program_id, payer);
        let permission = Permission {
            account_type: AccountType::Permission,
            owner: *payer,
            bump_seed: bump,
            status,
            user_payer: *payer,
            permissions,
        };
        (pda, bump, borsh::to_vec(&permission).unwrap())
    }

    #[test]
    fn test_permission_account_activated_with_matching_flag_allowed() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Activated,
            permission_flags::USER_ADMIN,
        );

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let gs = GlobalState::default();

        assert!(authorize(
            &program_id,
            &mut iter,
            &payer,
            &gs,
            permission_flags::USER_ADMIN
        )
        .is_ok());
    }

    #[test]
    fn test_permission_account_or_semantics_any_matching_flag_allowed() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        // Permission has only ACCESS_PASS_ADMIN, but we require USER_ADMIN | ACCESS_PASS_ADMIN
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Activated,
            permission_flags::ACCESS_PASS_ADMIN,
        );

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let gs = GlobalState::default();

        assert!(authorize(
            &program_id,
            &mut iter,
            &payer,
            &gs,
            permission_flags::USER_ADMIN | permission_flags::ACCESS_PASS_ADMIN
        )
        .is_ok());
    }

    #[test]
    fn test_permission_account_multiple_flags_granted_all_work() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let all_flags = permission_flags::USER_ADMIN
            | permission_flags::ACCESS_PASS_ADMIN
            | permission_flags::NETWORK_ADMIN;
        let (pda, _, mut data) =
            make_permission_data(&program_id, &payer, PermissionStatus::Activated, all_flags);

        let mut lamports = 100_000u64;
        let gs = GlobalState::default();

        for flag in [
            permission_flags::USER_ADMIN,
            permission_flags::ACCESS_PASS_ADMIN,
            permission_flags::NETWORK_ADMIN,
        ] {
            let account = AccountInfo::new(
                &pda,
                false,
                false,
                &mut lamports,
                &mut data,
                &program_id,
                false,
            );
            let accounts = [account];
            let mut iter = accounts.iter();
            assert!(
                authorize(&program_id, &mut iter, &payer, &gs, flag).is_ok(),
                "flag {flag} should be allowed"
            );
        }
    }

    #[test]
    fn test_permission_account_insufficient_flag_denied() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        // Has QA only, but instruction requires USER_ADMIN
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Activated,
            permission_flags::QA,
        );

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let gs = GlobalState::default();

        assert!(authorize(
            &program_id,
            &mut iter,
            &payer,
            &gs,
            permission_flags::USER_ADMIN
        )
        .is_err());
    }

    #[test]
    fn test_permission_account_no_flags_denied() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, mut data) =
            make_permission_data(&program_id, &payer, PermissionStatus::Activated, 0);

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let gs = GlobalState::default();

        assert!(authorize(
            &program_id,
            &mut iter,
            &payer,
            &gs,
            permission_flags::USER_ADMIN
        )
        .is_err());
    }

    #[test]
    fn test_permission_account_suspended_denied() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Suspended,
            permission_flags::USER_ADMIN,
        );

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let gs = GlobalState::default();

        assert!(authorize(
            &program_id,
            &mut iter,
            &payer,
            &gs,
            permission_flags::USER_ADMIN
        )
        .is_err());
    }

    #[test]
    fn test_permission_account_wrong_pda_denied() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let wrong_pda = Pubkey::new_unique(); // not the real PDA for payer

        let permission = Permission {
            account_type: AccountType::Permission,
            owner: payer,
            bump_seed: 255,
            status: PermissionStatus::Activated,
            user_payer: payer,
            permissions: permission_flags::USER_ADMIN,
        };
        let mut data = borsh::to_vec(&permission).unwrap();
        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &wrong_pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let gs = GlobalState::default();

        assert_eq!(
            authorize(
                &program_id,
                &mut iter,
                &payer,
                &gs,
                permission_flags::USER_ADMIN
            )
            .unwrap_err(),
            ProgramError::InvalidArgument
        );
    }

    #[test]
    fn test_permission_account_empty_data_denied() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, _) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Activated,
            permission_flags::USER_ADMIN,
        );

        let mut data = vec![]; // uninitialized
        let mut lamports = 0u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let gs = GlobalState::default();

        assert!(authorize(
            &program_id,
            &mut iter,
            &payer,
            &gs,
            permission_flags::USER_ADMIN
        )
        .is_err());
    }

    #[test]
    fn test_permission_account_wrong_owner_denied() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let wrong_owner = Pubkey::new_unique(); // not program_id
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Activated,
            permission_flags::USER_ADMIN,
        );

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &wrong_owner,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let gs = GlobalState::default();

        assert!(authorize(
            &program_id,
            &mut iter,
            &payer,
            &gs,
            permission_flags::USER_ADMIN
        )
        .is_err());
    }

    // ── Foundation lockout recovery (Permission account present but unusable) ──

    #[test]
    fn test_permission_account_foundation_recovery_when_suspended() {
        // Foundation member whose own Permission account is suspended must still be
        // able to exercise PERMISSION_ADMIN to repair/resume it, even though the SDK
        // auto-appends the (unusable) Permission account.
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Suspended,
            permission_flags::PERMISSION_ADMIN,
        );

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let gs = gs_with_foundation(&payer);

        assert!(authorize(
            &program_id,
            &mut iter,
            &payer,
            &gs,
            permission_flags::PERMISSION_ADMIN
        )
        .is_ok());
    }

    #[test]
    fn test_permission_account_foundation_recovery_when_missing_flag() {
        // Foundation member whose Permission account lacks PERMISSION_ADMIN can still
        // manage permissions via the recovery fallback.
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Activated,
            permission_flags::QA, // no PERMISSION_ADMIN
        );

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let gs = gs_with_foundation(&payer);

        assert!(authorize(
            &program_id,
            &mut iter,
            &payer,
            &gs,
            permission_flags::PERMISSION_ADMIN
        )
        .is_ok());
    }

    #[test]
    fn test_permission_account_non_foundation_suspended_still_denied() {
        // The recovery is foundation-only: a non-foundation payer with a suspended
        // Permission account is still denied.
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Suspended,
            permission_flags::PERMISSION_ADMIN,
        );

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let gs = GlobalState::default(); // payer not in foundation

        assert_eq!(
            authorize(
                &program_id,
                &mut iter,
                &payer,
                &gs,
                permission_flags::PERMISSION_ADMIN
            )
            .unwrap_err(),
            DoubleZeroError::NotAllowed.into()
        );
    }

    #[test]
    fn test_permission_account_foundation_recovery_only_for_permission_admin() {
        // Recovery applies only to PERMISSION_ADMIN. In strict mode a foundation
        // member with a suspended Permission account cannot use it to satisfy
        // USER_ADMIN. Strict mode is required to isolate the recovery semantics:
        // while RequirePermissionAccounts is off the legacy fallback would accept
        // the foundation member (see test_permission_account_insufficient_falls_back_to_legacy_when_flag_off).
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Suspended,
            permission_flags::USER_ADMIN,
        );

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let mut gs = gs_with_foundation(&payer);
        gs.feature_flags = FeatureFlag::RequirePermissionAccounts.to_mask();

        assert_eq!(
            authorize(
                &program_id,
                &mut iter,
                &payer,
                &gs,
                permission_flags::USER_ADMIN
            )
            .unwrap_err(),
            DoubleZeroError::NotAllowed.into()
        );
    }

    #[test]
    fn test_permission_account_insufficient_falls_back_to_legacy_when_flag_off() {
        // While RequirePermissionAccounts is off, a present-but-insufficient
        // Permission account must not lock out a legacy-authorized key. The SDK
        // auto-appends the payer's Permission PDA whenever one exists on-chain, so
        // a foundation member who also holds an unrelated, under-privileged
        // Permission account must still be authorized for legacy-mapped flags.
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        // Permission account grants only QA, but the instruction needs TOPOLOGY_ADMIN.
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Activated,
            permission_flags::QA,
        );

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        // Payer is a foundation member; flag is off (default).
        let gs = gs_with_foundation(&payer);

        assert!(authorize(
            &program_id,
            &mut iter,
            &payer,
            &gs,
            permission_flags::TOPOLOGY_ADMIN
        )
        .is_ok());
    }

    #[test]
    fn test_permission_account_insufficient_denied_when_flag_on() {
        // In strict mode the legacy fallback is disabled: the same foundation
        // member with a QA-only Permission account is denied TOPOLOGY_ADMIN.
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Activated,
            permission_flags::QA,
        );

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        let mut gs = gs_with_foundation(&payer);
        gs.feature_flags = FeatureFlag::RequirePermissionAccounts.to_mask();

        assert_eq!(
            authorize(
                &program_id,
                &mut iter,
                &payer,
                &gs,
                permission_flags::TOPOLOGY_ADMIN
            )
            .unwrap_err(),
            DoubleZeroError::NotAllowed.into()
        );
    }

    // ── New path overrides feature flag enforcement ───────────────────────────

    #[test]
    fn test_permission_account_works_even_when_require_flag_set() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Activated,
            permission_flags::FOUNDATION,
        );

        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let accounts = [account];
        let mut iter = accounts.iter();
        // Feature flag is set — Permission account path should still work
        let mut gs = gs_with_foundation(&payer);
        gs.feature_flags = FeatureFlag::RequirePermissionAccounts.to_mask();

        assert!(authorize(
            &program_id,
            &mut iter,
            &payer,
            &gs,
            permission_flags::FOUNDATION
        )
        .is_ok());
    }

    // ── split_trailing_permission ─────────────────────────────────────────────
    //
    // The peeling dispatches purely on the account keys, so the backing
    // lamports/data/owner are irrelevant here — only the key at `n - 1` (matched
    // against the payer-at-`n - 3` Permission PDA) decides the split.

    /// Build AccountInfos from `keys`, borrowing per-account lamports/data so the
    /// returned Vec can be collected into the `&[&AccountInfo]` slice the peeler
    /// takes.
    fn accounts_from_keys<'a>(
        keys: &'a [Pubkey],
        lamports: &'a mut [u64],
        data: &'a mut [Vec<u8>],
        owner: &'a Pubkey,
    ) -> Vec<AccountInfo<'a>> {
        keys.iter()
            .zip(lamports.iter_mut())
            .zip(data.iter_mut())
            .map(|((k, l), d)| AccountInfo::new(k, false, false, l, d, owner, false))
            .collect()
    }

    #[test]
    fn test_split_trailing_permission_too_short_errors() {
        let program_id = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        for len in [0usize, 1] {
            let keys: Vec<Pubkey> = (0..len).map(|_| Pubkey::new_unique()).collect();
            let mut lamports = vec![0u64; len];
            let mut data = vec![Vec::<u8>::new(); len];
            let accounts = accounts_from_keys(&keys, &mut lamports, &mut data, &owner);
            let remaining: Vec<&AccountInfo> = accounts.iter().collect();
            assert_eq!(
                split_trailing_permission(&program_id, &remaining).unwrap_err(),
                DoubleZeroError::InvalidArgument.into(),
                "len {len} must error"
            );
        }
    }

    #[test]
    fn test_split_trailing_permission_tail2_payer_system_only() {
        // [payer, system] — no leading accounts, no Permission PDA.
        let program_id = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let keys = vec![Pubkey::new_unique(), Pubkey::new_unique()];
        let mut lamports = vec![0u64; keys.len()];
        let mut data = vec![Vec::<u8>::new(); keys.len()];
        let accounts = accounts_from_keys(&keys, &mut lamports, &mut data, &owner);
        let remaining: Vec<&AccountInfo> = accounts.iter().collect();

        let (payer, system, leading, permission) =
            split_trailing_permission(&program_id, &remaining).unwrap();
        assert_eq!(payer.key, &keys[0]);
        assert_eq!(system.key, &keys[1]);
        assert!(leading.is_empty());
        assert!(permission.is_none());
    }

    #[test]
    fn test_split_trailing_permission_tail3_permission_no_leading() {
        // [payer, system, permission] — the tail the SDK appends when the payer
        // has a Permission PDA and the instruction supplies no leading accounts.
        let program_id = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let payer_key = Pubkey::new_unique();
        let (perm_pda, _) = get_permission_pda(&program_id, &payer_key);
        let keys = vec![payer_key, Pubkey::new_unique(), perm_pda];
        let mut lamports = vec![0u64; keys.len()];
        let mut data = vec![Vec::<u8>::new(); keys.len()];
        let accounts = accounts_from_keys(&keys, &mut lamports, &mut data, &owner);
        let remaining: Vec<&AccountInfo> = accounts.iter().collect();

        let (payer, system, leading, permission) =
            split_trailing_permission(&program_id, &remaining).unwrap();
        assert_eq!(payer.key, &payer_key);
        assert_eq!(system.key, &keys[1]);
        assert!(leading.is_empty());
        assert_eq!(permission.map(|p| p.key), Some(&perm_pda));
    }

    #[test]
    fn test_split_trailing_permission_tail3_single_leading_no_permission() {
        // [x, payer, system] where the last account is NOT the payer's Permission
        // PDA — the trailing account must be read as system, leaving one leading
        // account, not misdetected as a Permission account.
        let program_id = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let keys = vec![
            Pubkey::new_unique(),
            Pubkey::new_unique(),
            Pubkey::new_unique(),
        ];
        let mut lamports = vec![0u64; keys.len()];
        let mut data = vec![Vec::<u8>::new(); keys.len()];
        let accounts = accounts_from_keys(&keys, &mut lamports, &mut data, &owner);
        let remaining: Vec<&AccountInfo> = accounts.iter().collect();

        let (payer, system, leading, permission) =
            split_trailing_permission(&program_id, &remaining).unwrap();
        assert_eq!(payer.key, &keys[1]);
        assert_eq!(system.key, &keys[2]);
        assert_eq!(leading.len(), 1);
        assert_eq!(leading[0].key, &keys[0]);
        assert!(permission.is_none());
    }

    #[test]
    fn test_split_trailing_permission_tail4_tenant_pair_no_permission() {
        // [old_tenant, new_tenant, payer, system] — UpdateUser's tenant-update
        // variant without a Permission account. The peeler must recover both
        // tenants as leading and detect no permission.
        let program_id = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let keys = vec![
            Pubkey::new_unique(), // old_tenant
            Pubkey::new_unique(), // new_tenant
            Pubkey::new_unique(), // payer
            Pubkey::new_unique(), // system
        ];
        let mut lamports = vec![0u64; keys.len()];
        let mut data = vec![Vec::<u8>::new(); keys.len()];
        let accounts = accounts_from_keys(&keys, &mut lamports, &mut data, &owner);
        let remaining: Vec<&AccountInfo> = accounts.iter().collect();

        let (payer, system, leading, permission) =
            split_trailing_permission(&program_id, &remaining).unwrap();
        assert_eq!(payer.key, &keys[2]);
        assert_eq!(system.key, &keys[3]);
        assert_eq!(leading.len(), 2);
        assert_eq!(leading[0].key, &keys[0]);
        assert_eq!(leading[1].key, &keys[1]);
        assert!(permission.is_none());
    }

    #[test]
    fn test_split_trailing_permission_tail5_tenant_pair_plus_permission() {
        // [old_tenant, new_tenant, payer, system, permission] — the highest-risk
        // combination: UpdateUser's tenant-update variant WITH the payer's
        // auto-appended Permission PDA (tail length 5). The payer sits at n-3, so
        // the peeler must match the last account against that payer's PDA and
        // still recover both leading tenant accounts.
        let program_id = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let payer_key = Pubkey::new_unique();
        let (perm_pda, _) = get_permission_pda(&program_id, &payer_key);
        let keys = vec![
            Pubkey::new_unique(), // old_tenant
            Pubkey::new_unique(), // new_tenant
            payer_key,            // payer (n-3)
            Pubkey::new_unique(), // system
            perm_pda,             // permission
        ];
        let mut lamports = vec![0u64; keys.len()];
        let mut data = vec![Vec::<u8>::new(); keys.len()];
        let accounts = accounts_from_keys(&keys, &mut lamports, &mut data, &owner);
        let remaining: Vec<&AccountInfo> = accounts.iter().collect();

        let (payer, system, leading, permission) =
            split_trailing_permission(&program_id, &remaining).unwrap();
        assert_eq!(payer.key, &payer_key);
        assert_eq!(system.key, &keys[3]);
        assert_eq!(leading.len(), 2);
        assert_eq!(leading[0].key, &keys[0]);
        assert_eq!(leading[1].key, &keys[1]);
        assert_eq!(permission.map(|p| p.key), Some(&perm_pda));
    }

    // ── can_grant_foundation ─────────────────────────────────────────────────

    #[test]
    fn test_can_grant_foundation_allowlist_member() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_foundation(&payer);
        // Foundation members may grant FOUNDATION even without a Permission account.
        assert!(can_grant_foundation(&program_id, None, &payer, &gs));
    }

    #[test]
    fn test_can_grant_foundation_none_denied() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = GlobalState::default();
        assert!(!can_grant_foundation(&program_id, None, &payer, &gs));
    }

    #[test]
    fn test_can_grant_foundation_flag_holder_allowed() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Activated,
            permission_flags::FOUNDATION | permission_flags::PERMISSION_ADMIN,
        );
        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let gs = GlobalState::default();
        assert!(can_grant_foundation(
            &program_id,
            Some(&account),
            &payer,
            &gs
        ));
    }

    #[test]
    fn test_can_grant_foundation_permission_admin_only_denied() {
        // A plain PERMISSION_ADMIN holder (no FOUNDATION flag, not in the allowlist)
        // must NOT be able to grant FOUNDATION.
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Activated,
            permission_flags::PERMISSION_ADMIN,
        );
        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let gs = GlobalState::default();
        assert!(!can_grant_foundation(
            &program_id,
            Some(&account),
            &payer,
            &gs
        ));
    }

    #[test]
    fn test_can_grant_foundation_suspended_flag_holder_denied() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (pda, _, mut data) = make_permission_data(
            &program_id,
            &payer,
            PermissionStatus::Suspended,
            permission_flags::FOUNDATION,
        );
        let mut lamports = 100_000u64;
        let account = AccountInfo::new(
            &pda,
            false,
            false,
            &mut lamports,
            &mut data,
            &program_id,
            false,
        );
        let gs = GlobalState::default();
        assert!(!can_grant_foundation(
            &program_id,
            Some(&account),
            &payer,
            &gs
        ));
    }
}
