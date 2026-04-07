mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::ConfigureDistributionDebtAccounts, ProgramConfiguration, ProgramFlagConfiguration,
        RevenueDistributionInstructionData,
    },
    state::{self, Distribution},
    types::{BurnRate, DoubleZeroEpoch, SolanaValidatorDebt, ValidatorFee},
    ID,
};
use solana_program_test::{tokio, BanksClientError};
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use svm_hash::{merkle::merkle_root_from_indexed_pod_leaves, sha2::Hash};

//
// Setup.
//

struct ConfigureDistributionDebtSetup {
    test_setup: common::ProgramTestWithOwner,
    admin_signer: Keypair,
    debt_accountant_signer: Keypair,
}

/// Set up a configured program with two distributions (epoch 0 and 1),
/// but WITHOUT validator fee parameters set. This allows testing the
/// zero-fee rejection path.
async fn setup_for_configure_distribution_debt() -> ConfigureDistributionDebtSetup {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();
    let debt_accountant_signer = Keypair::new();
    let rewards_accountant_signer = Keypair::new();

    let calculation_grace_period_minutes = 60;
    let initialization_grace_period_minutes = 1;

    test_setup
        .initialize_program()
        .await
        .unwrap()
        .initialize_journal()
        .await
        .unwrap()
        .set_admin(&admin_signer.pubkey())
        .await
        .unwrap()
        .configure_program(
            &admin_signer,
            [
                ProgramConfiguration::DebtAccountant(debt_accountant_signer.pubkey()),
                ProgramConfiguration::RewardsAccountant(rewards_accountant_signer.pubkey()),
                ProgramConfiguration::CommunityBurnRateParameters {
                    limit: 500_000_000,
                    dz_epochs_to_increasing: 10,
                    dz_epochs_to_limit: 20,
                    initial_rate: Some(100_000_000),
                },
                ProgramConfiguration::DistributeRewardsRelayLamports(10_000),
                ProgramConfiguration::CalculationGracePeriodMinutes(
                    calculation_grace_period_minutes,
                ),
                ProgramConfiguration::DistributionInitializationGracePeriodMinutes(
                    initialization_grace_period_minutes,
                ),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(u32::from(initialization_grace_period_minutes) * 60)
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(u32::from(calculation_grace_period_minutes) * 60)
        .await
        .unwrap();

    ConfigureDistributionDebtSetup {
        test_setup,
        admin_signer,
        debt_accountant_signer,
    }
}

//
// Configure distribution debt — cannot configure with zero fees.
//

#[tokio::test]
async fn test_cannot_configure_distribution_debt_with_zero_fees() {
    let ConfigureDistributionDebtSetup {
        mut test_setup,
        debt_accountant_signer,
        ..
    } = setup_for_configure_distribution_debt().await;

    let dz_epoch = DoubleZeroEpoch::new(1);

    let (tx_err, program_logs) = simulate_program_revert(
        &mut test_setup,
        &debt_accountant_signer,
        dz_epoch,
        3,
        69,
        Hash::new_unique(),
    )
    .await
    .unwrap();

    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Configuring distribution debt disallowed"
    );
}

//
// Configure distribution debt — happy path.
//

#[tokio::test]
async fn test_configure_distribution_debt() {
    let ConfigureDistributionDebtSetup {
        mut test_setup,
        admin_signer,
        debt_accountant_signer,
    } = setup_for_configure_distribution_debt().await;

    let solana_validator_base_block_rewards_pct_fee = 500; // 5%.
    let dz_epoch = DoubleZeroEpoch::new(2);

    test_setup
        .configure_program(
            &admin_signer,
            [ProgramConfiguration::SolanaValidatorFeeParameters {
                base_block_rewards_pct: solana_validator_base_block_rewards_pct_fee,
                priority_block_rewards_pct: 0,
                inflation_rewards_pct: 0,
                jito_tips_pct: 0,
                fixed_sol_amount: 0,
                _unused: Default::default(),
            }],
        )
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60 * 60)
        .await
        .unwrap();

    let debt_data = (0..3)
        .map(|i| SolanaValidatorDebt {
            node_id: Pubkey::new_unique(),
            amount: 10_000_000_000 * (i + 1),
        })
        .collect::<Vec<_>>();

    let total_solana_validators = debt_data.len() as u32;
    let total_solana_validator_debt = debt_data.iter().map(|debt| debt.amount).sum();
    let solana_validator_debt_merkle_root =
        merkle_root_from_indexed_pod_leaves(&debt_data, Some(SolanaValidatorDebt::LEAF_PREFIX))
            .unwrap();

    test_setup
        .configure_distribution_debt(
            dz_epoch,
            &debt_accountant_signer,
            3,
            total_solana_validator_debt + 1,
            Hash::new_unique(),
        )
        .await
        .unwrap()
        .configure_distribution_debt(
            dz_epoch,
            &debt_accountant_signer,
            total_solana_validators,
            total_solana_validator_debt,
            solana_validator_debt_merkle_root,
        )
        .await
        .unwrap();

    let (distribution_key, distribution, _, _, _) = test_setup.fetch_distribution(dz_epoch).await;

    let initial_cbr = 100_000_000;
    let distribute_rewards_relay_lamports = 10_000;

    let mut expected_distribution = Distribution::default();
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
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;
    assert_eq!(distribution, expected_distribution);
}

//
// Helpers.
//

async fn simulate_program_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    debt_accountant_signer: &Keypair,
    dz_epoch: DoubleZeroEpoch,
    total_validators: u32,
    total_debt: u64,
    merkle_root: Hash,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let configure_distribution_debt_ix = try_build_instruction(
        &ID,
        ConfigureDistributionDebtAccounts::new(&debt_accountant_signer.pubkey(), dz_epoch),
        &RevenueDistributionInstructionData::ConfigureDistributionDebt {
            total_validators,
            total_debt,
            merkle_root,
        },
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[configure_distribution_debt_ix], &[debt_accountant_signer])
        .await
}
