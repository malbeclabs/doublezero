mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::{ConfigureDistributionRewardsAccounts, FinalizeDistributionRewardsAccounts},
        ProgramConfiguration, RevenueDistributionInstructionData,
    },
    integration::{find_integration_bucket_address, find_integration_distribution_address},
    state::{self, Distribution, Journal},
    types::{BurnRate, DoubleZeroEpoch, ValidatorFee},
    DOUBLEZERO_MINT_KEY, ID,
};
use solana_program_test::{tokio, BanksClientError};
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use spl_associated_token_account_interface::address::get_associated_token_address;
use svm_hash::sha2::Hash;

//
// Setup.
//

struct FinalizeDistributionRewardsSetup {
    test_setup: common::ProgramTestWithOwner,
    admin_signer: Keypair,
    debt_accountant_signer: Keypair,
    rewards_accountant_signer: Keypair,
    dz_epoch: DoubleZeroEpoch,
    total_solana_validators: u32,
    total_solana_validator_debt: u64,
    solana_validator_debt_merkle_root: Hash,
    total_contributors: u32,
    rewards_merkle_root: Hash,
}

/// Set up a configured program with distribution debt configured on epoch 1.
async fn setup_for_finalize_distribution_rewards() -> FinalizeDistributionRewardsSetup {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    let dz_epoch = DoubleZeroEpoch::new(1);
    let total_solana_validators = 2_048;
    let total_solana_validator_debt = 69;
    let solana_validator_debt_merkle_root = Hash::new_unique();
    let total_contributors = 69;
    let rewards_merkle_root = Hash::new_unique();

    test_setup
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .configure_distribution_debt(
            dz_epoch,
            &configured.debt_accountant_signer,
            total_solana_validators,
            total_solana_validator_debt,
            solana_validator_debt_merkle_root,
        )
        .await
        .unwrap();

    FinalizeDistributionRewardsSetup {
        test_setup,
        admin_signer: configured.admin_signer,
        debt_accountant_signer: configured.debt_accountant_signer,
        rewards_accountant_signer: configured.rewards_accountant_signer,
        dz_epoch,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
        total_contributors,
        rewards_merkle_root,
    }
}

//
// Finalize distribution rewards — happy path with sequential error checks.
//

#[tokio::test]
async fn test_finalize_distribution_rewards() {
    let FinalizeDistributionRewardsSetup {
        mut test_setup,
        admin_signer,
        debt_accountant_signer,
        rewards_accountant_signer,
        dz_epoch,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
        total_contributors,
        rewards_merkle_root,
    } = setup_for_finalize_distribution_rewards().await;

    let initial_cbr = 100_000_000;
    let solana_validator_base_block_rewards_pct_fee = 500;
    let distribute_rewards_relay_lamports = 10_000;

    // Cannot finalize rewards until debt has been finalized.
    let (tx_err, program_logs) = simulate_finalize_revert(&mut test_setup, dz_epoch)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Distribution debt calculation is not finalized yet"
    );

    test_setup
        .finalize_distribution_debt(dz_epoch, &debt_accountant_signer)
        .await
        .unwrap();

    // Cannot finalize if the rewards root is null.
    let (tx_err, program_logs) = simulate_finalize_revert(&mut test_setup, dz_epoch)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Rewards root cannot be null with calculated debt"
    );

    test_setup
        .configure_distribution_rewards(
            dz_epoch,
            &rewards_accountant_signer,
            total_contributors,
            rewards_merkle_root,
        )
        .await
        .unwrap();

    // Cannot finalize until the minimum number of epochs has been configured.
    let (tx_err, program_logs) = simulate_finalize_revert(&mut test_setup, dz_epoch)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Minimum epoch duration to finalize rewards is misconfigured"
    );

    let minimum_epoch_duration_to_finalize_rewards = 2;

    test_setup
        .configure_program(
            &admin_signer,
            [ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(
                minimum_epoch_duration_to_finalize_rewards,
            )],
        )
        .await
        .unwrap();

    let (_, program_config, _) = test_setup.fetch_program_config().await;

    let minimum_dz_epoch_to_finalize =
        dz_epoch.saturating_add_duration(minimum_epoch_duration_to_finalize_rewards.into());

    // Cannot finalize until the minimum number of epochs have passed.
    let (tx_err, program_logs) = simulate_finalize_revert(&mut test_setup, dz_epoch)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        &format!(
            "Program log: DZ epoch must be at least {} (currently {}) to finalize rewards",
            minimum_dz_epoch_to_finalize, program_config.next_completed_dz_epoch
        )
    );

    // Initialize another distribution to move next DZ epoch to allow rewards to
    // be finalized.
    test_setup
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap();

    let (_, program_config, _) = test_setup.fetch_program_config().await;
    assert_eq!(
        program_config.next_completed_dz_epoch,
        minimum_dz_epoch_to_finalize
    );

    let (_, _, remaining_distribution_data_before, distribution_lamports_balance_before, _) =
        test_setup.fetch_distribution(dz_epoch).await;
    let remaining_distribution_data_len_before = remaining_distribution_data_before.len();

    test_setup
        .finalize_distribution_rewards(dz_epoch)
        .await
        .unwrap();

    let (
        distribution_key,
        distribution,
        distribution_remaining_data,
        distribution_lamports_balance_after,
        _,
    ) = test_setup.fetch_distribution(dz_epoch).await;

    let expected_additional_data_len = 9;
    assert_eq!(total_contributors / 8 + 1, expected_additional_data_len);
    assert_eq!(
        distribution_lamports_balance_after,
        distribution_lamports_balance_before
            + 690_000
            + 6_960 * expected_additional_data_len as u64
    );

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_is_debt_calculation_finalized(true);
    expected_distribution.set_is_rewards_calculation_finalized(true);
    expected_distribution.bump_seed = Distribution::find_address(dz_epoch).1;
    expected_distribution.token_2z_pda_bump_seed =
        state::find_2z_token_pda_address(&distribution_key).1;
    expected_distribution.dz_epoch = dz_epoch;
    expected_distribution.community_burn_rate = BurnRate::new(initial_cbr).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(solana_validator_base_block_rewards_pct_fee).unwrap();
    expected_distribution.total_solana_validators = total_solana_validators;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.total_contributors = total_contributors;
    expected_distribution.rewards_merkle_root = rewards_merkle_root;
    expected_distribution.processed_solana_validator_debt_end_index = total_solana_validators / 8;
    expected_distribution.processed_rewards_start_index = total_solana_validators / 8;
    expected_distribution.processed_rewards_end_index =
        (total_solana_validators / 8) + (total_contributors / 8 + 1);
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;
    assert_eq!(distribution, expected_distribution);

    let expected_distribution_remaining_data_len =
        remaining_distribution_data_len_before + expected_additional_data_len as usize;
    assert_eq!(
        distribution_remaining_data,
        vec![0; expected_distribution_remaining_data_len]
    );

    // Cannot configure distribution rewards after finalization.
    let (tx_err, program_logs) =
        simulate_configure_rewards_revert(&mut test_setup, &rewards_accountant_signer, dz_epoch)
            .await
            .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Distribution rewards have already been finalized"
    );

    // Cannot finalize again.
    let (tx_err, program_logs) = simulate_finalize_revert(&mut test_setup, dz_epoch)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Distribution rewards have already been finalized"
    );
}

//
// Null-root guard — collected prepaid 2Z blocks finalize.
//

#[tokio::test]
async fn test_cannot_finalize_null_root_with_collected_prepaid_2z() {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    let dz_epoch = DoubleZeroEpoch::new(1);

    let journal_key = Journal::find_address().0;
    let journal_ata_key = get_associated_token_address(&journal_key, &DOUBLEZERO_MINT_KEY);
    let prepaid_2z_amount = 100_000_000;

    // The first initialize_distribution creates epoch 0; the tested epoch 1 is
    // created by the second one. Fund the journal's 2Z ATA between the two so it
    // is the epoch-1 distribution that sweeps the prepaid 2Z.
    test_setup
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .create_2z_ata(&journal_key)
        .await
        .unwrap()
        .transfer_2z(&journal_ata_key, prepaid_2z_amount)
        .await
        .unwrap()
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .configure_distribution_debt(
            dz_epoch,
            &configured.debt_accountant_signer,
            0,
            0,
            Hash::default(),
        )
        .await
        .unwrap()
        .finalize_distribution_debt(dz_epoch, &configured.debt_accountant_signer)
        .await
        .unwrap()
        .configure_program(
            &configured.admin_signer,
            [ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(
                2,
            )],
        )
        .await
        .unwrap()
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap();

    let (_, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;
    assert_eq!(
        distribution.collected_prepaid_2z_payments,
        prepaid_2z_amount
    );
    assert_eq!(distribution.checked_total_sol_debt().unwrap(), 0);

    // Zero SOL debt but collected prepaid 2Z means the null root is rejected by
    // the collected-2Z check.
    let (tx_err, program_logs) = simulate_finalize_revert(&mut test_setup, dz_epoch)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Rewards root cannot be null with collected 2Z"
    );

    // Posting a real root unblocks finalize.
    test_setup
        .configure_distribution_rewards(
            dz_epoch,
            &configured.rewards_accountant_signer,
            69,
            Hash::new_unique(),
        )
        .await
        .unwrap()
        .finalize_distribution_rewards(dz_epoch)
        .await
        .unwrap();

    let (_, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;
    assert!(distribution.is_rewards_calculation_finalized());
}

//
// Null-root guard — collected integration 2Z blocks finalize.
//

#[tokio::test]
async fn test_cannot_finalize_null_root_with_collected_integration_2z() {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    let dz_epoch = DoubleZeroEpoch::new(1);

    // Register the integration before the target distribution is initialized so
    // its snapshot captures the one registered integration.
    test_setup
        .initialize_rewards_integration(&configured.admin_signer, &mock_rewards_integration::ID)
        .await
        .unwrap()
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap();

    let (integration_distribution_key, _) =
        find_integration_distribution_address(&mock_rewards_integration::ID, dz_epoch);
    let (integration_2z_bucket_key, _) = find_integration_bucket_address(
        &mock_rewards_integration::ID,
        &integration_distribution_key,
    );

    let collected_integration_2z_amount = 100_000_000;

    // Epoch 1 (the tested epoch) is created by the second initialize_distribution
    // below; collect only after its distribution PDA exists.
    test_setup
        .mock_initialize_integration_distribution(dz_epoch)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap()
        .transfer_2z(&integration_2z_bucket_key, collected_integration_2z_amount)
        .await
        .unwrap()
        .collect_integration_rewards(
            dz_epoch,
            &mock_rewards_integration::ID,
            &integration_distribution_key,
            &integration_2z_bucket_key,
        )
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .configure_distribution_debt(
            dz_epoch,
            &configured.debt_accountant_signer,
            0,
            0,
            Hash::default(),
        )
        .await
        .unwrap()
        .finalize_distribution_debt(dz_epoch, &configured.debt_accountant_signer)
        .await
        .unwrap()
        .configure_program(
            &configured.admin_signer,
            [ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(
                2,
            )],
        )
        .await
        .unwrap()
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap();

    let (_, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;
    assert_eq!(
        distribution.collected_2z_from_integrations,
        collected_integration_2z_amount
    );
    assert!(distribution.are_all_integrations_collected());
    assert_eq!(distribution.checked_total_sol_debt().unwrap(), 0);

    // Nonzero collected integration 2Z rejects the null root via the
    // collected-2Z check (integrations are also fully collected here).
    let (tx_err, program_logs) = simulate_finalize_revert(&mut test_setup, dz_epoch)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Rewards root cannot be null with collected 2Z"
    );

    // Posting a real root unblocks finalize.
    test_setup
        .configure_distribution_rewards(
            dz_epoch,
            &configured.rewards_accountant_signer,
            69,
            Hash::new_unique(),
        )
        .await
        .unwrap()
        .finalize_distribution_rewards(dz_epoch)
        .await
        .unwrap();

    let (_, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;
    assert!(distribution.is_rewards_calculation_finalized());
}

//
// Null-root guard — uncollected integration blocks finalize, zero-value collect
// then unblocks it.
//

#[tokio::test]
async fn test_cannot_finalize_null_root_with_uncollected_integration() {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    let dz_epoch = DoubleZeroEpoch::new(1);

    test_setup
        .initialize_rewards_integration(&configured.admin_signer, &mock_rewards_integration::ID)
        .await
        .unwrap()
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap();

    let (integration_distribution_key, _) =
        find_integration_distribution_address(&mock_rewards_integration::ID, dz_epoch);
    let (integration_2z_bucket_key, _) = find_integration_bucket_address(
        &mock_rewards_integration::ID,
        &integration_distribution_key,
    );

    // Initialize the integration distribution but leave its bucket empty and do
    // not collect, so the integration stays pending.
    test_setup
        .mock_initialize_integration_distribution(dz_epoch)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .configure_distribution_debt(
            dz_epoch,
            &configured.debt_accountant_signer,
            0,
            0,
            Hash::default(),
        )
        .await
        .unwrap()
        .finalize_distribution_debt(dz_epoch, &configured.debt_accountant_signer)
        .await
        .unwrap()
        .configure_program(
            &configured.admin_signer,
            [ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(
                2,
            )],
        )
        .await
        .unwrap()
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap();

    let (_, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;
    assert_eq!(distribution.checked_total_sol_debt().unwrap(), 0);
    assert_eq!(distribution.total_collected_2z_tokens(), 0);
    assert!(!distribution.are_all_integrations_collected());

    // Zero debt and zero collected 2Z, but the pending integration blocks the
    // null root.
    let (tx_err, program_logs) = simulate_finalize_revert(&mut test_setup, dz_epoch)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Rewards root cannot be null with uncollected integrations"
    );

    // Collecting against the empty bucket adds no 2Z but marks the integration
    // collected, so the null-root finalize now succeeds.
    test_setup
        .collect_integration_rewards(
            dz_epoch,
            &mock_rewards_integration::ID,
            &integration_distribution_key,
            &integration_2z_bucket_key,
        )
        .await
        .unwrap();

    let (_, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;
    assert_eq!(distribution.total_collected_2z_tokens(), 0);
    assert!(distribution.are_all_integrations_collected());

    test_setup
        .finalize_distribution_rewards(dz_epoch)
        .await
        .unwrap();

    let (_, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;
    assert!(distribution.is_rewards_calculation_finalized());
}

//
// Rooted path is unaffected by an uncollected integration.
//

#[tokio::test]
async fn test_finalize_rooted_with_uncollected_integration() {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    let dz_epoch = DoubleZeroEpoch::new(1);

    test_setup
        .initialize_rewards_integration(&configured.admin_signer, &mock_rewards_integration::ID)
        .await
        .unwrap()
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap()
        .mock_initialize_integration_distribution(dz_epoch)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .configure_distribution_debt(
            dz_epoch,
            &configured.debt_accountant_signer,
            0,
            0,
            Hash::default(),
        )
        .await
        .unwrap()
        .finalize_distribution_debt(dz_epoch, &configured.debt_accountant_signer)
        .await
        .unwrap()
        .configure_distribution_rewards(
            dz_epoch,
            &configured.rewards_accountant_signer,
            69,
            Hash::new_unique(),
        )
        .await
        .unwrap()
        .configure_program(
            &configured.admin_signer,
            [ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(
                2,
            )],
        )
        .await
        .unwrap()
        .initialize_distribution(&configured.debt_accountant_signer)
        .await
        .unwrap();

    let (_, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;
    assert!(!distribution.are_all_integrations_collected());
    assert_ne!(distribution.rewards_merkle_root, Hash::default());

    // A non-null root skips the entire null-root guard block, so the pending
    // integration does not block finalize.
    test_setup
        .finalize_distribution_rewards(dz_epoch)
        .await
        .unwrap();

    let (_, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;
    assert!(distribution.is_rewards_calculation_finalized());
}

//
// Helpers.
//

async fn simulate_finalize_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    dz_epoch: DoubleZeroEpoch,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let payer_key = test_setup.payer_signer().pubkey();

    let finalize_distribution_rewards_ix = try_build_instruction(
        &ID,
        FinalizeDistributionRewardsAccounts::new(&payer_key, dz_epoch),
        &RevenueDistributionInstructionData::FinalizeDistributionRewards,
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[finalize_distribution_rewards_ix], &[])
        .await
}

async fn simulate_configure_rewards_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    rewards_accountant_signer: &Keypair,
    dz_epoch: DoubleZeroEpoch,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let configure_distribution_rewards_ix = try_build_instruction(
        &ID,
        ConfigureDistributionRewardsAccounts::new(&rewards_accountant_signer.pubkey(), dz_epoch),
        &RevenueDistributionInstructionData::ConfigureDistributionRewards {
            total_contributors: 69,
            merkle_root: Hash::new_unique(),
        },
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(
            &[configure_distribution_rewards_ix],
            &[rewards_accountant_signer],
        )
        .await
}
