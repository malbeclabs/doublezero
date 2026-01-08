use anyhow::{Context, Result, ensure};
use clap::Args;
use doublezero_contributor_rewards::calculator::proof::ShapleyOutputStorage;
use doublezero_scheduled_command::{Schedulable, ScheduleOption};
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    payer::{SolanaPayerOptions, TransactionOutcome, Wallet},
    rpc::{DoubleZeroLedgerConnection, DoubleZeroLedgerEnvironmentOverride},
};
use doublezero_solana_sdk::{
    build_memo_instruction, environment_2z_token_mint_key,
    revenue_distribution::{
        ID,
        fetch::{try_fetch_config, try_fetch_distribution},
        instruction::{RevenueDistributionInstructionData, account::DistributeRewardsAccounts},
        state::{ContributorRewards, Distribution, ProgramConfig},
        types::{RewardShare, UnitShare32},
    },
    try_build_instruction,
};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};
use spl_associated_token_account_interface::{
    address::get_associated_token_address_and_bump_seed,
    instruction::create_associated_token_account_idempotent,
};

use crate::command::revenue_distribution::{
    relay::{
        finalize_distribution_rewards::FinalizeDistributionRewardsContext,
        sweep_distribution_tokens::SweepDistributionTokensContext,
    },
    try_distribution_rewards_iter, try_fetch_shapley_record,
};

const RELAY_MEMO_CU: u32 = 5_000;

#[derive(Debug, Args, Clone)]
pub struct DistributeRewards {
    #[arg(long, short = 'e')]
    dz_epoch: Option<u64>,

    #[command(flatten)]
    schedule: ScheduleOption,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,

    #[command(flatten)]
    dz_env: DoubleZeroLedgerEnvironmentOverride,

    #[arg(hide = true, long)]
    rewards_accountant: Option<Pubkey>,
}

#[async_trait::async_trait]
impl Schedulable for DistributeRewards {
    fn schedule(&self) -> &ScheduleOption {
        &self.schedule
    }

    async fn execute_once(&self) -> Result<()> {
        let Self {
            dz_epoch,
            schedule,
            solana_payer_options,
            dz_env,
            rewards_accountant: rewards_accountant_key,
        } = self;

        ensure!(
            !schedule.is_scheduled() || dz_epoch.is_none(),
            "Cannot specify both dz_epoch and schedule"
        );

        let wallet = Wallet::try_from(solana_payer_options.clone())?;

        let (_, config) = try_fetch_config(&wallet.connection).await?;

        let dz_epoch_value = match dz_epoch {
            Some(dz_epoch) => *dz_epoch,
            None => {
                let deferral_period = config
                    .checked_minimum_epoch_duration_to_finalize_rewards()
                    .context("Minimum epoch duration to finalize rewards not set")?;
                config
                    .next_completed_dz_epoch
                    .value()
                    .saturating_sub(deferral_period.into())
            }
        };

        // Make sure the distribution's rewards calculation is finalized and
        // that 2Z tokens have been swept.
        let distribution =
            try_prepare_distribution_rewards(&wallet, &config, dz_epoch_value).await?;

        let network_env = wallet.connection.try_network_environment().await?;
        let dz_mint_key = environment_2z_token_mint_key(network_env);

        let dz_env = dz_env.dz_env.unwrap_or(network_env);
        let dz_connection = DoubleZeroLedgerConnection::from(dz_env);

        let shapley_output = try_fetch_shapley_record(
            &dz_connection,
            &rewards_accountant_key.unwrap_or(config.rewards_accountant_key),
            dz_epoch_value,
        )
        .await?;

        for (leaf_index, reward_share, is_processed_leaf) in
            try_distribution_rewards_iter(&distribution, &shapley_output)?
        {
            tracing::info!(
                "Processing epoch {dz_epoch_value} merkle leaf index {leaf_index}, contributor: {}, share: {:.9}",
                reward_share.contributor_key,
                reward_share.unit_share as f64 / u32::from(UnitShare32::MAX) as f64
            );

            if is_processed_leaf {
                tracing::warn!(
                    "Merkle leaf index {} has already been processed",
                    leaf_index
                );
                continue;
            }

            try_distribute_contributor_rewards(
                &wallet,
                &dz_mint_key,
                &distribution,
                &shapley_output,
                leaf_index,
                reward_share,
            )
            .await?;
        }

        Ok(())
    }
}

//

async fn try_prepare_distribution_rewards(
    wallet: &Wallet,
    config: &ProgramConfig,
    dz_epoch_value: u64,
) -> Result<ZeroCopyAccountOwnedData<Distribution>> {
    // Fetch distribution. If we had to finalize rewards, we will need to fetch
    // again at the end.
    let (_, distribution) = try_fetch_distribution(&wallet.connection, dz_epoch_value).await?;

    let mut instructions = Vec::new();
    let mut compute_unit_limit = 5_000;

    if !distribution.is_rewards_calculation_finalized() {
        let finalize_distribution_tokens_context =
            FinalizeDistributionRewardsContext::try_prepare(wallet, dz_epoch_value)?;

        instructions.push(finalize_distribution_tokens_context.instruction);
        compute_unit_limit += FinalizeDistributionRewardsContext::COMPUTE_UNIT_LIMIT;
    };

    if !distribution.has_swept_2z_tokens() {
        let sweep_distribution_tokens_context =
            SweepDistributionTokensContext::try_prepare(wallet, config, Some(&distribution))
                .await?;

        instructions.push(sweep_distribution_tokens_context.instruction);
        compute_unit_limit += sweep_distribution_tokens_context.compute_unit_limit;
    };

    if instructions.is_empty() {
        tracing::info!(
            "No instructions to prepare distribution rewards for epoch {dz_epoch_value}"
        );

        return Ok(distribution);
    }

    // Add simple memo to indicate that distributing rewards was relayed.
    instructions.push(build_memo_instruction(b"Relay"));
    compute_unit_limit += RELAY_MEMO_CU;

    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        compute_unit_limit,
    ));

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

    if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
        tracing::info!("Prepare distribution rewards for epoch {dz_epoch_value}: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    // Fetch the distribution again to get the remaining data.
    let (_, distribution) = try_fetch_distribution(&wallet.connection, dz_epoch_value).await?;

    Ok(distribution)
}

async fn try_distribute_contributor_rewards(
    wallet: &Wallet,
    dz_mint_key: &Pubkey,
    distribution: &Distribution,
    shapley_output: &ShapleyOutputStorage,
    leaf_index: usize,
    reward_share: &RewardShare,
) -> Result<()> {
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
                tracing::warn!(
                    "No recipients in {contributor_rewards_key} for contributor {}",
                    reward_share.contributor_key
                );

                return Ok(());
            }

            recipient_shares
        }
        _ => {
            tracing::warn!(
                "Contributor rewards {contributor_rewards_key} not found for contributor {}",
                reward_share.contributor_key
            );

            return Ok(());
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
        tracing::warn!("Creating {} ATAs", instructions.len());
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
        tracing::info!(
            "Distribute rewards for epoch {}: {tx_sig}",
            distribution.dz_epoch
        );

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}
