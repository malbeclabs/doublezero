mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::{ConfigureDistributionDebtAccounts, FinalizeDistributionDebtAccounts},
        RevenueDistributionInstructionData,
    },
    state::{self, Distribution},
    types::{BurnRate, DoubleZeroEpoch, ValidatorFee},
    ID,
};
use solana_program_test::{tokio, BanksClientError};
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use svm_hash::sha2::Hash;

//
// Setup.
//

struct FinalizeDistributionDebtSetup {
    test_setup: common::ProgramTestWithOwner,
    debt_accountant_signer: Keypair,
    dz_epoch: DoubleZeroEpoch,
    total_solana_validators: u32,
    total_solana_validator_debt: u64,
    solana_validator_debt_merkle_root: Hash,
}

/// Set up a configured program with distribution debt configured on epoch 1,
/// ready for finalization.
async fn setup_for_finalize_distribution_debt() -> FinalizeDistributionDebtSetup {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    let dz_epoch = DoubleZeroEpoch::new(1);
    let total_solana_validators = 2;
    let total_solana_validator_debt = 100 * u64::pow(10, 9);
    let solana_validator_debt_merkle_root = Hash::new_unique();

    test_setup
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
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
        .unwrap();

    FinalizeDistributionDebtSetup {
        test_setup,
        debt_accountant_signer: configured.debt_accountant_signer,
        dz_epoch,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
    }
}

//
// Finalize distribution debt — happy path.
//

#[tokio::test]
async fn test_finalize_distribution_debt() {
    let FinalizeDistributionDebtSetup {
        mut test_setup,
        debt_accountant_signer,
        dz_epoch,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
    } = setup_for_finalize_distribution_debt().await;

    test_setup
        .finalize_distribution_debt(dz_epoch, &debt_accountant_signer)
        .await
        .unwrap();

    let initial_cbr = 100_000_000;
    let solana_validator_base_block_rewards_pct_fee = 500;
    let distribute_rewards_relay_lamports = 10_000;

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
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.processed_solana_validator_debt_end_index =
        total_solana_validators / 8 + 1;
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;
    assert_eq!(distribution, expected_distribution);

    let expected_remaining_distribution_data_len = 1;
    assert_eq!(
        expected_remaining_distribution_data_len,
        total_solana_validators as usize / 8 + 1
    );
    assert_eq!(
        remaining_distribution_data,
        vec![0; expected_remaining_distribution_data_len]
    );
}

//
// Finalize distribution debt — cannot configure debt after finalization.
//

#[tokio::test]
async fn test_cannot_configure_debt_after_finalization() {
    let FinalizeDistributionDebtSetup {
        mut test_setup,
        debt_accountant_signer,
        dz_epoch,
        ..
    } = setup_for_finalize_distribution_debt().await;

    test_setup
        .finalize_distribution_debt(dz_epoch, &debt_accountant_signer)
        .await
        .unwrap();

    let (tx_err, program_logs) =
        simulate_configure_debt_revert(&mut test_setup, &debt_accountant_signer, dz_epoch)
            .await
            .unwrap();

    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Distribution debt calculation has already been finalized"
    );
}

//
// Finalize distribution debt — cannot finalize twice.
//

#[tokio::test]
async fn test_cannot_finalize_distribution_debt_twice() {
    let FinalizeDistributionDebtSetup {
        mut test_setup,
        debt_accountant_signer,
        dz_epoch,
        ..
    } = setup_for_finalize_distribution_debt().await;

    test_setup
        .finalize_distribution_debt(dz_epoch, &debt_accountant_signer)
        .await
        .unwrap();

    let (tx_err, program_logs) =
        simulate_finalize_debt_revert(&mut test_setup, &debt_accountant_signer, dz_epoch)
            .await
            .unwrap();

    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Distribution debt calculation has already been finalized"
    );
}

//
// Helpers.
//

async fn simulate_configure_debt_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    debt_accountant_signer: &Keypair,
    dz_epoch: DoubleZeroEpoch,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let configure_distribution_debt_ix = try_build_instruction(
        &ID,
        ConfigureDistributionDebtAccounts::new(&debt_accountant_signer.pubkey(), dz_epoch),
        &RevenueDistributionInstructionData::ConfigureDistributionDebt {
            total_validators: 3,
            total_debt: 1,
            merkle_root: Hash::new_unique(),
        },
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[configure_distribution_debt_ix], &[debt_accountant_signer])
        .await
}

async fn simulate_finalize_debt_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    debt_accountant_signer: &Keypair,
    dz_epoch: DoubleZeroEpoch,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let payer_key = test_setup.payer_signer().pubkey();

    let finalize_distribution_debt_ix = try_build_instruction(
        &ID,
        FinalizeDistributionDebtAccounts::new(
            &debt_accountant_signer.pubkey(),
            dz_epoch,
            &payer_key,
        ),
        &RevenueDistributionInstructionData::FinalizeDistributionDebt,
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[finalize_distribution_debt_ix], &[debt_accountant_signer])
        .await
}
