use anyhow::{Context, Result, ensure};
use clap::Args;
use doublezero_contributor_rewards::calculator::proof::ShapleyOutputStorage;
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    DOUBLEZERO_MINT_KEY, ID,
    instruction::{RevenueDistributionInstructionData, account::DistributeRewardsAccounts},
    state::{ContributorRewards, Distribution, ProgramConfig},
    types::{DoubleZeroEpoch, RewardShare, UnitShare32},
};
use doublezero_scheduled_command::{Schedulable, ScheduleOption};
use doublezero_solana_client_tools::{
    log_info, log_warn,
    payer::{SolanaPayerOptions, TransactionOutcome, Wallet},
    rpc::{DoubleZeroLedgerConnection, DoubleZeroLedgerConnectionOptions},
    zero_copy::{ZeroCopyAccountOwned, ZeroCopyAccountOwnedData},
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
    try_distribution_rewards_iter, try_fetch_distribution, try_fetch_program_config,
    try_fetch_shapley_record,
};

#[derive(Debug, Args, Clone)]
pub struct DistributeRewards {
    #[arg(long, short = 'e')]
    dz_epoch: Option<u64>,

    #[command(flatten)]
    schedule: ScheduleOption,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,

    #[command(flatten)]
    dz_ledger_connection_options: DoubleZeroLedgerConnectionOptions,

    #[arg(hide = true, long, value_name = "PUBKEY")]
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
            rewards_accountant: rewards_accountant_key,
            solana_payer_options,
            dz_ledger_connection_options,
        } = self;

        ensure!(
            !schedule.is_scheduled() || dz_epoch.is_none(),
            "Cannot specify both dz_epoch and schedule"
        );

        let wallet = Wallet::try_from(solana_payer_options.clone())?;

        let (_, config) = try_fetch_program_config(&wallet.connection).await?;

        let dz_epoch = match dz_epoch {
            Some(dz_epoch) => DoubleZeroEpoch::new(*dz_epoch),
            None => {
                let deferral_period = config
                    .checked_minimum_epoch_duration_to_finalize_rewards()
                    .context("Minimum epoch duration to finalize rewards not set")?;
                let allowed_dz_epoch = config
                    .next_completed_dz_epoch
                    .value()
                    .saturating_sub(deferral_period.into());

                DoubleZeroEpoch::new(allowed_dz_epoch)
            }
        };

        // Make sure the distribution's rewards calculation is finalized and
        // that 2Z tokens have been swept.
        let distribution = try_prepare_distribution_rewards(&wallet, &config, dz_epoch).await?;

        let dz_connection =
            DoubleZeroLedgerConnection::try_from(dz_ledger_connection_options.clone())?;

        let shapley_output = try_fetch_shapley_record(
            &dz_connection,
            &rewards_accountant_key.unwrap_or(config.rewards_accountant_key),
            dz_epoch,
        )
        .await?;

        for (leaf_index, reward_share, is_processed_leaf) in
            try_distribution_rewards_iter(&distribution, &shapley_output)?
        {
            log_info!(
                "Processing epoch {dz_epoch} merkle leaf index {leaf_index}, contributor: {}, share: {:.9}",
                reward_share.contributor_key,
                reward_share.unit_share as f64 / u32::from(UnitShare32::MAX) as f64
            );

            if is_processed_leaf {
                log_warn!(
                    "Merkle leaf index {} has already been processed",
                    leaf_index
                );
                continue;
            }

            try_distribute_contributor_rewards(
                &wallet,
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
    dz_epoch: DoubleZeroEpoch,
) -> Result<ZeroCopyAccountOwnedData<Distribution>> {
    // Fetch distribution. If we had to finalize rewards, we will need to fetch
    // again at the end.
    let (_, distribution) = try_fetch_distribution(&wallet.connection, dz_epoch).await?;

    let mut instructions = Vec::new();
    let mut compute_unit_limit = 5_000;

    if !distribution.is_rewards_calculation_finalized() {
        let finalize_distribution_tokens_context =
            FinalizeDistributionRewardsContext::try_prepare(wallet, distribution.dz_epoch)?;

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
        log_info!("No instructions to prepare distribution rewards for epoch {dz_epoch}");

        return Ok(distribution);
    }

    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        compute_unit_limit,
    ));

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

    if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
        log_info!("Prepare distribution rewards for epoch {dz_epoch}: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    // Fetch the distribution again to get the remaining data.
    let (_, distribution) =
        try_fetch_distribution(&wallet.connection, distribution.dz_epoch).await?;

    Ok(distribution)
}

async fn try_distribute_contributor_rewards(
    wallet: &Wallet,
    distribution: &Distribution,
    shapley_output: &ShapleyOutputStorage,
    leaf_index: usize,
    reward_share: &RewardShare,
) -> Result<()> {
    let wallet_key = wallet.pubkey();

    let (contributor_rewards_key, _) =
        ContributorRewards::find_address(&reward_share.contributor_key);

    // Fetch contributor reward recipients.
    let recipient_shares = match ZeroCopyAccountOwned::<ContributorRewards>::try_from_rpc_client(
        &wallet.connection,
        &contributor_rewards_key,
    )
    .await
    {
        Ok(ZeroCopyAccountOwned {
            data: Some(data), ..
        }) => {
            let recipient_shares = data
                .recipient_shares
                .active_iter()
                .copied()
                .collect::<Vec<_>>();

            if recipient_shares.is_empty() {
                log_warn!(
                    "No recipients in {contributor_rewards_key} for contributor {}",
                    reward_share.contributor_key
                );

                return Ok(());
            }

            recipient_shares
        }
        _ => {
            log_warn!(
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
            &DOUBLEZERO_MINT_KEY,
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
                &DOUBLEZERO_MINT_KEY,
                &spl_associated_token_account_interface::program::ID,
                &spl_token::id(),
            )
        })
        .unzip::<_, _, Vec<_>, Vec<_>>();

    // Build instructions to create missing ATAs. We are using idempotent just
    // in case there is a race when creating the ATAs.
    let (mut instructions, compute_unit_limits): (Vec<_>, Vec<_>) = wallet
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
                &DOUBLEZERO_MINT_KEY,
                &spl_token::id(),
            );

            let compute_unit_limit = 25_000 + Wallet::compute_units_for_bump_seed(bump);

            (ix, compute_unit_limit)
        })
        .unzip();

    if !instructions.is_empty() {
        log_warn!("Creating {} ATAs", instructions.len());
    }

    instructions.push(distribute_rewards_ix);

    let compute_unit_limit =
        30_000 // Base processing.
        + recipient_keys.len() as u32 * 12_500 // Transfer to recipients.
        + compute_unit_limits.iter().sum::<u32>() // ATA creation.
        ;
    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        compute_unit_limit,
    ));

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

    if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
        log_info!(
            "Distribute rewards for epoch {}: {tx_sig}",
            distribution.dz_epoch
        );

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}
