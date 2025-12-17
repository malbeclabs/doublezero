mod common;

//

use std::collections::HashMap;

use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};
use doublezero_revenue_distribution::{
    instruction::{
        account::DistributeRewardsAccounts, ContributorRewardsConfiguration,
        DistributionMerkleRootKind, ProgramConfiguration, ProgramFeatureConfiguration,
        ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    state::{self, Distribution, SolanaValidatorDeposit},
    types::{BurnRate, DoubleZeroEpoch, RewardShare, SolanaValidatorDebt, ValidatorFee},
    DOUBLEZERO_MINT_KEY, ID,
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
// Distribute rewards.
//

#[tokio::test]
async fn test_distribute_rewards() {
    let transfer_authority_signer = Keypair::new();

    let bootstrapped_accounts = common::generate_token_accounts_for_test(
        &DOUBLEZERO_MINT_KEY,
        &[transfer_authority_signer.pubkey()],
    );
    let src_token_account_key = bootstrapped_accounts.first().unwrap().key;

    let mut test_setup = common::start_test_with_accounts(bootstrapped_accounts).await;

    let admin_signer = Keypair::new();

    let contributor_manager_signer = Keypair::new();
    let debt_accountant_signer = Keypair::new();
    let rewards_accountant_signer = Keypair::new();
    let solana_validator_base_block_rewards_pct_fee = 500; // 5%.

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Relay settings. We are setting this to 128 * 6960 to ensure that the
    // relayer can get paid without the transaction reverting. But practically,
    // the relayer will have enough lamports to be rent exempt so this will not
    // be a problem if the configured value is less than this.
    let distribute_rewards_relay_lamports = 128 * 6_960;

    // Distribution debt.
    let debt_data = (0..8)
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

    // Do not pay all debt. Forgive one poor soul.
    let uncollectible_index = 2;
    let uncollectible_debt = debt_data[uncollectible_index];

    let expected_swept_2z_amount_1 = 69 * u64::pow(10, 8);
    let expected_swept_2z_amount_2 = 420 * u64::pow(10, 8);

    // Distribution rewards.
    let minimum_epoch_duration_to_finalize_rewards = 1;

    // Target epochs.
    let dz_epoch = DoubleZeroEpoch::new(1);
    let next_dz_epoch = dz_epoch.saturating_add_duration(1);

    test_setup
        .transfer_2z(
            &src_token_account_key,
            expected_swept_2z_amount_1 + expected_swept_2z_amount_2,
        )
        .await
        .unwrap()
        .initialize_program()
        .await
        .unwrap()
        .initialize_journal()
        .await
        .unwrap()
        .initialize_swap_destination(&DOUBLEZERO_MINT_KEY)
        .await
        .unwrap()
        .set_admin(&admin_signer.pubkey())
        .await
        .unwrap()
        .configure_program(
            &admin_signer,
            [
                ProgramConfiguration::Sol2zSwapProgram(mock_swap_sol_2z::ID),
                ProgramConfiguration::ContributorManager(contributor_manager_signer.pubkey()),
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
                ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(
                    minimum_epoch_duration_to_finalize_rewards,
                ),
                ProgramConfiguration::CalculationGracePeriodMinutes(1),
                ProgramConfiguration::DistributionInitializationGracePeriodMinutes(1),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
                ProgramConfiguration::FeatureActivation {
                    feature: ProgramFeatureConfiguration::SolanaValidatorDebtWriteOff,
                    activation_epoch: dz_epoch,
                },
            ],
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
        .finalize_distribution_debt(DoubleZeroEpoch::default(), &debt_accountant_signer)
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
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        .configure_distribution_debt(
            next_dz_epoch,
            &debt_accountant_signer,
            total_solana_validators,
            total_solana_validator_debt,
            solana_validator_debt_merkle_root,
        )
        .await
        .unwrap()
        .finalize_distribution_debt(next_dz_epoch, &debt_accountant_signer)
        .await
        .unwrap()
        .finalize_distribution_rewards(Default::default())
        .await
        .unwrap()
        .sweep_distribution_tokens(Default::default())
        .await
        .unwrap()
        .enable_solana_validator_debt_write_off(dz_epoch)
        .await
        .unwrap();

    // 1. Initialize Solana validator deposit accounts.
    // 2. Transfer amount each validator owes so each can pay its debt.
    // 3. Pay each validator's debt.
    for (i, debt) in debt_data.iter().enumerate() {
        let node_id = &debt.node_id;
        let amount = debt.amount;

        let (deposit_key, _) = SolanaValidatorDeposit::find_address(node_id);

        let proof = MerkleProof::from_indexed_pod_leaves(
            &debt_data,
            i.try_into().unwrap(),
            Some(SolanaValidatorDebt::LEAF_PREFIX),
        )
        .unwrap();

        // Just pay for the second distribution.
        if i == uncollectible_index {
            test_setup
                .initialize_solana_validator_deposit(node_id)
                .await
                .unwrap()
                .transfer_lamports(&deposit_key, amount)
                .await
                .unwrap()
                .pay_solana_validator_debt(next_dz_epoch, debt, proof.clone())
                .await
                .unwrap()
                .write_off_solana_validator_debt(
                    dz_epoch,
                    next_dz_epoch,
                    &debt_accountant_signer,
                    &uncollectible_debt,
                    proof,
                )
                .await
                .unwrap();
        } else {
            test_setup
                .initialize_solana_validator_deposit(node_id)
                .await
                .unwrap()
                .transfer_lamports(&deposit_key, 2 * amount)
                .await
                .unwrap()
                .pay_solana_validator_debt(dz_epoch, debt, proof.clone())
                .await
                .unwrap()
                .pay_solana_validator_debt(next_dz_epoch, debt, proof)
                .await
                .unwrap();
        }
    }

    let sol_destination_key = Pubkey::new_unique();

    test_setup
        .mock_buy_sol(
            &src_token_account_key,
            &transfer_authority_signer,
            &sol_destination_key,
            expected_swept_2z_amount_1,
            total_solana_validator_debt,
        )
        .await
        .unwrap()
        .mock_buy_sol(
            &src_token_account_key,
            &transfer_authority_signer,
            &sol_destination_key,
            expected_swept_2z_amount_2,
            total_solana_validator_debt - uncollectible_debt.amount,
        )
        .await
        .unwrap();

    // Set up network contributor data.

    let rewards_manager_signer = Keypair::new();

    let rewards_data = [
        RewardShare::new(Pubkey::new_unique(), 400_000_000, false, 0).unwrap(), // 40.0%
        RewardShare::new(Pubkey::new_unique(), 250_000_000, false, 0).unwrap(), // 25.0%
        RewardShare::new(Pubkey::new_unique(), 100_000_000, false, 0).unwrap(), // 10.0%
        RewardShare::new(Pubkey::new_unique(), 50_000_000, false, 0).unwrap(),  // 5.0%
        RewardShare::new(Pubkey::new_unique(), 50_000_000, false, 0).unwrap(),  // 5.0%
        RewardShare::new(Pubkey::new_unique(), 50_000_000, false, 0).unwrap(),  // 5.0%
        RewardShare::new(Pubkey::new_unique(), 40_000_000, false, 0).unwrap(),  // 4.0%
        RewardShare::new(Pubkey::new_unique(), 30_000_000, false, 0).unwrap(),  // 3.0%
        RewardShare::new(Pubkey::new_unique(), 20_000_000, false, 0).unwrap(),  // 2.0%
        RewardShare::new(Pubkey::new_unique(), 5_000_000, false, 0).unwrap(),   // 0.5%
        RewardShare::new(Pubkey::new_unique(), 3_000_000, false, 0).unwrap(),   // 0.3%
        RewardShare::new(Pubkey::new_unique(), 2_000_000, false, 0).unwrap(),   // 0.2%
    ];
    assert_eq!(
        rewards_data.iter().map(|r| r.unit_share).sum::<u32>(),
        1_000_000_000
    );

    let total_contributors = rewards_data.len() as u32;
    let rewards_merkle_root =
        merkle_root_from_indexed_pod_leaves(&rewards_data, Some(RewardShare::LEAF_PREFIX)).unwrap();

    // Cache computed recipient proportions to check 2Z token distribution later
    // in the test.
    let mut recipient_shares = HashMap::new();

    for (
        i,
        RewardShare {
            contributor_key, ..
        },
    ) in rewards_data.iter().enumerate()
    {
        let recipients_with_abs_shares = (0..(i + 1).min(8))
            .map(|j| (j as u16 + 1, Pubkey::new_unique()))
            .collect::<Vec<_>>();

        // Attempt to normalize the shares to 10,000.
        let sum_shares = recipients_with_abs_shares
            .iter()
            .map(|(share, _)| share)
            .copied()
            .map(u32::from)
            .sum::<u32>();

        let mut recipients = recipients_with_abs_shares
            .iter()
            .copied()
            .map(|(share, recipient)| (recipient, (u32::from(share) * 10_000 / sum_shares) as u16))
            .collect::<Vec<_>>();

        // Adjust the first element to ensure the sum of shares equals 10,000.
        recipients[0].1 += 10_000 - recipients.iter().map(|(_, share)| share).sum::<u16>();
        assert_eq!(
            recipients.iter().map(|(_, share)| share).sum::<u16>(),
            10_000
        );

        recipient_shares.insert(contributor_key, recipients.clone());

        let recipient_keys = recipients
            .iter()
            .map(|(recipient, _)| recipient)
            .collect::<Vec<_>>();

        for recipient_key in recipient_keys.iter() {
            test_setup.create_2z_ata(recipient_key).await.unwrap();
        }

        test_setup
            .initialize_contributor_rewards(contributor_key)
            .await
            .unwrap()
            .set_rewards_manager(
                contributor_key,
                &contributor_manager_signer,
                &rewards_manager_signer.pubkey(),
            )
            .await
            .unwrap()
            .configure_contributor_rewards(
                contributor_key,
                &rewards_manager_signer,
                [ContributorRewardsConfiguration::Recipients(recipients)],
            )
            .await
            .unwrap();
    }

    // Post rewards merkle root and verify each reward share. Calling the verify
    // instruction doesn't actually do anything in this test, but it is
    // something the offchain process should parse the logs for to make sure
    // everything checks out.
    //
    // Finalize the rewards root immediately after.

    let proofs = rewards_data
        .iter()
        .enumerate()
        .map(|(i, _)| {
            MerkleProof::from_indexed_pod_leaves(
                &rewards_data,
                i.try_into().unwrap(),
                Some(RewardShare::LEAF_PREFIX),
            )
            .unwrap()
        })
        .collect::<Vec<_>>();

    test_setup
        .configure_distribution_rewards(
            dz_epoch,
            &rewards_accountant_signer,
            total_contributors,
            rewards_merkle_root,
        )
        .await
        .unwrap()
        .configure_distribution_rewards(
            next_dz_epoch,
            &rewards_accountant_signer,
            total_contributors,
            rewards_merkle_root,
        )
        .await
        .unwrap();

    let kinds_and_proofs = rewards_data
        .iter()
        .copied()
        .zip(proofs.iter())
        .map(|(reward_share, proof)| {
            let kind = DistributionMerkleRootKind::RewardShare(reward_share);

            (kind, proof.clone())
        })
        .collect::<Vec<_>>();

    test_setup
        .verify_distribution_merkle_root(dz_epoch, kinds_and_proofs.clone())
        .await
        .unwrap()
        .finalize_distribution_rewards(dz_epoch)
        .await
        .unwrap()
        .verify_distribution_merkle_root(next_dz_epoch, kinds_and_proofs)
        .await
        .unwrap()
        .finalize_distribution_rewards(next_dz_epoch)
        .await
        .unwrap()
        .sweep_distribution_tokens(dz_epoch)
        .await
        .unwrap()
        .sweep_distribution_tokens(next_dz_epoch)
        .await
        .unwrap();

    let mut first_epoch_processed_rewards_count = 0;
    for (share, proof) in rewards_data.iter().copied().zip(proofs.iter()) {
        first_epoch_processed_rewards_count += 1;

        let contributor_key = &share.contributor_key;
        let recipient_keys = recipient_shares[contributor_key]
            .iter()
            .map(|(key, _)| key)
            .collect::<Vec<_>>();

        let relayer_key = Pubkey::new_unique();

        // Distribute for the first epoch.
        test_setup
            .distribute_rewards(
                dz_epoch,
                &share,
                &DOUBLEZERO_MINT_KEY,
                &relayer_key,
                &recipient_keys,
                proof.clone(),
            )
            .await
            .unwrap();

        let relayer_balance = test_setup
            .context
            .banks_client
            .get_balance(relayer_key)
            .await
            .unwrap();
        assert_eq!(relayer_balance, distribute_rewards_relay_lamports as u64);

        // Cannot distribute rewards again for the same contributor.
        let distribute_rewards_ix = try_build_instruction(
            &ID,
            DistributeRewardsAccounts::new(
                dz_epoch,
                &share.contributor_key,
                &DOUBLEZERO_MINT_KEY,
                &relayer_key,
                &recipient_keys,
            ),
            &RevenueDistributionInstructionData::DistributeRewards {
                unit_share: share.unit_share,
                economic_burn_rate: share.economic_burn_rate(),
                proof: proof.clone(),
            },
        )
        .unwrap();

        let (tx_err, program_logs) = test_setup
            .unwrap_simulation_error(&[distribute_rewards_ix], &[])
            .await;
        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
        );
        if first_epoch_processed_rewards_count == rewards_data.len() {
            assert_eq!(
                program_logs.get(3).unwrap(),
                "Program log: All rewards have already been distributed"
            );
        } else {
            assert_eq!(
                program_logs.get(3).unwrap(),
                &format!(
                    "Program log: Merkle leaf index {} has already been processed",
                    proof.leaf_index.unwrap()
                )
            );
        }

        // Distribute for the second epoch.
        test_setup
            .distribute_rewards(
                next_dz_epoch,
                &share,
                &DOUBLEZERO_MINT_KEY,
                &relayer_key,
                &recipient_keys,
                proof.clone(),
            )
            .await
            .unwrap();

        let relayer_balance = test_setup
            .context
            .banks_client
            .get_balance(relayer_key)
            .await
            .unwrap();
        assert_eq!(
            relayer_balance,
            2 * distribute_rewards_relay_lamports as u64
        );
    }

    // Check the first distribution.

    let (
        distribution_key,
        distribution,
        remaining_distribution_data,
        distribution_lamports,
        distribution_2z_token_pda,
    ) = test_setup.fetch_distribution(dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_is_debt_calculation_finalized(true);
    expected_distribution.set_is_rewards_calculation_finalized(true);
    expected_distribution.set_has_swept_2z_tokens(true);
    expected_distribution.set_is_solana_validator_debt_write_off_enabled(true);
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
    expected_distribution.solana_validator_payments_count = total_solana_validators - 1;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.collected_solana_validator_payments =
        total_solana_validator_debt - uncollectible_debt.amount;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.collected_2z_converted_from_sol = expected_swept_2z_amount_1;
    expected_distribution.total_contributors = total_contributors;
    expected_distribution.rewards_merkle_root = rewards_merkle_root;
    expected_distribution.distributed_rewards_count = total_contributors;
    expected_distribution.distributed_2z_amount = 6_210_000_000;
    expected_distribution.burned_2z_amount = 690_000_000;
    expected_distribution.processed_solana_validator_debt_end_index = total_solana_validators / 8;
    expected_distribution.processed_solana_validator_debt_write_off_start_index =
        total_solana_validators / 8;
    expected_distribution.processed_solana_validator_debt_write_off_end_index =
        2 * (total_solana_validators / 8);
    expected_distribution.processed_rewards_start_index = 2 * (total_solana_validators / 8);
    expected_distribution.processed_rewards_end_index =
        2 * (total_solana_validators / 8) + (total_contributors / 8 + 1);
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp = test_setup
        .get_clock()
        .await
        .unix_timestamp
        .saturating_sub(60) as u32;
    expected_distribution.solana_validator_write_off_count = 1;
    assert_eq!(distribution, expected_distribution);
    assert_eq!(
        distribution.distributed_2z_amount + distribution.burned_2z_amount,
        expected_swept_2z_amount_1
    );

    // First byte reflects debt tracking.
    let processed_debt_bitmap =
        &remaining_distribution_data[distribution.processed_solana_validator_debt_bitmap_range()];
    assert_eq!(processed_debt_bitmap, [0b11111111]);

    // Second byte reflects write off tracking.
    let write_off_bitmap = &remaining_distribution_data
        [distribution.processed_solana_validator_debt_write_off_bitmap_range()];
    assert_eq!(write_off_bitmap, [0b00000100]);

    // Third and fourth bytes reflect rewards tracking.
    let rewards_bitmap =
        &remaining_distribution_data[distribution.processed_rewards_bitmap_range()];
    assert_eq!(rewards_bitmap, [0b11111111, 0b1111]);

    // All relay lamports should have been paid, leaving only the rent exemption
    // as the remaining balance in the distribution account.
    let distribution_rent_exemption = test_setup
        .context
        .banks_client
        .get_rent()
        .await
        .unwrap()
        .minimum_balance(zero_copy::data_end::<Distribution>() + remaining_distribution_data.len());
    assert_eq!(distribution_lamports, distribution_rent_exemption);

    // All tokens should have been transferred to all recipients.
    assert_eq!(distribution_2z_token_pda.amount, 0);

    // Check the second distribution.

    let (
        distribution_key,
        distribution,
        remaining_distribution_data,
        distribution_lamports,
        distribution_2z_token_pda,
    ) = test_setup.fetch_distribution(next_dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_is_debt_calculation_finalized(true);
    expected_distribution.set_is_rewards_calculation_finalized(true);
    expected_distribution.set_has_swept_2z_tokens(true);
    expected_distribution.bump_seed = Distribution::find_address(next_dz_epoch).1;
    expected_distribution.token_2z_pda_bump_seed =
        state::find_2z_token_pda_address(&distribution_key).1;
    expected_distribution.dz_epoch = next_dz_epoch;
    expected_distribution.community_burn_rate = BurnRate::new(initial_cbr).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(solana_validator_base_block_rewards_pct_fee).unwrap();
    expected_distribution.total_solana_validators = total_solana_validators;
    expected_distribution.solana_validator_payments_count = total_solana_validators;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.collected_solana_validator_payments = total_solana_validator_debt;
    expected_distribution.uncollectible_sol_debt = uncollectible_debt.amount;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.collected_2z_converted_from_sol = expected_swept_2z_amount_2;
    expected_distribution.total_contributors = total_contributors;
    expected_distribution.rewards_merkle_root = rewards_merkle_root;
    expected_distribution.distributed_rewards_count = total_contributors;
    expected_distribution.distributed_2z_amount = 37_800_000_000;
    expected_distribution.burned_2z_amount = 4_200_000_000;
    expected_distribution.processed_solana_validator_debt_end_index = total_solana_validators / 8;
    expected_distribution.processed_rewards_start_index = total_solana_validators / 8;
    expected_distribution.processed_rewards_end_index =
        (total_solana_validators / 8) + (total_contributors / 8 + 1);
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;
    assert_eq!(distribution, expected_distribution);
    assert_eq!(
        distribution.distributed_2z_amount + distribution.burned_2z_amount,
        expected_swept_2z_amount_2
    );

    // First byte reflects debt tracking. Second and third bytes reflect rewards
    // tracking.
    assert_eq!(
        remaining_distribution_data,
        vec![0b11111111, 0b11111111, 0b1111]
    );

    // All relay lamports should have been paid, leaving only the rent exemption
    // as the remaining balance in the distribution account.
    let distribution_rent_exemption = test_setup
        .context
        .banks_client
        .get_rent()
        .await
        .unwrap()
        .minimum_balance(zero_copy::data_end::<Distribution>() + remaining_distribution_data.len());
    assert_eq!(distribution_lamports, distribution_rent_exemption);

    // All tokens should have been transferred to all recipients.
    assert_eq!(distribution_2z_token_pda.amount, 0);

    // Cannot distribute rewards again.
    for (share, proof) in rewards_data.iter().copied().zip(proofs) {
        let contributor_key = &share.contributor_key;
        let recipient_keys = recipient_shares[contributor_key]
            .iter()
            .map(|(key, _)| key)
            .collect::<Vec<_>>();

        let relayer_key = Pubkey::new_unique();

        // Attempt to distribute rewards again for the first epoch.
        let distribute_rewards_ix = try_build_instruction(
            &ID,
            DistributeRewardsAccounts::new(
                dz_epoch,
                &share.contributor_key,
                &DOUBLEZERO_MINT_KEY,
                &relayer_key,
                &recipient_keys,
            ),
            &RevenueDistributionInstructionData::DistributeRewards {
                unit_share: share.unit_share,
                economic_burn_rate: share.economic_burn_rate(),
                proof: proof.clone(),
            },
        )
        .unwrap();

        let (tx_err, program_logs) = test_setup
            .unwrap_simulation_error(&[distribute_rewards_ix], &[])
            .await;
        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
        );
        assert_eq!(
            program_logs.get(3).unwrap(),
            "Program log: All rewards have already been distributed"
        );

        // Attempt to distribute rewards again for the second epoch.
        let distribute_rewards_ix = try_build_instruction(
            &ID,
            DistributeRewardsAccounts::new(
                next_dz_epoch,
                &share.contributor_key,
                &DOUBLEZERO_MINT_KEY,
                &relayer_key,
                &recipient_keys,
            ),
            &RevenueDistributionInstructionData::DistributeRewards {
                unit_share: share.unit_share,
                economic_burn_rate: share.economic_burn_rate(),
                proof: proof.clone(),
            },
        )
        .unwrap();

        let (tx_err, program_logs) = test_setup
            .unwrap_simulation_error(&[distribute_rewards_ix], &[])
            .await;
        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
        );
        assert_eq!(
            program_logs.get(3).unwrap(),
            "Program log: All rewards have already been distributed"
        );
    }
}
