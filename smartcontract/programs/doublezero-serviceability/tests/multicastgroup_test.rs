use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::multicastgroup::{create::*, delete::*, update::*},
    resource::ResourceType,
    state::multicastgroup::*,
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_multicastgroup_deactivate_fails_when_counts_nonzero() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    println!("🟢  Start test_multicastgroup_deactivate_fails_when_counts_nonzero");

    let (_program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

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
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
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
        DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
            code: None,
            multicast_ip: None,
            max_bandwidth: None,
            publisher_count: Some(1),
            subscriber_count: Some(1),
            use_onchain_allocation: true,
            owner: None,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
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
        DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {
            use_onchain_deallocation: true,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
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
async fn test_multicastgroup_create_with_wrong_index_fails() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    println!("🟢  Start test_multicastgroup_create_with_wrong_index_fails");

    let (_program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    println!("1. Global Initialization...");
    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

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
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(wrong_multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
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
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(correct_multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
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
