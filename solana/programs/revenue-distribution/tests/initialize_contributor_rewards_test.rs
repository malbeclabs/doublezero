mod common;

//

use doublezero_revenue_distribution::{
    instruction::{ProgramConfiguration, ProgramFlagConfiguration},
    state::ContributorRewards,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::signature::{Keypair, Signer};

//
// Initialize contributor rewards.
//

#[tokio::test]
async fn test_initialize_contributor_rewards() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    let contributor_manager_signer = Keypair::new();

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
                ProgramConfiguration::ContributorManager(contributor_manager_signer.pubkey()),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap();

    // Test inputs.

    let rewards_manager_key = Pubkey::new_unique();
    let service_key = Pubkey::new_unique();

    test_setup
        .initialize_contributor_rewards(
            &service_key,
            &contributor_manager_signer,
            &rewards_manager_key,
        )
        .await
        .unwrap();

    let (_, contributor_rewards) = test_setup.fetch_contributor_rewards(&service_key).await;

    let mut expected_contributor_rewards = ContributorRewards::default();
    expected_contributor_rewards.service_key = service_key;
    expected_contributor_rewards.rewards_manager_key = rewards_manager_key;
    assert_eq!(contributor_rewards, expected_contributor_rewards);
}
