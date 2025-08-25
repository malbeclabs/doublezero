#![allow(unused_imports)]
mod common;

//

use doublezero_revenue_distribution::{
    instruction::{
        DistributionMerkleRootKind, DistributionPaymentsConfiguration, ProgramConfiguration,
        ProgramFlagConfiguration,
    },
    state::{
        self, find_2z_token_pda_address, find_swap_authority_address, Distribution,
        SolanaValidatorDeposit,
    },
    types::{BurnRate, DoubleZeroEpoch, SolanaValidatorPayment, ValidatorFee},
    DOUBLEZERO_MINT_KEY,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::signature::{Keypair, Signer};
use svm_hash::merkle::{merkle_root_from_indexed_pod_leaves, MerkleProof};

//
// Sweep distribution tokens.
//

#[cfg_attr(not(feature = "development"), ignore)]
#[tokio::test]
async fn test_sweep_distribution_tokens() {
    #[cfg(feature = "development")]
    test_sweep_distribution_tokens_development().await;

    #[cfg(not(feature = "development"))]
    test_sweep_distribution_tokens_mainnet().await;
}

#[cfg(feature = "development")]
async fn test_sweep_distribution_tokens_development() {
    use doublezero_revenue_distribution::FIXED_SOL_2Z_SWAP_RATE_FOR_DEVELOPMENT;

    //

    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    let payments_accountant_signer = Keypair::new();
    let rewards_accountant_signer = Keypair::new();
    let solana_validator_base_block_rewards_fee = 500; // 5%.

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Relay settings.
    let contributor_reward_claim_relay_lamports = 10_000;

    // Distribution payments.

    let dz_epoch = DoubleZeroEpoch::new(1);

    let payments_data = (0..8)
        .map(|i| SolanaValidatorPayment {
            node_id: Pubkey::new_unique(),
            amount: 10_000_000_000 * (i + 1),
        })
        .collect::<Vec<_>>();

    let total_solana_validators = payments_data.len() as u32;
    let total_solana_validator_debt = payments_data.iter().map(|payment| payment.amount).sum();
    let solana_validator_payments_merkle_root = merkle_root_from_indexed_pod_leaves(
        &payments_data,
        Some(SolanaValidatorPayment::LEAF_PREFIX),
    )
    .unwrap();

    let swap_authority_key = find_swap_authority_address().0;
    let swap_destination_key = find_2z_token_pda_address(&swap_authority_key).0;

    // Swap destination has more than enough 2Z tokens to cover the SOL debt.
    let swap_destination_balance_before = 42_069 * u64::pow(10, 8);
    let expected_swept_2z_amount =
        total_solana_validator_debt * FIXED_SOL_2Z_SWAP_RATE_FOR_DEVELOPMENT;
    assert!(swap_destination_balance_before >= expected_swept_2z_amount);

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
                ProgramConfiguration::PaymentsAccountant(payments_accountant_signer.pubkey()),
                ProgramConfiguration::RewardsAccountant(rewards_accountant_signer.pubkey()),
                ProgramConfiguration::SolanaValidatorFeeParameters {
                    base_block_rewards: solana_validator_base_block_rewards_fee,
                    priority_block_rewards: 0,
                    inflation_rewards: 0,
                    jito_tips: 0,
                    _unused: [0; 32],
                },
                ProgramConfiguration::CommunityBurnRateParameters {
                    limit: cbr_limit,
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
                },
                ProgramConfiguration::ContributorRewardClaimLamports(
                    contributor_reward_claim_relay_lamports,
                ),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .initialize_distribution(&payments_accountant_signer)
        .await
        .unwrap()
        .initialize_distribution(&payments_accountant_signer)
        .await
        .unwrap()
        .configure_distribution_payments(
            dz_epoch,
            &payments_accountant_signer,
            [
                DistributionPaymentsConfiguration::UpdateSolanaValidatorPayments {
                    total_validators: total_solana_validators,
                    total_debt: total_solana_validator_debt,
                    merkle_root: solana_validator_payments_merkle_root,
                },
            ],
        )
        .await
        .unwrap()
        .finalize_distribution_payments(dz_epoch, &payments_accountant_signer)
        .await
        .unwrap()
        .initialize_swap_destination(&DOUBLEZERO_MINT_KEY)
        .await
        .unwrap()
        .transfer_2z(&swap_destination_key, swap_destination_balance_before)
        .await
        .unwrap();

    // 1. Initialize Solana validator deposit accounts.
    // 2. Transfer amount each validator owes so each can pay its debt.
    // 3. Pay each validator's debt.
    for (i, payment) in payments_data.iter().enumerate() {
        let proof = MerkleProof::from_indexed_pod_leaves(
            &payments_data,
            i.try_into().unwrap(),
            Some(SolanaValidatorPayment::LEAF_PREFIX),
        )
        .unwrap();

        let node_id = &payment.node_id;
        let amount = payment.amount;

        let (deposit_key, _) = SolanaValidatorDeposit::find_address(node_id);

        test_setup
            .initialize_solana_validator_deposit(node_id)
            .await
            .unwrap()
            .transfer_lamports(&deposit_key, amount)
            .await
            .unwrap()
            .pay_solana_validator_debt(dz_epoch, node_id, amount, proof)
            .await
            .unwrap();
    }

    // Test.

    test_setup
        .sweep_distribution_tokens(dz_epoch)
        .await
        .unwrap();

    let (_, journal, _, _) = test_setup.fetch_journal().await;
    assert_eq!(journal.total_sol_balance, 0);

    // Swap destination account should have a balance change reflecting the
    // amount of SOL debt collected.
    let swap_destination_balance_after = test_setup
        .fetch_token_account(&swap_destination_key)
        .await
        .unwrap()
        .amount;
    assert_eq!(
        swap_destination_balance_before - swap_destination_balance_after,
        expected_swept_2z_amount
    );

    let (distribution_key, distribution, remaining_distribution_data, _, distribution_2z_token_pda) =
        test_setup.fetch_distribution(dz_epoch).await;

    assert_eq!(distribution_2z_token_pda.amount, expected_swept_2z_amount);

    let mut expected_distribution = Distribution::default();
    expected_distribution.set_are_payments_finalized(true);
    expected_distribution.set_has_swept_2z_tokens(true);
    expected_distribution.bump_seed = Distribution::find_address(dz_epoch).1;
    expected_distribution.token_2z_pda_bump_seed =
        state::find_2z_token_pda_address(&distribution_key).1;
    expected_distribution.dz_epoch = dz_epoch;
    expected_distribution.community_burn_rate = BurnRate::new(initial_cbr).unwrap();
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards = ValidatorFee::new(solana_validator_base_block_rewards_fee).unwrap();
    expected_distribution.total_solana_validators = total_solana_validators;
    expected_distribution.total_solana_validator_debt = total_solana_validator_debt;
    expected_distribution.collected_solana_validator_payments = total_solana_validator_debt;
    expected_distribution.solana_validator_payments_merkle_root =
        solana_validator_payments_merkle_root;
    expected_distribution.collected_sol_converted_to_2z = expected_swept_2z_amount;
    assert_eq!(distribution, expected_distribution);

    assert_eq!(remaining_distribution_data.len(), 1);
    assert_eq!(remaining_distribution_data, vec![255]);
}

#[cfg(not(feature = "development"))]
async fn test_sweep_distribution_tokens_mainnet() {
    todo!()
}
