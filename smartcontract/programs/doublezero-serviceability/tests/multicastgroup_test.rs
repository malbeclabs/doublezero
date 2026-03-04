use std::net::Ipv4Addr;

use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::multicastgroup::{
        activate::MulticastGroupActivateArgs, closeaccount::MulticastGroupDeactivateArgs,
        create::*, delete::*, reactivate::*, suspend::*, update::*,
    },
    state::{accounttype::AccountType, multicastgroup::*},
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_multicastgroup() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    /***********************************************************************************************************************************/
    println!("🟢  Start test_multicastgroup");

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    println!("🟢 1. Global Initialization...");
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

    /***********************************************************************************************************************************/
    // MulticastGroup _la

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    println!("1. Testing MulticastGroup initialization...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 0);

    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "la".to_string(),
            max_bandwidth: 1000,
            owner: Pubkey::new_unique(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup_la = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup_la.account_type, AccountType::MulticastGroup);
    assert_eq!(multicastgroup_la.code, "la".to_string());
    assert_eq!(
        multicastgroup_la.multicast_ip,
        std::net::Ipv4Addr::UNSPECIFIED
    );
    assert_eq!(multicastgroup_la.max_bandwidth, 1000);
    assert_eq!(multicastgroup_la.status, MulticastGroupStatus::Pending);

    println!("✅ MulticastGroup initialized successfully",);

    /*****************************************************************************************************************************************************/
    println!("2. Testing MulticastGroup suspend...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [224, 0, 0, 0].into(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup_la = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup_la.account_type, AccountType::MulticastGroup);
    assert_eq!(multicastgroup_la.code, "la".to_string());
    assert_eq!(multicastgroup_la.multicast_ip.to_string(), "224.0.0.0");
    assert_eq!(multicastgroup_la.status, MulticastGroupStatus::Activated);

    println!("✅ MulticastGroup activate successfully",);
    /*****************************************************************************************************************************************************/
    println!("2. Testing MulticastGroup suspend...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SuspendMulticastGroup(MulticastGroupSuspendArgs {}),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup_la = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup_la.account_type, AccountType::MulticastGroup);
    assert_eq!(multicastgroup_la.status, MulticastGroupStatus::Suspended);

    println!("✅ MulticastGroup suspended");
    /*****************************************************************************************************************************************************/
    println!("3. Testing MulticastGroup reactivated...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReactivateMulticastGroup(MulticastGroupReactivateArgs {}),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup.account_type, AccountType::MulticastGroup);
    assert_eq!(multicastgroup.status, MulticastGroupStatus::Activated);

    println!("✅ MulticastGroup reactivated");
    /*****************************************************************************************************************************************************/
    println!("4. Testing MulticastGroup update...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
            code: Some("la2".to_string()),
            multicast_ip: Some([239, 1, 1, 2].into()),
            max_bandwidth: Some(2000),
            // Keep publisher/subscriber counts at zero so that DeactivateMulticastGroup
            // can successfully close the account once it reaches Deleting status.
            publisher_count: None,
            subscriber_count: None,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup_la = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup_la.account_type, AccountType::MulticastGroup);
    assert_eq!(multicastgroup_la.code, "la2".to_string());
    assert_eq!(multicastgroup_la.multicast_ip, Ipv4Addr::new(239, 1, 1, 2));
    assert_eq!(multicastgroup_la.max_bandwidth, 2000);
    assert_eq!(multicastgroup_la.publisher_count, 0);
    assert_eq!(multicastgroup_la.subscriber_count, 0);
    assert_eq!(multicastgroup_la.status, MulticastGroupStatus::Activated);

    println!("✅ MulticastGroup updated");
    /*****************************************************************************************************************************************************/
    println!("5. Testing MulticastGroup deletion...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {}),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup_la = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup_la.account_type, AccountType::MulticastGroup);
    assert_eq!(multicastgroup_la.code, "la2".to_string());
    assert_eq!(multicastgroup_la.status, MulticastGroupStatus::Deleting);

    println!("✅ MulticastGroup deleted");
    /*****************************************************************************************************************************************************/
    println!("6. Testing MulticastGroup deactivation (final delete)...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeactivateMulticastGroup(MulticastGroupDeactivateArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(multicastgroup.owner, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup_la = get_account_data(&mut banks_client, multicastgroup_pubkey).await;
    assert_eq!(multicastgroup_la, None);

    println!("✅ MulticastGroup deleted successfully");
    println!("🟢  End test_multicastgroup");
}

#[tokio::test]
async fn test_multicastgroup_deactivate_fails_when_counts_nonzero() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    println!("🟢  Start test_multicastgroup_deactivate_fails_when_counts_nonzero");

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

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "la".to_string(),
            max_bandwidth: 1000,
            owner: Pubkey::new_unique(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Activate the multicast group so that DeleteMulticastGroup precondition
    // status == Activated is satisfied later in this test.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [224, 0, 0, 0].into(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
            code: None,
            multicast_ip: None,
            max_bandwidth: None,
            publisher_count: Some(1),
            subscriber_count: Some(1),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup.publisher_count, 1);
    assert_eq!(multicastgroup.subscriber_count, 1);

    // DeleteMulticastGroup should fail because counts are non-zero
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {}),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "DeleteMulticastGroup should fail when publisher/subscriber counts are non-zero"
    );

    // Verify the group is still Activated (delete was rejected)
    let multicastgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup.status, MulticastGroupStatus::Activated);

    println!("✅ MulticastGroup deletion correctly rejected with non-zero counts");
    println!("🟢  End test_multicastgroup_deactivate_fails_when_counts_nonzero");
}

#[tokio::test]
async fn test_multicastgroup_deactivate_fails_when_not_deleting() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    println!("🟢  Start test_multicastgroup_deactivate_fails_when_not_deleting");

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

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "la".to_string(),
            max_bandwidth: 1000,
            owner: Pubkey::new_unique(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Activate the multicast group (status = Activated, not Deleting)
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [224, 0, 0, 0].into(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup.status, MulticastGroupStatus::Activated);

    // Try to deactivate without first deleting (status is Activated, not Deleting)
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeactivateMulticastGroup(MulticastGroupDeactivateArgs {
            use_onchain_deallocation: false,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(multicastgroup.owner, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err());
    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(7)"),
        "Expected InvalidStatus error (Custom(7)), got: {}",
        error_string
    );

    println!("✅ Deactivating non-deleting multicastgroup correctly fails with InvalidStatus");
    println!("🟢  End test_multicastgroup_deactivate_fails_when_not_deleting");
}

#[tokio::test]
async fn test_multicastgroup_create_with_wrong_index_fails() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    println!("🟢  Start test_multicastgroup_create_with_wrong_index_fails");

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    println!("1. Global Initialization...");
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

    println!("2. Testing MulticastGroup creation with wrong index...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 0);

    // Client passes wrong index (999 instead of 1)
    let wrong_index = 999;
    let correct_index = globalstate_account.account_index + 1;

    // Derive PDA with the WRONG index (what a malicious/buggy client might do)
    let (wrong_multicastgroup_pubkey, _) = get_multicastgroup_pda(&program_id, wrong_index);

    // Try to create with wrong index - should fail
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "test".to_string(),
            max_bandwidth: 1000,
            owner: Pubkey::new_unique(),
        }),
        vec![
            AccountMeta::new(wrong_multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "Transaction should have failed with wrong index"
    );
    println!("✅ Correctly rejected wrong index");

    // Verify the correct index still works
    println!("3. Testing MulticastGroup creation with correct index...");
    let (correct_multicastgroup_pubkey, _) = get_multicastgroup_pda(&program_id, correct_index);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "test".to_string(),
            max_bandwidth: 1000,
            owner: Pubkey::new_unique(),
        }),
        vec![
            AccountMeta::new(correct_multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup = get_account_data(&mut banks_client, correct_multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup.index, correct_index);
    assert_eq!(multicastgroup.code, "test".to_string());
    println!("✅ Correct index accepted and stored properly");

    println!("🟢  End test_multicastgroup_create_with_wrong_index_fails");
}

#[tokio::test]
async fn test_multicastgroup_reactivate_invalid_status_fails() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    println!("🟢  Start test_multicastgroup_reactivate_invalid_status_fails");

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    println!("1. Global Initialization...");
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

    println!("2. Create MulticastGroup (status Pending)...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "reactivate-test".to_string(),
            max_bandwidth: 1000,
            owner: Pubkey::new_unique(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup.status, MulticastGroupStatus::Pending);

    println!("3. Attempt to reactivate while not Suspended (should fail)...");
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ReactivateMulticastGroup(MulticastGroupReactivateArgs {}),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "Reactivate should fail when status is not Suspended"
    );

    println!("✅ Correctly rejected ReactivateMulticastGroup for non-Suspended status");
    println!("🟢  End test_multicastgroup_reactivate_invalid_status_fails");
}

#[tokio::test]
async fn test_suspend_multicastgroup_from_pending_fails() {
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

    // Create a multicast group (starts in Pending status)
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "test".to_string(),
            max_bandwidth: 1000,
            owner: Pubkey::new_unique(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify multicast group is in Pending status
    let multicastgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup.status, MulticastGroupStatus::Pending);

    // Try to suspend from Pending (should fail with InvalidStatus)
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SuspendMulticastGroup(MulticastGroupSuspendArgs {}),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err());
    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(7)"),
        "Expected InvalidStatus error (Custom(7)), got: {}",
        error_string
    );
    println!("✅ Suspending pending multicastgroup correctly fails with InvalidStatus");
}

#[tokio::test]
async fn test_delete_multicastgroup_fails_with_active_publishers_or_subscribers() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    println!("🟢  Start test_delete_multicastgroup_fails_with_active_publishers_or_subscribers");

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    println!("1. Global Initialization...");
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

    println!("2. Create MulticastGroup...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "delete-test".to_string(),
            max_bandwidth: 1000,
            owner: Pubkey::new_unique(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    println!("3. Activate MulticastGroup...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [224, 0, 0, 1].into(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup.status, MulticastGroupStatus::Activated);

    println!("4. Set publisher_count to 1...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
            code: None,
            multicast_ip: None,
            max_bandwidth: None,
            publisher_count: Some(1),
            subscriber_count: None,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    println!("5. Try to delete with active publishers (should fail)...");
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {}),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err());
    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(71)"),
        "Expected MulticastGroupNotEmpty error (Custom(71)), got: {}",
        error_string
    );
    println!("✅ Delete correctly rejected with active publishers");

    println!("6. Reset publisher_count to 0, set subscriber_count to 1...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
            code: None,
            multicast_ip: None,
            max_bandwidth: None,
            publisher_count: Some(0),
            subscriber_count: Some(1),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    println!("7. Try to delete with active subscribers (should fail)...");
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {}),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err());
    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(71)"),
        "Expected MulticastGroupNotEmpty error (Custom(71)), got: {}",
        error_string
    );
    println!("✅ Delete correctly rejected with active subscribers");

    println!("8. Reset both counts to 0...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
            code: None,
            multicast_ip: None,
            max_bandwidth: None,
            publisher_count: Some(0),
            subscriber_count: Some(0),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    println!("9. Delete with zero counts (should succeed)...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {}),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let multicastgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
        .await
        .expect("Unable to get Account")
        .get_multicastgroup()
        .unwrap();
    assert_eq!(multicastgroup.status, MulticastGroupStatus::Deleting);
    println!("✅ Delete succeeded with zero counts");

    println!("🟢  End test_delete_multicastgroup_fails_with_active_publishers_or_subscribers");
}
