mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::SetDistributionEconomicBurnRateAccounts, ProgramConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    state::{self, Distribution},
    types::{BurnRate, DoubleZeroEpoch},
    ID,
};
use solana_program_test::tokio;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use svm_hash::sha2::Hash;

//
// Set distribution economic burn rate.
//

#[tokio::test]
async fn test_set_distribution_economic_burn_rate() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    let debt_accountant_signer = Keypair::new();
    let rewards_accountant_signer = Keypair::new();

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Relay settings.
    let distribute_rewards_relay_lamports = 10_000;

    let minimum_epoch_duration_to_finalize_rewards = 2;

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
                ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(
                    minimum_epoch_duration_to_finalize_rewards,
                ),
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
        .unwrap();

    let dz_epoch = DoubleZeroEpoch::new(1);

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

    // Finalize the distribution.
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

    // Cannot set economic burn rate after rewards have been finalized.

    let set_distribution_economic_burn_rate_ix = try_build_instruction(
        &ID,
        SetDistributionEconomicBurnRateAccounts::new(&rewards_accountant_signer.pubkey(), dz_epoch),
        &RevenueDistributionInstructionData::SetDistributionEconomicBurnRate(Default::default()),
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(
            &[set_distribution_economic_burn_rate_ix],
            &[&rewards_accountant_signer],
        )
        .await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Distribution rewards have already been finalized"
    );
}
