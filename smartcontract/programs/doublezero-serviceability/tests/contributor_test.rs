use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::contributor::{create::*, delete::*, resume::*, suspend::*, update::*},
    state::{accounttype::AccountType, contributor::*},
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
async fn test_contributor() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    /***********************************************************************************************************************************/
    println!("🟢  Start test_contributor");

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
    // Contributor _la

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    println!("Testing Contributor initialization...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 0);

    let (contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

    let owner_keypair = Keypair::new();
    let owner = owner_keypair.pubkey();

    // Fund the owner keypair so it can pay for transactions
    transfer(&mut banks_client, &payer, &owner, 100_000_000).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "la".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(owner, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let contributor_la = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor_la.account_type, AccountType::Contributor);
    assert_eq!(contributor_la.code, "la".to_string());
    assert_eq!(contributor_la.status, ContributorStatus::Activated);
    assert_eq!(contributor_la.ops_manager_pk, Pubkey::default());

    println!("✅ Contributor initialized successfully",);
    /*****************************************************************************************************************************************************/
    println!("Testing Contributor suspend...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SuspendContributor(ContributorSuspendArgs {}),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let contributor_la = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor_la.account_type, AccountType::Contributor);
    assert_eq!(contributor_la.status, ContributorStatus::Suspended);

    println!("✅ Contributor suspended");
    /*****************************************************************************************************************************************************/
    println!("Testing Contributor resumed...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ResumeContributor(ContributorResumeArgs {}),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let contributor = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.account_type, AccountType::Contributor);
    assert_eq!(contributor.status, ContributorStatus::Activated);

    println!("✅ Contributor resumed");
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ResumeContributor(ContributorResumeArgs {}),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err());
    let error = result.unwrap_err();
    let error_string = format!("{:?}", error);
    assert!(
        error_string.contains("Custom(7)"),
        "Expected error to contain 'Custom(7)' (InvalidStatus), but got: {}",
        error_string
    );
    /*****************************************************************************************************************************************************/
    println!("Testing Contributor update...");
    let ops_manager_pk = Pubkey::new_unique();
    let new_owner = Pubkey::new_unique();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateContributor(ContributorUpdateArgs {
            code: Some("la2".to_string()),
            owner: Some(new_owner),
            ops_manager_pk: Some(ops_manager_pk),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let contributor_la = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor_la.account_type, AccountType::Contributor);
    assert_eq!(contributor_la.code, "la2".to_string());
    assert_eq!(contributor_la.status, ContributorStatus::Activated);
    assert_eq!(contributor_la.ops_manager_pk, ops_manager_pk);
    assert_eq!(contributor_la.owner, new_owner);

    println!("✅ Contributor updated");
    /*****************************************************************************************************************************************************/
    println!("Testing Contributor owner can update only ops_manager_pk...");
    // Create a new contributor for owner update testing since the previous one's owner was changed
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (owner_test_contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "owner_test".to_string(),
        }),
        vec![
            AccountMeta::new(owner_test_contributor_pubkey, false),
            AccountMeta::new(owner, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let owner_only_ops_manager_pk = Pubkey::new_unique();
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateContributor(ContributorUpdateArgs {
            code: None,
            owner: None,
            ops_manager_pk: Some(owner_only_ops_manager_pk),
        }),
        vec![
            AccountMeta::new(owner_test_contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &owner_keypair,
    )
    .await;

    assert!(
        res.is_ok(),
        "Owner should be able to update only ops_manager_pk"
    );

    let contributor_la = get_account_data(&mut banks_client, owner_test_contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor_la.ops_manager_pk, owner_only_ops_manager_pk);
    assert_eq!(contributor_la.code, "owner_test".to_string()); // Code should be unchanged
    assert_eq!(contributor_la.owner, owner); // Owner should be unchanged

    println!("✅ Contributor owner updated ops_manager_pk successfully");
    /*****************************************************************************************************************************************************/
    println!("Testing Contributor owner cannot update other fields...");
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateContributor(ContributorUpdateArgs {
            code: Some("newcode".to_string()),
            owner: None,
            ops_manager_pk: None,
        }),
        vec![
            AccountMeta::new(owner_test_contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &owner_keypair,
    )
    .await;

    assert!(res.is_err(), "Owner should not be able to update code");

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateContributor(ContributorUpdateArgs {
            code: None,
            owner: Some(Pubkey::new_unique()),
            ops_manager_pk: None,
        }),
        vec![
            AccountMeta::new(owner_test_contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &owner_keypair,
    )
    .await;

    assert!(res.is_err(), "Owner should not be able to update owner");

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateContributor(ContributorUpdateArgs {
            code: Some("newcode".to_string()),
            owner: Some(Pubkey::new_unique()),
            ops_manager_pk: Some(Pubkey::new_unique()),
        }),
        vec![
            AccountMeta::new(owner_test_contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &owner_keypair,
    )
    .await;

    assert!(
        res.is_err(),
        "Owner should not be able to update multiple fields"
    );

    println!("✅ Contributor owner correctly denied updating other fields");
    /*****************************************************************************************************************************************************/
    println!("Testing non-owner, non-allowlist user cannot update ops_manager_pk...");
    let unauthorized_keypair = Keypair::new();
    let unauthorized_pubkey = unauthorized_keypair.pubkey();

    // Fund the unauthorized keypair
    transfer(&mut banks_client, &payer, &unauthorized_pubkey, 100_000_000).await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateContributor(ContributorUpdateArgs {
            code: None,
            owner: None,
            ops_manager_pk: Some(Pubkey::new_unique()),
        }),
        vec![
            AccountMeta::new(owner_test_contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &unauthorized_keypair,
    )
    .await;

    assert!(
        res.is_err(),
        "Unauthorized user should not be able to update ops_manager_pk"
    );

    println!("✅ Unauthorized user correctly denied");
    /*****************************************************************************************************************************************************/
    println!("Testing foundation allowlist member can still update only ops_manager_pk...");
    let another_ops_manager_pk = Pubkey::new_unique();
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateContributor(ContributorUpdateArgs {
            code: None,
            owner: None,
            ops_manager_pk: Some(another_ops_manager_pk),
        }),
        vec![
            AccountMeta::new(owner_test_contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        res.is_ok(),
        "Foundation allowlist member should be able to update only ops_manager_pk"
    );

    let contributor_la = get_account_data(&mut banks_client, owner_test_contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor_la.ops_manager_pk, another_ops_manager_pk);

    println!("✅ Foundation allowlist member updated ops_manager_pk successfully");
    /*****************************************************************************************************************************************************/
    println!("Testing Contributor deletion...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteContributor(ContributorDeleteArgs {}),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let contributor_la = get_account_data(&mut banks_client, contributor_pubkey).await;
    assert_eq!(contributor_la, None);

    println!("✅ Contributor deleted successfully");
    println!("🟢  End test_contributor");
}

#[tokio::test]
async fn test_contributor_delete_from_suspended() {
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

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "la".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Suspend and then delete directly from Suspended
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SuspendContributor(ContributorSuspendArgs {}),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let contributor_la = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor_la.status, ContributorStatus::Suspended);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteContributor(ContributorDeleteArgs {}),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let contributor_la = get_account_data(&mut banks_client, contributor_pubkey).await;
    assert_eq!(contributor_la, None);
}
