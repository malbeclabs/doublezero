mod common;

//

use doublezero_revenue_distribution::state::ContributorRewards;
use solana_program_test::tokio;
use solana_pubkey::Pubkey;

//
// Setup.
//

struct InitializeContributorRewardsSetup {
    test_setup: common::ProgramTestWithOwner,
}

async fn setup_for_initialize_contributor_rewards() -> InitializeContributorRewardsSetup {
    let mut test_setup = common::start_test().await;

    test_setup.setup_configured_program().await.unwrap();

    InitializeContributorRewardsSetup { test_setup }
}

//
// Initialize contributor rewards — happy path.
//

#[tokio::test]
async fn test_initialize_contributor_rewards() {
    let InitializeContributorRewardsSetup { mut test_setup } =
        setup_for_initialize_contributor_rewards().await;

    let service_key = Pubkey::new_unique();

    test_setup
        .initialize_contributor_rewards(&service_key)
        .await
        .unwrap();

    let (_, contributor_rewards) = test_setup.fetch_contributor_rewards(&service_key).await;

    let mut expected_contributor_rewards = ContributorRewards::default();
    expected_contributor_rewards.service_key = service_key;
    assert_eq!(contributor_rewards, expected_contributor_rewards);
}
