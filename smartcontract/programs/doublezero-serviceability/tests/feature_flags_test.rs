use doublezero_serviceability::{
    instructions::*, pda::*, processors::globalstate::setfeatureflags::SetFeatureFlagsArgs,
    state::feature_flags::FeatureFlag,
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, signature::Signer};

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

    // Set feature flags with OnChainAllocation enabled
    let feature_flags = FeatureFlag::OnChainAllocation.to_mask();
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
            FeatureFlag::OnChainAllocation,
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

    let feature_flags = FeatureFlag::OnChainAllocation.to_mask();
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
    let feature_flags = FeatureFlag::OnChainAllocation.to_mask();
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
