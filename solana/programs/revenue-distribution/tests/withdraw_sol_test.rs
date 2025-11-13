mod common;

//

use doublezero_revenue_distribution::{
    instruction::{ProgramConfiguration, ProgramFlagConfiguration},
    state::SolanaValidatorDeposit,
    types::{DoubleZeroEpoch, SolanaValidatorDebt},
    DOUBLEZERO_MINT_KEY,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::{signature::Keypair, signer::Signer};
use svm_hash::merkle::{merkle_root_from_indexed_pod_leaves, MerkleProof};

//
// Withdraw SOL.
//
// This test uses the mock SOL/2Z Swap program.
//

#[tokio::test]
async fn test_withdraw_sol() {
    let transfer_authority_signer = Keypair::new();

    let bootstrapped_accounts = common::generate_token_accounts_for_test(
        &DOUBLEZERO_MINT_KEY,
        &[transfer_authority_signer.pubkey()],
    );
    let src_token_account_key = bootstrapped_accounts.first().unwrap().key;

    let mut test_setup = common::start_test_with_accounts(bootstrapped_accounts).await;

    let benevolent_dictator_signer = Keypair::new();

    let solana_validator_base_block_rewards_pct_fee = 500; // 5%.

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Relay settings.
    let distribute_rewards_relay_lamports = 10_000;

    // Debt.

    let dz_epoch = DoubleZeroEpoch::new(0);

    let node_id = Pubkey::new_unique();
    let total_solana_validator_debt = 10 * u64::pow(10, 9); // 10 SOL.

    let debt_data = vec![SolanaValidatorDebt {
        node_id,
        amount: total_solana_validator_debt,
    }];

    let total_solana_validators = debt_data.len() as u32;
    let solana_validator_debt_merkle_root =
        merkle_root_from_indexed_pod_leaves(&debt_data, Some(SolanaValidatorDebt::LEAF_PREFIX))
            .unwrap();

    let proof =
        MerkleProof::from_indexed_pod_leaves(&debt_data, 0, Some(SolanaValidatorDebt::LEAF_PREFIX))
            .unwrap();

    test_setup
        .initialize_program()
        .await
        .unwrap()
        .set_admin(&benevolent_dictator_signer.pubkey())
        .await
        .unwrap()
        .initialize_journal()
        .await
        .unwrap()
        .initialize_swap_destination(&DOUBLEZERO_MINT_KEY)
        .await
        .unwrap()
        .configure_program(
            &benevolent_dictator_signer,
            [
                ProgramConfiguration::DebtAccountant(benevolent_dictator_signer.pubkey()),
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
                ProgramConfiguration::DistributeRewardsRelayLamports(
                    distribute_rewards_relay_lamports,
                ),
                ProgramConfiguration::CalculationGracePeriodMinutes(1),
                ProgramConfiguration::DistributionInitializationGracePeriodMinutes(1),
                ProgramConfiguration::Sol2zSwapProgram(mock_swap_sol_2z::ID),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .initialize_distribution(&benevolent_dictator_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .configure_distribution_debt(
            dz_epoch,
            &benevolent_dictator_signer,
            total_solana_validators,
            total_solana_validator_debt,
            solana_validator_debt_merkle_root,
        )
        .await
        .unwrap()
        .finalize_distribution_debt(dz_epoch, &benevolent_dictator_signer)
        .await
        .unwrap()
        .initialize_solana_validator_deposit(&node_id)
        .await
        .unwrap()
        .transfer_lamports(
            &SolanaValidatorDeposit::find_address(&node_id).0,
            total_solana_validator_debt,
        )
        .await
        .unwrap()
        .pay_solana_validator_debt(dz_epoch, &node_id, total_solana_validator_debt, proof)
        .await
        .unwrap();

    // Test inputs.

    let amount_2z_in = 2_500 * u64::pow(10, 8); // 2,500 2Z.
    let amount_sol_out = 2 * u64::pow(10, 9); // 2 SOL.

    let sol_destination_key = Pubkey::new_unique();

    test_setup
        .transfer_2z(&src_token_account_key, 2 * amount_2z_in)
        .await
        .unwrap();

    // Test.

    test_setup
        .mock_buy_sol(
            &src_token_account_key,
            &transfer_authority_signer,
            &sol_destination_key,
            amount_2z_in,
            amount_sol_out,
        )
        .await
        .unwrap();

    // Check the journal's balances.
    let (_, journal, _) = test_setup.fetch_journal().await;
    assert_eq!(
        journal.total_sol_balance,
        total_solana_validator_debt - amount_sol_out
    );
    assert_eq!(journal.swap_2z_destination_balance, amount_2z_in);
    assert_eq!(journal.lifetime_swapped_2z_amount(), amount_2z_in as u128);

    let sol_destination_balance = test_setup
        .context
        .banks_client
        .get_balance(sol_destination_key)
        .await
        .unwrap();
    assert_eq!(sol_destination_balance, amount_sol_out);

    let (_, program_config, _) = test_setup.fetch_program_config().await;
    let swap_destination_key = program_config
        .checked_swap_destination_2z_address()
        .unwrap();
    let swap_destination_balance = test_setup
        .fetch_token_account(&swap_destination_key)
        .await
        .unwrap()
        .amount;
    assert_eq!(swap_destination_balance, amount_2z_in);

    test_setup
        .mock_buy_sol(
            &src_token_account_key,
            &transfer_authority_signer,
            &sol_destination_key,
            amount_2z_in,
            amount_sol_out,
        )
        .await
        .unwrap();

    // Check the journal's balances.
    let (_, journal, _) = test_setup.fetch_journal().await;
    assert_eq!(
        journal.total_sol_balance,
        total_solana_validator_debt - 2 * amount_sol_out
    );
    assert_eq!(journal.swap_2z_destination_balance, 2 * amount_2z_in);
    assert_eq!(
        journal.lifetime_swapped_2z_amount(),
        2 * amount_2z_in as u128
    );
}
