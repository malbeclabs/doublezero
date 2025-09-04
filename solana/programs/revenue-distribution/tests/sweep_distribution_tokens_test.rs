mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::SweepDistributionTokensAccounts, ProgramConfiguration, ProgramFlagConfiguration,
        RevenueDistributionInstructionData,
    },
    state::{
        self, find_2z_token_pda_address, find_swap_authority_address, Distribution,
        SolanaValidatorDeposit,
    },
    types::{BurnRate, DoubleZeroEpoch, SolanaValidatorDebt, ValidatorFee},
    DOUBLEZERO_MINT_KEY, ID,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use svm_hash::merkle::{merkle_root_from_indexed_pod_leaves, MerkleProof};

//
// Sweep distribution tokens.
//

#[tokio::test]
async fn test_sweep_distribution_tokens() {
    let transfer_authority_signer = Keypair::new();

    let bootstrapped_accounts = common::generate_token_accounts_for_test(
        &DOUBLEZERO_MINT_KEY,
        &[transfer_authority_signer.pubkey()],
    );
    let src_token_account_key = bootstrapped_accounts.first().unwrap().key;

    let mut test_setup = common::start_test_with_accounts(bootstrapped_accounts).await;

    let admin_signer = Keypair::new();

    let payments_accountant_signer = Keypair::new();
    let rewards_accountant_signer = Keypair::new();
    let solana_validator_base_block_rewards_pct_fee = 500; // 5%.

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Relay settings.
    let distribute_rewards_relay_lamports = 10_000;

    // Distribution payments.

    let dz_epoch = DoubleZeroEpoch::new(1);

    let payments_data = (0..8)
        .map(|i| SolanaValidatorDebt {
            node_id: Pubkey::new_unique(),
            amount: 10_000_000_000 * (i + 1),
        })
        .collect::<Vec<_>>();

    let total_solana_validators = payments_data.len() as u32;
    let total_solana_validator_debt = payments_data.iter().map(|payment| payment.amount).sum();
    let solana_validator_payments_merkle_root =
        merkle_root_from_indexed_pod_leaves(&payments_data, Some(SolanaValidatorDebt::LEAF_PREFIX))
            .unwrap();

    // Do not pay all debt. Forgive one poor soul.
    let uncollectible_index = 2;
    let uncollectible_debt = payments_data[uncollectible_index];

    let expected_swept_2z_amount_1 = 69 * u64::pow(10, 8);
    let expected_swept_2z_amount_2 = 420 * u64::pow(10, 8);

    test_setup
        .transfer_2z(
            &src_token_account_key,
            expected_swept_2z_amount_1 + expected_swept_2z_amount_2,
        )
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
                ProgramConfiguration::Sol2zSwapProgram(mock_swap_sol_2z::ID),
                ProgramConfiguration::PaymentsAccountant(payments_accountant_signer.pubkey()),
                ProgramConfiguration::RewardsAccountant(rewards_accountant_signer.pubkey()),
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
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .initialize_distribution(&payments_accountant_signer)
        .await
        .unwrap()
        .initialize_distribution(&payments_accountant_signer)
        .await
        .unwrap()
        .configure_distribution_debt(
            dz_epoch,
            &payments_accountant_signer,
            total_solana_validators,
            total_solana_validator_debt,
            solana_validator_payments_merkle_root,
        )
        .await
        .unwrap()
        .finalize_distribution_debt(dz_epoch, &payments_accountant_signer)
        .await
        .unwrap()
        .initialize_swap_destination(&DOUBLEZERO_MINT_KEY)
        .await
        .unwrap();

    // 1. Initialize Solana validator deposit accounts.
    // 2. Transfer amount each validator owes so each can pay its debt.
    // 3. Pay each validator's debt.
    for (i, payment) in payments_data.iter().enumerate() {
        let node_id = &payment.node_id;

        // Just initialize this validator's deposit account.
        if i == uncollectible_index {
            test_setup
                .initialize_solana_validator_deposit(node_id)
                .await
                .unwrap();

            continue;
        }

        let proof = MerkleProof::from_indexed_pod_leaves(
            &payments_data,
            i.try_into().unwrap(),
            Some(SolanaValidatorDebt::LEAF_PREFIX),
        )
        .unwrap();

        let amount = payment.amount;

        let (deposit_key, _) = SolanaValidatorDeposit::find_address(node_id);

        test_setup
            .initialize_solana_validator_deposit(node_id)
            .await
            .unwrap()
            .transfer_lamports(&deposit_key, amount)
            .await
            .unwrap()
            .pay_solana_validator_debt(dz_epoch, node_id, amount, proof)
            .await
            .unwrap();
    }

    // Cannot sweep yet.

    let sol_2z_swap_fills_registry_key = test_setup.sol_2z_swap_fills_registry_key;

    let sweep_distribution_tokens_ix = try_build_instruction(
        &ID,
        SweepDistributionTokensAccounts::new(
            dz_epoch,
            &mock_swap_sol_2z::ID,
            &sol_2z_swap_fills_registry_key,
        ),
        &RevenueDistributionInstructionData::SweepDistributionTokens,
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(&[sweep_distribution_tokens_ix], &[])
        .await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Journal does not have enough swapped SOL to cover the SOL debt"
    );

    // Initialize another distribution. Out of convenience, use the same debt
    // calculations.

    let next_dz_epoch = dz_epoch.saturating_add_duration(1);

    test_setup
        .initialize_distribution(&payments_accountant_signer)
        .await
        .unwrap()
        .configure_distribution_debt(
            next_dz_epoch,
            &payments_accountant_signer,
            total_solana_validators,
            total_solana_validator_debt,
            solana_validator_payments_merkle_root,
        )
        .await
        .unwrap()
        .finalize_distribution_debt(next_dz_epoch, &payments_accountant_signer)
        .await
        .unwrap();

    // 1. Transfer amount each validator owes so each can pay its debt.
    // 2. Pay each validator's debt.
    for (i, payment) in payments_data.iter().enumerate() {
        let proof = MerkleProof::from_indexed_pod_leaves(
            &payments_data,
            i.try_into().unwrap(),
            Some(SolanaValidatorDebt::LEAF_PREFIX),
        )
        .unwrap();

        let node_id = &payment.node_id;
        let amount = payment.amount;

        let (deposit_key, _) = SolanaValidatorDeposit::find_address(node_id);

        test_setup
            .transfer_lamports(&deposit_key, amount)
            .await
            .unwrap()
            .pay_solana_validator_debt(next_dz_epoch, node_id, amount, proof)
            .await
            .unwrap();
    }

    let sol_destination_key = Pubkey::new_unique();

    // Swap twice to satisfy both distributions.
    test_setup
        .mock_buy_sol(
            &src_token_account_key,
            &transfer_authority_signer,
            &sol_destination_key,
            expected_swept_2z_amount_1,
            total_solana_validator_debt,
        )
        .await
        .unwrap()
        .mock_buy_sol(
            &src_token_account_key,
            &transfer_authority_signer,
            &sol_destination_key,
            expected_swept_2z_amount_2,
            total_solana_validator_debt - uncollectible_debt.amount,
        )
        .await
        .unwrap();

    // Test.

    let swap_authority_key = find_swap_authority_address().0;
    let swap_destination_key = find_2z_token_pda_address(&swap_authority_key).0;

    let swap_destination_balance_before = test_setup
        .fetch_token_account(&swap_destination_key)
        .await
        .unwrap()
        .amount;

    let (_, journal, _, _) = test_setup.fetch_journal().await;
    let journal_sol_balance_before = journal.swapped_sol_amount;

    test_setup
        .sweep_distribution_tokens(dz_epoch)
        .await
        .unwrap();

    let (_, journal, _, _) = test_setup.fetch_journal().await;
    assert_eq!(
        journal_sol_balance_before - journal.swapped_sol_amount,
        total_solana_validator_debt
    );

    // Swap destination account should have a balance change reflecting the
    // amount of SOL debt collected.
    let swap_destination_balance_after = test_setup
        .fetch_token_account(&swap_destination_key)
        .await
        .unwrap()
        .amount;
    assert_eq!(
        swap_destination_balance_before - swap_destination_balance_after,
        expected_swept_2z_amount_1
    );

    let (distribution_key, distribution, remaining_distribution_data, _, distribution_2z_token_pda) =
        test_setup.fetch_distribution(dz_epoch).await;

    assert_eq!(distribution_2z_token_pda.amount, expected_swept_2z_amount_1);

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_is_debt_calculation_finalized(true);
    expected_distribution.set_has_swept_2z_tokens(true);
    expected_distribution.bump_seed = Distribution::find_address(dz_epoch).1;
    expected_distribution.token_2z_pda_bump_seed =
        state::find_2z_token_pda_address(&distribution_key).1;
    expected_distribution.dz_epoch = dz_epoch;
    expected_distribution.community_burn_rate = BurnRate::new(initial_cbr).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(solana_validator_base_block_rewards_pct_fee).unwrap();
    expected_distribution.total_solana_validators = total_solana_validators;
    expected_distribution.solana_validator_payments_count = total_solana_validators - 1;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.collected_solana_validator_payments =
        total_solana_validator_debt - uncollectible_debt.amount;
    expected_distribution.solana_validator_payments_merkle_root =
        solana_validator_payments_merkle_root;
    expected_distribution.collected_2z_converted_from_sol = expected_swept_2z_amount_1;
    expected_distribution.processed_solana_validator_payments_end_index =
        total_solana_validators / 8;
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    assert_eq!(distribution, expected_distribution);

    assert_eq!(remaining_distribution_data, vec![0b11111011]);

    // Forgive debt for the uncollectible validator.
    let proof = MerkleProof::from_indexed_pod_leaves(
        &payments_data,
        uncollectible_index.try_into().unwrap(),
        Some(SolanaValidatorDebt::LEAF_PREFIX),
    )
    .unwrap();

    test_setup
        .forgive_solana_validator_debt(
            dz_epoch,
            next_dz_epoch,
            &payments_accountant_signer,
            &uncollectible_debt,
            proof,
        )
        .await
        .unwrap();

    // Sweep next distribution.
    test_setup
        .sweep_distribution_tokens(next_dz_epoch)
        .await
        .unwrap();

    let (_, journal, _, _) = test_setup.fetch_journal().await;
    assert_eq!(journal.total_sol_balance, 0);

    // No data in the distribution changes except for the bit reflecting the
    // uncollectible debt.
    let (_, distribution, remaining_distribution_data, _, _) =
        test_setup.fetch_distribution(dz_epoch).await;
    assert_eq!(distribution, expected_distribution);
    assert_eq!(remaining_distribution_data, vec![0b11111111]);

    let (
        distribution_key,
        distribution,
        remaining_distribution_data,
        _,
        _distribution_2z_token_pda,
    ) = test_setup.fetch_distribution(next_dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_is_debt_calculation_finalized(true);
    expected_distribution.set_has_swept_2z_tokens(true);
    expected_distribution.bump_seed = Distribution::find_address(next_dz_epoch).1;
    expected_distribution.token_2z_pda_bump_seed =
        state::find_2z_token_pda_address(&distribution_key).1;
    expected_distribution.dz_epoch = next_dz_epoch;
    expected_distribution.community_burn_rate = BurnRate::new(initial_cbr).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(solana_validator_base_block_rewards_pct_fee).unwrap();
    expected_distribution.total_solana_validators = total_solana_validators;
    expected_distribution.solana_validator_payments_count = total_solana_validators;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.collected_solana_validator_payments = total_solana_validator_debt;
    expected_distribution.solana_validator_payments_merkle_root =
        solana_validator_payments_merkle_root;
    expected_distribution.collected_2z_converted_from_sol = expected_swept_2z_amount_2;
    expected_distribution.uncollectible_sol_debt = uncollectible_debt.amount;
    expected_distribution.processed_solana_validator_payments_end_index =
        total_solana_validators / 8;
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    assert_eq!(distribution, expected_distribution);

    assert_eq!(remaining_distribution_data, vec![0b11111111]);
}
