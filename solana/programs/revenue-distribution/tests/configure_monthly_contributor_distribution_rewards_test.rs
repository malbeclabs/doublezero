mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::ConfigureMonthlyContributorDistributionRewardsAccounts, ProgramConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    ID,
};
use solana_program_test::{tokio, BanksClientError};
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use svm_hash::sha2::Hash;

//
// Setup.
//

const YEAR: u16 = 2026;
const MONTH: u8 = 9;
/// 2026-10-01T00:00:00Z, the earliest time this month can be finalized.
const MONTH_END_TIMESTAMP: i64 = 1_790_812_800;

struct ConfigureMonthlyRewardsSetup {
    test_setup: common::ProgramTestWithOwner,
    admin_signer: Keypair,
    rewards_accountant_signer: Keypair,
}

async fn setup_for_configure_monthly_rewards() -> ConfigureMonthlyRewardsSetup {
    let mut test_setup = common::start_test().await;
    let configured = test_setup.setup_configured_program().await.unwrap();

    test_setup
        .initialize_monthly_contributor_distribution(YEAR, MONTH)
        .await
        .unwrap();

    ConfigureMonthlyRewardsSetup {
        test_setup,
        admin_signer: configured.admin_signer,
        rewards_accountant_signer: configured.rewards_accountant_signer,
    }
}

async fn simulate_configure(
    test_setup: &mut common::ProgramTestWithOwner,
    signer: &Keypair,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let configure_ix = try_build_instruction(
        &ID,
        ConfigureMonthlyContributorDistributionRewardsAccounts::new(&signer.pubkey(), YEAR, MONTH),
        &RevenueDistributionInstructionData::ConfigureMonthlyContributorDistributionRewards {
            total_contributors: 1,
            merkle_root: Hash::new_unique(),
        },
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[configure_ix], &[signer])
        .await
}

//
// Configure monthly contributor distribution rewards — happy path.
//

#[tokio::test]
async fn test_configure_monthly_contributor_distribution_rewards() {
    let ConfigureMonthlyRewardsSetup {
        mut test_setup,
        rewards_accountant_signer,
        ..
    } = setup_for_configure_monthly_rewards().await;

    let (_, before, _, _, _) = test_setup
        .fetch_monthly_contributor_distribution(YEAR, MONTH)
        .await;

    let merkle_root = Hash::new_unique();
    test_setup
        .configure_monthly_contributor_distribution_rewards(
            YEAR,
            MONTH,
            &rewards_accountant_signer,
            69,
            merkle_root,
        )
        .await
        .unwrap();

    let (_, monthly_distribution, _, _, _) = test_setup
        .fetch_monthly_contributor_distribution(YEAR, MONTH)
        .await;

    let mut expected = before;
    expected.total_contributors = 69;
    expected.rewards_merkle_root = merkle_root;
    assert_eq!(monthly_distribution, expected);

    // Posting again before finalize overwrites the root.
    let new_merkle_root = Hash::new_unique();
    test_setup
        .configure_monthly_contributor_distribution_rewards(
            YEAR,
            MONTH,
            &rewards_accountant_signer,
            420,
            new_merkle_root,
        )
        .await
        .unwrap();

    let (_, monthly_distribution, _, _, _) = test_setup
        .fetch_monthly_contributor_distribution(YEAR, MONTH)
        .await;

    expected.total_contributors = 420;
    expected.rewards_merkle_root = new_merkle_root;
    assert_eq!(monthly_distribution, expected);
}

//
// Configure monthly contributor distribution rewards — failures.
//

#[tokio::test]
async fn test_cannot_configure_monthly_contributor_distribution_rewards_unauthorized() {
    let ConfigureMonthlyRewardsSetup { mut test_setup, .. } =
        setup_for_configure_monthly_rewards().await;

    let (tx_err, program_logs) = simulate_configure(&mut test_setup, &Keypair::new())
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Unauthorized rewards accountant (account 1)"
    );
}

#[tokio::test]
async fn test_cannot_configure_monthly_contributor_distribution_rewards_when_paused() {
    let ConfigureMonthlyRewardsSetup {
        mut test_setup,
        admin_signer,
        rewards_accountant_signer,
    } = setup_for_configure_monthly_rewards().await;

    test_setup
        .configure_program(
            &admin_signer,
            [ProgramConfiguration::Flag(
                ProgramFlagConfiguration::IsPaused(true),
            )],
        )
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_configure(&mut test_setup, &rewards_accountant_signer)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Program is paused"
    );
}

#[tokio::test]
async fn test_cannot_configure_monthly_contributor_distribution_rewards_after_finalize() {
    let ConfigureMonthlyRewardsSetup {
        mut test_setup,
        rewards_accountant_signer,
        ..
    } = setup_for_configure_monthly_rewards().await;

    test_setup
        .configure_monthly_contributor_distribution_rewards(
            YEAR,
            MONTH,
            &rewards_accountant_signer,
            1,
            Hash::new_unique(),
        )
        .await
        .unwrap()
        .set_unix_timestamp(MONTH_END_TIMESTAMP)
        .await
        .unwrap()
        .finalize_monthly_contributor_distribution_rewards(YEAR, MONTH, &rewards_accountant_signer)
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_configure(&mut test_setup, &rewards_accountant_signer)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Monthly contributor distribution rewards have already been finalized"
    );
}
