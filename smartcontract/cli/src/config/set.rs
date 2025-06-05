use crate::doublezerocommand::CliCommand;
use clap::{ArgGroup, Args};
use doublezero_sdk::{
    convert_program_moniker, convert_url_moniker, convert_url_to_ws, convert_ws_moniker,
    read_doublezero_config, write_doublezero_config,
};
use std::io::Write;

#[derive(Args, Debug)]
#[clap(group(
    ArgGroup::new("mandatory")
        .args(&["url", "ws", "keypair", "program_id"])
        .required(true)
        .multiple(true)
))]
pub struct SetConfigCliCommand {
    #[arg(
        long,
        help = "URL of the JSON RPC endpoint (devnet, testnet, mainnet, localhost)"
    )]
    url: Option<String>,
    #[arg(
        long,
        help = "URL of the WS RPC endpoint (devnet, testnet, mainnet, localhost)"
    )]
    ws: Option<String>,
    #[arg(long, help = "Keypair of the user")]
    keypair: Option<String>,
    #[arg(long, help = "Pubkey of the smart contract (devnet, testnet)")]
    program_id: Option<String>,
}

impl SetConfigCliCommand {
    pub fn execute<W: Write>(self, _client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        if self.url.is_none()
            && self.ws.is_none()
            && self.keypair.is_none()
            && self.program_id.is_none()
        {
            writeln!(out, "No arguments provided")?;
            return Ok(());
        }

        let (filename, mut config) = read_doublezero_config()?;
        if let Some(url) = self.url {
            config.json_rpc_url = convert_url_moniker(url);
            config.websocket_url = None;
        }
        if let Some(ws) = self.ws {
            config.websocket_url = Some(convert_ws_moniker(ws));
        }
        if let Some(keypair) = self.keypair {
            config.keypair_path = keypair;
        }
        if let Some(program_id) = self.program_id {
            config.program_id = Some(convert_program_moniker(program_id));
        }

        write_doublezero_config(&config)?;

        writeln!(
            out,
            "Config File: {}\nRPC URL: {}\nWebSocket URL: {}\nKeypair Path: {}\nProgram ID: {}\n",
            filename,
            config.json_rpc_url,
            config.websocket_url.unwrap_or(format!(
                "{} (computed)",
                convert_url_to_ws(&config.json_rpc_url)
            )),
            config.keypair_path,
            config.program_id.unwrap_or(format!(
                "{} (computed)",
                doublezero_sdk::testnet::program_id::id()
            ))
        )?;

        Ok(())
    }
}
