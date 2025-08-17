use anyhow::{anyhow, Result};
use clap::{Args, Subcommand};
use doublezero_passport::state::ProgramConfig;
use doublezero_program_tools::zero_copy;

use crate::rpc::{Connection, SolanaConnectionOptions};

#[derive(Debug, Args)]
pub struct PassportCliCommand {
    #[command(subcommand)]
    pub command: PassportSubCommand,
}

#[derive(Debug, Subcommand)]
pub enum PassportSubCommand {
    Fetch {
        #[arg(long)]
        program_config: bool,

        #[command(flatten)]
        solana_connection_options: SolanaConnectionOptions,
    },
}

impl PassportSubCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            PassportSubCommand::Fetch {
                program_config,
                solana_connection_options,
            } => execute_fetch(program_config, solana_connection_options).await,
        }
    }
}

//
// PassportSubCommand::Fetch.
//

async fn execute_fetch(
    program_config: bool,
    solana_connection_options: SolanaConnectionOptions,
) -> Result<()> {
    let connection = Connection::try_from(solana_connection_options)?;

    if program_config {
        let program_config_key = ProgramConfig::find_address().0;
        let program_config_info = connection.get_account(&program_config_key).await?;

        let (program_config, _) =
            zero_copy::checked_from_bytes_with_discriminator::<ProgramConfig>(
                &program_config_info.data,
            )
            .ok_or(anyhow!("Failed to deserialize program config"))?;

        // TODO: Pretty print.
        println!("Program config: {program_config:?}");
    }

    Ok(())
}
