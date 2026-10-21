use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::*,
    pda::*,
    processors::{
        allowlist::foundation::add::AddFoundationAllowlistArgs,
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        permission::{create::PermissionCreateArgs, suspend::PermissionSuspendArgs},
    },
    state::{feature_flags::FeatureFlag, permission::permission_flags},
};

use solana_program_test::*;
use solana_sdk::{
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_set_feature_flags_success() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // Initialize global state
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify initial feature_flags is 0
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate.feature_flags, 0);

    // Set feature flags with bit 0 (deprecated OnChainAllocation slot) toggled.
    let feature_flags = FeatureFlag::OnChainAllocationDeprecated.to_mask();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs { feature_flags }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // Verify feature_flags is now set
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate.feature_flags, 1);
    assert!(
        doublezero_serviceability::state::feature_flags::is_feature_enabled(
            globalstate.feature_flags,
            FeatureFlag::OnChainAllocationDeprecated,
        )
    );

    println!("✅ SetFeatureFlags succeeded");
}

#[tokio::test]
async fn test_set_feature_flags_non_foundation_fails() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // Initialize global state
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Try to set feature flags with a non-foundation member
    let non_foundation = solana_sdk::signature::Keypair::new();

    // Fund the non-foundation keypair
    transfer(
        &mut banks_client,
        &payer,
        &non_foundation.pubkey(),
        1_000_000_000,
    )
    .await;

    let feature_flags = FeatureFlag::OnChainAllocationDeprecated.to_mask();
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs { feature_flags }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &non_foundation,
    )
    .await;

    assert!(result.is_err(), "Should fail with non-foundation member");
    println!("✅ SetFeatureFlags correctly rejected non-foundation member");
}

/// A non-foundation key holding a GLOBALSTATE_ADMIN Permission account can set
/// feature flags — exercises the new Permission-account authorization path.
#[tokio::test]
async fn test_set_feature_flags_with_permission_account_allowed() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // Initialize global state (payer is the foundation member).
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // A keypair that is NOT in the foundation allowlist.
    let gs_admin = Keypair::new();
    transfer(&mut banks_client, &payer, &gs_admin.pubkey(), 10_000_000).await;

    // Foundation grants it a Permission account with GLOBALSTATE_ADMIN.
    let (permission_pda, _) = get_permission_pda(&program_id, &gs_admin.pubkey());
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: gs_admin.pubkey(),
            permissions: permission_flags::GLOBALSTATE_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // The GLOBALSTATE_ADMIN holder sets feature flags, passing its Permission PDA
    // as the optional trailing account that authorize() reads.
    let feature_flags = FeatureFlag::OnChainAllocationDeprecated.to_mask();
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    let mut tx = create_transaction_with_extra_accounts(
        program_id,
        &DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs { feature_flags }),
        &vec![AccountMeta::new(globalstate_pubkey, false)],
        &gs_admin,
        &[AccountMeta::new_readonly(permission_pda, false)],
    );
    tx.try_sign(&[&gs_admin], recent_blockhash).unwrap();
    banks_client
        .process_transaction(tx)
        .await
        .expect("GLOBALSTATE_ADMIN permission holder should be able to set feature flags");

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate.feature_flags, feature_flags);

    println!("✅ SetFeatureFlags with GLOBALSTATE_ADMIN permission succeeded");
}

#[tokio::test]
async fn test_feature_flags_persistence() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // Initialize global state
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Set feature flags
    let feature_flags = FeatureFlag::OnChainAllocationDeprecated.to_mask();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs { feature_flags }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // Read back and verify
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate.feature_flags, feature_flags);

    // Set to 0 (clear all flags)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs { feature_flags: 0 }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate.feature_flags, 0);

    println!("✅ Feature flags persistence verified");
}

// --- Foundation strict-mode rollback (foundation_strict_mode_disable_recovery) ---

fn strict() -> u128 {
    FeatureFlag::RequirePermissionAccounts.to_mask()
}

async fn setup_globalstate() -> (BanksClient, Pubkey, Keypair, Pubkey) {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    (banks_client, program_id, payer, globalstate_pubkey)
}

/// Sets `RequirePermissionAccounts | other_flags` through the payer's foundation authority
/// (legacy path when strict mode is off).
async fn enable_strict_mode(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    payer: &Keypair,
    globalstate_pubkey: Pubkey,
    other_flags: u128,
) {
    let recent_blockhash = wait_for_new_blockhash(banks_client).await;
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: strict() | other_flags,
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        payer,
    )
    .await;
    let globalstate = get_globalstate(banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate.feature_flags, strict() | other_flags);
}

/// Creates a funded keypair, optionally adds it to the foundation allowlist, and
/// optionally grants it a Permission account (suspended if requested). Must run while
/// strict mode is off. Returns the keypair and its Permission PDA.
async fn new_signer(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    payer: &Keypair,
    globalstate_pubkey: Pubkey,
    foundation: bool,
    permissions: Option<u128>,
    suspended: bool,
) -> (Keypair, Pubkey) {
    let signer = Keypair::new();
    transfer(banks_client, payer, &signer.pubkey(), 1_000_000_000).await;
    let recent_blockhash = wait_for_new_blockhash(banks_client).await;

    if foundation {
        execute_transaction(
            banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AddFoundationAllowlist(AddFoundationAllowlistArgs {
                pubkey: signer.pubkey(),
            }),
            vec![AccountMeta::new(globalstate_pubkey, false)],
            payer,
        )
        .await;
    }

    let (permission_pda, _) = get_permission_pda(&program_id, &signer.pubkey());
    if let Some(permissions) = permissions {
        execute_transaction(
            banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
                user_payer: signer.pubkey(),
                permissions,
            }),
            vec![
                AccountMeta::new(permission_pda, false),
                AccountMeta::new_readonly(globalstate_pubkey, false),
            ],
            payer,
        )
        .await;
        if suspended {
            execute_transaction(
                banks_client,
                recent_blockhash,
                program_id,
                DoubleZeroInstruction::SuspendPermission(PermissionSuspendArgs {}),
                vec![
                    AccountMeta::new(permission_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                ],
                payer,
            )
            .await;
        }
    }

    (signer, permission_pda)
}

async fn try_set_flags(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    signer: &Keypair,
    globalstate_pubkey: Pubkey,
    feature_flags: u128,
    extra_accounts: &[AccountMeta],
) -> Result<(), BanksClientError> {
    let recent_blockhash = wait_for_new_blockhash(banks_client).await;
    try_execute_transaction_with_extra_accounts(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs { feature_flags }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        signer,
        extra_accounts,
    )
    .await
}

/// A foundation member with no Permission account can disable strict mode, with or
/// without its (nonexistent) Permission PDA attached. Other flags are preserved.
#[tokio::test]
async fn test_foundation_disables_strict_mode_without_permission_account() {
    let other = FeatureFlag::AllowBondedFeeds.to_mask();

    let (mut banks_client, program_id, payer, globalstate_pubkey) = setup_globalstate().await;
    let (member, permission_pda) = new_signer(
        &mut banks_client,
        program_id,
        &payer,
        globalstate_pubkey,
        true,
        None,
        false,
    )
    .await;
    enable_strict_mode(
        &mut banks_client,
        program_id,
        &payer,
        globalstate_pubkey,
        other,
    )
    .await;

    // Strict mode denies this member any other GLOBALSTATE_ADMIN change.
    let result = try_set_flags(
        &mut banks_client,
        program_id,
        &member,
        globalstate_pubkey,
        strict(),
        &[],
    )
    .await;
    assert_custom_error(&result.unwrap_err(), DoubleZeroError::NotAllowed);

    // No trailing Permission account.
    try_set_flags(
        &mut banks_client,
        program_id,
        &member,
        globalstate_pubkey,
        other,
        &[],
    )
    .await
    .expect("foundation member should be able to disable strict mode");
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate.feature_flags, other);

    // Uninitialized Permission PDA attached: authorize() would reject it with
    // InvalidAccountData since it is not program-owned.
    enable_strict_mode(
        &mut banks_client,
        program_id,
        &payer,
        globalstate_pubkey,
        other,
    )
    .await;
    try_set_flags(
        &mut banks_client,
        program_id,
        &member,
        globalstate_pubkey,
        other,
        &[AccountMeta::new_readonly(permission_pda, false)],
    )
    .await
    .expect("foundation member should disable strict mode with a missing Permission PDA");
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate.feature_flags, other);
}

/// A foundation member whose Permission account is under-privileged or suspended can
/// still disable strict mode with that Permission PDA attached (as the SDK does).
#[tokio::test]
async fn test_foundation_disables_strict_mode_with_unusable_permission_account() {
    let (mut banks_client, program_id, payer, globalstate_pubkey) = setup_globalstate().await;
    let (underprivileged, underprivileged_pda) = new_signer(
        &mut banks_client,
        program_id,
        &payer,
        globalstate_pubkey,
        true,
        Some(permission_flags::ACTIVATOR),
        false,
    )
    .await;
    let (suspended, suspended_pda) = new_signer(
        &mut banks_client,
        program_id,
        &payer,
        globalstate_pubkey,
        true,
        Some(permission_flags::GLOBALSTATE_ADMIN),
        true,
    )
    .await;

    for (member, pda) in [
        (&underprivileged, underprivileged_pda),
        (&suspended, suspended_pda),
    ] {
        enable_strict_mode(&mut banks_client, program_id, &payer, globalstate_pubkey, 0).await;

        // The normal path rejects this Permission account under strict mode.
        let result = try_set_flags(
            &mut banks_client,
            program_id,
            member,
            globalstate_pubkey,
            strict() | FeatureFlag::AllowBondedFeeds.to_mask(),
            &[AccountMeta::new_readonly(pda, false)],
        )
        .await;
        assert_custom_error(&result.unwrap_err(), DoubleZeroError::NotAllowed);

        try_set_flags(
            &mut banks_client,
            program_id,
            member,
            globalstate_pubkey,
            0,
            &[AccountMeta::new_readonly(pda, false)],
        )
        .await
        .expect("foundation member should be able to disable strict mode");
        let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate.feature_flags, 0);
    }
}

/// The recovery covers only clearing RequirePermissionAccounts: a foundation member
/// without GLOBALSTATE_ADMIN cannot use it for a no-op, to enable a flag, or to change
/// another flag alongside disabling strict mode.
#[tokio::test]
async fn test_foundation_strict_mode_recovery_cannot_change_other_flags() {
    let other = FeatureFlag::AllowBondedFeeds.to_mask();
    let ip_proof = FeatureFlag::RequireIpOwnershipProof.to_mask();

    let (mut banks_client, program_id, payer, globalstate_pubkey) = setup_globalstate().await;
    let (member, permission_pda) = new_signer(
        &mut banks_client,
        program_id,
        &payer,
        globalstate_pubkey,
        true,
        Some(permission_flags::ACTIVATOR),
        false,
    )
    .await;
    enable_strict_mode(
        &mut banks_client,
        program_id,
        &payer,
        globalstate_pubkey,
        other,
    )
    .await;

    for extra in [
        vec![],
        vec![AccountMeta::new_readonly(permission_pda, false)],
    ] {
        for flags in [
            // No-op: strict stays on.
            strict() | other,
            // Enable another flag while strict stays on.
            strict() | other | ip_proof,
            // Disable strict but also enable another flag.
            other | ip_proof,
            // Disable strict but also clear another flag.
            0,
        ] {
            let result = try_set_flags(
                &mut banks_client,
                program_id,
                &member,
                globalstate_pubkey,
                flags,
                &extra,
            )
            .await;
            assert_custom_error(&result.unwrap_err(), DoubleZeroError::NotAllowed);
            let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
            assert_eq!(globalstate.feature_flags, strict() | other);
        }
    }
}

/// A key outside the foundation allowlist cannot disable strict mode, with no
/// Permission account or with an under-privileged one.
#[tokio::test]
async fn test_non_foundation_cannot_disable_strict_mode() {
    let (mut banks_client, program_id, payer, globalstate_pubkey) = setup_globalstate().await;
    let (no_permission, no_permission_pda) = new_signer(
        &mut banks_client,
        program_id,
        &payer,
        globalstate_pubkey,
        false,
        None,
        false,
    )
    .await;
    let (underprivileged, underprivileged_pda) = new_signer(
        &mut banks_client,
        program_id,
        &payer,
        globalstate_pubkey,
        false,
        Some(permission_flags::PERMISSION_ADMIN),
        false,
    )
    .await;
    enable_strict_mode(&mut banks_client, program_id, &payer, globalstate_pubkey, 0).await;

    let result = try_set_flags(
        &mut banks_client,
        program_id,
        &no_permission,
        globalstate_pubkey,
        0,
        &[],
    )
    .await;
    assert_custom_error(&result.unwrap_err(), DoubleZeroError::NotAllowed);

    // An uninitialized PDA counts as no Permission account.
    let result = try_set_flags(
        &mut banks_client,
        program_id,
        &no_permission,
        globalstate_pubkey,
        0,
        &[AccountMeta::new_readonly(no_permission_pda, false)],
    )
    .await;
    assert_custom_error(&result.unwrap_err(), DoubleZeroError::NotAllowed);

    let result = try_set_flags(
        &mut banks_client,
        program_id,
        &underprivileged,
        globalstate_pubkey,
        0,
        &[AccountMeta::new_readonly(underprivileged_pda, false)],
    )
    .await;
    assert_custom_error(&result.unwrap_err(), DoubleZeroError::NotAllowed);

    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate.feature_flags, strict());
}
