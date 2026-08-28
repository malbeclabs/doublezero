mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::SetDistributionEconomicBurnRateAccounts, ProgramConfiguration,
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

struct SetDistributionEconomicBurnRateSetup {
    test_setup: common::ProgramTestWithOwner,
    debt_accountant_signer: Keypair,
    rewards_accountant_signer: Keypair,
    dz_epoch: DoubleZeroEpoch,
}

/// Set up a configured program with two distributions (epoch 0 and 1).
/// Epoch 1 is ready for economic burn rate configuration.
async fn setup_for_set_distribution_economic_burn_rate() -> SetDistributionEconomicBurnRateSetup {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    let minimum_epoch_duration_to_finalize_rewards = 2;

    test_setup
        .configure_program(
            &configured.admin_signer,
            [ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(
                minimum_epoch_duration_to_finalize_rewards,
            )],
        )
        .await
        .unwrap()
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
        .unwrap();

    let dz_epoch = DoubleZeroEpoch::new(1);

    SetDistributionEconomicBurnRateSetup {
        test_setup,
        debt_accountant_signer: configured.debt_accountant_signer,
        rewards_accountant_signer: configured.rewards_accountant_signer,
        dz_epoch,
    }
}

//
// Set distribution economic burn rate — happy path.
//

#[tokio::test]
async fn test_set_distribution_economic_burn_rate() {
    let SetDistributionEconomicBurnRateSetup {
        mut test_setup,
        rewards_accountant_signer,
        dz_epoch,
        ..
    } = setup_for_set_distribution_economic_burn_rate().await;

    let initial_cbr = 100_000_000;
    let distribute_rewards_relay_lamports = 10_000;

    // Set economic burn rate.

    let economic_burn_rate_value = 200_000_000; // 20%.

    test_setup
        .set_distribution_economic_burn_rate(
            dz_epoch,
            &rewards_accountant_signer,
            economic_burn_rate_value,
        )
        .await
        .unwrap();

    let (distribution_key, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.bump_seed = Distribution::find_address(dz_epoch).1;
    expected_distribution.token_2z_pda_bump_seed =
        state::find_2z_token_pda_address(&distribution_key).1;
    expected_distribution.dz_epoch = dz_epoch;
    expected_distribution.community_burn_rate = BurnRate::new(initial_cbr).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards_pct = ValidatorFee::new(500).unwrap();
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;
    expected_distribution.economic_burn_rate = BurnRate::new(economic_burn_rate_value).unwrap();
    assert_eq!(distribution, expected_distribution);

    // Update economic burn rate to a different value.

    let updated_burn_rate_value = 350_000_000; // 35%.

    test_setup
        .set_distribution_economic_burn_rate(
            dz_epoch,
            &rewards_accountant_signer,
            updated_burn_rate_value,
        )
        .await
        .unwrap();

    let (_, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;

    expected_distribution.economic_burn_rate = BurnRate::new(updated_burn_rate_value).unwrap();
    assert_eq!(distribution, expected_distribution);
}

//
// Set distribution economic burn rate — cannot set after rewards finalized.
//

#[tokio::test]
async fn test_cannot_set_distribution_economic_burn_rate_after_rewards_finalized() {
    let SetDistributionEconomicBurnRateSetup {
        mut test_setup,
        debt_accountant_signer,
        rewards_accountant_signer,
        dz_epoch,
    } = setup_for_set_distribution_economic_burn_rate().await;

    let total_contributors = 69;
    let rewards_merkle_root = Hash::new_unique();

    test_setup
        .finalize_distribution_debt(dz_epoch, &debt_accountant_signer)
        .await
        .unwrap()
        .configure_distribution_rewards(
            dz_epoch,
            &rewards_accountant_signer,
            total_contributors,
            rewards_merkle_root,
        )
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .finalize_distribution_rewards(dz_epoch)
        .await
        .unwrap();

    let (tx_err, program_logs) =
        simulate_program_revert(&mut test_setup, &rewards_accountant_signer, dz_epoch)
            .await
            .unwrap();

    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Distribution rewards have already been finalized"
    );
}

//
// Helpers.
//

async fn simulate_program_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    rewards_accountant_signer: &Keypair,
    dz_epoch: DoubleZeroEpoch,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let set_distribution_economic_burn_rate_ix = try_build_instruction(
        &ID,
        SetDistributionEconomicBurnRateAccounts::new(&rewards_accountant_signer.pubkey(), dz_epoch),
        &RevenueDistributionInstructionData::SetDistributionEconomicBurnRate(Default::default()),
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(
            &[set_distribution_economic_burn_rate_ix],
            &[rewards_accountant_signer],
        )
        .await
}
