mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::CollectMonthlyContributorRewardsAccounts, ProgramConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    integration::find_integration_bucket_address,
    ID,
};
use mock_rewards_integration::state::MockMonthlyRewardsBucket;
use solana_program_test::{tokio, BanksClientError};
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError, signature::Keypair, transaction::TransactionError,
};
use svm_hash::sha2::Hash;

//
// Setup.
//

const YEAR: u16 = 2026;
const MONTH: u8 = 9;
/// 2026-10-01T00:00:00Z.
const MONTH_END_TIMESTAMP: i64 = 1_790_812_800;
const SEEDED_BUCKET_AMOUNT: u64 = 100_000_000;

struct CollectMonthlyRewardsSetup {
    test_setup: common::ProgramTestWithOwner,
    admin_signer: Keypair,
    rewards_accountant_signer: Keypair,
    bucket_key: Pubkey,
    bucket_2z_key: Pubkey,
}

async fn setup_for_collect_monthly_rewards() -> CollectMonthlyRewardsSetup {
    let mut test_setup = common::start_test().await;
    let configured = test_setup.setup_configured_program().await.unwrap();

    let bucket_key = MockMonthlyRewardsBucket::find_address(YEAR, MONTH).0;
    let bucket_2z_key =
        find_integration_bucket_address(&mock_rewards_integration::ID, &bucket_key).0;

    test_setup
        .configure_program(
            &configured.admin_signer,
            [ProgramConfiguration::MonthlyContributorRewardsProgram(
                mock_rewards_integration::ID,
            )],
        )
        .await
        .unwrap()
        .initialize_monthly_contributor_distribution(YEAR, MONTH)
        .await
        .unwrap()
        .mock_initialize_monthly_rewards_bucket(YEAR, MONTH)
        .await
        .unwrap()
        .transfer_2z(&bucket_2z_key, SEEDED_BUCKET_AMOUNT)
        .await
        .unwrap()
        .set_unix_timestamp(MONTH_END_TIMESTAMP)
        .await
        .unwrap();

    CollectMonthlyRewardsSetup {
        test_setup,
        admin_signer: configured.admin_signer,
        rewards_accountant_signer: configured.rewards_accountant_signer,
        bucket_key,
        bucket_2z_key,
    }
}

async fn simulate_collect(
    test_setup: &mut common::ProgramTestWithOwner,
    integration_program_id: &Pubkey,
    bucket_key: &Pubkey,
    bucket_2z_key: &Pubkey,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let collect_ix = try_build_instruction(
        &ID,
        CollectMonthlyContributorRewardsAccounts::new(
            YEAR,
            MONTH,
            integration_program_id,
            bucket_key,
            bucket_2z_key,
        ),
        &RevenueDistributionInstructionData::CollectMonthlyContributorRewards,
    )
    .unwrap();

    test_setup.unwrap_simulation_error(&[collect_ix], &[]).await
}

//
// Happy path.
//

#[tokio::test]
async fn test_collect_monthly_contributor_rewards() {
    let CollectMonthlyRewardsSetup {
        mut test_setup,
        bucket_key,
        bucket_2z_key,
        ..
    } = setup_for_collect_monthly_rewards().await;

    test_setup
        .collect_monthly_contributor_rewards(
            YEAR,
            MONTH,
            &mock_rewards_integration::ID,
            &bucket_key,
            &bucket_2z_key,
        )
        .await
        .unwrap();

    let (_, monthly_distribution, _, _, destination) = test_setup
        .fetch_monthly_contributor_distribution(YEAR, MONTH)
        .await;
    assert_eq!(
        monthly_distribution.collected_2z_amount,
        SEEDED_BUCKET_AMOUNT
    );
    assert!(monthly_distribution.is_collected());
    assert!(!monthly_distribution.is_rewards_calculation_finalized());
    assert_eq!(destination.amount, SEEDED_BUCKET_AMOUNT);

    let bucket_2z = test_setup
        .fetch_token_account(&bucket_2z_key)
        .await
        .unwrap();
    assert_eq!(bucket_2z.amount, 0);
}

//
// Rev-distr refusals before the CPI.
//

#[tokio::test]
async fn test_cannot_collect_monthly_contributor_rewards_twice() {
    let CollectMonthlyRewardsSetup {
        mut test_setup,
        bucket_key,
        bucket_2z_key,
        ..
    } = setup_for_collect_monthly_rewards().await;

    test_setup
        .collect_monthly_contributor_rewards(
            YEAR,
            MONTH,
            &mock_rewards_integration::ID,
            &bucket_key,
            &bucket_2z_key,
        )
        .await
        .unwrap()
        .transfer_2z(&bucket_2z_key, SEEDED_BUCKET_AMOUNT)
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_collect(
        &mut test_setup,
        &mock_rewards_integration::ID,
        &bucket_key,
        &bucket_2z_key,
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Monthly contributor distribution has already been collected"
    );

    let (_, monthly_distribution, _, _, _) = test_setup
        .fetch_monthly_contributor_distribution(YEAR, MONTH)
        .await;
    assert_eq!(
        monthly_distribution.collected_2z_amount,
        SEEDED_BUCKET_AMOUNT
    );
}

#[tokio::test]
async fn test_cannot_collect_monthly_contributor_rewards_before_month_end() {
    let CollectMonthlyRewardsSetup {
        mut test_setup,
        bucket_key,
        bucket_2z_key,
        ..
    } = setup_for_collect_monthly_rewards().await;

    test_setup
        .set_unix_timestamp(MONTH_END_TIMESTAMP - 1)
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_collect(
        &mut test_setup,
        &mock_rewards_integration::ID,
        &bucket_key,
        &bucket_2z_key,
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Month has not ended yet"
    );

    test_setup
        .set_unix_timestamp(MONTH_END_TIMESTAMP)
        .await
        .unwrap()
        .collect_monthly_contributor_rewards(
            YEAR,
            MONTH,
            &mock_rewards_integration::ID,
            &bucket_key,
            &bucket_2z_key,
        )
        .await
        .unwrap();
}

#[tokio::test]
async fn test_cannot_collect_monthly_contributor_rewards_after_finalize() {
    let CollectMonthlyRewardsSetup {
        mut test_setup,
        rewards_accountant_signer,
        bucket_key,
        bucket_2z_key,
        ..
    } = setup_for_collect_monthly_rewards().await;

    // Finalize requires the collect, so the refused 2Z arrives after it.
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
        .collect_monthly_contributor_rewards(
            YEAR,
            MONTH,
            &mock_rewards_integration::ID,
            &bucket_key,
            &bucket_2z_key,
        )
        .await
        .unwrap()
        .finalize_monthly_contributor_distribution_rewards(YEAR, MONTH, &rewards_accountant_signer)
        .await
        .unwrap()
        .transfer_2z(&bucket_2z_key, SEEDED_BUCKET_AMOUNT)
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_collect(
        &mut test_setup,
        &mock_rewards_integration::ID,
        &bucket_key,
        &bucket_2z_key,
    )
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

    let bucket_2z = test_setup
        .fetch_token_account(&bucket_2z_key)
        .await
        .unwrap();
    assert_eq!(bucket_2z.amount, SEEDED_BUCKET_AMOUNT);
}

#[tokio::test]
async fn test_cannot_collect_monthly_contributor_rewards_when_paused() {
    let CollectMonthlyRewardsSetup {
        mut test_setup,
        admin_signer,
        bucket_key,
        bucket_2z_key,
        ..
    } = setup_for_collect_monthly_rewards().await;

    test_setup
        .configure_program(
            &admin_signer,
            [ProgramConfiguration::Flag(
                ProgramFlagConfiguration::IsPaused(true),
            )],
        )
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_collect(
        &mut test_setup,
        &mock_rewards_integration::ID,
        &bucket_key,
        &bucket_2z_key,
    )
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
async fn test_cannot_collect_monthly_contributor_rewards_when_program_is_not_set() {
    let mut test_setup = common::start_test().await;
    test_setup.setup_configured_program().await.unwrap();

    let bucket_key = MockMonthlyRewardsBucket::find_address(YEAR, MONTH).0;
    let bucket_2z_key =
        find_integration_bucket_address(&mock_rewards_integration::ID, &bucket_key).0;

    test_setup
        .initialize_monthly_contributor_distribution(YEAR, MONTH)
        .await
        .unwrap()
        .mock_initialize_monthly_rewards_bucket(YEAR, MONTH)
        .await
        .unwrap()
        .transfer_2z(&bucket_2z_key, SEEDED_BUCKET_AMOUNT)
        .await
        .unwrap()
        .set_unix_timestamp(MONTH_END_TIMESTAMP)
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_collect(
        &mut test_setup,
        &mock_rewards_integration::ID,
        &bucket_key,
        &bucket_2z_key,
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Monthly contributor rewards program is not set"
    );
}

#[tokio::test]
async fn test_cannot_collect_monthly_contributor_rewards_from_another_program() {
    let CollectMonthlyRewardsSetup {
        mut test_setup,
        bucket_key,
        bucket_2z_key,
        ..
    } = setup_for_collect_monthly_rewards().await;

    let (tx_err, program_logs) = simulate_collect(
        &mut test_setup,
        &Pubkey::new_unique(),
        &bucket_key,
        &bucket_2z_key,
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::IncorrectProgramId)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Account 5 must be the monthly contributor rewards program"
    );
}

//
// Refusals after the CPI. Neither leaves the month collected.
//

#[tokio::test]
async fn test_cannot_collect_monthly_contributor_rewards_from_another_month() {
    let CollectMonthlyRewardsSetup { mut test_setup, .. } =
        setup_for_collect_monthly_rewards().await;

    // August's bucket holds 2Z, but September's distribution signs the withdraw.
    let august_bucket_key = MockMonthlyRewardsBucket::find_address(YEAR, MONTH - 1).0;
    let august_bucket_2z_key =
        find_integration_bucket_address(&mock_rewards_integration::ID, &august_bucket_key).0;
    test_setup
        .mock_initialize_monthly_rewards_bucket(YEAR, MONTH - 1)
        .await
        .unwrap()
        .transfer_2z(&august_bucket_2z_key, SEEDED_BUCKET_AMOUNT)
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_collect(
        &mut test_setup,
        &mock_rewards_integration::ID,
        &august_bucket_key,
        &august_bucket_2z_key,
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(5).unwrap(),
        "Program log: Month mismatch: integration=2026-08, monthly distribution=2026-09"
    );

    let (_, monthly_distribution, _, _, _) = test_setup
        .fetch_monthly_contributor_distribution(YEAR, MONTH)
        .await;
    assert!(!monthly_distribution.is_collected());
}

#[tokio::test]
async fn test_cannot_collect_monthly_contributor_rewards_when_nothing_moves() {
    let mut test_setup = common::start_test().await;
    let configured = test_setup.setup_configured_program().await.unwrap();

    let bucket_key = MockMonthlyRewardsBucket::find_address(YEAR, MONTH).0;
    let bucket_2z_key =
        find_integration_bucket_address(&mock_rewards_integration::ID, &bucket_key).0;

    // The bucket is never funded, so the mock withdraw moves 0 2Z.
    test_setup
        .configure_program(
            &configured.admin_signer,
            [ProgramConfiguration::MonthlyContributorRewardsProgram(
                mock_rewards_integration::ID,
            )],
        )
        .await
        .unwrap()
        .initialize_monthly_contributor_distribution(YEAR, MONTH)
        .await
        .unwrap()
        .mock_initialize_monthly_rewards_bucket(YEAR, MONTH)
        .await
        .unwrap()
        .set_unix_timestamp(MONTH_END_TIMESTAMP)
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_collect(
        &mut test_setup,
        &mock_rewards_integration::ID,
        &bucket_key,
        &bucket_2z_key,
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(12).unwrap(),
        &format!(
            "Program log: Integration {} transferred no 2Z",
            mock_rewards_integration::ID
        )
    );

    let (_, monthly_distribution, _, _, _) = test_setup
        .fetch_monthly_contributor_distribution(YEAR, MONTH)
        .await;
    assert!(!monthly_distribution.is_collected());
}
