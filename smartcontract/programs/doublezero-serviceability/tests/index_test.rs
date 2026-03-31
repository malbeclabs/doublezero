use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        index::{create::IndexCreateArgs, delete::IndexDeleteArgs},
        multicastgroup::create::MulticastGroupCreateArgs,
    },
    seeds::SEED_MULTICAST_GROUP,
    state::accounttype::AccountType,
};
use solana_program_test::*;
use solana_sdk::{
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
};

mod test_helpers;
use test_helpers::*;

/// Helper: create a multicast group and return its pubkey.
/// The multicast group is created without onchain allocation (Pending status).
async fn create_multicast_group(
    banks_client: &mut BanksClient,
    payer: &Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    code: &str,
) -> Pubkey {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let globalstate = get_globalstate(banks_client, globalstate_pubkey).await;
    let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: code.to_string(),
            max_bandwidth: 1000,
            owner: Pubkey::new_unique(),
            use_onchain_allocation: false,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    mgroup_pubkey
}

#[tokio::test]
async fn test_create_index() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create a multicast group to use as the entity account
    let mgroup_pubkey = create_multicast_group(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        "test-mg",
    )
    .await;

    // Derive the Index PDA for a new code on the same entity seed
    let code = "my-index";
    let (index_pda, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, code);

    // Create the Index
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateIndex(IndexCreateArgs {
            entity_seed: String::from_utf8(SEED_MULTICAST_GROUP.to_vec()).unwrap(),
            code: code.to_string(),
        }),
        vec![
            AccountMeta::new(index_pda, false),
            AccountMeta::new_readonly(mgroup_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify the Index account exists and contains the entity pubkey
    let index_data = get_account_data(&mut banks_client, index_pda)
        .await
        .expect("Index account should exist");
    let index = index_data.get_index().unwrap();
    assert_eq!(index.account_type, AccountType::Index);
    assert_eq!(
        index.pk, mgroup_pubkey,
        "Index should point to the multicast group"
    );
}

#[tokio::test]
async fn test_create_index_duplicate_fails() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create a multicast group as the entity
    let mgroup_pubkey = create_multicast_group(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        "dup-mg",
    )
    .await;

    let code = "dup-code";
    let (index_pda, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, code);

    // First CreateIndex should succeed
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateIndex(IndexCreateArgs {
            entity_seed: String::from_utf8(SEED_MULTICAST_GROUP.to_vec()).unwrap(),
            code: code.to_string(),
        }),
        vec![
            AccountMeta::new(index_pda, false),
            AccountMeta::new_readonly(mgroup_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Wait for a new blockhash to avoid transaction deduplication
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

    // Second CreateIndex with the same entity_seed+code should fail
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateIndex(IndexCreateArgs {
            entity_seed: String::from_utf8(SEED_MULTICAST_GROUP.to_vec()).unwrap(),
            code: code.to_string(),
        }),
        vec![
            AccountMeta::new(index_pda, false),
            AccountMeta::new_readonly(mgroup_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("AccountAlreadyInitialized")
            || error_string.contains("already in use"),
        "Expected AccountAlreadyInitialized error, got: {error_string}",
    );
}

#[tokio::test]
async fn test_create_index_unauthorized_fails() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create a multicast group as the entity
    let mgroup_pubkey = create_multicast_group(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        "unauth-mg",
    )
    .await;

    // Create an unauthorized keypair with some lamports
    let unauthorized = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &unauthorized.pubkey(),
        10_000_000,
    )
    .await;

    let code = "unauth-code";
    let (index_pda, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, code);

    // Attempt CreateIndex with the unauthorized payer
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateIndex(IndexCreateArgs {
            entity_seed: String::from_utf8(SEED_MULTICAST_GROUP.to_vec()).unwrap(),
            code: code.to_string(),
        }),
        vec![
            AccountMeta::new(index_pda, false),
            AccountMeta::new_readonly(mgroup_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &unauthorized,
    )
    .await;

    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(8)"),
        "Expected NotAllowed error (Custom(8)), got: {error_string}",
    );
}

#[tokio::test]
async fn test_delete_index() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create a multicast group as the entity
    let mgroup_pubkey = create_multicast_group(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        "del-mg",
    )
    .await;

    let code = "del-code";
    let (index_pda, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, code);

    // Create the Index
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateIndex(IndexCreateArgs {
            entity_seed: String::from_utf8(SEED_MULTICAST_GROUP.to_vec()).unwrap(),
            code: code.to_string(),
        }),
        vec![
            AccountMeta::new(index_pda, false),
            AccountMeta::new_readonly(mgroup_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify the Index exists
    let index_data = get_account_data(&mut banks_client, index_pda).await;
    assert!(
        index_data.is_some(),
        "Index account should exist before deletion"
    );

    // Delete the Index
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteIndex(IndexDeleteArgs {}),
        vec![
            AccountMeta::new(index_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify the account is closed
    let index_after = get_account_data(&mut banks_client, index_pda).await;
    assert!(
        index_after.is_none(),
        "Index account should be closed after deletion"
    );
}

#[tokio::test]
async fn test_delete_index_unauthorized_fails() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    // Create a multicast group as the entity
    let mgroup_pubkey = create_multicast_group(
        &mut banks_client,
        &payer,
        program_id,
        globalstate_pubkey,
        "delauth-mg",
    )
    .await;

    let code = "delauth-code";
    let (index_pda, _) = get_index_pda(&program_id, SEED_MULTICAST_GROUP, code);

    // Create the Index with the authorized payer
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateIndex(IndexCreateArgs {
            entity_seed: String::from_utf8(SEED_MULTICAST_GROUP.to_vec()).unwrap(),
            code: code.to_string(),
        }),
        vec![
            AccountMeta::new(index_pda, false),
            AccountMeta::new_readonly(mgroup_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Create an unauthorized keypair with some lamports
    let unauthorized = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &unauthorized.pubkey(),
        10_000_000,
    )
    .await;

    // Attempt DeleteIndex with the unauthorized payer
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteIndex(IndexDeleteArgs {}),
        vec![
            AccountMeta::new(index_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &unauthorized,
    )
    .await;

    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(8)"),
        "Expected NotAllowed error (Custom(8)), got: {error_string}",
    );

    // Verify the Index is still intact
    let index_data = get_account_data(&mut banks_client, index_pda).await;
    assert!(
        index_data.is_some(),
        "Index account should still exist after unauthorized delete"
    );
}
