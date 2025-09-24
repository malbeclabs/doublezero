pub mod fetch;
pub mod find;
pub mod request;

//

use anyhow::Result;
use clap::{Args, Subcommand};
use doublezero_passport::state::{AccessRequest, ProgramConfig};
use doublezero_solana_client_tools::{rpc::SolanaConnection, zero_copy::ZeroCopyAccountOwned};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Args)]
pub struct PassportCommand {
    #[command(subcommand)]
    pub command: PassportSubcommand,
}

#[derive(Debug, Subcommand)]
pub enum PassportSubcommand {
    Fetch(fetch::FetchCommand),

    Find(find::FindCommand),

    RequestSolanaValidatorAccess(request::RequestSolanaValidatorAccessCommand),
}

impl PassportSubcommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::Find(command) => command.try_into_execute().await,
            Self::Fetch(command) => command.try_into_execute().await,
            Self::RequestSolanaValidatorAccess(command) => command.try_into_execute().await,
        }
    }
}

//

async fn fetch_program_config(connection: &SolanaConnection) -> Result<(Pubkey, ProgramConfig)> {
    let (program_config_key, _) = ProgramConfig::find_address();

    let program_config =
        ZeroCopyAccountOwned::from_rpc_client(&connection.rpc_client, &program_config_key).await?;

    Ok((program_config_key, program_config.data))
}

async fn fetch_access_request(
    connection: &SolanaConnection,
    service_key: &Pubkey,
) -> Result<(Pubkey, Option<AccessRequest>)> {
    let (access_request_key, _) = AccessRequest::find_address(service_key);

    let access_request =
        ZeroCopyAccountOwned::from_rpc_client(&connection.rpc_client, &access_request_key)
            .await
            .ok()
            .map(|access_request| access_request.data);

    Ok((access_request_key, access_request))
}
