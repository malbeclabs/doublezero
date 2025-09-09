mod admin;
mod ata;
mod contributor;
mod passport;
mod prepaid;
mod revenue_distribution;
mod validator;

pub use admin::*;
pub use ata::*;
pub use contributor::*;
pub use passport::*;
pub use prepaid::*;
pub use revenue_distribution::*;
pub use validator::*;

//

use anyhow::Result;
use clap::Subcommand;

#[derive(Debug, Subcommand)]
pub enum DoubleZeroSolanaCommand {
    /// Admin commands.
    #[command(hide = true)]
    Admin(AdminCliCommand),

    /// Passport programcommands.
    Passport(PassportCliCommand),

    /// Revenue distribution program commands.
    RevenueDistribution(RevenueDistributionCliCommand),

    /// Associated Token Account commands.
    Ata(AtaCliCommand),

    /// Network contributor reward commands.
    Contributor(ContributorCliCommand),

    /// Prepaid connection commands.
    Prepaid(PrepaidCliCommand),

    /// Solana validator commands.
    Validator(ValidatorCliCommand),
}

impl DoubleZeroSolanaCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            DoubleZeroSolanaCommand::Admin(admin) => admin.command.try_into_execute().await,
            DoubleZeroSolanaCommand::Passport(passport) => {
                passport.command.try_into_execute().await
            }
            DoubleZeroSolanaCommand::RevenueDistribution(revenue_distribution) => {
                revenue_distribution.command.try_into_execute().await
            }
            DoubleZeroSolanaCommand::Ata(ata) => ata.command.try_into_execute().await,
            DoubleZeroSolanaCommand::Contributor(contributor) => {
                contributor.command.try_into_execute().await
            }
            DoubleZeroSolanaCommand::Prepaid(prepaid) => prepaid.command.try_into_execute().await,
            DoubleZeroSolanaCommand::Validator(validator) => {
                validator.command.try_into_execute().await
            }
        }
    }
}
