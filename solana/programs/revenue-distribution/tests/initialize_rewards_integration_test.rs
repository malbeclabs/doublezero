mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::InitializeRewardsIntegrationAccounts, RevenueDistributionInstructionData,
    },
    state::RewardsIntegration,
    DOUBLEZERO_MINT_KEY, ID,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError, signature::Keypair, signer::Signer,
    transaction::TransactionError,
};

//
// Setup.
//

struct InitializeRewardsIntegrationSetup {
    test_setup: common::ProgramTestWithOwner,
    admin_signer: Keypair,
    integration_program_id: Pubkey,
}

async fn setup_for_initialize_rewards_integration() -> InitializeRewardsIntegrationSetup {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    InitializeRewardsIntegrationSetup {
        test_setup,
        admin_signer: configured.admin_signer,
        // Any program loaded by `common::start_test` will do as a stand-in for
        // a real integration. `mock_swap_sol_2z` is already loaded by the test
        // harness.
        integration_program_id: mock_swap_sol_2z::ID,
    }
}

fn build_initialize_rewards_integration_ix(
    accounts: InitializeRewardsIntegrationAccounts,
    integration_program_id: &Pubkey,
) -> solana_sdk::instruction::Instruction {
    try_build_instruction(
        &ID,
        accounts,
        &RevenueDistributionInstructionData::InitializeRewardsIntegration(*integration_program_id),
    )
    .unwrap()
}

//
// Initialize rewards integration — happy path.
//

#[tokio::test]
async fn test_initialize_rewards_integration() {
    let InitializeRewardsIntegrationSetup {
        mut test_setup,
        admin_signer,
        integration_program_id,
    } = setup_for_initialize_rewards_integration().await;

    test_setup
        .initialize_rewards_integration(&admin_signer, &integration_program_id)
        .await
        .unwrap();

    let (_, rewards_integration) = test_setup
        .fetch_rewards_integration(&integration_program_id)
        .await;

    let mut expected = RewardsIntegration::default();
    expected.bump_seed = RewardsIntegration::find_address(&integration_program_id).1;
    expected.program_id = integration_program_id;
    assert_eq!(rewards_integration, expected);
}

//
// Initialize rewards integration — unauthorized signer cannot register.
//

#[tokio::test]
async fn test_initialize_rewards_integration_unauthorized() {
    let InitializeRewardsIntegrationSetup {
        mut test_setup,
        integration_program_id,
        ..
    } = setup_for_initialize_rewards_integration().await;

    let impostor_signer = Keypair::new();
    let payer_key = test_setup.payer_signer().pubkey();

    let ix = build_initialize_rewards_integration_ix(
        InitializeRewardsIntegrationAccounts::new(
            &impostor_signer.pubkey(),
            &payer_key,
            &integration_program_id,
        ),
        &integration_program_id,
    );

    let (tx_err, _) = test_setup
        .unwrap_simulation_error(&[ix], &[&impostor_signer])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
}

//
// Initialize rewards integration — non-executable integration program is rejected.
//

#[tokio::test]
async fn test_initialize_rewards_integration_not_executable() {
    let InitializeRewardsIntegrationSetup {
        mut test_setup,
        admin_signer,
        ..
    } = setup_for_initialize_rewards_integration().await;

    // The 2Z mint exists in the test bank but is not executable.
    let non_executable_program_id = DOUBLEZERO_MINT_KEY;
    let payer_key = test_setup.payer_signer().pubkey();

    let ix = build_initialize_rewards_integration_ix(
        InitializeRewardsIntegrationAccounts::new(
            &admin_signer.pubkey(),
            &payer_key,
            &non_executable_program_id,
        ),
        &non_executable_program_id,
    );

    let (tx_err, _) = test_setup
        .unwrap_simulation_error(&[ix], &[&admin_signer])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
}

//
// Initialize rewards integration — wrong PDA seeds are rejected.
//

#[tokio::test]
async fn test_initialize_rewards_integration_wrong_seeds() {
    let InitializeRewardsIntegrationSetup {
        mut test_setup,
        admin_signer,
        integration_program_id,
    } = setup_for_initialize_rewards_integration().await;

    // Overwrite the computed PDA with a random key.
    let payer_key = test_setup.payer_signer().pubkey();
    let mut accounts = InitializeRewardsIntegrationAccounts::new(
        &admin_signer.pubkey(),
        &payer_key,
        &integration_program_id,
    );
    accounts.new_rewards_integration_key = Pubkey::new_unique();

    let ix = build_initialize_rewards_integration_ix(accounts, &integration_program_id);

    let (tx_err, _) = test_setup
        .unwrap_simulation_error(&[ix], &[&admin_signer])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidSeeds)
    );
}

//
// Initialize rewards integration — cannot re-register the same program.
//

#[tokio::test]
async fn test_initialize_rewards_integration_already_registered() {
    let InitializeRewardsIntegrationSetup {
        mut test_setup,
        admin_signer,
        integration_program_id,
    } = setup_for_initialize_rewards_integration().await;

    test_setup
        .initialize_rewards_integration(&admin_signer, &integration_program_id)
        .await
        .unwrap();

    let payer_key = test_setup.payer_signer().pubkey();
    let ix = build_initialize_rewards_integration_ix(
        InitializeRewardsIntegrationAccounts::new(
            &admin_signer.pubkey(),
            &payer_key,
            &integration_program_id,
        ),
        &integration_program_id,
    );

    let (tx_err, _) = test_setup
        .unwrap_simulation_error(&[ix], &[&admin_signer])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
}
