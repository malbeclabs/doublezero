use crate::command::passport::{
    fetch::execute_fetch, find::execute_find, request::execute_request_solana_validator_access,
};
use anyhow::Result;
use clap::{Args, Subcommand};
use doublezero_solana_client_tools::{payer::SolanaPayerOptions, rpc::SolanaConnectionOptions};
use solana_sdk::pubkey::Pubkey;

pub mod fetch;
pub mod find;
pub mod request;

#[derive(Debug, Args)]
pub struct PassportCliCommand {
    #[command(subcommand)]
    pub command: PassportSubCommand,
}

#[derive(Debug, Subcommand)]
pub enum PassportSubCommand {
    Find {
        #[arg(long, value_name = "PUBKEY")]
        node_id: Option<Pubkey>,

        #[arg(long, value_name = "IP_ADDRESS")]
        server_ip: Option<String>,

        #[command(flatten)]
        solana_connection_options: SolanaConnectionOptions,
    },

    Fetch {
        #[arg(long)]
        program_config: bool,

        #[command(flatten)]
        solana_connection_options: SolanaConnectionOptions,
    },

    RequestSolanaValidatorAccess {
        service_key: Pubkey,

        #[arg(long, value_name = "PUBKEY")]
        node_id: Pubkey,

        #[arg(long, short = 's', value_name = "BASE58_STRING")]
        signature: String,

        /// Offchain message version. ONLY 0 IS SUPPORTED.
        #[arg(long, value_name = "U8", default_value = "0")]
        message_version: u8,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },
}

impl PassportSubCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            PassportSubCommand::Find {
                node_id,
                server_ip,
                solana_connection_options,
            } => execute_find(node_id, server_ip, solana_connection_options).await,
            PassportSubCommand::Fetch {
                program_config,
                solana_connection_options,
            } => execute_fetch(program_config, solana_connection_options).await,
            PassportSubCommand::RequestSolanaValidatorAccess {
                service_key,
                node_id,
                signature,
                message_version,
                solana_payer_options,
            } => {
                execute_request_solana_validator_access(
                    service_key,
                    node_id,
                    signature,
                    message_version,
                    solana_payer_options,
                )
                .await
            }
        }
    }
}
