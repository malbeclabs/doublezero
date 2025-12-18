use anyhow::{Result, bail, ensure};
use clap::Args;
use doublezero_scheduled_command::{Schedulable, ScheduleOption};
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    revenue_distribution::{
        ID,
        fetch::try_fetch_config,
        instruction::{
            RevenueDistributionInstructionData, account::SweepDistributionTokensAccounts,
        },
        state::{Distribution, ProgramConfig},
        types::DoubleZeroEpoch,
    },
    sol_conversion::state::MAX_FILLS_QUEUE_SIZE,
    try_build_instruction,
};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, instruction::Instruction};

use crate::command::revenue_distribution::{SolConversionState, try_fetch_distribution};

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
            schedule,
            solana_payer_options,
        } = self;
        let wallet = Wallet::try_from(solana_payer_options.clone())?;

        let (_, config) = try_fetch_config(&wallet.connection).await?;

        let sweep_distribution_tokens_context = match SweepDistributionTokensContext::try_prepare(
            &wallet, &config, None, // dz_epoch
        )
        .await
        {
            Ok(context) => context,
            Err(e) => {
                if schedule.is_scheduled() {
                    tracing::warn!("{e}");

                    return Ok(());
                } else {
                    bail!(e);
                }
            }
        };

        let mut instructions = vec![
            sweep_distribution_tokens_context.instruction,
            ComputeBudgetInstruction::set_compute_unit_limit(
                sweep_distribution_tokens_context.compute_unit_limit,
            ),
        ];

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;

        // TODO: We should fetch the distribution and journal to check whether
        // there are enough 2Z tokens to sweep instead of warning on an RPC
        // error.
        let tx_sig = match wallet.send_or_simulate_transaction(&transaction).await {
            Ok(tx_sig) => tx_sig,
            Err(e) => {
                if schedule.is_scheduled() {
                    tracing::warn!("{e}");

                    return Ok(());
                } else {
                    bail!(e);
                }
            }
        };

        if let TransactionOutcome::Executed(tx_sig) = tx_sig {
            tracing::info!(
                "Sweep distribution tokens for epoch {}: {tx_sig}",
                sweep_distribution_tokens_context.dz_epoch
            );

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}

pub struct SweepDistributionTokensContext {
    pub instruction: Instruction,
    pub compute_unit_limit: u32,
    pub dz_epoch: DoubleZeroEpoch,
}

impl SweepDistributionTokensContext {
    pub async fn try_prepare(
        wallet: &Wallet,
        config: &ProgramConfig,
        distribution: Option<&Distribution>,
    ) -> Result<Self> {
        let SolConversionState {
            program_state: (_, sol_conversion_program_state),
            configuration_registry: _,
            journal: (_, journal),
            fixed_fill_quantity,
        } = SolConversionState::try_fetch(&wallet.connection).await?;

        let expected_dz_epoch = journal.next_dz_epoch_to_sweep_tokens;
        let distribution = match distribution {
            Some(distribution) => {
                ensure!(
                    distribution.dz_epoch == expected_dz_epoch,
                    "DZ epoch does not match next epoch to sweep tokens"
                );

                *distribution
            }
            None => {
                let (_, distribution_data) =
                    try_fetch_distribution(&wallet.connection, expected_dz_epoch).await?;
                *distribution_data.mucked_data
            }
        };

        let expected_fill_count =
            distribution.checked_total_sol_debt().unwrap() / fixed_fill_quantity + 1;
        ensure!(
            expected_fill_count <= MAX_FILLS_QUEUE_SIZE as u64,
            "Expected fill count is too large"
        );

        let sweep_distribution_tokens_ix = try_build_instruction(
            &ID,
            SweepDistributionTokensAccounts::new(
                expected_dz_epoch,
                &config.sol_2z_swap_program_id,
                &sol_conversion_program_state.fills_registry_key,
            ),
            &RevenueDistributionInstructionData::SweepDistributionTokens,
        )?;
        let compute_unit_limit = 35_000 + 80 * expected_fill_count as u32;

        Ok(Self {
            instruction: sweep_distribution_tokens_ix,
            compute_unit_limit,
            dz_epoch: expected_dz_epoch,
        })
    }
}
