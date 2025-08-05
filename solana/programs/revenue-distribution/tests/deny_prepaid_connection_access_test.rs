mod common;

//

use doublezero_revenue_distribution::{
    instruction::{JournalConfiguration, ProgramConfiguration, ProgramFlagConfiguration},
    state::PrepaidConnection,
    DOUBLEZERO_MINT_KEY,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::signature::{Keypair, Signer};

//
// Deny prepaid connection access.
//

#[tokio::test]
async fn test_deny_prepaid_connection_access() {
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

    // Relay settings.
    let termination_relay_lamports = 100_000;

    test_setup
        .transfer_lamports(&dz_ledger_sentinel_signer.pubkey(), 128 * 6_960)
        .await
        .unwrap()
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
                ProgramConfiguration::PrepaidConnectionTerminationRelayLamports(
                    termination_relay_lamports,
                ),
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

    let sentinel_balance_before = test_setup
        .banks_client
        .get_balance(dz_ledger_sentinel_signer.pubkey())
        .await
        .unwrap();

    test_setup
        .deny_prepaid_connection_access(
            &dz_ledger_sentinel_signer,
            &src_token_account_key,
            &user_key,
        )
        .await
        .unwrap();

    let (prepaid_connection_key, _) = PrepaidConnection::find_address(&user_key);
    let prepaid_connection_info = test_setup
        .banks_client
        .get_account(prepaid_connection_key)
        .await
        .unwrap();
    assert!(prepaid_connection_info.is_none());

    let sentinel_balance_after = test_setup
        .banks_client
        .get_balance(dz_ledger_sentinel_signer.pubkey())
        .await
        .unwrap();
    assert_eq!(
        sentinel_balance_after,
        sentinel_balance_before + termination_relay_lamports as u64
    );

    // Create another prepaid connection with the same user key.
    test_setup
        .initialize_prepaid_connection(
            &user_key,
            &transfer_authority_signer,
            &src_token_account_key,
            8,
        )
        .await
        .unwrap();
}
