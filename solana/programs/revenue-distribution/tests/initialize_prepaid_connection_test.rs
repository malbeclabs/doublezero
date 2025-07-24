mod common;

//

use doublezero_revenue_distribution::{
    instruction::JournalConfiguration, state::PrepaidConnection, DOUBLEZERO_MINT_KEY,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::signature::{Keypair, Signer};

//
// Initialize prepaid connection.
//

#[tokio::test]
async fn test_initialize_prepaid_connection() {
    let transfer_authority_signer = Keypair::new();

    let bootstrapped_accounts = common::generate_token_accounts_for_test(
        &DOUBLEZERO_MINT_KEY,
        &[transfer_authority_signer.pubkey()],
    );
    let src_token_account_key = bootstrapped_accounts.first().unwrap().key;

    let mut test_setup = common::start_test_with_accounts(bootstrapped_accounts).await;

    let admin_signer = Keypair::new();

    // Prepaid connection settings.
    let prepaid_connection_activation_cost = 20_000;

    let expected_activation_cost = u64::from(prepaid_connection_activation_cost) * u64::pow(10, 8);

    test_setup
        .transfer_2z(&src_token_account_key, expected_activation_cost)
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
        .configure_journal(
            [JournalConfiguration::ActivationCost(
                prepaid_connection_activation_cost,
            )],
            &admin_signer,
        )
        .await
        .unwrap();

    let src_balance = test_setup
        .fetch_token_account(&src_token_account_key)
        .await
        .unwrap()
        .amount;
    assert_eq!(src_balance, expected_activation_cost);

    // Test inputs.

    let user_key = Pubkey::new_unique();

    test_setup
        .initialize_prepaid_connection(
            &transfer_authority_signer,
            &src_token_account_key,
            &user_key,
            8,
        )
        .await
        .unwrap();

    // Activation fee must have been transferred from the source token account.
    let src_balance = test_setup
        .fetch_token_account(&src_token_account_key)
        .await
        .unwrap()
        .amount;
    assert_eq!(src_balance, 0);

    // Did the tokens arrive in the reserve account?
    let (_, _, reserve_2z) = test_setup.fetch_program_config().await;
    assert_eq!(reserve_2z.amount, expected_activation_cost);

    let (_, prepaid_connection) = test_setup.fetch_prepaid_connection(&user_key).await;

    let mut expected_prepaid_connection = PrepaidConnection::default();
    expected_prepaid_connection.user_key = user_key;
    expected_prepaid_connection.termination_beneficiary_key = test_setup.payer_signer.pubkey();
    assert_eq!(prepaid_connection, expected_prepaid_connection);
}
