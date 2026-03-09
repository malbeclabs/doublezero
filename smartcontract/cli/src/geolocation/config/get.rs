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

#[cfg(test)]
mod tests {
    use serial_test::serial;
    use tempfile::TempDir;

    use doublezero_config::Environment;
    use doublezero_sdk::{create_new_pubkey_user, write_doublezero_config, ClientConfig};

    use super::*;

    const CONFIG_ENV_VAR: &str = "DOUBLEZERO_CONFIG_FILE";

    fn new_test_config(
        mutator: impl Fn(&mut ClientConfig),
    ) -> (TempDir, std::path::PathBuf, ClientConfig) {
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
    fn test_geo_config_get() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut output = Vec::new();
            GetGeoConfigCliCommand.execute(&mut output).unwrap();
            let output_str = String::from_utf8(output).unwrap();

            assert!(output_str.contains("Config File:"));
            assert!(output_str.contains("RPC URL:"));
            assert!(output_str.contains("WebSocket URL:"));
            assert!(output_str.contains("Keypair Path:"));
            assert!(output_str.contains("Serviceability Program ID:"));
            assert!(output_str.contains("Geolocation Program ID:"));

            let devnet_config = Environment::Devnet.config().unwrap();
            assert!(output_str.contains(&devnet_config.ledger_public_rpc_url));
            assert!(output_str.contains(&devnet_config.geolocation_program_id.to_string()));
        });
    }

    #[test]
    #[serial]
    fn test_geo_config_get_computed_defaults() {
        let (_tmp, config_path, cfg) = new_test_config(|cfg| {
            cfg.program_id = None;
            cfg.geo_program_id = None;
        });

        temp_env::with_var(CONFIG_ENV_VAR, Some(config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut output = Vec::new();
            GetGeoConfigCliCommand.execute(&mut output).unwrap();
            let output_str = String::from_utf8(output).unwrap();

            assert!(output_str.contains("(computed)"));
            assert!(output_str.contains(&doublezero_sdk::default_program_id().to_string()));
            assert!(output_str.contains(&default_geolocation_program_id().to_string()));
        });
    }
}
