mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::SweepDistributionTokensAccounts, ProgramConfiguration,
        ProgramFeatureConfiguration, ProgramFlagConfiguration, RevenueDistributionInstructionData,
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
use svm_hash::{
    merkle::{merkle_root_from_indexed_pod_leaves, MerkleProof},
    sha2::Hash,
};

//
// Setup.
//

struct SweepDistributionTokensSetup {
    test_setup: common::ProgramTestWithOwner,
    debt_accountant_signer: Keypair,
    rewards_accountant_signer: Keypair,
    src_token_account_key: Pubkey,
    transfer_authority_signer: Keypair,
    debt_data: Vec<SolanaValidatorDebt>,
    total_solana_validators: u32,
    total_solana_validator_debt: u64,
    solana_validator_debt_merkle_root: Hash,
    uncollectible_debt: SolanaValidatorDebt,
    expected_swept_2z_amount_1: u64,
    expected_swept_2z_amount_2: u64,
    total_contributors: u32,
    rewards_merkle_root: Hash,
    dz_epoch: DoubleZeroEpoch,
    next_dz_epoch: DoubleZeroEpoch,
    and_another_dz_epoch: DoubleZeroEpoch,
}

/// Set up a configured program with two distributions, debt paid (except one
/// uncollectible validator), rewards configured, and swap destination ready.
/// Uses custom setup because it needs `start_test_with_accounts` for
/// pre-bootstrapped token accounts and extra config (`Sol2zSwapProgram`,
/// `FeatureActivation`, `MinimumEpochDurationToFinalizeRewards`).
async fn setup_for_sweep_distribution_tokens() -> SweepDistributionTokensSetup {
    let transfer_authority_signer = Keypair::new();

    let bootstrapped_accounts = common::generate_token_accounts_for_test(
        &DOUBLEZERO_MINT_KEY,
        &[transfer_authority_signer.pubkey()],
    );
    let src_token_account_key = bootstrapped_accounts.first().unwrap().key;

    let mut test_setup = common::start_test_with_accounts(bootstrapped_accounts).await;

    let admin_signer = Keypair::new();
    let debt_accountant_signer = Keypair::new();
    let rewards_accountant_signer = Keypair::new();

    let initial_cbr = 100_000_000;
    let cbr_limit = 500_000_000;
    let solana_validator_base_block_rewards_pct_fee = 500;
    let distribute_rewards_relay_lamports = 10_000;
    let minimum_epoch_duration_to_finalize_rewards = 1;

    let debt_data = (0..8)
        .map(|i| SolanaValidatorDebt {
            node_id: Pubkey::new_unique(),
            amount: 10_000_000_000 * (i + 1),
        })
        .collect::<Vec<_>>();

    let total_solana_validators = debt_data.len() as u32;
    let total_solana_validator_debt = debt_data.iter().map(|debt| debt.amount).sum();
    let solana_validator_debt_merkle_root =
        merkle_root_from_indexed_pod_leaves(&debt_data, Some(SolanaValidatorDebt::LEAF_PREFIX))
            .unwrap();

    let uncollectible_index = 2;
    let uncollectible_debt = debt_data[uncollectible_index];

    let expected_swept_2z_amount_1 = 69 * u64::pow(10, 8);
    let expected_swept_2z_amount_2 = 420 * u64::pow(10, 8);

    let total_contributors = 2;
    let rewards_merkle_root = Hash::new_unique();

    let dz_epoch = DoubleZeroEpoch::new(0);
    let next_dz_epoch = dz_epoch.saturating_add_duration(1);

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
                ProgramConfiguration::DebtAccountant(debt_accountant_signer.pubkey()),
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
                    dz_epochs_to_increasing: 10,
                    dz_epochs_to_limit: 20,
                    initial_rate: Some(initial_cbr),
                },
                ProgramConfiguration::DistributeRewardsRelayLamports(
                    distribute_rewards_relay_lamports,
                ),
                ProgramConfiguration::CalculationGracePeriodMinutes(1),
                ProgramConfiguration::DistributionInitializationGracePeriodMinutes(1),
                ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(
                    minimum_epoch_duration_to_finalize_rewards,
                ),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
                ProgramConfiguration::FeatureActivation {
                    feature: ProgramFeatureConfiguration::SolanaValidatorDebtWriteOff,
                    activation_epoch: DoubleZeroEpoch::new(1),
                },
            ],
        )
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .configure_distribution_debt(
            next_dz_epoch,
            &debt_accountant_signer,
            total_solana_validators,
            total_solana_validator_debt,
            solana_validator_debt_merkle_root,
        )
        .await
        .unwrap()
        .finalize_distribution_debt(next_dz_epoch, &debt_accountant_signer)
        .await
        .unwrap()
        .enable_solana_validator_debt_write_off(next_dz_epoch)
        .await
        .unwrap()
        .configure_distribution_rewards(
            next_dz_epoch,
            &rewards_accountant_signer,
            total_contributors,
            rewards_merkle_root,
        )
        .await
        .unwrap()
        .initialize_swap_destination(&DOUBLEZERO_MINT_KEY)
        .await
        .unwrap();

    // Pay all validators except the uncollectible one.
    for (i, debt) in debt_data.iter().enumerate() {
        let node_id = &debt.node_id;

        if i == uncollectible_index {
            test_setup
                .initialize_solana_validator_deposit(node_id)
                .await
                .unwrap();
            continue;
        }

        let proof = MerkleProof::from_indexed_pod_leaves(
            &debt_data,
            i.try_into().unwrap(),
            Some(SolanaValidatorDebt::LEAF_PREFIX),
        )
        .unwrap();

        let (deposit_key, _) = SolanaValidatorDeposit::find_address(node_id);

        test_setup
            .initialize_solana_validator_deposit(node_id)
            .await
            .unwrap()
            .transfer_lamports(&deposit_key, debt.amount)
            .await
            .unwrap()
            .pay_solana_validator_debt(next_dz_epoch, debt, proof)
            .await
            .unwrap();
    }

    let and_another_dz_epoch = next_dz_epoch.saturating_add_duration(1);

    SweepDistributionTokensSetup {
        test_setup,
        debt_accountant_signer,
        rewards_accountant_signer,
        src_token_account_key,
        transfer_authority_signer,
        debt_data,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
        uncollectible_debt,
        expected_swept_2z_amount_1,
        expected_swept_2z_amount_2,
        total_contributors,
        rewards_merkle_root,
        dz_epoch,
        next_dz_epoch,
        and_another_dz_epoch,
    }
}

//
// Sweep distribution tokens — happy path with sequential error checks.
//

#[tokio::test]
async fn test_sweep_distribution_tokens() {
    let SweepDistributionTokensSetup {
        mut test_setup,
        debt_accountant_signer,
        rewards_accountant_signer,
        src_token_account_key,
        transfer_authority_signer,
        debt_data,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
        uncollectible_debt,
        expected_swept_2z_amount_1,
        expected_swept_2z_amount_2,
        total_contributors,
        rewards_merkle_root,
        dz_epoch,
        next_dz_epoch,
        and_another_dz_epoch,
        ..
    } = setup_for_sweep_distribution_tokens().await;

    let initial_cbr = 100_000_000;
    let solana_validator_base_block_rewards_pct_fee = 500;
    let distribute_rewards_relay_lamports = 10_000;

    // Cannot sweep until distribution 0 is finalized.
    let sweep_distribution_tokens_ix = try_build_instruction(
        &ID,
        SweepDistributionTokensAccounts::new(
            dz_epoch,
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
        ),
        &RevenueDistributionInstructionData::SweepDistributionTokens,
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(&[sweep_distribution_tokens_ix], &[])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Distribution rewards have not been finalized"
    );

    test_setup
        .finalize_distribution_debt(dz_epoch, &debt_accountant_signer)
        .await
        .unwrap()
        .finalize_distribution_rewards(dz_epoch)
        .await
        .unwrap()
        .finalize_distribution_rewards(next_dz_epoch)
        .await
        .unwrap();

    // Cannot sweep until distribution 0 is swept.
    let sweep_distribution_tokens_ix = try_build_instruction(
        &ID,
        SweepDistributionTokensAccounts::new(
            next_dz_epoch,
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
        ),
        &RevenueDistributionInstructionData::SweepDistributionTokens,
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(&[sweep_distribution_tokens_ix], &[])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Can only sweep tokens for DZ epoch 0"
    );

    test_setup
        .sweep_distribution_tokens(dz_epoch)
        .await
        .unwrap();

    // Cannot sweep if there is not enough swapped SOL to cover the debt.
    let sol_2z_swap_fills_registry_key = test_setup.sol_2z_swap_fills_registry_key;

    let sweep_distribution_tokens_ix = try_build_instruction(
        &ID,
        SweepDistributionTokensAccounts::new(
            next_dz_epoch,
            &mock_swap_sol_2z::ID,
            &sol_2z_swap_fills_registry_key,
        ),
        &RevenueDistributionInstructionData::SweepDistributionTokens,
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(&[sweep_distribution_tokens_ix], &[])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Journal does not have enough swapped SOL to cover the SOL debt"
    );

    // Initialize another distribution. Out of convenience, use the same debt
    // calculations.
    test_setup
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .configure_distribution_debt(
            and_another_dz_epoch,
            &debt_accountant_signer,
            total_solana_validators,
            total_solana_validator_debt,
            solana_validator_debt_merkle_root,
        )
        .await
        .unwrap()
        .finalize_distribution_debt(and_another_dz_epoch, &debt_accountant_signer)
        .await
        .unwrap()
        .configure_distribution_rewards(
            and_another_dz_epoch,
            &rewards_accountant_signer,
            total_contributors,
            rewards_merkle_root,
        )
        .await
        .unwrap()
        .finalize_distribution_rewards(and_another_dz_epoch)
        .await
        .unwrap();

    // Pay all validators' debt for the new distribution.
    for (i, debt) in debt_data.iter().enumerate() {
        let proof = MerkleProof::from_indexed_pod_leaves(
            &debt_data,
            i.try_into().unwrap(),
            Some(SolanaValidatorDebt::LEAF_PREFIX),
        )
        .unwrap();

        let (deposit_key, _) = SolanaValidatorDeposit::find_address(&debt.node_id);

        test_setup
            .transfer_lamports(&deposit_key, debt.amount)
            .await
            .unwrap()
            .pay_solana_validator_debt(and_another_dz_epoch, debt, proof)
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

    let (_, journal, _) = test_setup.fetch_journal().await;
    let journal_sol_balance_before = journal.swapped_sol_amount;

    test_setup
        .sweep_distribution_tokens(next_dz_epoch)
        .await
        .unwrap();

    let (_, journal, _) = test_setup.fetch_journal().await;
    assert_eq!(
        journal_sol_balance_before - journal.swapped_sol_amount,
        total_solana_validator_debt
    );

    let swap_destination_balance_after = test_setup
        .fetch_token_account(&swap_destination_key)
        .await
        .unwrap()
        .amount;
    assert_eq!(
        swap_destination_balance_before - swap_destination_balance_after,
        expected_swept_2z_amount_1
    );
    assert_eq!(
        swap_destination_balance_after,
        journal.swap_2z_destination_balance
    );
    assert_eq!(
        journal.lifetime_swapped_2z_amount(),
        u128::from(expected_swept_2z_amount_1 + expected_swept_2z_amount_2)
    );

    let (distribution_key, distribution, remaining_distribution_data, _, distribution_2z_token_pda) =
        test_setup.fetch_distribution(next_dz_epoch).await;

    assert_eq!(distribution_2z_token_pda.amount, expected_swept_2z_amount_1);

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_is_debt_calculation_finalized(true);
    expected_distribution.set_is_rewards_calculation_finalized(true);
    expected_distribution.set_has_swept_2z_tokens(true);
    expected_distribution.set_is_solana_validator_debt_write_off_enabled(true);
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
    expected_distribution.solana_validator_payments_count = total_solana_validators - 1;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.collected_solana_validator_payments =
        total_solana_validator_debt - uncollectible_debt.amount;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.total_contributors = total_contributors;
    expected_distribution.rewards_merkle_root = rewards_merkle_root;
    expected_distribution.collected_2z_converted_from_sol = expected_swept_2z_amount_1;
    expected_distribution.processed_solana_validator_debt_end_index = total_solana_validators / 8;
    expected_distribution.processed_solana_validator_debt_write_off_start_index =
        total_solana_validators / 8;
    expected_distribution.processed_solana_validator_debt_write_off_end_index =
        2 * (total_solana_validators / 8);
    expected_distribution.processed_rewards_start_index = 2 * (total_solana_validators / 8);
    expected_distribution.processed_rewards_end_index =
        2 * (total_solana_validators / 8) + (total_contributors / 8 + 1);
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp = test_setup
        .get_clock()
        .await
        .unix_timestamp
        .saturating_sub(60) as u32;
    assert_eq!(distribution, expected_distribution);

    // First byte reflects debt tracking.
    let processed_debt_bitmap =
        &remaining_distribution_data[distribution.processed_solana_validator_debt_bitmap_range()];
    assert_eq!(processed_debt_bitmap, [0b11111011]);

    // Second byte reflects write off tracking.
    let write_off_bitmap = &remaining_distribution_data
        [distribution.processed_solana_validator_debt_write_off_bitmap_range()];
    assert_eq!(write_off_bitmap, [0]);

    // Third byte reflects rewards tracking.
    let rewards_bitmap =
        &remaining_distribution_data[distribution.processed_rewards_bitmap_range()];
    assert_eq!(rewards_bitmap, [0]);

    // Write off debt for the uncollectible validator.
    let uncollectible_index = 2;
    let proof = MerkleProof::from_indexed_pod_leaves(
        &debt_data,
        uncollectible_index,
        Some(SolanaValidatorDebt::LEAF_PREFIX),
    )
    .unwrap();

    test_setup
        .write_off_solana_validator_debt(
            next_dz_epoch,
            and_another_dz_epoch,
            &debt_accountant_signer,
            &uncollectible_debt,
            proof,
        )
        .await
        .unwrap();

    // Sweep next distribution.
    test_setup
        .sweep_distribution_tokens(and_another_dz_epoch)
        .await
        .unwrap();

    let (_, journal, _) = test_setup.fetch_journal().await;
    assert_eq!(journal.total_sol_balance, 0);

    let swap_destination_balance_before = swap_destination_balance_after;

    let swap_destination_balance_after = test_setup
        .fetch_token_account(&swap_destination_key)
        .await
        .unwrap()
        .amount;
    assert_eq!(
        swap_destination_balance_before - swap_destination_balance_after,
        expected_swept_2z_amount_2
    );
    assert_eq!(
        swap_destination_balance_after,
        journal.swap_2z_destination_balance
    );
    assert_eq!(journal.swap_2z_destination_balance, 0);
    assert_eq!(
        journal.lifetime_swapped_2z_amount(),
        u128::from(expected_swept_2z_amount_1 + expected_swept_2z_amount_2)
    );

    // No data in the distribution changes except for the bit reflecting the
    // uncollectible debt.
    let (_, distribution, remaining_distribution_data, _, _) =
        test_setup.fetch_distribution(next_dz_epoch).await;

    expected_distribution.solana_validator_write_off_count = 1;
    assert_eq!(distribution, expected_distribution);

    // First byte reflects debt tracking.
    let processed_debt_bitmap =
        &remaining_distribution_data[distribution.processed_solana_validator_debt_bitmap_range()];
    assert_eq!(processed_debt_bitmap, [0b11111111]);

    // Second byte reflects write off tracking.
    let write_off_bitmap = &remaining_distribution_data
        [distribution.processed_solana_validator_debt_write_off_bitmap_range()];
    assert_eq!(write_off_bitmap, [0b00000100]);

    // Third byte reflects rewards tracking.
    let rewards_bitmap =
        &remaining_distribution_data[distribution.processed_rewards_bitmap_range()];
    assert_eq!(rewards_bitmap, [0]);

    let (
        distribution_key,
        distribution,
        remaining_distribution_data,
        _,
        _distribution_2z_token_pda,
    ) = test_setup.fetch_distribution(and_another_dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_is_debt_calculation_finalized(true);
    expected_distribution.set_is_rewards_calculation_finalized(true);
    expected_distribution.set_has_swept_2z_tokens(true);
    expected_distribution.bump_seed = Distribution::find_address(and_another_dz_epoch).1;
    expected_distribution.token_2z_pda_bump_seed =
        state::find_2z_token_pda_address(&distribution_key).1;
    expected_distribution.dz_epoch = and_another_dz_epoch;
    expected_distribution.community_burn_rate = BurnRate::new(initial_cbr).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(solana_validator_base_block_rewards_pct_fee).unwrap();
    expected_distribution.total_solana_validators = total_solana_validators;
    expected_distribution.solana_validator_payments_count = total_solana_validators;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.collected_solana_validator_payments = total_solana_validator_debt;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.total_contributors = total_contributors;
    expected_distribution.rewards_merkle_root = rewards_merkle_root;
    expected_distribution.collected_2z_converted_from_sol = expected_swept_2z_amount_2;
    expected_distribution.uncollectible_sol_debt = uncollectible_debt.amount;
    expected_distribution.processed_solana_validator_debt_end_index = total_solana_validators / 8;
    expected_distribution.processed_rewards_start_index = total_solana_validators / 8;
    expected_distribution.processed_rewards_end_index =
        (total_solana_validators / 8) + (total_contributors / 8 + 1);
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;
    assert_eq!(distribution, expected_distribution);

    // First byte reflects debt tracking.
    let processed_debt_bitmap =
        &remaining_distribution_data[distribution.processed_solana_validator_debt_bitmap_range()];
    assert_eq!(processed_debt_bitmap, [0b11111111]);

    // Second byte reflects rewards tracking.
    let rewards_bitmap =
        &remaining_distribution_data[distribution.processed_rewards_bitmap_range()];
    assert_eq!(rewards_bitmap, [0]);
}
