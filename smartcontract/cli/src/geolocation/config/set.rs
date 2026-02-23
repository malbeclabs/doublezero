use clap::{ArgGroup, Args};
use doublezero_config::Environment;
use doublezero_sdk::{
    convert_geo_program_moniker, convert_program_moniker, convert_url_moniker, convert_url_to_ws,
    convert_ws_moniker, read_doublezero_config, write_doublezero_config,
};
use std::{io::Write, path::PathBuf};

#[derive(Args, Debug)]
#[clap(group(
    ArgGroup::new("mandatory")
        .args(&["env", "url", "ws", "keypair", "program_id", "geo_program_id"])
        .required(true)
        .multiple(true)
))]
pub struct SetGeoConfigCliCommand {
    /// DZ env shorthand (local [l], devnet [d], testnet [t], or mainnet-beta [m])
    #[arg(long, value_name = "ENV")]
    pub env: Option<String>,
    /// URL of the JSON RPC endpoint
    #[arg(long)]
    pub url: Option<String>,
    /// URL of the WS RPC endpoint
    #[arg(long)]
    pub ws: Option<String>,
    /// Keypair of the user
    #[arg(long)]
    pub keypair: Option<PathBuf>,
    /// Serviceability program ID
    #[arg(long)]
    pub program_id: Option<String>,
    /// Geolocation program ID
    #[arg(long)]
    pub geo_program_id: Option<String>,
}

impl SetGeoConfigCliCommand {
    pub fn execute<W: Write>(self, out: &mut W) -> eyre::Result<()> {
        let (ledger_url, ledger_ws, program_id, geo_program_id) = if let Some(env) = self.env {
            if self.url.is_some()
                || self.ws.is_some()
                || self.program_id.is_some()
                || self.geo_program_id.is_some()
            {
                writeln!(
                    out,
                    "Invalid flag combination: Use either --env for environment shortcuts OR individual flags, but not both."
                )?;
                return Ok(());
            }

            let config = env.parse::<Environment>()?.config()?;
            (
                Some(config.ledger_public_rpc_url),
                Some(config.ledger_public_ws_rpc_url),
                Some(config.serviceability_program_id.to_string()),
                Some(config.geolocation_program_id.to_string()),
            )
        } else {
            (self.url, self.ws, self.program_id, self.geo_program_id)
        };

        let (filename, mut config) = read_doublezero_config()?;

        if let Some(url) = ledger_url {
            config.json_rpc_url = convert_url_moniker(url);
            config.websocket_url = None;
        }
        if let Some(ws) = ledger_ws {
            config.websocket_url = Some(convert_ws_moniker(ws));
        }
        if let Some(keypair) = self.keypair {
            config.keypair_path = keypair;
        }
        if let Some(program_id) = program_id {
            config.program_id = Some(convert_program_moniker(program_id));
        }
        if let Some(geo_program_id) = geo_program_id {
            config.geo_program_id = Some(convert_geo_program_moniker(geo_program_id));
        }

        write_doublezero_config(&config)?;

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
                doublezero_sdk::default_geolocation_program_id()
            )),
        )?;

        Ok(())
    }
}
