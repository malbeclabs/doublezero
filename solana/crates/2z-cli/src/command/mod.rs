mod admin;
mod ata;
mod contributor;
mod prepaid;
mod validator;

pub use admin::*;
pub use ata::*;
pub use contributor::*;
pub use prepaid::*;
pub use validator::*;

//

use anyhow::Result;
use clap::Subcommand;

#[derive(Debug, Subcommand)]
pub enum DoubleZero2zSolanaCommand {
    /// Admin commands.
    #[command(hide = true)]
    Admin(AdminCliCommand),

    /// Associated Token Account commands.
    Ata(AtaCliCommand),

    /// Network contributor reward commands.
    Contributor(ContributorCliCommand),

    /// Prepaid connection commands.
    Prepaid(PrepaidCliCommand),

    /// Solana validator commands.
    Validator(ValidatorCliCommand),
}

impl DoubleZero2zSolanaCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            DoubleZero2zSolanaCommand::Admin(admin) => admin.command.try_into_execute().await,
            DoubleZero2zSolanaCommand::Ata(ata) => ata.command.try_into_execute().await,
            DoubleZero2zSolanaCommand::Contributor(contributor) => {
                contributor.command.try_into_execute().await
            }
            DoubleZero2zSolanaCommand::Prepaid(prepaid) => prepaid.command.try_into_execute().await,
            DoubleZero2zSolanaCommand::Validator(validator) => {
                validator.command.try_into_execute().await
            }
        }
    }
}
