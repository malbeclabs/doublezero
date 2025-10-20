use anyhow::{Result, anyhow, ensure};
use clap::Args;
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    ID,
    instruction::{
        RevenueDistributionInstructionData, account::FinalizeDistributionRewardsAccounts,
    },
    types::DoubleZeroEpoch,
};
use doublezero_scheduled_command::{Schedulable, ScheduleOption};
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, Wallet};
use solana_sdk::compute_budget::ComputeBudgetInstruction;

use crate::command::revenue_distribution::try_fetch_program_config;

#[derive(Debug, Args, Clone)]
pub struct FinalizeDistributionRewards {
    #[arg(long, short = 'e')]
    dz_epoch: Option<u64>,

    #[command(flatten)]
    schedule: ScheduleOption,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

#[async_trait::async_trait]
impl Schedulable for FinalizeDistributionRewards {
    fn schedule(&self) -> &ScheduleOption {
        &self.schedule
    }

    async fn execute_once(&self) -> Result<()> {
        let Self {
            dz_epoch,
            schedule,
            solana_payer_options,
        } = self;

        ensure!(
            !schedule.is_scheduled() || dz_epoch.is_none(),
            "Cannot specify both dz_epoch and schedule"
        );

        let wallet = Wallet::try_from(solana_payer_options.clone())?;
        let wallet_key = wallet.pubkey();

        let dz_epoch = match dz_epoch {
            Some(dz_epoch) => DoubleZeroEpoch::new(*dz_epoch),
            None => {
                let (_, program_config) = try_fetch_program_config(&wallet.connection).await?;
                let deferral_period = program_config
                    .checked_minimum_epoch_duration_to_finalize_rewards()
                    .ok_or(anyhow!(
                        "Minimum epoch duration to finalize rewards not set"
                    ))?;
                let allowed_dz_epoch = program_config
                    .next_completed_dz_epoch
                    .value()
                    .saturating_sub(deferral_period.into());

                DoubleZeroEpoch::new(allowed_dz_epoch)
            }
        };

        let mut instructions = Vec::new();
        let mut compute_unit_limit = 5_000;

        let finalize_distribution_tokens_ix = try_build_instruction(
            &ID,
            FinalizeDistributionRewardsAccounts::new(&wallet_key, dz_epoch),
            &RevenueDistributionInstructionData::FinalizeDistributionRewards,
        )?;
        instructions.push(finalize_distribution_tokens_ix);
        compute_unit_limit += 7_500;

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
            compute_unit_limit,
        ));

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let Some(tx_sig) = tx_sig {
            println!("Finalize distribution rewards: {tx_sig}");

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}
