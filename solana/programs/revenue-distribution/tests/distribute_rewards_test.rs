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
    state::{self, Distribution, Journal, SolanaValidatorDeposit},
    types::{BurnRate, DoubleZeroEpoch, RewardShare, SolanaValidatorDebt, ValidatorFee},
    DOUBLEZERO_MINT_KEY, ID,
};
use solana_program_test::{tokio, BanksClientError};
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use spl_associated_token_account_interface::address::get_associated_token_address;
use svm_hash::merkle::{merkle_root_from_indexed_pod_leaves, MerkleProof};

//
// Constants (round numbers to avoid rounding issues).
//

const INITIAL_CBR: u32 = 100_000_000; // 10%.
const CBR_LIMIT: u32 = 500_000_000; // 50%.
const SOLANA_VALIDATOR_BASE_BLOCK_REWARDS_PCT_FEE: u16 = 500; // 5%.
const DISTRIBUTE_REWARDS_RELAY_LAMPORTS: u32 = 128 * 6_960;
const DIRECT_2Z_PAYMENT_AMOUNT: u64 = 1_000 * 100_000_000; // 1,000 2Z.
const SWEPT_2Z_AMOUNT_1: u64 = 9_000 * 100_000_000; // 9,000 2Z (for dz_epoch).
const SWEPT_2Z_AMOUNT_2: u64 = 5_000 * 100_000_000; // 5,000 2Z (for next_dz_epoch).

// dz_epoch total pool: SWEPT_2Z_AMOUNT_1 + DIRECT_2Z = 10,000 2Z = 1_000_000_000_000.
//   With 10% CBR: burned = 100_000_000_000, distributed = 900_000_000_000.
//   With 25% economic burn: burned = 250_000_000_000, distributed = 750_000_000_000.
// next_dz_epoch total pool: SWEPT_2Z_AMOUNT_2 = 5,000 2Z = 500_000_000_000.
//   With 10% CBR: burned = 50_000_000_000, distributed = 450_000_000_000.

//
// Setup — Layer 1: Distributions with debt paid and tokens swept.
//

struct DistributeRewardsBaseSetup {
    test_setup: common::ProgramTestWithOwner,
    contributor_manager_signer: Keypair,
    debt_accountant_signer: Keypair,
    rewards_accountant_signer: Keypair,
    total_solana_validators: u32,
    total_solana_validator_debt: u64,
    solana_validator_debt_merkle_root: svm_hash::sha2::Hash,
    uncollectible_debt: SolanaValidatorDebt,
    dz_epoch: DoubleZeroEpoch,
    next_dz_epoch: DoubleZeroEpoch,
}

/// Set up a fully configured program with:
/// - Two distributions (dz_epoch=1, next_dz_epoch=2)
/// - Debt configured, finalized, and paid (with one uncollectible validator)
/// - Write-offs processed for the uncollectible validator
/// - SOL swaps completed
/// - Direct 2Z payments funded to journal ATA
/// - Distribution 0 finalized and swept (prerequisite)
///
/// Stops BEFORE contributor rewards setup and rewards finalization.
async fn setup_distributions_with_debt() -> DistributeRewardsBaseSetup {
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

    let dz_epoch = DoubleZeroEpoch::new(1);
    let next_dz_epoch = dz_epoch.saturating_add_duration(1);

    // Debt data: 8 validators with round amounts.
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

    let uncollectible_index = 2;
    let uncollectible_debt = debt_data[uncollectible_index];

    let (journal_key, _) = Journal::find_address();
    let journal_ata_key = get_associated_token_address(&journal_key, &DOUBLEZERO_MINT_KEY);

    // Configure program and initialize distributions.
    test_setup
        .transfer_2z(
            &src_token_account_key,
            SWEPT_2Z_AMOUNT_1 + SWEPT_2Z_AMOUNT_2,
        )
        .await
        .unwrap()
        .initialize_program()
        .await
        .unwrap()
        .initialize_journal()
        .await
        .unwrap()
        .create_2z_ata(&journal_key)
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
                    base_block_rewards_pct: SOLANA_VALIDATOR_BASE_BLOCK_REWARDS_PCT_FEE,
                    priority_block_rewards_pct: 0,
                    inflation_rewards_pct: 0,
                    jito_tips_pct: 0,
                    fixed_sol_amount: 0,
                    _unused: Default::default(),
                },
                ProgramConfiguration::CommunityBurnRateParameters {
                    limit: CBR_LIMIT,
                    dz_epochs_to_increasing: 10,
                    dz_epochs_to_limit: 20,
                    initial_rate: Some(INITIAL_CBR),
                },
                ProgramConfiguration::DistributeRewardsRelayLamports(
                    DISTRIBUTE_REWARDS_RELAY_LAMPORTS,
                ),
                ProgramConfiguration::MinimumEpochDurationToFinalizeRewards(1),
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
        // Distribution 0.
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .transfer_2z(&journal_ata_key, DIRECT_2Z_PAYMENT_AMOUNT)
        .await
        .unwrap()
        .warp_timestamp_by(60)
        .await
        .unwrap()
        // Distribution 1 (dz_epoch).
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
        // Distribution 2 (next_dz_epoch).
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

    // Pay debt for all validators. The uncollectible one only pays for
    // next_dz_epoch and gets written off for dz_epoch.
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

    // SOL swaps — one per distribution.
    let sol_destination_key = Pubkey::new_unique();

    test_setup
        .mock_buy_sol(
            &src_token_account_key,
            &transfer_authority_signer,
            &sol_destination_key,
            SWEPT_2Z_AMOUNT_1,
            total_solana_validator_debt,
        )
        .await
        .unwrap()
        .mock_buy_sol(
            &src_token_account_key,
            &transfer_authority_signer,
            &sol_destination_key,
            SWEPT_2Z_AMOUNT_2,
            total_solana_validator_debt - uncollectible_debt.amount,
        )
        .await
        .unwrap();

    DistributeRewardsBaseSetup {
        test_setup,
        contributor_manager_signer,
        debt_accountant_signer,
        rewards_accountant_signer,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
        uncollectible_debt,
        dz_epoch,
        next_dz_epoch,
    }
}

//
// Setup — Layer 2: Contributor rewards configured and rewards merkle root posted.
// Stops BEFORE finalize_distribution_rewards and sweep_distribution_tokens.
//

struct DistributeRewardsReadySetup {
    test_setup: common::ProgramTestWithOwner,
    debt_accountant_signer: Keypair,
    rewards_accountant_signer: Keypair,
    total_solana_validators: u32,
    total_solana_validator_debt: u64,
    solana_validator_debt_merkle_root: svm_hash::sha2::Hash,
    uncollectible_debt: SolanaValidatorDebt,
    dz_epoch: DoubleZeroEpoch,
    next_dz_epoch: DoubleZeroEpoch,
    rewards_data: Vec<RewardShare>,
    proofs: Vec<MerkleProof>,
    total_contributors: u32,
    rewards_merkle_root: svm_hash::sha2::Hash,
    recipient_shares: HashMap<Pubkey, Vec<(Pubkey, u16)>>,
}

/// Build on layer 1: configure contributor rewards with 5 contributors
/// (clean proportions: 40%, 25%, 20%, 10%, 5%), post + verify rewards
/// merkle root for both epochs.
///
/// Stops BEFORE finalize/sweep so the caller can optionally set
/// economic burn rate before finalizing.
async fn setup_ready_to_distribute() -> DistributeRewardsReadySetup {
    let DistributeRewardsBaseSetup {
        mut test_setup,
        contributor_manager_signer,
        debt_accountant_signer,
        rewards_accountant_signer,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
        uncollectible_debt,
        dz_epoch,
        next_dz_epoch,
    } = setup_distributions_with_debt().await;

    // 5 contributors with clean proportions (no rounding issues).
    let rewards_data = vec![
        RewardShare::new(Pubkey::new_unique(), 400_000_000, false, 0).unwrap(), // 40%
        RewardShare::new(Pubkey::new_unique(), 250_000_000, false, 0).unwrap(), // 25%
        RewardShare::new(Pubkey::new_unique(), 200_000_000, false, 0).unwrap(), // 20%
        RewardShare::new(Pubkey::new_unique(), 100_000_000, false, 0).unwrap(), // 10%
        RewardShare::new(Pubkey::new_unique(), 50_000_000, false, 0).unwrap(),  // 5%
    ];
    assert_eq!(
        rewards_data.iter().map(|r| r.unit_share).sum::<u32>(),
        1_000_000_000
    );

    let total_contributors = rewards_data.len() as u32;
    let rewards_merkle_root =
        merkle_root_from_indexed_pod_leaves(&rewards_data, Some(RewardShare::LEAF_PREFIX)).unwrap();

    let rewards_manager_signer = Keypair::new();
    let mut recipient_shares = HashMap::new();

    // Each contributor has a single recipient at 100% share.
    for RewardShare {
        contributor_key, ..
    } in rewards_data.iter()
    {
        let recipient_key = Pubkey::new_unique();
        let recipients = vec![(recipient_key, 10_000)]; // 100%

        recipient_shares.insert(*contributor_key, recipients.clone());

        test_setup
            .create_2z_ata(&recipient_key)
            .await
            .unwrap()
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

    // Build proofs.
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

    // Post rewards merkle root and verify for both epochs.
    let kinds_and_proofs = rewards_data
        .iter()
        .copied()
        .zip(proofs.iter())
        .map(|(reward_share, proof)| {
            (
                DistributionMerkleRootKind::RewardShare(reward_share),
                proof.clone(),
            )
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
        .unwrap()
        .verify_distribution_merkle_root(dz_epoch, kinds_and_proofs.clone())
        .await
        .unwrap()
        .verify_distribution_merkle_root(next_dz_epoch, kinds_and_proofs)
        .await
        .unwrap();

    DistributeRewardsReadySetup {
        test_setup,
        debt_accountant_signer,
        rewards_accountant_signer,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
        uncollectible_debt,
        dz_epoch,
        next_dz_epoch,
        rewards_data,
        proofs,
        total_contributors,
        rewards_merkle_root,
        recipient_shares,
    }
}

//
// Distribute rewards — happy path.
//

#[tokio::test]
async fn test_distribute_rewards() {
    let DistributeRewardsReadySetup {
        mut test_setup,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
        uncollectible_debt,
        dz_epoch,
        next_dz_epoch,
        rewards_data,
        proofs,
        total_contributors,
        rewards_merkle_root,
        recipient_shares,
        ..
    } = setup_ready_to_distribute().await;

    // Finalize and sweep both epochs.
    test_setup
        .finalize_distribution_rewards(dz_epoch)
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

    // Distribute rewards for both epochs.
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
        assert_eq!(relayer_balance, DISTRIBUTE_REWARDS_RELAY_LAMPORTS as u64);

        // Cannot distribute rewards again for the same contributor.
        let (tx_err, program_logs) = simulate_distribute_rewards_revert(
            &mut test_setup,
            dz_epoch,
            &share,
            &relayer_key,
            &recipient_keys,
            proof.clone(),
        )
        .await
        .unwrap();

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
            2 * DISTRIBUTE_REWARDS_RELAY_LAMPORTS as u64
        );
    }

    // Check the first distribution (dz_epoch).
    // Total pool: SWEPT_2Z_AMOUNT_1 + DIRECT_2Z_PAYMENT_AMOUNT = 1_000_000_000_000.
    // CBR 10%: burned = 100_000_000_000, distributed = 900_000_000_000.

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
    expected_distribution.community_burn_rate = BurnRate::new(INITIAL_CBR).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(SOLANA_VALIDATOR_BASE_BLOCK_REWARDS_PCT_FEE).unwrap();
    expected_distribution.total_solana_validators = total_solana_validators;
    expected_distribution.solana_validator_payments_count = total_solana_validators - 1;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.collected_solana_validator_payments =
        total_solana_validator_debt - uncollectible_debt.amount;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.collected_2z_converted_from_sol = SWEPT_2Z_AMOUNT_1;
    expected_distribution.collected_prepaid_2z_payments = DIRECT_2Z_PAYMENT_AMOUNT;
    expected_distribution.total_contributors = total_contributors;
    expected_distribution.rewards_merkle_root = rewards_merkle_root;
    expected_distribution.distributed_rewards_count = total_contributors;
    expected_distribution.distributed_2z_amount = 900_000_000_000;
    expected_distribution.burned_2z_amount = 100_000_000_000;
    expected_distribution.processed_solana_validator_debt_end_index = total_solana_validators / 8;
    expected_distribution.processed_solana_validator_debt_write_off_start_index =
        total_solana_validators / 8;
    expected_distribution.processed_solana_validator_debt_write_off_end_index =
        2 * (total_solana_validators / 8);
    expected_distribution.processed_rewards_start_index = 2 * (total_solana_validators / 8);
    expected_distribution.processed_rewards_end_index =
        2 * (total_solana_validators / 8) + (total_contributors / 8 + 1);
    expected_distribution.distribute_rewards_relay_lamports = DISTRIBUTE_REWARDS_RELAY_LAMPORTS;
    expected_distribution.calculation_allowed_timestamp = test_setup
        .get_clock()
        .await
        .unix_timestamp
        .saturating_sub(60) as u32;
    expected_distribution.solana_validator_write_off_count = 1;
    assert_eq!(distribution, expected_distribution);
    assert_eq!(
        distribution.distributed_2z_amount + distribution.burned_2z_amount,
        SWEPT_2Z_AMOUNT_1 + DIRECT_2Z_PAYMENT_AMOUNT
    );

    // First byte reflects debt tracking.
    let processed_debt_bitmap =
        &remaining_distribution_data[distribution.processed_solana_validator_debt_bitmap_range()];
    assert_eq!(processed_debt_bitmap, [0b11111111]);

    // Second byte reflects write off tracking.
    let write_off_bitmap = &remaining_distribution_data
        [distribution.processed_solana_validator_debt_write_off_bitmap_range()];
    assert_eq!(write_off_bitmap, [0b00000100]);

    // Third byte reflects rewards tracking.
    let rewards_bitmap =
        &remaining_distribution_data[distribution.processed_rewards_bitmap_range()];
    assert_eq!(rewards_bitmap, [0b00011111]);

    // All relay lamports should have been paid, leaving only the rent exemption.
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

    // Verify the journal's ATA was fully drained.
    let journal_ata_key =
        get_associated_token_address(&Journal::find_address().0, &DOUBLEZERO_MINT_KEY);
    let journal_ata_after = test_setup
        .fetch_token_account(&journal_ata_key)
        .await
        .unwrap();
    assert_eq!(journal_ata_after.amount, 0);

    // Check the second distribution (next_dz_epoch).
    // Total pool: SWEPT_2Z_AMOUNT_2 = 5,000 2Z = 500_000_000_000.
    // CBR 10%: burned = 50_000_000_000, distributed = 450_000_000_000.

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
    expected_distribution.community_burn_rate = BurnRate::new(INITIAL_CBR).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(SOLANA_VALIDATOR_BASE_BLOCK_REWARDS_PCT_FEE).unwrap();
    expected_distribution.total_solana_validators = total_solana_validators;
    expected_distribution.solana_validator_payments_count = total_solana_validators;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.collected_solana_validator_payments = total_solana_validator_debt;
    expected_distribution.uncollectible_sol_debt = uncollectible_debt.amount;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.collected_2z_converted_from_sol = SWEPT_2Z_AMOUNT_2;
    expected_distribution.total_contributors = total_contributors;
    expected_distribution.rewards_merkle_root = rewards_merkle_root;
    expected_distribution.distributed_rewards_count = total_contributors;
    expected_distribution.distributed_2z_amount = 450_000_000_000;
    expected_distribution.burned_2z_amount = 50_000_000_000;
    expected_distribution.processed_solana_validator_debt_end_index = total_solana_validators / 8;
    expected_distribution.processed_rewards_start_index = total_solana_validators / 8;
    expected_distribution.processed_rewards_end_index =
        (total_solana_validators / 8) + (total_contributors / 8 + 1);
    expected_distribution.distribute_rewards_relay_lamports = DISTRIBUTE_REWARDS_RELAY_LAMPORTS;
    expected_distribution.calculation_allowed_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;
    assert_eq!(distribution, expected_distribution);
    assert_eq!(
        distribution.distributed_2z_amount + distribution.burned_2z_amount,
        SWEPT_2Z_AMOUNT_2
    );

    // Debt + rewards tracking.
    assert_eq!(remaining_distribution_data, vec![0b11111111, 0b00011111]);

    let distribution_rent_exemption = test_setup
        .context
        .banks_client
        .get_rent()
        .await
        .unwrap()
        .minimum_balance(zero_copy::data_end::<Distribution>() + remaining_distribution_data.len());
    assert_eq!(distribution_lamports, distribution_rent_exemption);

    assert_eq!(distribution_2z_token_pda.amount, 0);

    // Cannot distribute rewards again for either epoch.
    for (share, proof) in rewards_data.iter().copied().zip(proofs.iter()) {
        let contributor_key = &share.contributor_key;
        let recipient_keys = recipient_shares[contributor_key]
            .iter()
            .map(|(key, _)| key)
            .collect::<Vec<_>>();
        let relayer_key = Pubkey::new_unique();

        for epoch in [dz_epoch, next_dz_epoch] {
            let (tx_err, program_logs) = simulate_distribute_rewards_revert(
                &mut test_setup,
                epoch,
                &share,
                &relayer_key,
                &recipient_keys,
                proof.clone(),
            )
            .await
            .unwrap();

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
}

//
// Distribute rewards with economic burn rate.
//
// Verifies that the economic burn rate on a distribution correctly overrides
// the community burn rate when higher. Uses the same full setup (SOL debt +
// swaps + direct 2Z payments) to test the aggregate pool.
//

#[tokio::test]
async fn test_distribute_rewards_with_economic_burn_rate() {
    let DistributeRewardsReadySetup {
        mut test_setup,
        debt_accountant_signer,
        rewards_accountant_signer,
        total_solana_validators,
        total_solana_validator_debt,
        solana_validator_debt_merkle_root,
        uncollectible_debt,
        dz_epoch,
        rewards_data,
        proofs,
        total_contributors,
        rewards_merkle_root,
        recipient_shares,
        ..
    } = setup_ready_to_distribute().await;

    let distribution_economic_burn_rate = 250_000_000; // 25%.

    // Set economic burn rate before finalizing rewards.
    test_setup
        .set_distribution_economic_burn_rate(
            dz_epoch,
            &rewards_accountant_signer,
            distribution_economic_burn_rate,
        )
        .await
        .unwrap();

    // Finalize and sweep only dz_epoch.
    test_setup
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap()
        .finalize_distribution_rewards(dz_epoch)
        .await
        .unwrap()
        .sweep_distribution_tokens(dz_epoch)
        .await
        .unwrap();

    // Distribute rewards.
    for (share, proof) in rewards_data.iter().copied().zip(proofs.iter()) {
        let contributor_key = &share.contributor_key;
        let recipient_keys = recipient_shares[contributor_key]
            .iter()
            .map(|(key, _)| key)
            .collect::<Vec<_>>();
        let relayer_key = Pubkey::new_unique();

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
    }

    // Check the distribution.
    // Total pool: SWEPT_2Z_AMOUNT_1 + DIRECT_2Z_PAYMENT_AMOUNT = 1_000_000_000_000.
    // Economic burn rate 25% (overrides 10% CBR):
    //   burned = 250_000_000_000, distributed = 750_000_000_000.

    let (
        distribution_key,
        distribution,
        _remaining_distribution_data,
        _distribution_lamports,
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
    expected_distribution.community_burn_rate = BurnRate::new(INITIAL_CBR).unwrap();
    expected_distribution.economic_burn_rate =
        BurnRate::new(distribution_economic_burn_rate).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(SOLANA_VALIDATOR_BASE_BLOCK_REWARDS_PCT_FEE).unwrap();
    expected_distribution.total_solana_validators = total_solana_validators;
    expected_distribution.solana_validator_payments_count = total_solana_validators - 1;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.collected_solana_validator_payments =
        total_solana_validator_debt - uncollectible_debt.amount;
    expected_distribution.solana_validator_debt_merkle_root = solana_validator_debt_merkle_root;
    expected_distribution.collected_2z_converted_from_sol = SWEPT_2Z_AMOUNT_1;
    expected_distribution.collected_prepaid_2z_payments = DIRECT_2Z_PAYMENT_AMOUNT;
    expected_distribution.total_contributors = total_contributors;
    expected_distribution.rewards_merkle_root = rewards_merkle_root;
    expected_distribution.distributed_rewards_count = total_contributors;
    expected_distribution.distributed_2z_amount = 750_000_000_000;
    expected_distribution.burned_2z_amount = 250_000_000_000;
    expected_distribution.processed_solana_validator_debt_end_index = total_solana_validators / 8;
    expected_distribution.processed_solana_validator_debt_write_off_start_index =
        total_solana_validators / 8;
    expected_distribution.processed_solana_validator_debt_write_off_end_index =
        2 * (total_solana_validators / 8);
    expected_distribution.processed_rewards_start_index = 2 * (total_solana_validators / 8);
    expected_distribution.processed_rewards_end_index =
        2 * (total_solana_validators / 8) + (total_contributors / 8 + 1);
    expected_distribution.distribute_rewards_relay_lamports = DISTRIBUTE_REWARDS_RELAY_LAMPORTS;
    expected_distribution.calculation_allowed_timestamp = test_setup
        .get_clock()
        .await
        .unix_timestamp
        .saturating_sub(60) as u32;
    expected_distribution.solana_validator_write_off_count = 1;
    assert_eq!(distribution, expected_distribution);
    assert_eq!(
        distribution.distributed_2z_amount + distribution.burned_2z_amount,
        SWEPT_2Z_AMOUNT_1 + DIRECT_2Z_PAYMENT_AMOUNT
    );

    // All tokens should have been transferred to all recipients.
    assert_eq!(distribution_2z_token_pda.amount, 0);
}

//
// Helpers.
//

async fn simulate_distribute_rewards_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    dz_epoch: DoubleZeroEpoch,
    share: &RewardShare,
    relayer_key: &Pubkey,
    recipient_keys: &[&Pubkey],
    proof: MerkleProof,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let distribute_rewards_ix = try_build_instruction(
        &ID,
        DistributeRewardsAccounts::new(
            dz_epoch,
            &share.contributor_key,
            &DOUBLEZERO_MINT_KEY,
            relayer_key,
            recipient_keys,
        ),
        &RevenueDistributionInstructionData::DistributeRewards {
            unit_share: share.unit_share,
            economic_burn_rate: share.economic_burn_rate(),
            proof,
        },
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[distribute_rewards_ix], &[])
        .await
}
