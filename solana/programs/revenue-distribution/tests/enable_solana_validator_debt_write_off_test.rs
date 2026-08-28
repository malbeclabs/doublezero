mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::EnableSolanaValidatorDebtWriteOffAccounts, ProgramConfiguration,
        ProgramFeatureConfiguration, RevenueDistributionInstructionData,
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

struct EnableSolanaValidatorDebtWriteOffSetup {
    test_setup: common::ProgramTestWithOwner,
    admin_signer: Keypair,
    debt_accountant_signer: Keypair,
    dz_epoch: DoubleZeroEpoch,
    activation_epoch: DoubleZeroEpoch,
    total_solana_validators: u32,
    total_solana_validator_debt: u64,
    solana_validator_debt_merkle_root: Hash,
}

/// Set up a configured program with distribution debt configured on epoch 1,
/// ready to test enabling debt write-off. The feature activation epoch is NOT
/// yet configured — tests do that as needed.
async fn setup_for_enable_solana_validator_debt_write_off() -> EnableSolanaValidatorDebtWriteOffSetup
{
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    let dz_epoch = DoubleZeroEpoch::new(1);
    let activation_epoch = dz_epoch.saturating_add_duration(2);
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

    EnableSolanaValidatorDebtWriteOffSetup {
        test_setup,
        admin_signer: configured.admin_signer,
        debt_accountant_signer: configured.debt_accountant_signer,
        dz_epoch,
        activation_epoch,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
    }
}

//
// Enable Solana validator debt write off — happy path with sequential error checks.
//

#[tokio::test]
async fn test_enable_solana_validator_debt_write_off() {
    let EnableSolanaValidatorDebtWriteOffSetup {
        mut test_setup,
        admin_signer,
        debt_accountant_signer,
        dz_epoch,
        activation_epoch,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
    } = setup_for_enable_solana_validator_debt_write_off().await;

    let initial_cbr = 100_000_000;
    let solana_validator_base_block_rewards_pct_fee = 500;
    let distribute_rewards_relay_lamports = 10_000;

    let payer_key = test_setup.payer_signer().pubkey();

    let enable_ix = try_build_instruction(
        &ID,
        EnableSolanaValidatorDebtWriteOffAccounts::new(dz_epoch, &payer_key),
        &RevenueDistributionInstructionData::EnableSolanaValidatorDebtWriteOff,
    )
    .unwrap();

    // Cannot enable write-offs before the feature is activated.
    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(std::slice::from_ref(&enable_ix), &[])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Debt write-off feature activation epoch not configured"
    );

    test_setup
        .configure_program(
            &admin_signer,
            [ProgramConfiguration::FeatureActivation {
                feature: ProgramFeatureConfiguration::SolanaValidatorDebtWriteOff,
                activation_epoch,
            }],
        )
        .await
        .unwrap();

    // Cannot enable write-offs before the activation epoch.
    let program_config = test_setup.fetch_program_config().await.1;
    assert!(!program_config.is_debt_write_off_feature_activated());

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(std::slice::from_ref(&enable_ix), &[])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        &format!(
            "Program log: Debt write-off feature activates at epoch {}",
            activation_epoch
        )
    );

    test_setup
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap();

    let program_config = test_setup.fetch_program_config().await.1;
    assert!(program_config.is_debt_write_off_feature_activated());

    // Cannot enable write offs before debt calculation is finalized.
    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(std::slice::from_ref(&enable_ix), &[])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Distribution debt calculation is not finalized yet"
    );

    test_setup
        .finalize_distribution_debt(dz_epoch, &debt_accountant_signer)
        .await
        .unwrap()
        .enable_solana_validator_debt_write_off(dz_epoch)
        .await
        .unwrap();

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
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.processed_solana_validator_debt_end_index =
        total_solana_validators / 8 + 1;
    expected_distribution.processed_solana_validator_debt_write_off_start_index =
        total_solana_validators / 8 + 1;
    expected_distribution.processed_solana_validator_debt_write_off_end_index =
        2 * (total_solana_validators / 8 + 1);
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;
    assert_eq!(distribution, expected_distribution);

    let expected_remaining_distribution_data_len = 2;
    assert_eq!(
        expected_remaining_distribution_data_len,
        2 * (total_solana_validators as usize / 8 + 1)
    );
    assert_eq!(
        remaining_distribution_data,
        vec![0; expected_remaining_distribution_data_len]
    );

    // Cannot enable write offs again.
    let (tx_err, program_logs) = simulate_program_revert(&mut test_setup, dz_epoch)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Solana validator debt write off is already enabled"
    );
}

//
// Helpers.
//

async fn simulate_program_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    dz_epoch: DoubleZeroEpoch,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let payer_key = test_setup.payer_signer().pubkey();

    let enable_ix = try_build_instruction(
        &ID,
        EnableSolanaValidatorDebtWriteOffAccounts::new(dz_epoch, &payer_key),
        &RevenueDistributionInstructionData::EnableSolanaValidatorDebtWriteOff,
    )
    .unwrap();

    test_setup.unwrap_simulation_error(&[enable_ix], &[]).await
}
