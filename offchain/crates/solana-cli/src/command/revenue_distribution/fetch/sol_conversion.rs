use std::io::Write;

use anyhow::Result;
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::rpc::SolanaConnectionOptions;
use doublezero_solana_sdk::revenue_distribution::fetch::SolConversionState;

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
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        let Self { connection_options } = self;
        let connection = crate::command::solana_connection(ctx, &connection_options);

        let SolConversionState {
            journal: (_, journal),
            fixed_fill_quantity,
            ..
        } = SolConversionState::try_fetch(&connection).await?;

        let value_rows = vec![
            SolConversionTableRow {
                field: "Journal balance",
                description: "SOL available for conversion",
                value: format!("{:.9}", journal.total_sol_balance as f64 * 1e-9),
                note: Default::default(),
            },
            SolConversionTableRow {
                field: "SOL per swap",
                description: "Fixed amount",
                value: format!("{:.9}", fixed_fill_quantity as f64 * 1e-9),
                note: Default::default(),
            },
        ];

        super::write_table(out, value_rows, Default::default())?;

        Ok(())
    }
}
