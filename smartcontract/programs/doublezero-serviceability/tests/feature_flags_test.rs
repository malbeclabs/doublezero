use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        globalstate::setfeatureflags::SetFeatureFlagsArgs, permission::create::PermissionCreateArgs,
    },
    state::{feature_flags::FeatureFlag, permission::permission_flags},
};
use solana_program_test::*;
use solana_sdk::{
    instruction::AccountMeta,
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
