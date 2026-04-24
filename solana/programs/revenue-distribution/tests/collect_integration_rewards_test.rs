mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::CollectIntegrationRewardsAccounts, ProgramConfiguration, ProgramFlagConfiguration,
        RevenueDistributionInstructionData,
    },
    integration::{find_integration_bucket_address, find_integration_distribution_address},
    types::DoubleZeroEpoch,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError, signature::Keypair, transaction::TransactionError,
};

//
// Setup.
//

const SEEDED_BUCKET_AMOUNT: u64 = 100_000_000;

struct CollectIntegrationRewardsSetup {
    test_setup: common::ProgramTestWithOwner,
    admin_signer: Keypair,
    dz_epoch: DoubleZeroEpoch,
    integration_distribution_key: Pubkey,
    integration_2z_bucket_key: Pubkey,
}

async fn setup_for_collect_integration_rewards() -> CollectIntegrationRewardsSetup {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    test_setup
        .initialize_rewards_integration(&configured.admin_signer, &mock_rewards_integration::ID)
        .await
        .unwrap();

    // Snapshot the epoch this distribution will occupy.
    let (_, program_config, _) = test_setup.fetch_program_config().await;
    let dz_epoch = program_config.next_completed_dz_epoch;

    test_setup
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap();

    // Initialize the mock's per-epoch integration distribution PDA. The mock
    // also creates the 2Z bucket PDA as part of this instruction.
    let (integration_distribution_key, _) =
        find_integration_distribution_address(&mock_rewards_integration::ID, dz_epoch);
    let (integration_2z_bucket_key, _) = find_integration_bucket_address(
        &mock_rewards_integration::ID,
        &integration_distribution_key,
    );
    test_setup
        .mock_initialize_integration_distribution(dz_epoch)
        .await
        .unwrap();

    // Seed the bucket with 2Z.
    test_setup
        .transfer_2z(&integration_2z_bucket_key, SEEDED_BUCKET_AMOUNT)
        .await
        .unwrap();

    CollectIntegrationRewardsSetup {
        test_setup,
        admin_signer: configured.admin_signer,
        dz_epoch,
        integration_distribution_key,
        integration_2z_bucket_key,
    }
}

//
// Happy path.
//

#[tokio::test]
async fn test_collect_integration_rewards() {
    let CollectIntegrationRewardsSetup {
        mut test_setup,
        dz_epoch,
        integration_distribution_key,
        integration_2z_bucket_key,
        ..
    } = setup_for_collect_integration_rewards().await;

    let (_, distribution_before, _, _, destination_before) =
        test_setup.fetch_distribution(dz_epoch).await;

    test_setup
        .collect_integration_rewards(
            dz_epoch,
            &mock_rewards_integration::ID,
            &integration_distribution_key,
            &integration_2z_bucket_key,
        )
        .await
        .unwrap();

    let (_, distribution_after, _, _, destination_after) =
        test_setup.fetch_distribution(dz_epoch).await;

    assert_eq!(
        distribution_after.collected_2z_from_integrations,
        distribution_before.collected_2z_from_integrations + SEEDED_BUCKET_AMOUNT,
    );
    assert_eq!(
        distribution_after.integrations_collected_count,
        distribution_before.integrations_collected_count + 1,
    );
    assert_eq!(
        destination_after.amount,
        destination_before.amount + SEEDED_BUCKET_AMOUNT,
    );

    let bucket_after = test_setup
        .fetch_token_account(&integration_2z_bucket_key)
        .await
        .unwrap();
    assert_eq!(bucket_after.amount, 0);
}

//
// Unregistered integration is rejected before the CPI.
//

#[tokio::test]
async fn test_cannot_collect_integration_rewards_when_unregistered() {
    let CollectIntegrationRewardsSetup {
        mut test_setup,
        dz_epoch,
        integration_distribution_key,
        integration_2z_bucket_key,
        ..
    } = setup_for_collect_integration_rewards().await;

    // A program ID that was never registered as a rewards integration.
    let unregistered_program_id = Pubkey::new_unique();

    let ix = try_build_instruction(
        &doublezero_revenue_distribution::ID,
        CollectIntegrationRewardsAccounts::new(
            dz_epoch,
            &unregistered_program_id,
            &integration_distribution_key,
            &integration_2z_bucket_key,
        ),
        &RevenueDistributionInstructionData::CollectIntegrationRewards,
    )
    .unwrap();

    // The PDA derived from an unregistered program ID has no on-chain data;
    // its owner defaults to the system program, so the `ZeroCopyAccount`
    // owner check fires before the discriminator check.
    let (tx_err, _) = test_setup
        .unwrap_simulation_error(&[ix], &[])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountOwner)
    );
}

//
// Epoch mismatch — the integration's handler refuses and the error bubbles up.
//

#[tokio::test]
async fn test_cannot_collect_integration_rewards_when_epoch_mismatched() {
    let CollectIntegrationRewardsSetup {
        mut test_setup,
        dz_epoch,
        integration_2z_bucket_key,
        ..
    } = setup_for_collect_integration_rewards().await;

    // Initialize a second mock integration distribution for a different epoch
    // and point the instruction at it.
    let wrong_dz_epoch = DoubleZeroEpoch::new(dz_epoch.value() + 1);
    let (wrong_integration_distribution_key, _) =
        find_integration_distribution_address(&mock_rewards_integration::ID, wrong_dz_epoch);

    test_setup
        .mock_initialize_integration_distribution(wrong_dz_epoch)
        .await
        .unwrap();

    let ix = try_build_instruction(
        &doublezero_revenue_distribution::ID,
        CollectIntegrationRewardsAccounts::new(
            dz_epoch,
            &mock_rewards_integration::ID,
            &wrong_integration_distribution_key,
            &integration_2z_bucket_key,
        ),
        &RevenueDistributionInstructionData::CollectIntegrationRewards,
    )
    .unwrap();

    let (tx_err, _) = test_setup
        .unwrap_simulation_error(&[ix], &[])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
}

//
// Second collect for the same (epoch, integration) is rejected by rev-distr's
// bitmap check.
//

#[tokio::test]
async fn test_cannot_collect_integration_rewards_twice() {
    let CollectIntegrationRewardsSetup {
        mut test_setup,
        dz_epoch,
        integration_distribution_key,
        integration_2z_bucket_key,
        ..
    } = setup_for_collect_integration_rewards().await;

    test_setup
        .collect_integration_rewards(
            dz_epoch,
            &mock_rewards_integration::ID,
            &integration_distribution_key,
            &integration_2z_bucket_key,
        )
        .await
        .unwrap();

    let ix = try_build_instruction(
        &doublezero_revenue_distribution::ID,
        CollectIntegrationRewardsAccounts::new(
            dz_epoch,
            &mock_rewards_integration::ID,
            &integration_distribution_key,
            &integration_2z_bucket_key,
        ),
        &RevenueDistributionInstructionData::CollectIntegrationRewards,
    )
    .unwrap();

    let (tx_err, _) = test_setup
        .unwrap_simulation_error(&[ix], &[])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
}

//
// Paused program refuses to run.
//

#[tokio::test]
async fn test_cannot_collect_integration_rewards_when_paused() {
    let CollectIntegrationRewardsSetup {
        mut test_setup,
        admin_signer,
        dz_epoch,
        integration_distribution_key,
        integration_2z_bucket_key,
    } = setup_for_collect_integration_rewards().await;

    test_setup
        .configure_program(
            &admin_signer,
            [ProgramConfiguration::Flag(
                ProgramFlagConfiguration::IsPaused(true),
            )],
        )
        .await
        .unwrap();

    let ix = try_build_instruction(
        &doublezero_revenue_distribution::ID,
        CollectIntegrationRewardsAccounts::new(
            dz_epoch,
            &mock_rewards_integration::ID,
            &integration_distribution_key,
            &integration_2z_bucket_key,
        ),
        &RevenueDistributionInstructionData::CollectIntegrationRewards,
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(&[ix], &[])
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
