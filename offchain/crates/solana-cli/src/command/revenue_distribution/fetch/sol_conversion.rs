use anyhow::{Context, Result};
use clap::Args;
use doublezero_sol_conversion_interface::oracle::DiscountParameters;
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};

use crate::command::revenue_distribution::{
    SolConversionState, try_request_oracle_conversion_price,
};

#[derive(Debug, Args)]
pub struct SolConversionCommand {
    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

#[derive(Debug, tabled::Tabled)]
struct SolConversionTableRow {
    field: &'static str,
    description: &'static str,
    value: String,
    note: String,
}

impl SolConversionCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self { connection_options } = self;

        let connection = SolanaConnection::try_from(connection_options)?;

        let SolConversionState {
            program_state: (_, program_state),
            configuration_registry: (_, configuration_registry),
            journal: (_, journal),
        } = SolConversionState::try_fetch(&connection).await?;
        let last_slot = program_state.last_trade_slot;

        let current_slot = connection.rpc_client.get_slot().await?;

        let discount_parameters = DiscountParameters {
            coefficient: configuration_registry.coefficient,
            max_discount: configuration_registry.max_discount_rate,
            min_discount: configuration_registry.min_discount_rate,
        };
        let discount = discount_parameters
            .checked_compute(current_slot - last_slot)
            .context("Failed to calculate discount")?;

        let oracle_price_data = try_request_oracle_conversion_price().await?;

        let discounted_swap_rate = oracle_price_data
            .checked_discounted_swap_rate(discount)
            .context("Failed to calculate discounted swap rate")?;

        let value_rows = vec![
            SolConversionTableRow {
                field: "Swap rate",
                description: "2Z amount for 1 SOL",
                value: format!("{:.8}", oracle_price_data.swap_rate as f64 * 1e-8),
                note: Default::default(),
            },
            SolConversionTableRow {
                field: "Swap rate",
                description: "2Z amount for 1 SOL",
                value: format!("{:.8}", discounted_swap_rate as f64 * 1e-8),
                note: format!("Includes {:.8}% discount", discount as f64 * 1e-6),
            },
            SolConversionTableRow {
                field: "Journal balance",
                description: "SOL available for conversion",
                value: format!("{:.9}", journal.total_sol_balance as f64 * 1e-9),
                note: Default::default(),
            },
        ];

        super::print_table(value_rows);

        Ok(())
    }
}
