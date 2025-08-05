mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::DenyPrepaidConnectionAccessAccounts, JournalConfiguration, ProgramConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    DOUBLEZERO_MINT_KEY, ID,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

//
// Grant prepaid connection access.
//

#[tokio::test]
async fn test_grant_prepaid_connection_access() {
    let transfer_authority_signer = Keypair::new();

    let bootstrapped_accounts = common::generate_token_accounts_for_test(
        &DOUBLEZERO_MINT_KEY,
        &[transfer_authority_signer.pubkey()],
    );
    let src_token_account_key = bootstrapped_accounts.first().unwrap().key;

    let mut test_setup = common::start_test_with_accounts(bootstrapped_accounts).await;

    let admin_signer = Keypair::new();
    let dz_ledger_sentinel_signer = Keypair::new();

    // Prepaid connection settings.
    let prepaid_activation_cost = 20_000;

    let user_key = Pubkey::new_unique();

    test_setup
        .transfer_2z(&src_token_account_key, 1_000_000 * u64::pow(10, 8))
        .await
        .unwrap()
        .initialize_program()
        .await
        .unwrap()
        .initialize_journal()
        .await
        .unwrap()
        .set_admin(&admin_signer.pubkey())
        .await
        .unwrap()
        .configure_program(
            &admin_signer,
            [
                ProgramConfiguration::DoubleZeroLedgerSentinel(dz_ledger_sentinel_signer.pubkey()),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .configure_journal(
            &admin_signer,
            [JournalConfiguration::ActivationCost(
                prepaid_activation_cost,
            )],
        )
        .await
        .unwrap()
        .initialize_prepaid_connection(
            &user_key,
            &transfer_authority_signer,
            &src_token_account_key,
            8,
        )
        .await
        .unwrap();

    // No test inputs.

    let (_, mut expected_prepaid_connection) = test_setup.fetch_prepaid_connection(&user_key).await;
    assert!(!expected_prepaid_connection.has_access_granted());

    test_setup
        .grant_prepaid_connection_access(&dz_ledger_sentinel_signer, &user_key)
        .await
        .unwrap();

    let (_, prepaid_connection) = test_setup.fetch_prepaid_connection(&user_key).await;
    expected_prepaid_connection.set_has_access_granted(true);
    assert_eq!(prepaid_connection, expected_prepaid_connection);

    // Cannot deny access to a prepaid connection that already has access.
    let deny_prepaid_connection_access_ix = try_build_instruction(
        &ID,
        DenyPrepaidConnectionAccessAccounts::new(
            &dz_ledger_sentinel_signer.pubkey(),
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
            &user_key,
        ),
        &RevenueDistributionInstructionData::DenyPrepaidConnectionAccess,
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(
            &[deny_prepaid_connection_access_ix],
            &[&dz_ledger_sentinel_signer],
        )
        .await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Prepaid connection already has access"
    );
}
