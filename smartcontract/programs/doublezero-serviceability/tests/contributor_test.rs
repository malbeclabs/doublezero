use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::contributor::{create::*, delete::*, resume::*, suspend::*, update::*},
    state::{accounttype::AccountType, contributor::*},
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

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

    let owner = Pubkey::new_unique();

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
    /*****************************************************************************************************************************************************/
    println!("Testing Contributor update...");
    let ops_manager_pk = Pubkey::new_unique();
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateContributor(ContributorUpdateArgs {
            code: Some("la2".to_string()),
            owner: Some(Pubkey::new_unique()),
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

    println!("✅ Contributor updated");
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
