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
                return Err(eyre::eyre!(
                    "Invalid flag combination: Use either --env for environment shortcuts OR individual flags, but not both."
                ));
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

#[cfg(test)]
mod tests {
    use std::path::PathBuf;

    use serial_test::serial;
    use tempfile::TempDir;

    use doublezero_config::Environment;
    use doublezero_sdk::{
        create_new_pubkey_user, read_doublezero_config, write_doublezero_config, ClientConfig,
    };

    use super::*;

    const CONFIG_ENV_VAR: &str = "DOUBLEZERO_CONFIG_FILE";

    fn new_test_config(mutator: impl Fn(&mut ClientConfig)) -> (TempDir, PathBuf, ClientConfig) {
        let tmp = TempDir::new().unwrap();
        let keypair_path = tmp.path().join("id.json");
        let config_path = tmp.path().join("config.yml");

        let devnet_config = Environment::Devnet.config().unwrap();

        let mut cfg = ClientConfig {
            json_rpc_url: devnet_config.ledger_public_rpc_url.clone(),
            websocket_url: Some(devnet_config.ledger_public_ws_rpc_url.clone()),
            keypair_path,
            program_id: Some(devnet_config.serviceability_program_id.to_string()),
            geo_program_id: Some(devnet_config.geolocation_program_id.to_string()),
            tenant: None,
            address_labels: Default::default(),
        };

        mutator(&mut cfg);
        (tmp, config_path, cfg)
    }

    #[test]
    #[serial]
    fn test_geo_config_set_env() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut output = Vec::new();
            SetGeoConfigCliCommand {
                env: Some(Environment::Devnet.to_string()),
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                geo_program_id: None,
            }
            .execute(&mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();

            let devnet_config = Environment::Devnet.config().unwrap();
            assert!(output_str.contains(&devnet_config.ledger_public_rpc_url));
            assert!(output_str.contains(&devnet_config.serviceability_program_id.to_string()));
            assert!(output_str.contains(&devnet_config.geolocation_program_id.to_string()));

            let (_, saved) = read_doublezero_config().unwrap();
            assert_eq!(
                saved.geo_program_id,
                Some(devnet_config.geolocation_program_id.to_string())
            );
        });
    }

    #[test]
    #[serial]
    fn test_geo_config_set_individual_flags() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut output = Vec::new();
            SetGeoConfigCliCommand {
                env: None,
                url: Some("https://example.com".to_string()),
                ws: None,
                keypair: None,
                program_id: None,
                geo_program_id: Some("MyGeoProgram123".to_string()),
            }
            .execute(&mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();

            assert!(output_str.contains("https://example.com"));
            assert!(output_str.contains("MyGeoProgram123"));

            let (_, saved) = read_doublezero_config().unwrap();
            assert_eq!(saved.json_rpc_url, "https://example.com");
            assert_eq!(saved.geo_program_id, Some("MyGeoProgram123".to_string()));
        });
    }

    #[test]
    #[serial]
    fn test_geo_config_set_env_with_url_errors() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut output = Vec::new();
            let result = SetGeoConfigCliCommand {
                env: Some(Environment::Devnet.to_string()),
                url: Some("https://example.com".to_string()),
                ws: None,
                keypair: None,
                program_id: None,
                geo_program_id: None,
            }
            .execute(&mut output);

            assert!(result.is_err());
            assert!(result
                .unwrap_err()
                .to_string()
                .contains("Invalid flag combination"));
        });
    }
}
