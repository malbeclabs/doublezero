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
use solana_program_test::{tokio, BanksClientError};
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

//
// Setup.
//

struct SetRewardsManagerSetup {
    test_setup: common::ProgramTestWithOwner,
    contributor_manager_signer: Keypair,
    service_key: Pubkey,
}

async fn setup_for_set_rewards_manager() -> SetRewardsManagerSetup {
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

    SetRewardsManagerSetup {
        test_setup,
        contributor_manager_signer,
        service_key,
    }
}

//
// Set rewards manager — happy path.
//

#[tokio::test]
async fn test_set_rewards_manager() {
    let SetRewardsManagerSetup {
        mut test_setup,
        contributor_manager_signer,
        service_key,
    } = setup_for_set_rewards_manager().await;

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
}

//
// Set rewards manager — cannot set when blocked.
//

#[tokio::test]
async fn test_cannot_set_rewards_manager_when_blocked() {
    let SetRewardsManagerSetup {
        mut test_setup,
        contributor_manager_signer,
        service_key,
    } = setup_for_set_rewards_manager().await;

    let rewards_manager_signer = Keypair::new();

    test_setup
        .set_rewards_manager(
            &service_key,
            &contributor_manager_signer,
            &rewards_manager_signer.pubkey(),
        )
        .await
        .unwrap()
        .configure_contributor_rewards(
            &service_key,
            &rewards_manager_signer,
            [ContributorRewardsConfiguration::IsSetRewardsManagerBlocked(
                true,
            )],
        )
        .await
        .unwrap();

    let (tx_err, program_logs) =
        simulate_program_revert(&mut test_setup, &contributor_manager_signer, &service_key)
            .await
            .unwrap();

    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(program_logs.get(3).unwrap(), "Program log: Blocked");
}

//
// Set rewards manager — can set after unblocking.
//

#[tokio::test]
async fn test_set_rewards_manager_after_unblocking() {
    let SetRewardsManagerSetup {
        mut test_setup,
        contributor_manager_signer,
        service_key,
    } = setup_for_set_rewards_manager().await;

    let rewards_manager_signer = Keypair::new();
    let rewards_manager_key = rewards_manager_signer.pubkey();

    test_setup
        .set_rewards_manager(
            &service_key,
            &contributor_manager_signer,
            &rewards_manager_key,
        )
        .await
        .unwrap()
        .configure_contributor_rewards(
            &service_key,
            &rewards_manager_signer,
            [ContributorRewardsConfiguration::IsSetRewardsManagerBlocked(
                true,
            )],
        )
        .await
        .unwrap()
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

//
// Helpers.
//

async fn simulate_program_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    contributor_manager_signer: &Keypair,
    service_key: &Pubkey,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let set_rewards_manager_ix = try_build_instruction(
        &ID,
        SetRewardsManagerAccounts::new(&contributor_manager_signer.pubkey(), service_key),
        &RevenueDistributionInstructionData::SetRewardsManager(Pubkey::new_unique()),
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[set_rewards_manager_ix], &[contributor_manager_signer])
        .await
}
