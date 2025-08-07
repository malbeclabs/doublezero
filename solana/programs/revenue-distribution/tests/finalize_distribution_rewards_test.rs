mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::{ConfigureDistributionRewardsAccounts, FinalizeDistributionRewardsAccounts},
        DistributionPaymentsConfiguration, ProgramConfiguration, ProgramFlagConfiguration,
        RevenueDistributionInstructionData,
    },
    state::{self, Distribution},
    types::{BurnRate, DoubleZeroEpoch, ValidatorFee},
    ID,
};
use solana_hash::Hash;
use solana_program_test::tokio;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

//
// Finalize distribution rewards.
//

#[tokio::test]
async fn test_finalize_distribution_rewards() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    let payments_accountant_signer = Keypair::new();
    let rewards_accountant_signer = Keypair::new();
    let solana_validator_base_block_rewards_fee = 500; // 5%.

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Relay settings.
    let contributor_reward_claim_relay_lamports = 10_000;

    // Distribution rewards.

    let dz_epoch = DoubleZeroEpoch::new(1);

    let total_contributors = 69;
    let rewards_merkle_root = Hash::new_unique();

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
                ProgramConfiguration::PaymentsAccountant(payments_accountant_signer.pubkey()),
                ProgramConfiguration::RewardsAccountant(rewards_accountant_signer.pubkey()),
                ProgramConfiguration::SolanaValidatorFeeParameters {
                    base_block_rewards: solana_validator_base_block_rewards_fee,
                    priority_block_rewards: 0,
                    inflation_rewards: 0,
                    jito_tips: 0,
                    _unused: [0; 32],
                },
                ProgramConfiguration::CommunityBurnRateParameters {
                    limit: cbr_limit,
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
                },
                ProgramConfiguration::ContributorRewardClaimLamports(
                    contributor_reward_claim_relay_lamports,
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
        .configure_distribution_rewards(
            dz_epoch,
            &rewards_accountant_signer,
            total_contributors,
            rewards_merkle_root,
        )
        .await
        .unwrap();

    // Cannot finalize until payments have not been finalized.

    let (tx_err, program_logs) = cannot_finalize_distribution_rewards(&test_setup, dz_epoch).await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Payments must be finalized before rewards can be finalized"
    );

    test_setup
        .configure_distribution_payments(
            dz_epoch,
            &payments_accountant_signer,
            [DistributionPaymentsConfiguration::FinalizePayments],
        )
        .await
        .unwrap();

    // Cannot finalize until the minimum number of epochs has been configured.

    let (tx_err, program_logs) = cannot_finalize_distribution_rewards(&test_setup, dz_epoch).await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Minimum epoch duration to finalize rewards is misconfigured"
    );

    let minimum_epoch_duration_to_finalize_rewards = 2;

    test_setup
        .configure_program(
            &admin_signer,
            [ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(
                minimum_epoch_duration_to_finalize_rewards,
            )],
        )
        .await
        .unwrap();

    let (_, program_config, _) = test_setup.fetch_program_config().await;

    let minimum_dz_epoch_to_finalize =
        dz_epoch.saturating_add_duration(minimum_epoch_duration_to_finalize_rewards.into());

    // Cannot finalize until the minimum number of epochs have passed.

    let (tx_err, program_logs) = cannot_finalize_distribution_rewards(&test_setup, dz_epoch).await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        &format!(
            "Program log: DZ epoch must be at least {} (currently {}) to finalize rewards",
            minimum_dz_epoch_to_finalize, program_config.next_dz_epoch
        )
    );

    // Initialize another distribution to move next DZ epoch to allow rewards to
    // be finalized.

    test_setup
        .initialize_distribution(&payments_accountant_signer)
        .await
        .unwrap();

    let (_, program_config, _) = test_setup.fetch_program_config().await;
    assert_eq!(program_config.next_dz_epoch, minimum_dz_epoch_to_finalize);

    //

    let (_, _, distribution_lamports_balance_before, _) =
        test_setup.fetch_distribution(dz_epoch).await;

    test_setup
        .finalize_distribution_rewards(dz_epoch)
        .await
        .unwrap();

    let (distribution_key, distribution, distribution_lamports_balance_after, _) =
        test_setup.fetch_distribution(dz_epoch).await;

    assert_eq!(
        distribution_lamports_balance_after,
        distribution_lamports_balance_before + 690_000
    );

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_are_payments_finalized(true);
    expected_distribution.set_are_rewards_finalized(true);
    expected_distribution.bump_seed = Distribution::find_address(dz_epoch).1;
    expected_distribution.token_2z_pda_bump_seed =
        state::find_2z_token_pda_address(&distribution_key).1;
    expected_distribution.dz_epoch = dz_epoch;
    expected_distribution.community_burn_rate = BurnRate::new(initial_cbr).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards = ValidatorFee::new(solana_validator_base_block_rewards_fee).unwrap();
    expected_distribution.total_contributors = total_contributors;
    expected_distribution.rewards_merkle_root = rewards_merkle_root;
    assert_eq!(distribution, expected_distribution);

    // Cannot configure distribution rewards after finalization.

    let configure_distribution_rewards_ix = try_build_instruction(
        &ID,
        ConfigureDistributionRewardsAccounts::new(&rewards_accountant_signer.pubkey(), dz_epoch),
        &RevenueDistributionInstructionData::ConfigureDistributionRewards {
            total_contributors,
            merkle_root: rewards_merkle_root,
        },
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(
            &[configure_distribution_rewards_ix],
            &[&rewards_accountant_signer],
        )
        .await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Distribution rewards have already been finalized"
    );

    // Cannot finalize again.

    let (tx_err, program_logs) = cannot_finalize_distribution_rewards(&test_setup, dz_epoch).await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Distribution rewards have already been finalized"
    );
}

async fn cannot_finalize_distribution_rewards(
    test_setup: &common::ProgramTestWithOwner,
    dz_epoch: DoubleZeroEpoch,
) -> (TransactionError, Vec<String>) {
    let payer_key = test_setup.payer_signer.pubkey();

    let finalize_distribution_rewards_ix = try_build_instruction(
        &ID,
        FinalizeDistributionRewardsAccounts::new(&payer_key, dz_epoch),
        &RevenueDistributionInstructionData::FinalizeDistributionRewards,
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[finalize_distribution_rewards_ix], &[])
        .await
}
