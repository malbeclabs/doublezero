mod common;

//

use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};
use doublezero_revenue_distribution::{
    instruction::{
        account::FinalizeMonthlyContributorDistributionRewardsAccounts, ProgramConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    state::MonthlyContributorDistribution,
    ID,
};
use solana_program_test::{tokio, BanksClientError};
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

async fn setup_for_finalize_monthly_rewards() -> FinalizeMonthlyRewardsSetup {
    let mut test_setup = common::start_test().await;
    let configured = test_setup.setup_configured_program().await.unwrap();

    test_setup
        .initialize_monthly_contributor_distribution(YEAR, MONTH)
        .await
        .unwrap();
    test_setup
        .set_unix_timestamp(MONTH_END_TIMESTAMP)
        .await
        .unwrap();

    FinalizeMonthlyRewardsSetup {
        test_setup,
        admin_signer: configured.admin_signer,
        rewards_accountant_signer: configured.rewards_accountant_signer,
    }
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

/// Collect does not exist yet, so write `collected_2z_amount` directly.
async fn override_collected_2z_amount(test_setup: &mut common::ProgramTestWithOwner, amount: u64) {
    let key = MonthlyContributorDistribution::find_address(YEAR, MONTH).0;
    let mut account = test_setup
        .context
        .banks_client
        .get_account(key)
        .await
        .unwrap()
        .unwrap();

    bytemuck::from_bytes_mut::<MonthlyContributorDistribution>(
        &mut account.data[zero_copy::data_range::<MonthlyContributorDistribution>()],
    )
    .collected_2z_amount = amount;

    test_setup.context.set_account(&key, &account.into());
}

//
// Finalize monthly contributor distribution rewards — happy path.
//

#[tokio::test]
async fn test_finalize_monthly_contributor_distribution_rewards() {
    // Bitmap length is ceil(total_contributors / 8).
    for (total_contributors, expected_bitmap_len) in [(0, 0), (8, 1), (9, 2)] {
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

#[tokio::test]
async fn test_finalize_monthly_contributor_distribution_rewards_null_root_without_2z() {
    let FinalizeMonthlyRewardsSetup {
        mut test_setup,
        rewards_accountant_signer,
        ..
    } = setup_for_finalize_monthly_rewards().await;

    test_setup
        .finalize_monthly_contributor_distribution_rewards(YEAR, MONTH, &rewards_accountant_signer)
        .await
        .unwrap();

    let (_, monthly_distribution, _, _, _) = test_setup
        .fetch_monthly_contributor_distribution(YEAR, MONTH)
        .await;
    assert!(monthly_distribution.is_rewards_calculation_finalized());
}

//
// Finalize monthly contributor distribution rewards — failures.
//

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
        override_collected_2z_amount(&mut test_setup, 1).await;

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
        .unwrap()
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
