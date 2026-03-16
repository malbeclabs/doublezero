use anyhow::{Result, ensure};
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    payer::{TransactionOutcome, Wallet},
    rpc::DoubleZeroLedgerConnection,
};
use doublezero_solana_sdk::{
    DOUBLEZERO_MINT_DECIMALS, build_memo_instruction, environment_2z_token_mint_key,
    revenue_distribution::{
        ID,
        fetch::try_fetch_distribution,
        instruction::{RevenueDistributionInstructionData, account::DistributeRewardsAccounts},
        state::{ContributorRewards, Distribution},
        try_is_processed_leaf,
        types::{RewardShare, UnitShare32},
    },
    try_build_instruction,
};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};
use spl_associated_token_account_interface::{
    address::get_associated_token_address_and_bump_seed,
    instruction::create_associated_token_account_idempotent,
};
use tracing::{debug, info, warn};

use crate::calculator::{ledger_operations::try_fetch_shapley_output, proof::ShapleyOutputStorage};

const RELAY_MEMO_CU: u32 = 5_000;

/// Outcome of a distribution attempt.
#[derive(Debug)]
pub enum DistributionOutcome {
    /// Finalization or sweep guards not met — will retry.
    NotReady,
    /// All contributors distributed (distributed_rewards_count == total_contributors).
    Complete { total_contributors: u32 },
    /// Some contributors skipped (missing ContributorRewards accounts onchain).
    /// No further progress possible without contributor action.
    PartiallyComplete {
        total_contributors: u32,
        distributed: u32,
        skipped: u32,
    },
}

/// Per-contributor result from a distribution attempt.
#[derive(Debug)]
pub struct ContributorDistributionResult {
    pub index: usize,
    pub contributor_key: Pubkey,
    /// unit_share / UnitShare32::MAX, range 0.0–1.0
    pub proportion: f64,
    /// Human-readable 2Z amount (already divided by decimals)
    pub reward_tokens: f64,
    /// Whether this leaf is processed (includes both pre-existing and newly distributed)
    pub distributed: bool,
}

/// Full summary of a distribution attempt.
#[derive(Debug)]
pub struct DistributionSummary {
    pub dz_epoch: u64,
    pub outcome: DistributionOutcome,
    pub contributors: Vec<ContributorDistributionResult>,
}

/// Attempt to distribute rewards for the current eligible epoch.
pub async fn try_distribute_epoch_rewards(
    wallet: &Wallet,
    dz_connection: &DoubleZeroLedgerConnection,
    rewards_accountant_key: &Pubkey,
    dz_epoch_value: u64,
    shapley_prefix: &[u8],
) -> Result<DistributionSummary> {
    // Fetch the distribution for this epoch.
    let (_, distribution) = try_fetch_distribution(&wallet.connection, dz_epoch_value).await?;

    // Check readiness: rewards must be finalized and tokens swept.
    if !distribution.is_rewards_calculation_finalized() {
        debug!(
            "Distribution for epoch {} is not finalized yet, skipping",
            dz_epoch_value
        );
        return Ok(DistributionSummary {
            dz_epoch: dz_epoch_value,
            outcome: DistributionOutcome::NotReady,
            contributors: vec![],
        });
    }

    if !distribution.has_swept_2z_tokens() {
        debug!(
            "Distribution for epoch {} has not swept 2Z tokens yet, skipping",
            dz_epoch_value
        );
        return Ok(DistributionSummary {
            dz_epoch: dz_epoch_value,
            outcome: DistributionOutcome::NotReady,
            contributors: vec![],
        });
    }

    let total_contributors = distribution.total_contributors;

    if distribution.distributed_rewards_count == total_contributors {
        debug!(
            "All {} contributors already distributed for epoch {}, fetching summary",
            total_contributors, dz_epoch_value
        );
    } else {
        info!(
            "Distributing rewards for epoch {}: {}/{} already distributed onchain",
            dz_epoch_value, distribution.distributed_rewards_count, total_contributors
        );
    }

    let network_env = wallet.connection.try_network_environment().await?;
    let dz_mint_key = environment_2z_token_mint_key(network_env);

    // Fetch shapley output from DoubleZero Ledger.
    let shapley_output = try_fetch_shapley_output(
        dz_connection,
        shapley_prefix,
        rewards_accountant_key,
        dz_epoch_value,
    )
    .await?;

    let mut distributed_count = 0u32;
    let mut skipped_count = 0u32;

    for (leaf_index, reward_share, is_processed) in
        try_distribution_rewards_iter(&distribution, &shapley_output)?
    {
        if is_processed {
            continue;
        }

        info!(
            "Distributing epoch {} leaf {}, contributor: {}",
            dz_epoch_value, leaf_index, reward_share.contributor_key
        );

        let was_distributed = try_distribute_contributor_rewards(
            wallet,
            &dz_mint_key,
            &distribution,
            &shapley_output,
            leaf_index,
            reward_share,
        )
        .await?;

        if was_distributed {
            distributed_count += 1;
        } else {
            skipped_count += 1;
        }
    }

    let outcome = if skipped_count > 0 {
        DistributionOutcome::PartiallyComplete {
            total_contributors,
            distributed: distribution.distributed_rewards_count + distributed_count,
            skipped: skipped_count,
        }
    } else {
        DistributionOutcome::Complete { total_contributors }
    };

    // Build per-contributor summary from in-scope data (no additional RPC calls).
    let collected_rewards = distribution.total_collected_2z_tokens();
    let burnable_rewards = distribution
        .community_burn_rate
        .mul_scalar(collected_rewards);
    let distributable_rewards = collected_rewards - burnable_rewards;
    let decimals_divisor = f64::powi(10.0, DOUBLEZERO_MINT_DECIMALS as i32);

    let contributors = try_distribution_rewards_iter(&distribution, &shapley_output)?
        .map(|(index, reward_share, is_processed)| {
            let proportion = reward_share.unit_share as f64 / u32::from(UnitShare32::MAX) as f64;
            let reward_tokens = reward_share
                .checked_unit_share()
                .unwrap()
                .mul_scalar(distributable_rewards) as f64
                / decimals_divisor;

            ContributorDistributionResult {
                index,
                contributor_key: reward_share.contributor_key,
                proportion,
                reward_tokens,
                distributed: is_processed,
            }
        })
        .collect();

    Ok(DistributionSummary {
        dz_epoch: dz_epoch_value,
        outcome,
        contributors,
    })
}

/// Iterate over distribution rewards, yielding (leaf_index, reward_share, is_processed)
/// for each contributor in the shapley output.
pub fn try_distribution_rewards_iter<'a>(
    distribution: &ZeroCopyAccountOwnedData<Distribution>,
    shapley_output: &'a ShapleyOutputStorage,
) -> Result<impl Iterator<Item = (usize, &'a RewardShare, bool)>> {
    let start_index = distribution.processed_rewards_start_index as usize;
    let end_index = distribution.processed_rewards_end_index as usize;
    let processed_leaf_data = &distribution.remaining_data[start_index..end_index];

    let num_rewards = shapley_output.rewards.len();
    let max_supported_rewards = processed_leaf_data.len() * 8;

    ensure!(
        max_supported_rewards >= num_rewards,
        "Insufficient processed leaf data for epoch {}: can support {max_supported_rewards} rewards, but got {num_rewards}",
        distribution.dz_epoch
    );

    Ok(shapley_output
        .rewards
        .iter()
        .enumerate()
        .map(|(index, reward_share)| {
            let is_processed = try_is_processed_leaf(processed_leaf_data, index).unwrap();
            (index, reward_share, is_processed)
        }))
}

async fn try_distribute_contributor_rewards(
    wallet: &Wallet,
    dz_mint_key: &Pubkey,
    distribution: &Distribution,
    shapley_output: &ShapleyOutputStorage,
    leaf_index: usize,
    reward_share: &RewardShare,
) -> Result<bool> {
    const DISTRIBUTE_REWARDS_CU_BASE: u32 = 30_000;
    const CREATE_ATA_CU_BASE: u32 = 25_000;
    const PER_RECIPIENT_CU: u32 = 12_500;

    let wallet_key = wallet.pubkey();

    let (contributor_rewards_key, _) =
        ContributorRewards::find_address(&reward_share.contributor_key);

    // Fetch contributor reward recipients.
    let recipient_shares = match wallet
        .connection
        .try_fetch_zero_copy_data::<ContributorRewards>(&contributor_rewards_key)
        .await
    {
        Ok(contributor_rewards) => {
            let recipient_shares = contributor_rewards
                .recipient_shares
                .active_iter()
                .copied()
                .collect::<Vec<_>>();

            if recipient_shares.is_empty() {
                warn!(
                    "No recipients in {contributor_rewards_key} for contributor {}",
                    reward_share.contributor_key
                );

                return Ok(false);
            }

            recipient_shares
        }
        _ => {
            warn!(
                "Contributor rewards {contributor_rewards_key} not found for contributor {}",
                reward_share.contributor_key
            );

            return Ok(false);
        }
    };

    let recipient_keys = recipient_shares
        .iter()
        .map(|share| &share.recipient_key)
        .collect::<Vec<_>>();

    let distribute_rewards_ix = try_build_instruction(
        &ID,
        DistributeRewardsAccounts::new(
            distribution.dz_epoch,
            &reward_share.contributor_key,
            dz_mint_key,
            &wallet_key,
            &recipient_keys,
        ),
        &RevenueDistributionInstructionData::DistributeRewards {
            unit_share: reward_share.unit_share,
            economic_burn_rate: reward_share.economic_burn_rate(),
            proof: shapley_output.generate_merkle_proof(leaf_index)?,
        },
    )?;

    // Derive ATA keys and bumps. We will need these bumps to set the CU
    // precisely.
    let (ata_keys, ata_bumps) = recipient_keys
        .iter()
        .map(|recipient_key| {
            get_associated_token_address_and_bump_seed(
                recipient_key,
                dz_mint_key,
                &spl_associated_token_account_interface::program::ID,
                &spl_token_interface::ID,
            )
        })
        .unzip::<_, _, Vec<_>, Vec<_>>();

    // Build instructions to create missing ATAs. We are using idempotent just
    // in case there is a race when creating the ATAs.
    let (mut instructions, create_ata_compute_units) = wallet
        .connection
        .get_multiple_accounts(&ata_keys)
        .await?
        .into_iter()
        .zip(recipient_keys.iter())
        .zip(ata_bumps)
        .filter_map(|((account_info, recipient_key), bump)| match account_info {
            Some(account_info) if account_info.owner == Pubkey::default() => {
                Some((recipient_key, bump))
            }
            None => Some((recipient_key, bump)),
            _ => None,
        })
        .map(|(recipient_key, bump)| {
            let ix = create_associated_token_account_idempotent(
                &wallet_key,
                recipient_key,
                dz_mint_key,
                &spl_token_interface::ID,
            );

            let compute_unit_limit = CREATE_ATA_CU_BASE + Wallet::compute_units_for_bump_seed(bump);

            (ix, compute_unit_limit)
        })
        .unzip::<_, _, Vec<_>, Vec<_>>();

    if !instructions.is_empty() {
        warn!("Creating {} ATAs", instructions.len());
    }

    instructions.push(distribute_rewards_ix);

    // Add simple memo to indicate that distributing rewards was relayed.
    instructions.push(build_memo_instruction(b"Relay"));

    let compute_unit_limit = DISTRIBUTE_REWARDS_CU_BASE
        + recipient_keys.len() as u32 * PER_RECIPIENT_CU
        + create_ata_compute_units.iter().sum::<u32>()
        + RELAY_MEMO_CU;

    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        compute_unit_limit,
    ));

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

    if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
        info!(
            "Distribute rewards for epoch {}: {tx_sig}",
            distribution.dz_epoch
        );

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(true)
}

#[cfg(test)]
mod tests {
    use doublezero_revenue_distribution::{state::Distribution, types::DoubleZeroEpoch};
    use doublezero_solana_client_tools::account::zero_copy::ZeroCopyAccountOwnedData;
    use network_shapley::shapley::{ShapleyOutput, ShapleyValue};
    use solana_sdk::pubkey::Pubkey;

    use super::try_distribution_rewards_iter;
    use crate::calculator::proof::ShapleyOutputStorage;

    fn create_test_shapley_output(num_contributors: usize) -> ShapleyOutput {
        let mut output = ShapleyOutput::new();
        for i in 0..num_contributors {
            let mut bytes = [0u8; 32];
            bytes[0] = (i + 1) as u8;
            bytes[1] = ((i + 1) >> 8) as u8;
            let pubkey = Pubkey::new_from_array(bytes);
            output.insert(
                pubkey.to_string(),
                ShapleyValue {
                    value: 100.0,
                    proportion: 1.0 / num_contributors as f64,
                },
            );
        }
        output
    }

    fn make_distribution(
        epoch: u64,
        bitmap: Vec<u8>,
        total_contributors: u32,
    ) -> ZeroCopyAccountOwnedData<Distribution> {
        let mut dist = Distribution::default();
        dist.dz_epoch = DoubleZeroEpoch::new(epoch);
        dist.processed_rewards_start_index = 0;
        dist.processed_rewards_end_index = bitmap.len() as u32;
        dist.total_contributors = total_contributors;

        ZeroCopyAccountOwnedData {
            mucked_data: Box::new(dist),
            remaining_data: bitmap,
        }
    }

    #[test]
    fn test_distribution_rewards_iter_all_unprocessed() {
        let shapley = create_test_shapley_output(3);
        let shapley_storage = ShapleyOutputStorage::new(42, &shapley).unwrap();

        // 1 byte = 8 bits, all zeros = all unprocessed
        let distribution = make_distribution(42, vec![0x00], 3);

        let results: Vec<_> = try_distribution_rewards_iter(&distribution, &shapley_storage)
            .unwrap()
            .collect();

        assert_eq!(results.len(), 3);
        for (index, _reward, is_processed) in &results {
            assert!(!is_processed, "leaf {} should be unprocessed", index);
        }
    }

    #[test]
    fn test_distribution_rewards_iter_all_processed() {
        let shapley = create_test_shapley_output(3);
        let shapley_storage = ShapleyOutputStorage::new(42, &shapley).unwrap();

        // 0xFF = all bits set = all processed
        let distribution = make_distribution(42, vec![0xFF], 3);

        let results: Vec<_> = try_distribution_rewards_iter(&distribution, &shapley_storage)
            .unwrap()
            .collect();

        assert_eq!(results.len(), 3);
        for (index, _reward, is_processed) in &results {
            assert!(is_processed, "leaf {} should be processed", index);
        }
    }

    #[test]
    fn test_distribution_rewards_iter_partial() {
        let shapley = create_test_shapley_output(3);
        let shapley_storage = ShapleyOutputStorage::new(42, &shapley).unwrap();

        // 0b00000101 = bits 0 and 2 set (leaves 0 and 2 processed, leaf 1 unprocessed)
        let distribution = make_distribution(42, vec![0b0000_0101], 3);

        let results: Vec<_> = try_distribution_rewards_iter(&distribution, &shapley_storage)
            .unwrap()
            .collect();

        assert_eq!(results.len(), 3);
        assert!(results[0].2, "leaf 0 should be processed");
        assert!(!results[1].2, "leaf 1 should be unprocessed");
        assert!(results[2].2, "leaf 2 should be processed");
    }

    #[test]
    fn test_distribution_rewards_iter_insufficient_bitmap() {
        let shapley = create_test_shapley_output(10);
        let shapley_storage = ShapleyOutputStorage::new(42, &shapley).unwrap();

        // Only 1 byte = 8 bits, but 10 contributors need at least 2 bytes
        let distribution = make_distribution(42, vec![0x00], 10);

        let result = try_distribution_rewards_iter(&distribution, &shapley_storage);
        let err = result.err().expect("should be an error");
        assert!(
            err.to_string().contains("Insufficient"),
            "error should mention insufficient bitmap"
        );
    }

    #[test]
    fn test_outcome_all_processed_maps_to_already_complete() {
        let shapley = create_test_shapley_output(3);
        let storage = ShapleyOutputStorage::new(42, &shapley).unwrap();
        let distribution = make_distribution(42, vec![0xFF], 3);

        let unprocessed_count = try_distribution_rewards_iter(&distribution, &storage)
            .unwrap()
            .filter(|(_, _, is_processed)| !is_processed)
            .count();

        assert_eq!(unprocessed_count, 0);
    }

    #[test]
    fn test_distribution_rewards_iter_empty_rewards() {
        let shapley_storage = ShapleyOutputStorage {
            epoch: 42,
            rewards: vec![],
            total_unit_shares: 0,
        };

        let distribution = make_distribution(42, vec![], 0);

        let results: Vec<_> = try_distribution_rewards_iter(&distribution, &shapley_storage)
            .unwrap()
            .collect();

        assert!(results.is_empty());
    }
}
