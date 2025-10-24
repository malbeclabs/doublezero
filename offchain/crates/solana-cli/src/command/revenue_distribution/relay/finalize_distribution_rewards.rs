use anyhow::{Result, anyhow, bail, ensure};
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
use doublezero_solana_client_tools::{
    log_info, log_warn,
    payer::{SolanaPayerOptions, Wallet},
};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, instruction::Instruction};

use crate::command::revenue_distribution::{try_fetch_distribution, try_fetch_program_config};

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

        let (_, distribution) = try_fetch_distribution(&wallet.connection, dz_epoch).await?;

        if distribution.is_rewards_calculation_finalized() {
            if schedule.is_scheduled() {
                log_warn!("Rewards calculation already finalized for epoch {dz_epoch}");

                return Ok(());
            } else {
                bail!("Rewards calculation already finalized for epoch {dz_epoch}");
            }
        }

        let finalize_distribution_tokens_context =
            FinalizeDistributionRewardsContext::try_prepare(&wallet, dz_epoch)?;

        let mut instructions = vec![
            finalize_distribution_tokens_context.instruction,
            ComputeBudgetInstruction::set_compute_unit_limit(
                FinalizeDistributionRewardsContext::COMPUTE_UNIT_LIMIT,
            ),
        ];

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let Some(tx_sig) = tx_sig {
            log_info!("Finalize distribution rewards for epoch {dz_epoch}: {tx_sig}");

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}

pub struct FinalizeDistributionRewardsContext {
    pub instruction: Instruction,
}

impl FinalizeDistributionRewardsContext {
    pub const COMPUTE_UNIT_LIMIT: u32 = 7_500;

    pub fn try_prepare(wallet: &Wallet, dz_epoch: DoubleZeroEpoch) -> Result<Self> {
        let instruction = try_build_instruction(
            &ID,
            FinalizeDistributionRewardsAccounts::new(&wallet.pubkey(), dz_epoch),
            &RevenueDistributionInstructionData::FinalizeDistributionRewards,
        )?;

        Ok(Self { instruction })
    }
}
