mod common;

//

use doublezero_revenue_distribution::{
    instruction::{DistributionConfiguration, ProgramConfiguration, ProgramFlagConfiguration},
    state::{self, Distribution},
    types::{BurnRate, DoubleZeroEpoch, ValidatorFee},
};
use solana_hash::Hash;
use solana_program_test::tokio;
use solana_sdk::signature::{Keypair, Signer};

//
// Configure distribution.
//

#[tokio::test]
async fn test_configure_distribution() {
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
        .initialize_distribution(&rewards_accountant_signer)
        .await
        .unwrap();

    // Test inputs.

    let dz_epoch = DoubleZeroEpoch::new(1);

    let total_solana_validator_payments_owed = 100_000_000_000; // 100 SOL.
    let solana_validator_payments_merkle_root = Hash::new_unique();

    let total_contributors = 69;
    let contributor_rewards_merkle_root = Hash::new_unique();

    let (_, _, distribution_lamports_balance_before, _) =
        test_setup.fetch_distribution(dz_epoch).await;

    test_setup
        .configure_distribution(
            dz_epoch,
            &payments_accountant_signer,
            [
                DistributionConfiguration::UpdateSolanaValidatorPayments {
                    total_lamports_owed: total_solana_validator_payments_owed + 1,
                    merkle_root: solana_validator_payments_merkle_root,
                },
                DistributionConfiguration::UpdateSolanaValidatorPayments {
                    total_lamports_owed: total_solana_validator_payments_owed,
                    merkle_root: solana_validator_payments_merkle_root,
                },
                DistributionConfiguration::FinalizeSolanaValidatorPayments,
            ],
        )
        .await
        .unwrap()
        .configure_distribution(
            dz_epoch,
            &rewards_accountant_signer,
            [
                DistributionConfiguration::UpdateContributorRewards {
                    total_contributors: total_contributors + 1,
                    merkle_root: contributor_rewards_merkle_root,
                },
                DistributionConfiguration::UpdateContributorRewards {
                    total_contributors,
                    merkle_root: contributor_rewards_merkle_root,
                },
                DistributionConfiguration::FinalizeContributorRewards,
            ],
        )
        .await
        .unwrap();

    let (distribution_key, distribution, distribution_lamports_balance_after, _) =
        test_setup.fetch_distribution(dz_epoch).await;

    assert_eq!(
        distribution_lamports_balance_after,
        distribution_lamports_balance_before + 690_000
    );

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_is_solana_validator_payments_finalized(true);
    expected_distribution.set_is_contributor_rewards_finalized(true);
    expected_distribution.bump_seed = Distribution::find_address(dz_epoch).1;
    expected_distribution.token_2z_pda_bump_seed =
        state::find_2z_token_pda_address(&distribution_key).1;
    expected_distribution.dz_epoch = dz_epoch;
    expected_distribution.community_burn_rate = BurnRate::new(initial_cbr).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards = ValidatorFee::new(solana_validator_base_block_rewards_fee).unwrap();
    expected_distribution.total_solana_validator_payments_owed =
        total_solana_validator_payments_owed;
    expected_distribution.solana_validator_payments_merkle_root =
        solana_validator_payments_merkle_root;
    expected_distribution.total_contributors = total_contributors;
    expected_distribution.contributor_rewards_merkle_root = contributor_rewards_merkle_root;
    assert_eq!(distribution, expected_distribution);
}
