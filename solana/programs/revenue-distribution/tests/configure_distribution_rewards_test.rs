mod common;

//

use doublezero_revenue_distribution::{
    state::{self, Distribution},
    types::{BurnRate, DoubleZeroEpoch, ValidatorFee},
};
use solana_program_test::tokio;
use solana_sdk::signature::Keypair;
use svm_hash::sha2::Hash;

//
// Setup.
//

struct ConfigureDistributionRewardsSetup {
    test_setup: common::ProgramTestWithOwner,
    rewards_accountant_signer: Keypair,
    dz_epoch: DoubleZeroEpoch,
}

/// Set up a configured program with two distributions (epoch 0 and 1).
/// Epoch 1 is ready for rewards configuration.
async fn setup_for_configure_distribution_rewards() -> ConfigureDistributionRewardsSetup {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

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
        .unwrap();

    let dz_epoch = DoubleZeroEpoch::new(1);

    ConfigureDistributionRewardsSetup {
        test_setup,
        rewards_accountant_signer: configured.rewards_accountant_signer,
        dz_epoch,
    }
}

//
// Configure distribution rewards — happy path.
//

#[tokio::test]
async fn test_configure_distribution_rewards() {
    let ConfigureDistributionRewardsSetup {
        mut test_setup,
        rewards_accountant_signer,
        dz_epoch,
        ..
    } = setup_for_configure_distribution_rewards().await;

    let total_contributors = 69;
    let rewards_merkle_root = Hash::new_unique();

    test_setup
        .configure_distribution_rewards(
            dz_epoch,
            &rewards_accountant_signer,
            total_contributors,
            rewards_merkle_root,
        )
        .await
        .unwrap();

    let initial_cbr = 100_000_000;
    let solana_validator_base_block_rewards_pct_fee = 500;
    let distribute_rewards_relay_lamports = 10_000;

    let (distribution_key, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.bump_seed = Distribution::find_address(dz_epoch).1;
    expected_distribution.token_2z_pda_bump_seed =
        state::find_2z_token_pda_address(&distribution_key).1;
    expected_distribution.dz_epoch = dz_epoch;
    expected_distribution.community_burn_rate = BurnRate::new(initial_cbr).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(solana_validator_base_block_rewards_pct_fee).unwrap();
    expected_distribution.total_contributors = total_contributors;
    expected_distribution.rewards_merkle_root = rewards_merkle_root;
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;
    assert_eq!(distribution, expected_distribution);
}
