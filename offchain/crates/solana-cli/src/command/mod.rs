mod ata;
mod passport;
mod revenue_distribution;

//

use anyhow::Result;
use clap::Subcommand;

#[derive(Debug, Subcommand)]
pub enum DoubleZeroSolanaCommand {
    /// Associated Token Account commands.
    Ata(ata::AtaCommand),

    /// Passport program commands.
    Passport(passport::PassportCommand),

    /// Revenue distribution program commands.
    RevenueDistribution(revenue_distribution::RevenueDistributionCommand),
}

impl DoubleZeroSolanaCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::Ata(ata) => ata.command.try_into_execute().await,
            Self::Passport(passport) => passport.command.try_into_execute().await,
            Self::RevenueDistribution(revenue_distribution) => {
                revenue_distribution.command.try_into_execute().await
            }
        }
    }
}
