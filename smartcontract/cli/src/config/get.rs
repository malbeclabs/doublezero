use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::{convert_url_to_ws, read_doublezero_config};
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetConfigCliCommand;

impl GetConfigCliCommand {
    pub fn execute<W: Write>(self, _client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        let (filename, config) = read_doublezero_config()?;

        writeln!(
            out,
            "Config File: {}\nRPC URL: {}\nWebSocket URL: {}\nKeypair Path: {}\nProgram ID: {}\nTenant: {}\n",
            filename.display(),
            config.json_rpc_url,
            config.websocket_url.unwrap_or(format!(
                "{} (computed)",
                convert_url_to_ws(&config.json_rpc_url)?
            )),
            config.keypair_path.display(),
            config.program_id.unwrap_or(format!(
                "{} (computed)",
                doublezero_sdk::default_program_id()
            )),
            config.tenant.unwrap_or("(not set)".to_string())
        )?;

        Ok(())
    }
}
