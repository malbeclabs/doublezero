mod common;

//

use doublezero_revenue_distribution::state::SolanaValidatorDeposit;
use solana_program_test::tokio;
use solana_pubkey::Pubkey;

//
// Initialize Solana validator deposit.
//

#[tokio::test]
async fn test_initialize_solana_validator_deposit() {
    let mut test_setup = common::start_test().await;

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
