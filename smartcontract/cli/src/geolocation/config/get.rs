use clap::Args;
use doublezero_sdk::{convert_url_to_ws, default_geolocation_program_id, read_doublezero_config};
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetGeoConfigCliCommand;

impl GetGeoConfigCliCommand {
    pub fn execute<W: Write>(self, out: &mut W) -> eyre::Result<()> {
        let (filename, config) = read_doublezero_config()?;

        writeln!(
            out,
            "Config File: {}\nRPC URL: {}\nWebSocket URL: {}\nKeypair Path: {}\nServiceability Program ID: {}\nGeolocation Program ID: {}\n",
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
            config.geo_program_id.unwrap_or(format!(
                "{} (computed)",
                default_geolocation_program_id()
            )),
        )?;

        Ok(())
    }
}
