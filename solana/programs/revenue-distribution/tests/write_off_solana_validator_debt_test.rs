mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::WriteOffSolanaValidatorDebtAccounts, ProgramConfiguration,
        ProgramFeatureConfiguration, RevenueDistributionInstructionData,
    },
    state::{self, Distribution, SolanaValidatorDeposit},
    types::{BurnRate, DoubleZeroEpoch, SolanaValidatorDebt, ValidatorFee},
    ID,
};
use solana_program_test::{tokio, BanksClientError};
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use svm_hash::merkle::{merkle_root_from_indexed_pod_leaves, MerkleProof};

//
// Setup.
//

struct WriteOffSolanaValidatorDebtSetup {
    test_setup: common::ProgramTestWithOwner,
    debt_accountant_signer: Keypair,
    dz_epoch: DoubleZeroEpoch,
    next_dz_epoch: DoubleZeroEpoch,
    debt_data: Vec<SolanaValidatorDebt>,
    total_solana_validators: u32,
    total_solana_validator_debt: u64,
    solana_validator_debt_merkle_root: svm_hash::sha2::Hash,
}

/// Set up a configured program with:
/// - Three distributions (epoch 0, 1, 2) — debt configured on epochs 1 and 2
/// - Write-off feature activated and enabled on epoch 1
/// - Validator deposit accounts initialized (but no debt paid yet)
///
/// Stops BEFORE any debt payment or write-off so the test can exercise
/// the full write-off lifecycle with sequential error checks.
async fn setup_for_write_off_solana_validator_debt() -> WriteOffSolanaValidatorDebtSetup {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    let dz_epoch = DoubleZeroEpoch::new(1);
    let next_dz_epoch = dz_epoch.saturating_add_duration(1);

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

    test_setup
        .configure_program(
            &configured.admin_signer,
            [ProgramConfiguration::FeatureActivation {
                feature: ProgramFeatureConfiguration::SolanaValidatorDebtWriteOff,
                activation_epoch: DoubleZeroEpoch::new(1),
            }],
        )
        .await
        .unwrap()
        // Distribution 0.
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        // Distribution 1 (dz_epoch).
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        // Distribution 2 (next_dz_epoch).
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .configure_distribution_debt(
            dz_epoch,
            &configured.debt_accountant_signer,
            total_solana_validators,
            total_solana_validator_debt,
            solana_validator_debt_merkle_root,
        )
        .await
        .unwrap()
        .configure_distribution_debt(
            next_dz_epoch,
            &configured.debt_accountant_signer,
            total_solana_validators,
            total_solana_validator_debt,
            solana_validator_debt_merkle_root,
        )
        .await
        .unwrap();

    // Initialize all validator deposit accounts.
    for debt in debt_data.iter() {
        test_setup
            .initialize_solana_validator_deposit(&debt.node_id)
            .await
            .unwrap();
    }

    WriteOffSolanaValidatorDebtSetup {
        test_setup,
        debt_accountant_signer: configured.debt_accountant_signer,
        dz_epoch,
        next_dz_epoch,
        debt_data,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
    }
}

//
// Write off Solana validator debt — happy path with sequential error checks.
//

#[tokio::test]
async fn test_write_off_solana_validator_debt() {
    let WriteOffSolanaValidatorDebtSetup {
        mut test_setup,
        debt_accountant_signer,
        dz_epoch,
        next_dz_epoch,
        debt_data,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
    } = setup_for_write_off_solana_validator_debt().await;

    let initial_cbr = 100_000_000;
    let solana_validator_base_block_rewards_pct_fee = 500;
    let distribute_rewards_relay_lamports = 10_000;

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

    // Pick an arbitrary validator to test error scenarios.
    let arbitrary_bad_debt_index = 2;
    let arbitrary_bad_debt = debt_data[arbitrary_bad_debt_index];
    let proof = MerkleProof::from_indexed_pod_leaves(
        &debt_data,
        arbitrary_bad_debt_index.try_into().unwrap(),
        Some(SolanaValidatorDebt::LEAF_PREFIX),
    )
    .unwrap();

    // Cannot write off debt before enabling write-off.
    let (tx_err, program_logs) = simulate_write_off_revert(
        &mut test_setup,
        &debt_accountant_signer,
        dz_epoch,
        &arbitrary_bad_debt,
        next_dz_epoch,
        proof.clone(),
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(4).unwrap(),
        "Program log: Solana validator debt write off is not enabled yet"
    );

    test_setup
        .finalize_distribution_debt(dz_epoch, &debt_accountant_signer)
        .await
        .unwrap()
        .enable_solana_validator_debt_write_off(dz_epoch)
        .await
        .unwrap();

    // Cannot write off debt for distribution with unfinalized debt.
    let (tx_err, program_logs) = simulate_write_off_revert(
        &mut test_setup,
        &debt_accountant_signer,
        dz_epoch,
        &arbitrary_bad_debt,
        next_dz_epoch,
        proof.clone(),
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(5).unwrap(),
        "Program log: Distribution debt calculation is not finalized yet"
    );
    assert_eq!(
        program_logs.get(6).unwrap(),
        &format!("Program log: Write-off epoch {next_dz_epoch} has unfinalized debt")
    );

    test_setup
        .finalize_distribution_debt(next_dz_epoch, &debt_accountant_signer)
        .await
        .unwrap();

    // Cannot write off debt using an epoch that is below the current epoch.
    let (tx_err, program_logs) = simulate_write_off_revert(
        &mut test_setup,
        &debt_accountant_signer,
        dz_epoch,
        &arbitrary_bad_debt,
        DoubleZeroEpoch::new(0),
        proof.clone(),
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(5).unwrap(),
        "Program log: Write-off distribution's epoch must be at least the epoch of the current distribution"
    );

    // Pay debt for one upstanding citizen.
    let upstanding_citizen_index = 3;
    let paid_debt = debt_data[upstanding_citizen_index];
    let paid_proof = MerkleProof::from_indexed_pod_leaves(
        &debt_data,
        upstanding_citizen_index.try_into().unwrap(),
        Some(SolanaValidatorDebt::LEAF_PREFIX),
    )
    .unwrap();

    test_setup
        .transfer_lamports(
            &SolanaValidatorDeposit::find_address(&paid_debt.node_id).0,
            paid_debt.amount,
        )
        .await
        .unwrap();

    // Cannot write off debt for a deposit that has enough lamports.
    let (tx_err, program_logs) = simulate_write_off_revert(
        &mut test_setup,
        &debt_accountant_signer,
        dz_epoch,
        &paid_debt,
        dz_epoch,
        paid_proof.clone(),
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(4).unwrap(),
        "Program log: Lamports balance in deposit account is enough to cover debt amount"
    );

    test_setup
        .pay_solana_validator_debt(dz_epoch, &paid_debt, paid_proof)
        .await
        .unwrap();

    // Write off some debt at epoch 1 (validators 8..16).
    for (i, debt) in debt_data.iter().enumerate().skip(split_write_off_index) {
        assert_ne!(i, arbitrary_bad_debt_index);
        assert_ne!(i, upstanding_citizen_index);

        let proof = MerkleProof::from_indexed_pod_leaves(
            &debt_data,
            i.try_into().unwrap(),
            Some(SolanaValidatorDebt::LEAF_PREFIX),
        )
        .unwrap();

        test_setup
            .write_off_solana_validator_debt(
                dz_epoch,
                dz_epoch,
                &debt_accountant_signer,
                debt,
                proof,
            )
            .await
            .unwrap();
    }

    // Write off remaining debt at epoch 2 (validators 0..8 except paid).
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
            .write_off_solana_validator_debt(
                dz_epoch,
                next_dz_epoch,
                &debt_accountant_signer,
                debt,
                proof,
            )
            .await
            .unwrap();
    }

    // Verify dz_epoch distribution state.
    let (distribution_key, distribution, remaining_distribution_data, _, _) =
        test_setup.fetch_distribution(dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_is_debt_calculation_finalized(true);
    expected_distribution.set_is_solana_validator_debt_write_off_enabled(true);
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
    expected_distribution.processed_solana_validator_debt_write_off_start_index =
        total_solana_validators / 8;
    expected_distribution.processed_solana_validator_debt_write_off_end_index =
        2 * (total_solana_validators / 8);
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp = test_setup
        .get_clock()
        .await
        .unix_timestamp
        .saturating_sub(60) as u32;
    expected_distribution.solana_validator_write_off_count = total_solana_validators - 1;
    assert_eq!(distribution, expected_distribution);

    // First two bytes reflect debt tracking.
    let processed_bitmap =
        &remaining_distribution_data[distribution.processed_solana_validator_debt_bitmap_range()];
    assert_eq!(processed_bitmap, [0b11111111, 0b11111111]);

    // Third and fourth bytes reflect write off tracking.
    let write_off_bitmap = &remaining_distribution_data
        [distribution.processed_solana_validator_debt_write_off_bitmap_range()];
    assert_eq!(write_off_bitmap, [0b11110111, 0b11111111]);

    // Verify next_dz_epoch distribution state.
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

    // First two bytes reflect debt tracking.
    let processed_bitmap =
        &remaining_distribution_data[distribution.processed_solana_validator_debt_bitmap_range()];
    assert_eq!(processed_bitmap, [0; 2]);

    let write_off_bitmap = &remaining_distribution_data
        [distribution.processed_solana_validator_debt_write_off_bitmap_range()];
    assert!(write_off_bitmap.is_empty());

    let (_, journal, _) = test_setup.fetch_journal().await;
    assert_eq!(journal.total_sol_balance, paid_debt.amount);

    // Verify deposit accounts have written off debt updated.
    for (i, debt) in debt_data.iter().enumerate() {
        let (_, solana_validator_deposit) = test_setup
            .fetch_solana_validator_deposit(&debt.node_id)
            .await;

        if i == upstanding_citizen_index {
            assert_eq!(solana_validator_deposit.written_off_sol_debt, 0);
        } else {
            assert_eq!(solana_validator_deposit.written_off_sol_debt, debt.amount);
        }
    }

    // Cannot write off debt again for any validator (including the paid one).
    for (i, debt) in debt_data.iter().enumerate() {
        let leaf_index = u32::try_from(i).unwrap();

        let proof = MerkleProof::from_indexed_pod_leaves(
            &debt_data,
            leaf_index,
            Some(SolanaValidatorDebt::LEAF_PREFIX),
        )
        .unwrap();

        let (tx_err, program_logs) = simulate_write_off_revert(
            &mut test_setup,
            &debt_accountant_signer,
            dz_epoch,
            debt,
            next_dz_epoch,
            proof,
        )
        .await
        .unwrap();

        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
        );
        assert_eq!(
            program_logs.get(4).unwrap(),
            &format!("Program log: Merkle leaf index {leaf_index} has already been processed")
        );
        if i == upstanding_citizen_index {
            assert_eq!(
                program_logs.get(5).unwrap(),
                &format!(
                    "Program log: Solana validator debt already processed for epoch {dz_epoch}"
                )
            )
        } else {
            assert_eq!(
                program_logs.get(5).unwrap(),
                &format!(
                    "Program log: Solana validator debt already written off for epoch {dz_epoch}"
                )
            )
        }
    }
}

//
// Helpers.
//

async fn simulate_write_off_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    debt_accountant_signer: &Keypair,
    dz_epoch: DoubleZeroEpoch,
    debt: &SolanaValidatorDebt,
    write_off_dz_epoch: DoubleZeroEpoch,
    proof: MerkleProof,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let write_off_ix = try_build_instruction(
        &ID,
        WriteOffSolanaValidatorDebtAccounts::new(
            &debt_accountant_signer.pubkey(),
            dz_epoch,
            &debt.node_id,
            write_off_dz_epoch,
        ),
        &RevenueDistributionInstructionData::WriteOffSolanaValidatorDebt {
            amount: debt.amount,
            proof,
        },
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[write_off_ix], &[debt_accountant_signer])
        .await
}
