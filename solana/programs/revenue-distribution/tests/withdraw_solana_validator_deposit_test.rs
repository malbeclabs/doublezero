mod common;

//

use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};
use doublezero_revenue_distribution::{
    instruction::{
        account::WithdrawSolanaValidatorDepositAccounts, ProgramConfiguration,
        ProgramFeatureConfiguration, ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    state::SolanaValidatorDeposit,
    types::{DoubleZeroEpoch, SolanaValidatorDebt},
    ID,
};
use solana_program_test::{tokio, BanksClientError};
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use svm_hash::merkle::{merkle_root_from_indexed_pod_leaves, MerkleProof};

//
// Setup.
//

struct WithdrawSolanaValidatorDepositSetup {
    test_setup: common::ProgramTestWithOwner,
    admin_signer: Keypair,
    debt_accountant_signer: Keypair,
    node_signer: Keypair,
    deposit_key: Pubkey,
    deposit_rent_exemption: u64,
}

/// Set up a configured program with a single validator deposit account
/// initialized. No debt has been configured.
async fn setup_for_withdraw_solana_validator_deposit() -> WithdrawSolanaValidatorDepositSetup {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    let node_signer = Keypair::new();
    let node_id = node_signer.pubkey();
    let deposit_key = SolanaValidatorDeposit::find_address(&node_id).0;

    test_setup
        .initialize_solana_validator_deposit(&node_id)
        .await
        .unwrap();

    let deposit_rent_exemption =
        (128 + zero_copy::data_end::<SolanaValidatorDeposit>() as u64) * 6_960;

    WithdrawSolanaValidatorDepositSetup {
        test_setup,
        admin_signer: configured.admin_signer,
        debt_accountant_signer: configured.debt_accountant_signer,
        node_signer,
        deposit_key,
        deposit_rent_exemption,
    }
}

struct WithdrawDelinquentDepositSetup {
    test_setup: common::ProgramTestWithOwner,
    node_signer: Keypair,
    deposit_key: Pubkey,
    deposit_rent_exemption: u64,
    debt_amount: u64,
}

/// Set up a configured program with a single validator whose debt has been
/// written off. The deposit account has only rent-exemption lamports.
async fn setup_with_written_off_debt() -> WithdrawDelinquentDepositSetup {
    let WithdrawSolanaValidatorDepositSetup {
        mut test_setup,
        admin_signer,
        debt_accountant_signer,
        node_signer,
        deposit_key,
        deposit_rent_exemption,
    } = setup_for_withdraw_solana_validator_deposit().await;

    let node_id = node_signer.pubkey();
    let dz_epoch = DoubleZeroEpoch::new(1);

    let debt_amount = 2_000_000_000;
    let debt_data = vec![SolanaValidatorDebt {
        node_id,
        amount: debt_amount,
    }];
    let merkle_root =
        merkle_root_from_indexed_pod_leaves(&debt_data, Some(SolanaValidatorDebt::LEAF_PREFIX))
            .unwrap();

    test_setup
        .configure_program(
            &admin_signer,
            [ProgramConfiguration::FeatureActivation {
                feature: ProgramFeatureConfiguration::SolanaValidatorDebtWriteOff,
                activation_epoch: DoubleZeroEpoch::new(1),
            }],
        )
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .configure_distribution_debt(
            dz_epoch,
            &debt_accountant_signer,
            1,
            debt_amount,
            merkle_root,
        )
        .await
        .unwrap()
        .finalize_distribution_debt(dz_epoch, &debt_accountant_signer)
        .await
        .unwrap()
        .enable_solana_validator_debt_write_off(dz_epoch)
        .await
        .unwrap();

    let proof =
        MerkleProof::from_indexed_pod_leaves(&debt_data, 0, Some(SolanaValidatorDebt::LEAF_PREFIX))
            .unwrap();

    test_setup
        .write_off_solana_validator_debt(
            dz_epoch,
            dz_epoch,
            &debt_accountant_signer,
            &debt_data[0],
            proof,
        )
        .await
        .unwrap();

    WithdrawDelinquentDepositSetup {
        test_setup,
        node_signer,
        deposit_key,
        deposit_rent_exemption,
        debt_amount,
    }
}

//
// Withdraw Solana validator deposit — error scenarios.
//

#[tokio::test]
async fn test_cannot_withdraw_solana_validator_deposit_with_wrong_node() {
    let WithdrawSolanaValidatorDepositSetup {
        mut test_setup,
        deposit_key,
        ..
    } = setup_for_withdraw_solana_validator_deposit().await;

    let wrong_node_id = Pubkey::new_unique();
    let (tx_err, program_logs) =
        simulate_program_revert(&mut test_setup, &wrong_node_id, Some(&deposit_key), None)
            .await
            .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Invalid address for validator node (account 2)"
    );
}

#[tokio::test]
async fn test_cannot_withdraw_solana_validator_deposit_when_paused() {
    let WithdrawSolanaValidatorDepositSetup {
        mut test_setup,
        admin_signer,
        node_signer,
        ..
    } = setup_for_withdraw_solana_validator_deposit().await;

    test_setup
        .configure_program(
            &admin_signer,
            [ProgramConfiguration::Flag(
                ProgramFlagConfiguration::IsPaused(true),
            )],
        )
        .await
        .unwrap();

    let (tx_err, program_logs) =
        simulate_program_revert(&mut test_setup, &node_signer.pubkey(), None, None)
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
async fn test_cannot_withdraw_solana_validator_deposit_with_delinquent_debt() {
    let WithdrawDelinquentDepositSetup {
        mut test_setup,
        node_signer,
        debt_amount,
        ..
    } = setup_with_written_off_debt().await;

    let node_id = node_signer.pubkey();

    // Verify written_off_sol_debt is set.
    let (_, deposit) = test_setup.fetch_solana_validator_deposit(&node_id).await;
    assert_eq!(deposit.written_off_sol_debt, debt_amount);

    // Cannot withdraw when there are no excess lamports (only rent exemption
    // with delinquent debt).
    let (tx_err, program_logs) = simulate_program_revert(&mut test_setup, &node_id, None, None)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        &format!(
            "Program log: No excess lamports to withdraw. Delinquent debt: {}",
            debt_amount
        )
    );
}

#[tokio::test]
async fn test_cannot_withdraw_solana_validator_deposit_without_excess() {
    let WithdrawSolanaValidatorDepositSetup {
        mut test_setup,
        node_signer,
        ..
    } = setup_for_withdraw_solana_validator_deposit().await;

    let node_id = node_signer.pubkey();

    // Cannot withdraw when there are no excess lamports (only rent exemption).
    let (tx_err, program_logs) = simulate_program_revert(&mut test_setup, &node_id, None, None)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: No excess lamports to withdraw. Delinquent debt: 0"
    );
}

#[tokio::test]
async fn test_cannot_withdraw_solana_validator_deposit_to_beneficiary_without_signer() {
    let WithdrawSolanaValidatorDepositSetup {
        mut test_setup,
        node_signer,
        deposit_key,
        ..
    } = setup_for_withdraw_solana_validator_deposit().await;

    let extra_lamports = 5_000_000_000;
    test_setup
        .transfer_lamports(&deposit_key, extra_lamports)
        .await
        .unwrap();

    // Provide a beneficiary but do not sign with the node key. Build the
    // instruction manually to avoid the automatic signer marking that the
    // From impl applies when beneficiary_key is set.
    let node_id = node_signer.pubkey();
    let accounts = WithdrawSolanaValidatorDepositAccounts::new(&node_id, None);
    let mut withdraw_ix = try_build_instruction(
        &ID,
        accounts,
        &RevenueDistributionInstructionData::WithdrawSolanaValidatorDeposit,
    )
    .unwrap();
    withdraw_ix
        .accounts
        .push(solana_sdk::instruction::AccountMeta::new(
            Pubkey::new_unique(),
            false,
        ));

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(&[withdraw_ix], &[])
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::MissingRequiredSignature)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Validator node must be a signer when a beneficiary is provided"
    );
}

//
// Withdraw Solana validator deposit — happy paths.
//

#[tokio::test]
async fn test_withdraw_solana_validator_deposit() {
    let WithdrawSolanaValidatorDepositSetup {
        mut test_setup,
        node_signer,
        deposit_key,
        deposit_rent_exemption,
        ..
    } = setup_for_withdraw_solana_validator_deposit().await;

    let node_id = node_signer.pubkey();

    // Fund the deposit account with extra lamports.
    let extra_lamports = 5_000_000_000;
    test_setup
        .transfer_lamports(&deposit_key, extra_lamports)
        .await
        .unwrap();

    // Withdraw with written_off_sol_debt == 0: only excess lamports beyond
    // rent exemption should be transferred to the node.
    let node_balance_before = test_setup
        .context
        .banks_client
        .get_balance(node_id)
        .await
        .unwrap();

    test_setup
        .withdraw_solana_validator_deposit(&node_signer, None)
        .await
        .unwrap();

    let node_balance_after = test_setup
        .context
        .banks_client
        .get_balance(node_id)
        .await
        .unwrap();

    assert_eq!(node_balance_after - node_balance_before, extra_lamports);

    // Account should still exist with rent-exemption lamports.
    let deposit_account = test_setup
        .context
        .banks_client
        .get_account(deposit_key)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(deposit_account.lamports, deposit_rent_exemption);
}

#[tokio::test]
async fn test_withdraw_solana_validator_deposit_to_beneficiary() {
    let WithdrawSolanaValidatorDepositSetup {
        mut test_setup,
        node_signer,
        deposit_key,
        deposit_rent_exemption,
        ..
    } = setup_for_withdraw_solana_validator_deposit().await;

    let beneficiary_key = Pubkey::new_unique();

    // Fund the deposit account with extra lamports.
    let extra_lamports = 5_000_000_000;
    test_setup
        .transfer_lamports(&deposit_key, extra_lamports)
        .await
        .unwrap();

    // Withdraw to beneficiary with written_off_sol_debt == 0: only excess
    // lamports beyond rent exemption should be transferred to the beneficiary.
    let beneficiary_balance_before = test_setup
        .context
        .banks_client
        .get_balance(beneficiary_key)
        .await
        .unwrap();

    test_setup
        .withdraw_solana_validator_deposit(&node_signer, Some(&beneficiary_key))
        .await
        .unwrap();

    let beneficiary_balance_after = test_setup
        .context
        .banks_client
        .get_balance(beneficiary_key)
        .await
        .unwrap();

    assert_eq!(
        beneficiary_balance_after - beneficiary_balance_before,
        extra_lamports
    );

    // Account should still exist with rent-exemption lamports.
    let deposit_account = test_setup
        .context
        .banks_client
        .get_account(deposit_key)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(deposit_account.lamports, deposit_rent_exemption);
}

#[tokio::test]
async fn test_withdraw_solana_validator_deposit_with_written_off_debt() {
    let WithdrawDelinquentDepositSetup {
        mut test_setup,
        node_signer,
        deposit_key,
        deposit_rent_exemption,
        debt_amount,
    } = setup_with_written_off_debt().await;

    let node_id = node_signer.pubkey();

    // Fund extra lamports beyond rent + written_off_sol_debt.
    let extra_lamports = 3_000_000_000;
    test_setup
        .transfer_lamports(&deposit_key, extra_lamports)
        .await
        .unwrap();

    // Withdraw: should only get extra_lamports - written_off_sol_debt.
    let node_balance_before = test_setup
        .context
        .banks_client
        .get_balance(node_id)
        .await
        .unwrap();

    test_setup
        .withdraw_solana_validator_deposit(&node_signer, None)
        .await
        .unwrap();

    let node_balance_after = test_setup
        .context
        .banks_client
        .get_balance(node_id)
        .await
        .unwrap();

    let expected_withdrawal = extra_lamports - debt_amount;
    assert_eq!(
        node_balance_after - node_balance_before,
        expected_withdrawal
    );

    // Account should still exist.
    let deposit_account = test_setup
        .context
        .banks_client
        .get_account(deposit_key)
        .await
        .unwrap();
    assert!(deposit_account.is_some());

    // Remaining lamports = rent_exemption + written_off_sol_debt.
    let remaining_lamports = deposit_account.unwrap().lamports;
    assert_eq!(remaining_lamports, deposit_rent_exemption + debt_amount);

    // Cannot withdraw again (nothing left beyond rent + debt).
    let (tx_err, program_logs) = simulate_program_revert(&mut test_setup, &node_id, None, None)
        .await
        .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        &format!(
            "Program log: No excess lamports to withdraw. Delinquent debt: {}",
            debt_amount
        )
    );
}

#[tokio::test]
async fn test_withdraw_solana_validator_deposit_with_written_off_debt_to_beneficiary() {
    let WithdrawDelinquentDepositSetup {
        mut test_setup,
        node_signer,
        deposit_key,
        deposit_rent_exemption,
        debt_amount,
    } = setup_with_written_off_debt().await;

    let beneficiary_key = Pubkey::new_unique();

    // Fund extra lamports beyond rent + written_off_sol_debt.
    let extra_lamports = 3_000_000_000;
    test_setup
        .transfer_lamports(&deposit_key, extra_lamports)
        .await
        .unwrap();

    // Withdraw to beneficiary: should only get extra_lamports - written_off_sol_debt.
    let beneficiary_balance_before = test_setup
        .context
        .banks_client
        .get_balance(beneficiary_key)
        .await
        .unwrap();

    test_setup
        .withdraw_solana_validator_deposit(&node_signer, Some(&beneficiary_key))
        .await
        .unwrap();

    let beneficiary_balance_after = test_setup
        .context
        .banks_client
        .get_balance(beneficiary_key)
        .await
        .unwrap();

    let expected_withdrawal = extra_lamports - debt_amount;
    assert_eq!(
        beneficiary_balance_after - beneficiary_balance_before,
        expected_withdrawal
    );

    // Remaining lamports = rent_exemption + written_off_sol_debt.
    let deposit_account = test_setup
        .context
        .banks_client
        .get_account(deposit_key)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(
        deposit_account.lamports,
        deposit_rent_exemption + debt_amount
    );
}

//
// Helpers.
//

async fn simulate_program_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    node_id: &Pubkey,
    deposit_key: Option<&Pubkey>,
    beneficiary_key: Option<&Pubkey>,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let mut accounts = WithdrawSolanaValidatorDepositAccounts::new(node_id, beneficiary_key);

    if let Some(deposit_key) = deposit_key {
        accounts.solana_validator_deposit_key = *deposit_key;
    }

    let withdraw_ix = try_build_instruction(
        &ID,
        accounts,
        &RevenueDistributionInstructionData::WithdrawSolanaValidatorDeposit,
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[withdraw_ix], &[])
        .await
}
