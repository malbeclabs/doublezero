mod common;

//

use doublezero_revenue_distribution::{
    instruction::{JournalConfiguration, ProgramConfiguration, ProgramFlagConfiguration},
    state::PrepaidConnection,
    types::DoubleZeroEpoch,
    DOUBLEZERO_MINT_KEY,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::signature::{Keypair, Signer};

#[tokio::test]
async fn test_terminate_prepaid_connection() {
    let transfer_authority_signer = Keypair::new();

    let bootstrapped_accounts = common::generate_token_accounts_for_test(
        &DOUBLEZERO_MINT_KEY,
        &[transfer_authority_signer.pubkey()],
    );
    let src_token_account_key = bootstrapped_accounts.first().unwrap().key;

    let mut test_setup = common::start_test_with_accounts(bootstrapped_accounts).await;

    let admin_signer = Keypair::new();
    let dz_ledger_sentinel_signer = Keypair::new();

    let debt_accountant_signer = Keypair::new();
    let solana_validator_base_block_rewards_pct_fee = 500; // 5%.

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Prepaid connection settings.
    let prepaid_connection_activation_cost = 20_000;
    let prepaid_cost_per_dz_epoch = 11_000;
    let prepaid_minimum_prepaid_dz_epochs = 1;
    let prepaid_maximum_entries = 100;

    // Relay settings.
    let prepaid_connection_termination_relay_lamports = 42_069;
    let distribute_rewards_relay_lamports = 10_000;

    let user_key = Pubkey::new_unique();
    let valid_through_dz_epoch = DoubleZeroEpoch::new(1);

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
        .configure_journal(
            &admin_signer,
            [
                JournalConfiguration::ActivationCost(prepaid_connection_activation_cost),
                JournalConfiguration::CostPerDoubleZeroEpoch(prepaid_cost_per_dz_epoch),
                JournalConfiguration::EntryBoundaries {
                    minimum_prepaid_dz_epochs: prepaid_minimum_prepaid_dz_epochs,
                    maximum_entries: prepaid_maximum_entries,
                },
            ],
        )
        .await
        .unwrap()
        .configure_program(
            &admin_signer,
            [
                ProgramConfiguration::DoubleZeroLedgerSentinel(dz_ledger_sentinel_signer.pubkey()),
                ProgramConfiguration::DebtAccountant(debt_accountant_signer.pubkey()),
                ProgramConfiguration::SolanaValidatorFeeParameters {
                    base_block_rewards_pct: solana_validator_base_block_rewards_pct_fee,
                    priority_block_rewards_pct: 0,
                    inflation_rewards_pct: 0,
                    jito_tips_pct: 0,
                    fixed_sol_amount: 0,
                    _unused: Default::default(),
                },
                ProgramConfiguration::CommunityBurnRateParameters {
                    limit: cbr_limit,
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
                },
                ProgramConfiguration::PrepaidConnectionTerminationRelayLamports(
                    prepaid_connection_termination_relay_lamports,
                ),
                ProgramConfiguration::DistributeRewardsRelayLamports(
                    distribute_rewards_relay_lamports,
                ),
                ProgramConfiguration::CalculationGracePeriodSeconds(1),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
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
        .unwrap()
        .grant_prepaid_connection_access(&dz_ledger_sentinel_signer, &user_key)
        .await
        .unwrap()
        .load_prepaid_connection(
            &user_key,
            &transfer_authority_signer,
            &src_token_account_key,
            valid_through_dz_epoch,
            8,
        )
        .await
        .unwrap();

    // Move the DZ epoch forward by two by initializing distributions.

    test_setup
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap();

    // Test inputs.

    let termination_relayer_key = Pubkey::new_unique();
    let termination_beneficiary_key = test_setup.payer_signer().pubkey();

    let relayer_balance_before = 128 * 6_960;

    test_setup
        .transfer_lamports(&termination_relayer_key, relayer_balance_before)
        .await
        .unwrap()
        .terminate_prepaid_connection(
            &user_key,
            &termination_beneficiary_key,
            Some(&termination_relayer_key),
        )
        .await
        .unwrap();

    let prepaid_connection_key = PrepaidConnection::find_address(&user_key).0;
    let closed_account = test_setup
        .context
        .banks_client
        .get_account(prepaid_connection_key)
        .await
        .unwrap();
    assert!(closed_account.is_none());

    let relayer_balance_after = test_setup
        .context
        .banks_client
        .get_balance(termination_relayer_key)
        .await
        .unwrap();
    assert_eq!(
        relayer_balance_after - relayer_balance_before,
        u64::from(prepaid_connection_termination_relay_lamports)
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
