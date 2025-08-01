mod common;

//

use doublezero_revenue_distribution::{
    instruction::{
        ContributorRewardsConfiguration, ProgramConfiguration, ProgramFlagConfiguration,
    },
    state::{ContributorRewards, RecipientShares},
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::signature::{Keypair, Signer};

//
// Configure contributor rewards.
//

#[tokio::test]
async fn test_initialize_contributor_rewards() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();
    let contributor_manager_signer = Keypair::new();

    let rewards_manager_signer = Keypair::new();
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
        .configure_program(
            &admin_signer,
            [
                ProgramConfiguration::ContributorManager(contributor_manager_signer.pubkey()),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .initialize_contributor_rewards(
            &service_key,
            &contributor_manager_signer,
            &rewards_manager_signer.pubkey(),
        )
        .await
        .unwrap();

    // Test inputs.

    let recipients = [
        (Pubkey::new_unique(), 1_000),
        (Pubkey::new_unique(), 2_000),
        (Pubkey::new_unique(), 3_000),
        (Pubkey::new_unique(), 4_000),
    ];

    test_setup
        .configure_contributor_rewards(
            &service_key,
            &rewards_manager_signer,
            [ContributorRewardsConfiguration::Recipients(
                recipients.to_vec(),
            )],
        )
        .await
        .unwrap();

    let (_, contributor_rewards) = test_setup.fetch_contributor_rewards(&service_key).await;

    let mut expected_contributor_rewards = ContributorRewards::default();
    expected_contributor_rewards.service_key = service_key;
    expected_contributor_rewards.rewards_manager_key = rewards_manager_signer.pubkey();
    expected_contributor_rewards.recipient_shares = RecipientShares::new(&recipients).unwrap();
    assert_eq!(contributor_rewards, expected_contributor_rewards);
}
