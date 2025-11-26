mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::ForgiveSolanaValidatorDebtAccounts, ProgramConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    state::{self, Distribution, SolanaValidatorDeposit},
    types::{BurnRate, DoubleZeroEpoch, SolanaValidatorDebt, ValidatorFee},
    ID,
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
// Forgive Solana validator debt.
//

#[tokio::test]
async fn test_forgive_solana_validator_debt() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    let debt_accountant_signer = Keypair::new();
    let rewards_accountant_signer = Keypair::new();
    let solana_validator_base_block_rewards_pct_fee = 500; // 5%.

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Relay settings.
    let distribute_rewards_relay_lamports = 10_000;

    let dz_epoch = DoubleZeroEpoch::new(1);
    let next_dz_epoch = dz_epoch.saturating_add_duration(1);

    // Distribution debt accounting.

    let debt_data = (0..16)
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

    let split_write_off_index = 8;
    let debt_write_off_first = debt_data
        .iter()
        .skip(split_write_off_index)
        .map(|debt| debt.amount)
        .sum::<u64>();
    let debt_write_off_remaining = debt_data
        .iter()
        .take(split_write_off_index)
        .map(|debt| debt.amount)
        .sum::<u64>();

    test_setup
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
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
                },
                ProgramConfiguration::DistributeRewardsRelayLamports(
                    distribute_rewards_relay_lamports,
                ),
                ProgramConfiguration::CalculationGracePeriodMinutes(1),
                ProgramConfiguration::DistributionInitializationGracePeriodMinutes(1),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
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
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .configure_distribution_debt(
            dz_epoch,
            &debt_accountant_signer,
            total_solana_validators,
            total_solana_validator_debt,
            solana_validator_debt_merkle_root,
        )
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
        .unwrap();

    // Pay debt for one validator.
    let arbitrary_index = 2;
    let debt = debt_data[arbitrary_index];
    let proof = MerkleProof::from_indexed_pod_leaves(
        &debt_data,
        arbitrary_index.try_into().unwrap(),
        Some(SolanaValidatorDebt::LEAF_PREFIX),
    )
    .unwrap();

    // Cannot forgive debt for unfinalized distributions.
    let forgive_solana_validator_debt_ix = try_build_instruction(
        &ID,
        ForgiveSolanaValidatorDebtAccounts::new(
            &debt_accountant_signer.pubkey(),
            dz_epoch,
            next_dz_epoch,
        ),
        &RevenueDistributionInstructionData::ForgiveSolanaValidatorDebt {
            debt,
            proof: proof.clone(),
        },
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(
            &[forgive_solana_validator_debt_ix.clone()],
            &[&debt_accountant_signer],
        )
        .await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Distribution debt calculation is not finalized yet"
    );
    assert_eq!(
        program_logs.get(4).unwrap(),
        &format!("Program log: Epoch {dz_epoch} has unfinalized debt")
    );

    test_setup
        .finalize_distribution_debt(dz_epoch, &debt_accountant_signer)
        .await
        .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(
            &[forgive_solana_validator_debt_ix],
            &[&debt_accountant_signer],
        )
        .await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(4).unwrap(),
        "Program log: Distribution debt calculation is not finalized yet"
    );
    assert_eq!(
        program_logs.get(5).unwrap(),
        &format!("Program log: Write-off epoch {next_dz_epoch} has unfinalized debt")
    );

    test_setup
        .finalize_distribution_debt(next_dz_epoch, &debt_accountant_signer)
        .await
        .unwrap();

    // Cannot forgive debt using an epoch that is not at least the current
    // epoch we intend to forgive debt for.
    let forgive_solana_validator_debt_ix = try_build_instruction(
        &ID,
        ForgiveSolanaValidatorDebtAccounts::new(
            &debt_accountant_signer.pubkey(),
            dz_epoch,
            DoubleZeroEpoch::new(0),
        ),
        &RevenueDistributionInstructionData::ForgiveSolanaValidatorDebt {
            debt,
            proof: proof.clone(),
        },
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(
            &[forgive_solana_validator_debt_ix],
            &[&debt_accountant_signer],
        )
        .await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(4).unwrap(),
        "Program log: Write-off distribution's epoch must be at least the epoch of the current distribution"
    );

    // Pay debt for one validator.
    let upstanding_citizen_index = 3;
    let paid_debt = debt_data[upstanding_citizen_index];
    let proof = MerkleProof::from_indexed_pod_leaves(
        &debt_data,
        upstanding_citizen_index.try_into().unwrap(),
        Some(SolanaValidatorDebt::LEAF_PREFIX),
    )
    .unwrap();

    test_setup
        .initialize_solana_validator_deposit(&paid_debt.node_id)
        .await
        .unwrap()
        .transfer_lamports(
            &SolanaValidatorDeposit::find_address(&paid_debt.node_id).0,
            paid_debt.amount,
        )
        .await
        .unwrap()
        .pay_solana_validator_debt(dz_epoch, &paid_debt.node_id, paid_debt.amount, proof)
        .await
        .unwrap();

    // Forgive some debt at epoch 1.
    for (i, debt) in debt_data.iter().enumerate().skip(split_write_off_index) {
        assert_ne!(i, upstanding_citizen_index);

        let proof = MerkleProof::from_indexed_pod_leaves(
            &debt_data,
            i.try_into().unwrap(),
            Some(SolanaValidatorDebt::LEAF_PREFIX),
        )
        .unwrap();

        test_setup
            .forgive_solana_validator_debt(dz_epoch, dz_epoch, &debt_accountant_signer, debt, proof)
            .await
            .unwrap();
    }

    // Forgive debt for the rest at epoch 2.
    for (i, debt) in debt_data.iter().enumerate().take(split_write_off_index) {
        if i == upstanding_citizen_index {
            continue;
        }

        let proof = MerkleProof::from_indexed_pod_leaves(
            &debt_data,
            i.try_into().unwrap(),
            Some(SolanaValidatorDebt::LEAF_PREFIX),
        )
        .unwrap();

        test_setup
            .forgive_solana_validator_debt(
                dz_epoch,
                next_dz_epoch,
                &debt_accountant_signer,
                debt,
                proof,
            )
            .await
            .unwrap();
    }

    let (distribution_key, distribution, remaining_distribution_data, _, _) =
        test_setup.fetch_distribution(dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_is_debt_calculation_finalized(true);
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
    expected_distribution.solana_validator_payments_count = 1;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.uncollectible_sol_debt = debt_write_off_first;
    expected_distribution.collected_solana_validator_payments = paid_debt.amount;
    expected_distribution.processed_solana_validator_debt_end_index = total_solana_validators / 8;
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp = test_setup
        .get_clock()
        .await
        .unix_timestamp
        .saturating_sub(60) as u32;
    assert_eq!(distribution, expected_distribution);

    assert_eq!(remaining_distribution_data, vec![0b11111111, 0b11111111]);

    let (distribution_key, distribution, remaining_distribution_data, _, _) =
        test_setup.fetch_distribution(next_dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_is_debt_calculation_finalized(true);
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
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.uncollectible_sol_debt = debt_write_off_remaining - paid_debt.amount;
    expected_distribution.processed_solana_validator_debt_end_index = total_solana_validators / 8;
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;
    assert_eq!(distribution, expected_distribution);

    assert_eq!(remaining_distribution_data, vec![0, 0]);

    let (_, journal, _) = test_setup.fetch_journal().await;
    assert_eq!(journal.total_sol_balance, paid_debt.amount);

    // Cannot forgive debt again. This includes attempting to forgive debt for
    // the upstanding citizen who paid.
    //
    // NOTE: This test also demonstrates that even though the debt was written
    // off at epoch 1, it cannot be written off using another epoch's
    // distribution, too.
    for (i, debt) in debt_data.iter().enumerate() {
        let leaf_index = u32::try_from(i).unwrap();

        let proof = MerkleProof::from_indexed_pod_leaves(
            &debt_data,
            leaf_index,
            Some(SolanaValidatorDebt::LEAF_PREFIX),
        )
        .unwrap();

        let forgive_solana_validator_debt_ix = try_build_instruction(
            &ID,
            ForgiveSolanaValidatorDebtAccounts::new(
                &debt_accountant_signer.pubkey(),
                dz_epoch,
                next_dz_epoch,
            ),
            &RevenueDistributionInstructionData::ForgiveSolanaValidatorDebt { debt: *debt, proof },
        )
        .unwrap();

        let (tx_err, program_logs) = test_setup
            .unwrap_simulation_error(
                &[forgive_solana_validator_debt_ix],
                &[&debt_accountant_signer],
            )
            .await;
        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
        );

        assert_eq!(
            program_logs.get(3).unwrap(),
            &format!("Program log: Merkle leaf index {leaf_index} has already been processed")
        );
        assert_eq!(
            program_logs.get(4).unwrap(),
            &format!("Program log: Solana validator debt already processed for epoch {dz_epoch}")
        )
    }
}
