mod common;

//

use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};
use doublezero_revenue_distribution::{
    instruction::{
        account::FinalizeMonthlyContributorDistributionRewardsAccounts, ProgramConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    integration::find_integration_bucket_address,
    state::MonthlyContributorDistribution,
    ID,
};
use mock_rewards_integration::state::MockMonthlyRewardsBucket;
use solana_program_test::{tokio, BanksClientError};
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    rent::Rent,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use svm_hash::sha2::Hash;

//
// Setup.
//

const YEAR: u16 = 2026;
const MONTH: u8 = 9;
/// 2026-10-01T00:00:00Z.
const MONTH_END_TIMESTAMP: i64 = 1_790_812_800;

struct FinalizeMonthlyRewardsSetup {
    test_setup: common::ProgramTestWithOwner,
    admin_signer: Keypair,
    rewards_accountant_signer: Keypair,
}

/// Funds the month's bucket and posts a one-contributor root, leaving collect
/// to the caller.
async fn setup_for_finalize_monthly_rewards() -> FinalizeMonthlyRewardsSetup {
    let mut test_setup = common::start_test().await;
    let configured = test_setup.setup_configured_program().await.unwrap();

    let (_, bucket_2z_key) = bucket_keys();

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
        .configure_monthly_contributor_distribution_rewards(
            YEAR,
            MONTH,
            &configured.rewards_accountant_signer,
            1,
            Hash::new_unique(),
        )
        .await
        .unwrap()
        .mock_initialize_monthly_rewards_bucket(YEAR, MONTH)
        .await
        .unwrap()
        .transfer_2z(&bucket_2z_key, 1)
        .await
        .unwrap()
        .set_unix_timestamp(MONTH_END_TIMESTAMP)
        .await
        .unwrap();

    FinalizeMonthlyRewardsSetup {
        test_setup,
        admin_signer: configured.admin_signer,
        rewards_accountant_signer: configured.rewards_accountant_signer,
    }
}

fn bucket_keys() -> (Pubkey, Pubkey) {
    let bucket_key = MockMonthlyRewardsBucket::find_address(YEAR, MONTH).0;
    let bucket_2z_key =
        find_integration_bucket_address(&mock_rewards_integration::ID, &bucket_key).0;

    (bucket_key, bucket_2z_key)
}

async fn collect(test_setup: &mut common::ProgramTestWithOwner) {
    let (bucket_key, bucket_2z_key) = bucket_keys();

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
}

async fn simulate_finalize(
    test_setup: &mut common::ProgramTestWithOwner,
    signer: &Keypair,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let finalize_ix = try_build_instruction(
        &ID,
        FinalizeMonthlyContributorDistributionRewardsAccounts::new(
            &signer.pubkey(),
            YEAR,
            MONTH,
            &test_setup.payer_signer().pubkey(),
        ),
        &RevenueDistributionInstructionData::FinalizeMonthlyContributorDistributionRewards,
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[finalize_ix], &[signer])
        .await
}

//
// Finalize monthly contributor distribution rewards — happy path.
//

#[tokio::test]
async fn test_finalize_monthly_contributor_distribution_rewards() {
    // Bitmap length is ceil(total_contributors / 8).
    for (total_contributors, expected_bitmap_len) in [(1, 1), (8, 1), (9, 2)] {
        let FinalizeMonthlyRewardsSetup {
            mut test_setup,
            rewards_accountant_signer,
            ..
        } = setup_for_finalize_monthly_rewards().await;

        let merkle_root = Hash::new_unique();
        test_setup
            .configure_monthly_contributor_distribution_rewards(
                YEAR,
                MONTH,
                &rewards_accountant_signer,
                total_contributors,
                merkle_root,
            )
            .await
            .unwrap();
        collect(&mut test_setup).await;

        let (_, before, _, _, _) = test_setup
            .fetch_monthly_contributor_distribution(YEAR, MONTH)
            .await;

        test_setup
            .finalize_monthly_contributor_distribution_rewards(
                YEAR,
                MONTH,
                &rewards_accountant_signer,
            )
            .await
            .unwrap();

        let (_, monthly_distribution, remaining_data, lamports, _) = test_setup
            .fetch_monthly_contributor_distribution(YEAR, MONTH)
            .await;

        let mut expected = before;
        expected.set_is_rewards_calculation_finalized(true);
        expected.processed_rewards_start_index = 0;
        expected.processed_rewards_end_index = expected_bitmap_len;
        assert_eq!(monthly_distribution, expected);
        assert_eq!(remaining_data, vec![0; expected_bitmap_len as usize]);
        assert_eq!(
            lamports,
            Rent::default().minimum_balance(
                zero_copy::data_end::<MonthlyContributorDistribution>()
                    + expected_bitmap_len as usize
            )
        );
    }
}

//
// Finalize monthly contributor distribution rewards — failures.
//

#[tokio::test]
async fn test_cannot_finalize_monthly_contributor_distribution_rewards_before_collect() {
    let FinalizeMonthlyRewardsSetup {
        mut test_setup,
        rewards_accountant_signer,
        ..
    } = setup_for_finalize_monthly_rewards().await;

    let (tx_err, program_logs) = simulate_finalize(&mut test_setup, &rewards_accountant_signer)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Monthly contributor distribution has not been collected yet"
    );

    collect(&mut test_setup).await;
    test_setup
        .finalize_monthly_contributor_distribution_rewards(YEAR, MONTH, &rewards_accountant_signer)
        .await
        .unwrap();
}

#[tokio::test]
async fn test_cannot_finalize_monthly_contributor_distribution_rewards_unpayable_with_2z() {
    // A null root or a root with zero contributors has no leaf to pay.
    for (total_contributors, merkle_root) in [(1, Hash::default()), (0, Hash::new_unique())] {
        let FinalizeMonthlyRewardsSetup {
            mut test_setup,
            rewards_accountant_signer,
            ..
        } = setup_for_finalize_monthly_rewards().await;

        test_setup
            .configure_monthly_contributor_distribution_rewards(
                YEAR,
                MONTH,
                &rewards_accountant_signer,
                total_contributors,
                merkle_root,
            )
            .await
            .unwrap();
        collect(&mut test_setup).await;

        let (tx_err, program_logs) = simulate_finalize(&mut test_setup, &rewards_accountant_signer)
            .await
            .unwrap();
        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
        );
        assert_eq!(
            program_logs.get(3).unwrap(),
            "Program log: Rewards root cannot be null or empty with collected 2Z"
        );
    }
}

#[tokio::test]
async fn test_cannot_finalize_monthly_contributor_distribution_rewards_before_month_end() {
    let FinalizeMonthlyRewardsSetup {
        mut test_setup,
        rewards_accountant_signer,
        ..
    } = setup_for_finalize_monthly_rewards().await;

    test_setup
        .set_unix_timestamp(MONTH_END_TIMESTAMP - 1)
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_finalize(&mut test_setup, &rewards_accountant_signer)
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
        .unwrap();
    collect(&mut test_setup).await;
    test_setup
        .finalize_monthly_contributor_distribution_rewards(YEAR, MONTH, &rewards_accountant_signer)
        .await
        .unwrap();
}

#[tokio::test]
async fn test_cannot_finalize_monthly_contributor_distribution_rewards_unauthorized() {
    let FinalizeMonthlyRewardsSetup { mut test_setup, .. } =
        setup_for_finalize_monthly_rewards().await;

    let (tx_err, program_logs) = simulate_finalize(&mut test_setup, &Keypair::new())
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
async fn test_cannot_finalize_monthly_contributor_distribution_rewards_when_paused() {
    let FinalizeMonthlyRewardsSetup {
        mut test_setup,
        admin_signer,
        rewards_accountant_signer,
    } = setup_for_finalize_monthly_rewards().await;

    test_setup
        .configure_program(
            &admin_signer,
            [ProgramConfiguration::Flag(
                ProgramFlagConfiguration::IsPaused(true),
            )],
        )
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_finalize(&mut test_setup, &rewards_accountant_signer)
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
async fn test_cannot_finalize_monthly_contributor_distribution_rewards_twice() {
    let FinalizeMonthlyRewardsSetup {
        mut test_setup,
        rewards_accountant_signer,
        ..
    } = setup_for_finalize_monthly_rewards().await;

    collect(&mut test_setup).await;
    test_setup
        .finalize_monthly_contributor_distribution_rewards(YEAR, MONTH, &rewards_accountant_signer)
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_finalize(&mut test_setup, &rewards_accountant_signer)
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
