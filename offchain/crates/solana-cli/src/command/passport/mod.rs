use anyhow::Result;
use clap::{Args, Subcommand};
use doublezero_passport::state::{AccessRequest, ProgramConfig};
use doublezero_solana_client_tools::{rpc::SolanaConnection, zero_copy::ZeroCopyAccountOwned};
use solana_sdk::pubkey::Pubkey;

pub mod fetch;
pub mod find_validator;
pub mod prepare_access;
pub mod request_access;

#[derive(Debug, Args)]
pub struct PassportCommand {
    #[command(subcommand)]
    pub command: PassportSubcommand,
}

#[derive(Debug, Subcommand)]
pub enum PassportSubcommand {
    /// Fetch and display the current program configuration and access request (if any)
    Fetch(fetch::FetchCommand),
    /// Find and display the Current Identity
    FindValidator(find_validator::FindValidatorCommand),
    /// Validate arguments and generate the required transaction signature command
    PrepareValidatorAccess(prepare_access::PrepareValidatorAccessCommand),
    /// Request access as a Solana Validator
    RequestValidatorAccess(request_access::RequestValidatorAccessCommand),
}

impl PassportSubcommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::Fetch(command) => command.try_into_execute().await,
            Self::FindValidator(command) => command.try_into_execute().await,
            Self::PrepareValidatorAccess(command) => command.try_into_execute().await,
            Self::RequestValidatorAccess(command) => command.try_into_execute().await,
        }
    }
}

//

async fn fetch_program_config(connection: &SolanaConnection) -> Result<(Pubkey, ProgramConfig)> {
    let (program_config_key, _) = ProgramConfig::find_address();

    let program_config =
        ZeroCopyAccountOwned::from_rpc_client(&connection.rpc_client, &program_config_key).await?;

    Ok((program_config_key, *program_config.data.unwrap().0))
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
            .and_then(|access_request| access_request.data);

    Ok((
        access_request_key,
        access_request.map(|access_request| *access_request.0),
    ))
}
