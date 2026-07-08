mod config;
mod contributor_rewards;
mod distribution;
mod sol_conversion;
mod validator_debts;
mod validator_deposits;

//

use std::io::Write;

use anyhow::Result;
use clap::{Args, Subcommand};
use doublezero_cli_core::CliContext;
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

    /// Show contributor rewards accounts with optional filters. Use --view
    /// recipients to see recipient details (requires --service-key).
    ContributorRewards(contributor_rewards::ContributorRewardsCommand),

    /// Show distribution account with optional epoch filter. Default is to show
    /// the distribution account for the current epoch.
    Distribution(distribution::DistributionCommand),

    /// Show the current SOL/2Z conversion price.
    SolConversion(sol_conversion::SolConversionCommand),

    /// Show validator debts owed to the Revenue Distribution program.
    ValidatorDebts(validator_debts::ValidatorDebtsCommand),

    /// List Solana validator deposit accounts with their balances with optional
    /// node ID filter
    ValidatorDeposits(validator_deposits::ValidatorDepositsCommand),

    /// Show configured Solana validator fee parameters (if any).
    ValidatorFees(config::ValidatorFeesCommand),
}

impl FetchCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        match self.cmd {
            FetchSubcommand::Config(command) => command.execute(ctx, out).await,
            FetchSubcommand::ContributorRewards(command) => command.execute(ctx, out).await,
            FetchSubcommand::Distribution(command) => command.execute(ctx, out).await,
            FetchSubcommand::SolConversion(command) => command.execute(ctx, out).await,
            FetchSubcommand::ValidatorDebts(command) => command.execute(ctx, out).await,
            FetchSubcommand::ValidatorDeposits(command) => command.execute(ctx, out).await,
            FetchSubcommand::ValidatorFees(command) => command.execute(ctx, out).await,
        }
    }
}

//

#[derive(Debug, Default)]
struct TableOptions<'a> {
    columns_aligned_right: Option<&'a [usize]>,
}

fn write_table(
    out: &mut impl Write,
    value_rows: Vec<impl Tabled>,
    options: TableOptions,
) -> Result<()> {
    let mut table = Table::new(value_rows);
    table.with(Style::markdown());

    if let Some(columns_aligned_right) = options.columns_aligned_right {
        for column_index in columns_aligned_right {
            table.modify(Columns::one(*column_index), Alignment::right());
        }
    }
    writeln!(out, "{table}")?;
    Ok(())
}
