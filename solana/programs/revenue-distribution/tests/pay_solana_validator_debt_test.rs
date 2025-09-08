mod common;

//

use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};
use doublezero_revenue_distribution::{
    instruction::{
        account::PaySolanaValidatorDebtAccounts, DistributionMerkleRootKind, ProgramConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    state::{self, Distribution, SolanaValidatorDeposit},
    types::{BurnRate, DoubleZeroEpoch, SolanaValidatorDebt, ValidatorFee},
    ID,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use svm_hash::merkle::{merkle_root_from_indexed_pod_leaves, MerkleProof};

//
// Pay Solana validator debt.
//

#[tokio::test]
async fn test_pay_solana_validator_debt() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    let debt_accountant_signer = Keypair::new();
    let rewards_accountant_signer = Keypair::new();
    let solana_validator_base_block_rewards_pct_fee = 500; // 5%.

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Relay settings.
    let distribute_rewards_relay_lamports = 10_000;

    // Distribution debt.

    let dz_epoch = DoubleZeroEpoch::new(1);

    let debt_data = (0..16)
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
                ProgramConfiguration::SolanaValidatorFeeParameters {
                    base_block_rewards_pct: solana_validator_base_block_rewards_pct_fee,
                    priority_block_rewards_pct: 0,
                    inflation_rewards_pct: 0,
                    jito_tips_pct: 0,
                    fixed_sol_amount: 0,
                    _unused: Default::default(),
                },
                ProgramConfiguration::CommunityBurnRateParameters {
                    limit: cbr_limit,
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
                },
                ProgramConfiguration::DistributeRewardsRelayLamports(
                    distribute_rewards_relay_lamports,
                ),
                ProgramConfiguration::CalculationGracePeriodSeconds(1),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(1)
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
        .unwrap()
        .finalize_distribution_debt(dz_epoch, &debt_accountant_signer)
        .await
        .unwrap();

    // Show that verification passes.
    let kinds_and_proofs = debt_data
        .iter()
        .enumerate()
        .map(|(i, debt)| {
            let kind = DistributionMerkleRootKind::SolanaValidatorDebt(*debt);
            let proof = MerkleProof::from_indexed_pod_leaves(
                &debt_data,
                i.try_into().unwrap(),
                Some(SolanaValidatorDebt::LEAF_PREFIX),
            )
            .unwrap();
            (kind, proof)
        })
        .collect::<Vec<_>>();

    // Clone proofs to pay after verification.
    let proofs = kinds_and_proofs
        .iter()
        .map(|(_, proof)| proof.clone())
        .collect::<Vec<_>>();

    let deposit_rent_exemption =
        (128 + zero_copy::data_end::<SolanaValidatorDeposit>() as u64) * 6_960;

    // Initialize Solana validator deposit accounts and transfer an amount one
    // less than the debt amount.
    for SolanaValidatorDebt { node_id, amount } in debt_data.iter() {
        let (deposit_key, _) = SolanaValidatorDeposit::find_address(node_id);

        test_setup
            .initialize_solana_validator_deposit(node_id)
            .await
            .unwrap()
            .transfer_lamports(&deposit_key, amount - 1)
            .await
            .unwrap();
    }

    for (debt, proof) in debt_data.iter().zip(proofs.clone()) {
        // Cannot pay any amount except the exact debt amount.

        let invalid_merkle_root = proof.root_from_pod_leaf(
            &SolanaValidatorDebt {
                node_id: debt.node_id,
                amount: debt.amount - 1,
            },
            Some(SolanaValidatorDebt::LEAF_PREFIX),
        );

        let pay_solana_validator_debt_ix = try_build_instruction(
            &ID,
            PaySolanaValidatorDebtAccounts::new(dz_epoch, &debt.node_id),
            &RevenueDistributionInstructionData::PaySolanaValidatorDebt {
                amount: debt.amount - 1,
                proof: proof.clone(),
            },
        )
        .unwrap();

        let (tx_err, program_logs) = test_setup
            .unwrap_simulation_error(&[pay_solana_validator_debt_ix], &[])
            .await;
        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidInstructionData)
        );
        assert_eq!(
            program_logs.get(4).unwrap(),
            &format!("Program log: Invalid computed merkle root: {invalid_merkle_root}")
        );

        // Cannot pay debt with insufficient funds.

        let pay_solana_validator_debt_ix = try_build_instruction(
            &ID,
            PaySolanaValidatorDebtAccounts::new(dz_epoch, &debt.node_id),
            &RevenueDistributionInstructionData::PaySolanaValidatorDebt {
                amount: debt.amount,
                proof,
            },
        )
        .unwrap();

        let (tx_err, program_logs) = test_setup
            .unwrap_simulation_error(&[pay_solana_validator_debt_ix], &[])
            .await;
        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
        );
        assert_eq!(
            program_logs.get(4).unwrap(),
            "Program log: Insufficient funds in Solana validator deposit to pay debt"
        );
    }

    // Set up Solana validator deposit accounts with lamports to satisfy debt.
    let mut balances_before = vec![0; debt_data.len()];

    // Send last lamport to each deposit account to satisfy debt.
    for (SolanaValidatorDebt { node_id, amount }, balance_before) in
        debt_data.iter().zip(balances_before.iter_mut())
    {
        let (deposit_key, _) = SolanaValidatorDeposit::find_address(node_id);

        test_setup.transfer_lamports(&deposit_key, 1).await.unwrap();

        let balance = test_setup
            .context
            .banks_client
            .get_balance(deposit_key)
            .await
            .unwrap();

        // Balance must include rent.
        assert_eq!(balance, amount + deposit_rent_exemption);

        // Store balance before paying debt.
        *balance_before = balance;
    }

    test_setup
        .verify_distribution_merkle_root(dz_epoch, kinds_and_proofs)
        .await
        .unwrap();

    let (_, journal, _) = test_setup.fetch_journal().await;
    assert_eq!(journal.total_sol_balance, 0);

    // Pay debt.
    for ((debt, balance_before), proof) in debt_data.iter().zip(balances_before).zip(proofs.clone())
    {
        test_setup
            .pay_solana_validator_debt(dz_epoch, &debt.node_id, debt.amount, proof)
            .await
            .unwrap();

        let balance_after = test_setup
            .context
            .banks_client
            .get_balance(SolanaValidatorDeposit::find_address(&debt.node_id).0)
            .await
            .unwrap();

        assert_eq!(balance_before - balance_after, debt.amount);
    }

    let (distribution_key, distribution, remaining_distribution_data, _, _) =
        test_setup.fetch_distribution(dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_is_debt_calculation_finalized(true);
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
    expected_distribution.solana_validator_payments_count = total_solana_validators;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.collected_solana_validator_payments = total_solana_validator_debt;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.processed_solana_validator_debt_end_index = total_solana_validators / 8;
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;
    assert_eq!(distribution, expected_distribution);

    assert_eq!(remaining_distribution_data, vec![0b11111111, 0b11111111]);

    let (_, journal, _) = test_setup.fetch_journal().await;
    assert_eq!(journal.total_sol_balance, total_solana_validator_debt);

    // Cannot pay debt again.
    for (debt, proof) in debt_data.iter().zip(proofs) {
        let leaf_index = proof.leaf_index.unwrap();

        let pay_solana_validator_debt_ix = try_build_instruction(
            &ID,
            PaySolanaValidatorDebtAccounts::new(dz_epoch, &debt.node_id),
            &RevenueDistributionInstructionData::PaySolanaValidatorDebt {
                amount: debt.amount,
                proof,
            },
        )
        .unwrap();

        let (tx_err, program_logs) = test_setup
            .unwrap_simulation_error(&[pay_solana_validator_debt_ix], &[])
            .await;
        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
        );
        assert_eq!(
            program_logs.get(4).unwrap(),
            &format!("Program log: Merkle leaf index {leaf_index} has already been processed")
        );
        assert_eq!(
            program_logs.get(5).unwrap(),
            "Program log: Solana validator debt already processed"
        )
    }
}
