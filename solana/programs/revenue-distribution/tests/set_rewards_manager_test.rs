mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::SetRewardsManagerAccounts, ContributorRewardsConfiguration, ProgramConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    state::ContributorRewards,
    ID,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

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

    let rewards_manager_signer = Keypair::new();
    let rewards_manager_key = rewards_manager_signer.pubkey();

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

    // Cannot set rewards manager if it is blocked.

    test_setup
        .configure_contributor_rewards(
            &service_key,
            &rewards_manager_signer,
            [ContributorRewardsConfiguration::IsSetRewardsManagerBlocked(
                true,
            )],
        )
        .await
        .unwrap();

    let set_rewards_manager_ix = try_build_instruction(
        &ID,
        SetRewardsManagerAccounts::new(&contributor_manager_signer.pubkey(), &service_key),
        &RevenueDistributionInstructionData::SetRewardsManager(Pubkey::new_unique()),
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(&[set_rewards_manager_ix], &[&contributor_manager_signer])
        .await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(program_logs.get(2).unwrap(), "Program log: Blocked");

    // Can set after unblocking.

    test_setup
        .configure_contributor_rewards(
            &service_key,
            &rewards_manager_signer,
            [ContributorRewardsConfiguration::IsSetRewardsManagerBlocked(
                false,
            )],
        )
        .await
        .unwrap()
        .set_rewards_manager(
            &service_key,
            &contributor_manager_signer,
            &rewards_manager_key,
        )
        .await
        .unwrap();
}
