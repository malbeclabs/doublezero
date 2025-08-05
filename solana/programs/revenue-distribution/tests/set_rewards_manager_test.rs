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
// Set rewards manager.
//

#[tokio::test]
async fn test_set_rewards_manager() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    let contributor_manager_signer = Keypair::new();

    let service_key = Pubkey::new_unique();

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
        .initialize_contributor_rewards(&service_key)
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

    // Test input.

    let rewards_manager_key = Pubkey::new_unique();

    test_setup
        .set_rewards_manager(
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
