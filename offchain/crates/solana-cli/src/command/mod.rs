mod ata;
mod passport;
mod revenue_distribution;

pub use ata::*;
pub use passport::*;
pub use revenue_distribution::*;

//

use anyhow::Result;
use clap::Subcommand;

#[derive(Debug, Subcommand)]
pub enum DoubleZeroSolanaCommand {
    /// Associated Token Account commands.
    Ata(AtaCliCommand),

    /// Passport program commands.
    Passport(PassportCliCommand),

    /// Revenue distribution program commands.
    RevenueDistribution(RevenueDistributionCliCommand),
}

impl DoubleZeroSolanaCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            DoubleZeroSolanaCommand::Ata(ata) => ata.command.try_into_execute().await,
            DoubleZeroSolanaCommand::Passport(passport) => {
                passport.command.try_into_execute().await
            }
            DoubleZeroSolanaCommand::RevenueDistribution(revenue_distribution) => {
                revenue_distribution.command.try_into_execute().await
            }
        }
    }
}
