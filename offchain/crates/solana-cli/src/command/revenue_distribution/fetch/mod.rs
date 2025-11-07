mod config;
mod distribution;
mod journal;
mod sol_conversion;
mod validator_deposits;

//

use anyhow::Result;
use clap::{Args, Subcommand};
use tabled::{
    Table, Tabled,
    settings::{Alignment, Style, object::Columns},
};

#[derive(Debug, Args)]
pub struct FetchCommand {
    #[command(subcommand)]
    cmd: FetchSubcommand,
}

#[derive(Debug, Subcommand)]
pub enum FetchSubcommand {
    /// Show program config and parameters.
    Config(config::ConfigCommand),

    /// Print the on-chain journal account (debug format for now).
    Journal(journal::JournalCommand),

    /// Show configured Solana validator fee parameters (if any).
    ValidatorFees(config::ValidatorFeesCommand),

    /// List Solana validator deposit accounts with their balances with optional
    /// node ID filter
    ValidatorDeposits(validator_deposits::ValidatorDepositsCommand),

    /// Show distribution account with optional epoch filter. Default is to show
    /// the distribution account for the current epoch.
    Distribution(distribution::DistributionCommand),

    /// Show the current SOL/2Z conversion price.
    SolConversion(sol_conversion::SolConversionCommand),
}

impl FetchCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self.cmd {
            FetchSubcommand::Config(command) => command.try_into_execute().await,
            FetchSubcommand::ValidatorFees(command) => command.try_into_execute().await,
            FetchSubcommand::Journal(command) => command.try_into_execute().await,
            FetchSubcommand::ValidatorDeposits(command) => command.try_into_execute().await,
            FetchSubcommand::Distribution(command) => command.try_into_execute().await,
            FetchSubcommand::SolConversion(command) => command.try_into_execute().await,
        }
    }
}

//

#[derive(Debug, Default)]
struct TableOptions<'a> {
    columns_aligned_right: Option<&'a [usize]>,
}

fn print_table(value_rows: Vec<impl Tabled>, options: TableOptions) {
    let mut table = Table::new(value_rows);
    table.with(Style::markdown());

    if let Some(columns_aligned_right) = options.columns_aligned_right {
        for column_index in columns_aligned_right {
            table.modify(Columns::one(*column_index), Alignment::right());
        }
    }
    println!("{table}");
}
