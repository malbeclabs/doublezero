use anyhow::Result;
use clap::Args;
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    ID,
    instruction::{RevenueDistributionInstructionData, account::SweepDistributionTokensAccounts},
};
use doublezero_scheduled_command::{Schedulable, ScheduleOption};
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, Wallet};
use solana_sdk::compute_budget::ComputeBudgetInstruction;

use crate::command::revenue_distribution::{
    SolConversionState, try_fetch_journal, try_fetch_program_config,
};

#[derive(Debug, Args, Clone)]
pub struct SweepDistributionTokens {
    #[command(flatten)]
    schedule: ScheduleOption,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

#[async_trait::async_trait]
impl Schedulable for SweepDistributionTokens {
    fn schedule(&self) -> &ScheduleOption {
        &self.schedule
    }

    async fn execute_once(&self) -> Result<()> {
        let Self {
            schedule: _,
            solana_payer_options,
        } = self;
        let wallet = Wallet::try_from(solana_payer_options.clone())?;

        let (_, program_config) = try_fetch_program_config(&wallet.connection).await?;
        let sol_2z_swap_program_id = program_config.sol_2z_swap_program_id;

        let (_, journal) = try_fetch_journal(&wallet.connection).await?;
        let dz_epoch = journal.next_dz_epoch_to_sweep_tokens;

        let SolConversionState {
            program_state: (_, sol_conversion_program_state),
            ..
        } = SolConversionState::try_fetch(&wallet.connection).await?;

        let mut instructions = Vec::new();
        let mut compute_unit_limit = 5_000;

        let sweep_distribution_tokens_ix = try_build_instruction(
            &ID,
            SweepDistributionTokensAccounts::new(
                dz_epoch,
                &sol_2z_swap_program_id,
                &sol_conversion_program_state.fills_registry_key,
            ),
            &RevenueDistributionInstructionData::SweepDistributionTokens,
        )?;
        instructions.push(sweep_distribution_tokens_ix);
        compute_unit_limit += 30_000;

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
            compute_unit_limit,
        ));

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let Some(tx_sig) = tx_sig {
            println!("Sweep distribution tokens: {tx_sig}");

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}
