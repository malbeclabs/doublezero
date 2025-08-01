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

    let accountant_signer = Keypair::new();
    let solana_validator_base_block_rewards_fee = 500; // 5%

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Test inputs.

    let dz_epoch = DoubleZeroEpoch::new(1);

    let total_solana_validator_payments_owed = 100_000_000_000; // 100 SOL.
    let solana_validator_payments_merkle_root = Hash::new_unique();

    let total_contributors = 69;
    let contributor_rewards_merkle_root = Hash::new_unique();

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
                ProgramConfiguration::Accountant(accountant_signer.pubkey()),
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
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .initialize_distribution(&accountant_signer)
        .await
        .unwrap()
        .initialize_distribution(&accountant_signer)
        .await
        .unwrap()
        .configure_distribution(
            dz_epoch,
            &accountant_signer,
            [
                DistributionConfiguration::SolanaValidatorPayments {
                    total_lamports_owed: total_solana_validator_payments_owed,
                    merkle_root: solana_validator_payments_merkle_root,
                },
                DistributionConfiguration::ContributorRewards {
                    total_contributors,
                    merkle_root: contributor_rewards_merkle_root,
                },
            ],
        )
        .await
        .unwrap();

    let (distribution_key, distribution, _) = test_setup.fetch_distribution(dz_epoch).await;

    let mut expected_distribution = Distribution::default();
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
