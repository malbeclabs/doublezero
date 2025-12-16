use anyhow::{Context, Result};
use clap::{Args, Subcommand};
use doublezero_solana_client_tools::rpc::SolanaConnection;
use doublezero_solana_sdk::passport::state::{AccessRequest, ProgramConfig};
use solana_sdk::pubkey::Pubkey;

mod access_validation;
pub mod fetch;
pub mod find_validator;
pub mod prepare_access;
pub mod request_access;

#[derive(Debug, Args, Clone)]
pub struct SharedAccessArgs {
    /// The DoubleZero service key to request access from
    #[arg(long)]
    pub doublezero_address: Pubkey,
    /// The validator's node ID (identity pubkey)
    #[arg(long, value_name = "PUBKEY")]
    pub primary_validator_id: Pubkey,
    /// Optional backup validator IDs (identity pubkeys)
    #[arg(long, value_name = "PUBKEY,PUBKEY,PUBKEY", value_delimiter = ',')]
    pub backup_validator_ids: Vec<Pubkey>,
    /// Number of previous epochs to check when evaluating the leader schedule (defaults to ENV_PREVIOUS_LEADER_EPOCHS)
    #[arg(long, hide = true)]
    pub leader_schedule_epochs: Option<u8>,
}

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

    let program_config = connection
        .try_fetch_zero_copy_data(&program_config_key)
        .await?;
    Ok((program_config_key, *program_config))
}

async fn fetch_access_request(
    connection: &SolanaConnection,
    service_key: &Pubkey,
) -> Result<(Pubkey, AccessRequest)> {
    let (access_request_key, _) = AccessRequest::find_address(service_key);

    let access_request = connection
        .try_fetch_zero_copy_data(&access_request_key)
        .await
        .with_context(|| format!("Access request not found for service key {service_key}"))?;

    Ok((access_request_key, *access_request.mucked_data))
}
