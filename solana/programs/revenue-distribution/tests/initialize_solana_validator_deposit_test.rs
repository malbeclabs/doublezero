mod common;

//

use doublezero_revenue_distribution::state::SolanaValidatorDeposit;
use solana_program_test::tokio;
use solana_pubkey::Pubkey;

//
// Setup.
//

struct InitializeSolanaValidatorDepositSetup {
    test_setup: common::ProgramTestWithOwner,
}

async fn setup_for_initialize_solana_validator_deposit() -> InitializeSolanaValidatorDepositSetup {
    let test_setup = common::start_test().await;
    InitializeSolanaValidatorDepositSetup { test_setup }
}

//
// Initialize Solana validator deposit — happy path.
//

#[tokio::test]
async fn test_initialize_solana_validator_deposit() {
    let InitializeSolanaValidatorDepositSetup { mut test_setup } =
        setup_for_initialize_solana_validator_deposit().await;

    let node_id = Pubkey::new_unique();

    test_setup
        .initialize_solana_validator_deposit(&node_id)
        .await
        .unwrap();

    let (_, solana_validator_deposit) = test_setup.fetch_solana_validator_deposit(&node_id).await;

    let mut expected_solana_validator_deposit = SolanaValidatorDeposit::default();
    expected_solana_validator_deposit.node_id = node_id;
    assert_eq!(solana_validator_deposit, expected_solana_validator_deposit);
}
