//! Integration tests for onchain allocation for CreateMulticastGroup / UpdateMulticastGroup / DeleteMulticastGroup
//!
//! These tests verify that MulticastGroups can be atomically created+activated,
//! updated with IP reallocation, and deleted+deallocated+closed using
//! ResourceExtension accounts (MulticastGroupBlock).

use std::net::Ipv4Addr;

use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        multicastgroup::{
            create::MulticastGroupCreateArgs, delete::MulticastGroupDeleteArgs,
            update::MulticastGroupUpdateArgs,
        },
    },
    resource::ResourceType,
    state::{feature_flags::FeatureFlag, multicastgroup::*},
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

mod test_helpers;
use test_helpers::*;

/// Test atomic create+activate with onchain allocation
#[tokio::test]
async fn test_create_multicastgroup_atomic_with_onchain_allocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation feature flag
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let (multicast_group_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);

    let owner = Pubkey::new_unique();
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "mg1".to_string(),
            max_bandwidth: 1000,
            owner,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_group_block_pda, false),
        ],
        &payer,
    )
    .await;

    let mgroup = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup not found")
        .get_multicastgroup()
        .unwrap();

    assert_eq!(mgroup.status, MulticastGroupStatus::Activated);
    assert_ne!(
        mgroup.multicast_ip,
        Ipv4Addr::UNSPECIFIED,
        "multicast_ip should be allocated"
    );
    assert_eq!(mgroup.owner, owner);

    println!("test_create_multicastgroup_atomic_with_onchain_allocation PASSED");
}

/// Test backward compatibility: use_onchain_allocation=false creates Pending group
#[tokio::test]
async fn test_create_multicastgroup_atomic_backward_compat() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let owner = Pubkey::new_unique();
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "mg1".to_string(),
            max_bandwidth: 1000,
            owner,
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let mgroup = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup not found")
        .get_multicastgroup()
        .unwrap();

    assert_eq!(mgroup.status, MulticastGroupStatus::Pending);
    assert_eq!(mgroup.multicast_ip, Ipv4Addr::UNSPECIFIED);

    println!("test_create_multicastgroup_atomic_backward_compat PASSED");
}

/// Test atomic delete+deallocate+close for an activated multicast group
#[tokio::test]
async fn test_delete_multicastgroup_atomic_with_deallocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation feature flag
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let (multicast_group_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);

    let owner = Pubkey::new_unique();
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

    // Create with atomic onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "mg1".to_string(),
            max_bandwidth: 1000,
            owner,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_group_block_pda, false),
        ],
        &payer,
    )
    .await;

    // Verify it's activated
    let mgroup = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup not found")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup.status, MulticastGroupStatus::Activated);

    // Atomic delete+deallocate+close
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {
            use_onchain_deallocation: true,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_group_block_pda, false),
            AccountMeta::new(owner, false),
        ],
        &payer,
    )
    .await;

    // Verify account is closed
    let mgroup_after = get_account_data(&mut banks_client, mgroup_pubkey).await;
    assert!(
        mgroup_after.is_none(),
        "MulticastGroup account should be closed"
    );

    println!("test_delete_multicastgroup_atomic_with_deallocation PASSED");
}

/// Test backward compatibility: use_onchain_deallocation=false uses legacy path
#[tokio::test]
async fn test_delete_multicastgroup_atomic_backward_compat() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation feature flag for create
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let (multicast_group_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);

    let owner = Pubkey::new_unique();
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

    // Create with atomic onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "mg1".to_string(),
            max_bandwidth: 1000,
            owner,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_group_block_pda, false),
        ],
        &payer,
    )
    .await;

    // Legacy delete (use_onchain_deallocation=false, default)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify status is Deleting (legacy behavior), account still exists
    let mgroup = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup should still exist")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup.status, MulticastGroupStatus::Deleting);

    println!("test_delete_multicastgroup_atomic_backward_compat PASSED");
}

/// Test update with onchain reallocation: change multicast_ip atomically
#[tokio::test]
async fn test_update_multicastgroup_with_onchain_reallocation() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation feature flag
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let (multicast_group_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);

    let owner = Pubkey::new_unique();
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

    // Create with atomic onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "mg1".to_string(),
            max_bandwidth: 1000,
            owner,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_group_block_pda, false),
        ],
        &payer,
    )
    .await;

    let mgroup = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup not found")
        .get_multicastgroup()
        .unwrap();
    let original_ip = mgroup.multicast_ip;
    assert_ne!(original_ip, Ipv4Addr::UNSPECIFIED);

    // Update multicast_ip with onchain reallocation
    let new_ip: Ipv4Addr = "239.0.0.100".parse().unwrap();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
            code: None,
            multicast_ip: Some(new_ip),
            max_bandwidth: None,
            publisher_count: None,
            subscriber_count: None,
            use_onchain_allocation: true,
            owner: None,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_group_block_pda, false),
        ],
        &payer,
    )
    .await;

    let mgroup_updated = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup not found")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup_updated.multicast_ip, new_ip);

    println!("test_update_multicastgroup_with_onchain_reallocation PASSED");
}

/// Test backward compatibility: update without onchain allocation flag
#[tokio::test]
async fn test_update_multicastgroup_backward_compat() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Enable OnChainAllocation feature flag for create
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::OnChainAllocationDeprecated.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let (multicast_group_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);

    let owner = Pubkey::new_unique();
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (mgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

    // Create with atomic onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "mg1".to_string(),
            max_bandwidth: 1000,
            owner,
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_group_block_pda, false),
        ],
        &payer,
    )
    .await;

    // Legacy update without onchain allocation
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
            code: Some("mg1_updated".to_string()),
            multicast_ip: None,
            max_bandwidth: Some(2000),
            publisher_count: None,
            subscriber_count: None,
            use_onchain_allocation: false,
            owner: None,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let mgroup = get_account_data(&mut banks_client, mgroup_pubkey)
        .await
        .expect("MulticastGroup not found")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(mgroup.code, "mg1_updated");
    assert_eq!(mgroup.max_bandwidth, 2000);

    println!("test_update_multicastgroup_backward_compat PASSED");
}
