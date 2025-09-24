use clap::{ArgGroup, Args};
use doublezero_cli::doublezerocommand::CliCommand;
use doublezero_config::Environment;
use std::{io::Write, path::PathBuf};

use crate::servicecontroller::{ConfigRequest, ServiceController, ServiceControllerImpl};

#[derive(Args, Debug)]
#[clap(group(
    ArgGroup::new("mandatory")
        .args(&["env", "url", "ws", "keypair", "program_id"])
        .required(true)
        .multiple(true)
))]
pub struct SetConfigCliCommand {
    /// DZ env shorthand to set the config to (testnet, devnet, or mainnet)
    #[arg(long, value_name = "ENV")]
    pub env: Option<String>,
    /// URL of the JSON RPC endpoint (devnet, testnet, mainnet, localhost)
    #[arg(long)]
    pub url: Option<String>,
    /// URL of the WS RPC endpoint (devnet, testnet, mainnet, localhost)
    #[arg(long)]
    pub ws: Option<String>,
    /// Keypair of the user
    #[arg(long)]
    pub keypair: Option<PathBuf>,
    /// Pubkey of the smart contract (devnet, testnet)
    #[arg(long)]
    pub program_id: Option<String>,
}

impl SetConfigCliCommand {
    pub async fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        doublezero_cli::config::set::SetConfigCliCommand {
            env: self.env.clone(),
            url: self.url,
            ws: self.ws,
            keypair: self.keypair,
            program_id: self.program_id,
        }
        .execute(client, out)?;

        if let Some(env) = &self.env {
            let config = env.parse::<Environment>()?.config()?;

            let controller = ServiceControllerImpl::new(None);
            controller
                .config(ConfigRequest {
                    ledger_rpc_url: config.ledger_public_rpc_url,
                    serviceability_program_id: config.serviceability_program_id.to_string(),
                })
                .await
                .map_err(|e| eyre::eyre!(e))?;
        }

        Ok(())
    }
}
