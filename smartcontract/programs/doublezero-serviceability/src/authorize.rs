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
/// Legacy fallback mapping (used when no Permission account is provided and
/// `FeatureFlag::RequirePermissionAccounts` is not set):
///   FOUNDATION        → foundation_allowlist
///   QA                → qa_allowlist
///   ACTIVATOR         → activator_authority_pk
///   SENTINEL          → sentinel_authority_pk
///   HEALTH_ORACLE     → health_oracle_pk
///   FEED_AUTHORITY     → feed_authority_pk
///   USER_ADMIN        → foundation_allowlist OR activator_authority_pk
///   ACCESS_PASS_ADMIN → foundation_allowlist OR sentinel_authority_pk OR feed_authority_pk
///   NETWORK_ADMIN     → foundation_allowlist OR activator_authority_pk
///   TENANT_ADMIN      → foundation_allowlist OR sentinel_authority_pk
///   MULTICAST_ADMIN   → foundation_allowlist OR activator_authority_pk OR sentinel_authority_pk
///   PERMISSION_ADMIN  → foundation_allowlist (also allowed even when RequirePermissionAccounts is set)
///   INFRA_ADMIN       → foundation_allowlist
///   GLOBALSTATE_ADMIN → foundation_allowlist
///   CONTRIBUTOR_ADMIN → foundation_allowlist
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
            if permission_account.data_is_empty() {
                return Err(DoubleZeroError::NotAllowed.into());
            }
            if permission_account.owner != program_id {
                return Err(ProgramError::InvalidAccountData);
            }
            let permission = Permission::try_from(permission_account)?;
            if permission.status != PermissionStatus::Activated {
                return Err(DoubleZeroError::NotAllowed.into());
            }
            if permission.permissions & any_of_flags == 0 {
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
                if any_of_flags & permission_flags::PERMISSION_ADMIN != 0
                    && globalstate.foundation_allowlist.contains(payer_key)
                {
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
    // USER_ADMIN in legacy = foundation or activator (historical user management authorities).
    if any_of & permission_flags::USER_ADMIN != 0
        && (globalstate.foundation_allowlist.contains(payer)
            || globalstate.activator_authority_pk == *payer)
    {
        return true;
    }
    // ACCESS_PASS_ADMIN in legacy = foundation, sentinel, or feed authority.
    if any_of & permission_flags::ACCESS_PASS_ADMIN != 0
        && (globalstate.foundation_allowlist.contains(payer)
            || globalstate.sentinel_authority_pk == *payer
            || globalstate.feed_authority_pk == *payer)
    {
        return true;
    }
    // NETWORK_ADMIN in legacy = foundation or activator.
    if any_of & permission_flags::NETWORK_ADMIN != 0
        && (globalstate.foundation_allowlist.contains(payer)
            || globalstate.activator_authority_pk == *payer)
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
    // MULTICAST_ADMIN in legacy = foundation, activator, or sentinel.
    if any_of & permission_flags::MULTICAST_ADMIN != 0
        && (globalstate.foundation_allowlist.contains(payer)
            || globalstate.activator_authority_pk == *payer
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
    false
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
    use solana_program::{account_info::AccountInfo, clock::Epoch, pubkey::Pubkey};

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
    fn test_legacy_user_admin_via_activator() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer);
        assert!(authorize_legacy(&program_id, &payer, &gs, permission_flags::USER_ADMIN).is_ok());
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
    fn test_legacy_network_admin_via_activator() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer);
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::NETWORK_ADMIN).is_ok()
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
    fn test_legacy_multicast_admin_via_activator() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let gs = gs_with_activator(&payer);
        assert!(
            authorize_legacy(&program_id, &payer, &gs, permission_flags::MULTICAST_ADMIN).is_ok()
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
        gs.feature_flags = FeatureFlag::OnChainAllocation.to_mask(); // unrelated flag
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
            Epoch::default(),
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
            Epoch::default(),
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
                Epoch::default(),
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
            Epoch::default(),
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
            Epoch::default(),
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
            Epoch::default(),
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
            Epoch::default(),
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
            Epoch::default(),
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
            Epoch::default(),
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
            Epoch::default(),
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
}
