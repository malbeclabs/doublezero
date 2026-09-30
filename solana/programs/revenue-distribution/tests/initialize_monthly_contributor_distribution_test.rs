mod common;

//

use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};
use doublezero_revenue_distribution::{
    instruction::{
        account::InitializeMonthlyContributorDistributionAccounts, ProgramConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    state::{self, MonthlyContributorDistribution},
    DOUBLEZERO_MINT_KEY, ID,
};
use solana_program_test::{tokio, BanksClientError};
use solana_sdk::{
    instruction::InstructionError, rent::Rent, signature::Signer, transaction::TransactionError,
};

//
// Setup.
//

const YEAR: u16 = 2026;
const MONTH: u8 = 9;

async fn simulate_initialize(
    test_setup: &mut common::ProgramTestWithOwner,
    year: u16,
    month: u8,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let initialize_ix = try_build_instruction(
        &ID,
        InitializeMonthlyContributorDistributionAccounts::new(
            &test_setup.payer_signer().pubkey(),
            year,
            month,
            &DOUBLEZERO_MINT_KEY,
        ),
        &RevenueDistributionInstructionData::InitializeMonthlyContributorDistribution {
            year,
            month,
        },
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[initialize_ix], &[])
        .await
}

//
// Initialize monthly contributor distribution — happy path.
//

#[tokio::test]
async fn test_initialize_monthly_contributor_distribution() {
    let mut test_setup = common::start_test().await;
    test_setup.setup_configured_program().await.unwrap();

    test_setup
        .initialize_monthly_contributor_distribution(YEAR, MONTH)
        .await
        .unwrap();

    let (monthly_distribution_key, monthly_distribution, remaining_data, lamports, token_pda) =
        test_setup
            .fetch_monthly_contributor_distribution(YEAR, MONTH)
            .await;

    let mut expected = MonthlyContributorDistribution::default();
    expected.year = YEAR;
    expected.month = MONTH;
    expected.bump_seed = MonthlyContributorDistribution::find_address(YEAR, MONTH).1;
    expected.token_2z_pda_bump_seed = state::find_2z_token_pda_address(&monthly_distribution_key).1;
    assert_eq!(monthly_distribution, expected);
    assert!(remaining_data.is_empty());
    assert_eq!(
        lamports,
        Rent::default().minimum_balance(zero_copy::data_end::<MonthlyContributorDistribution>())
    );

    assert_eq!(token_pda.mint, DOUBLEZERO_MINT_KEY);
    assert_eq!(token_pda.owner, monthly_distribution_key);
    assert_eq!(token_pda.amount, 0);

    // Other months are independent accounts.
    test_setup
        .initialize_monthly_contributor_distribution(YEAR, 12)
        .await
        .unwrap()
        .initialize_monthly_contributor_distribution(YEAR + 1, 1)
        .await
        .unwrap();
}

//
// Initialize monthly contributor distribution — failures.
//

#[tokio::test]
async fn test_cannot_initialize_monthly_contributor_distribution_twice() {
    let mut test_setup = common::start_test().await;
    test_setup.setup_configured_program().await.unwrap();

    test_setup
        .initialize_monthly_contributor_distribution(YEAR, MONTH)
        .await
        .unwrap();

    let (tx_err, _) = simulate_initialize(&mut test_setup, YEAR, MONTH)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::Custom(0))
    );
}

#[tokio::test]
async fn test_cannot_initialize_monthly_contributor_distribution_invalid_month() {
    let mut test_setup = common::start_test().await;
    test_setup.setup_configured_program().await.unwrap();

    for month in [0, 13] {
        let (tx_err, program_logs) = simulate_initialize(&mut test_setup, YEAR, month)
            .await
            .unwrap();
        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidInstructionData)
        );
        assert_eq!(
            program_logs.get(2).unwrap(),
            &format!("Program log: Invalid month: {month}")
        );
    }
}

#[tokio::test]
async fn test_cannot_initialize_monthly_contributor_distribution_when_paused() {
    let mut test_setup = common::start_test().await;
    let configured = test_setup.setup_configured_program().await.unwrap();

    test_setup
        .configure_program(
            &configured.admin_signer,
            [ProgramConfiguration::Flag(
                ProgramFlagConfiguration::IsPaused(true),
            )],
        )
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_initialize(&mut test_setup, YEAR, MONTH)
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
