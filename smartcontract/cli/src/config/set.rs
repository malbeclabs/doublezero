use crate::doublezerocommand::CliCommand;
use clap::{ArgGroup, Args};
use doublezero_config::Environment;
use doublezero_sdk::{
    convert_program_moniker, convert_url_moniker, convert_url_to_ws, convert_ws_moniker,
    read_doublezero_config, write_doublezero_config,
};
use std::{io::Write, path::PathBuf};

#[derive(Args, Debug)]
#[clap(group(
    ArgGroup::new("mandatory")
        .args(&["env", "url", "ws", "keypair", "program_id", "tenant", "no_tenant"])
        .required(true)
        .multiple(true)
))]
pub struct SetConfigCliCommand {
    /// DZ env shorthand to set the config to (testnet [t], devnet [d], or mainnet-beta [m])
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
    /// Default tenant code or pubkey
    #[arg(long, conflicts_with = "no_tenant")]
    pub tenant: Option<String>,
    /// Clear the default tenant
    #[arg(long, conflicts_with = "tenant")]
    pub no_tenant: bool,
}

impl SetConfigCliCommand {
    pub fn execute<W: Write>(self, _client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        let (ledger_url, ledger_ws, program_id) = if let Some(env) = self.env {
            if self.url.is_some() || self.ws.is_some() || self.program_id.is_some() {
                writeln!(
                    out,
                    "Invalid flag combination: Use either --env for environment shortcuts OR individual --url/--ws/--program-id flags, but not both."
                )?;
                return Ok(());
            }

            let config = env.parse::<Environment>()?.config()?;
            (
                Some(config.ledger_public_rpc_url),
                Some(config.ledger_public_ws_rpc_url),
                Some(config.serviceability_program_id.to_string()),
            )
        } else {
            (self.url, self.ws, self.program_id)
        };

        if ledger_url.is_none()
            && ledger_ws.is_none()
            && self.keypair.is_none()
            && program_id.is_none()
            && self.tenant.is_none()
            && !self.no_tenant
        {
            writeln!(out, "No arguments provided")?;
            return Ok(());
        }

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
        if self.no_tenant {
            config.tenant = None;
        } else if self.tenant.is_some() {
            config.tenant = self.tenant;
        }

        write_doublezero_config(&config)?;

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

#[cfg(test)]
mod tests {
    use serial_test::serial;
    use tempfile::TempDir;

    use doublezero_sdk::{create_new_pubkey_user, ClientConfig};

    use crate::tests::utils::create_test_client;

    use super::*;

    const CONFIG_ENV_VAR: &str = "DOUBLEZERO_CONFIG_FILE";

    #[test]
    #[serial]
    fn test_cli_config_set_missing_keypair_file() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            let client = create_test_client();
            write_doublezero_config(&cfg).unwrap();

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: Some(Environment::Devnet.to_string()),
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                tenant: None,
                no_tenant: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();
            let devnet_config = Environment::Devnet.config().unwrap();
            validate_config_output(
                &output_str,
                &devnet_config.ledger_public_rpc_url,
                &devnet_config.serviceability_program_id.to_string(),
            );
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_no_flags() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let client = create_test_client();

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: None,
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                tenant: None,
                no_tenant: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();
            assert_eq!(output_str, "No arguments provided\n");
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_env() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let client = create_test_client();

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: Some(Environment::Devnet.to_string()),
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                tenant: None,
                no_tenant: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();

            let devnet_config = Environment::Devnet.config().unwrap();
            validate_config_output(
                &output_str,
                &devnet_config.ledger_public_rpc_url,
                &devnet_config.serviceability_program_id.to_string(),
            );
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_rpc_url() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let client = create_test_client();

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: None,
                url: Some("https://example.com".to_string()),
                ws: None,
                keypair: None,
                program_id: None,
                tenant: None,
                no_tenant: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();

            let devnet_config = Environment::Devnet.config().unwrap();
            validate_config_output(
                &output_str,
                "https://example.com",
                &devnet_config.serviceability_program_id.to_string(),
            );
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_program_id() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let client = create_test_client();

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: None,
                url: None,
                ws: None,
                keypair: None,
                program_id: Some("1234567890".to_string()),
                tenant: None,
                no_tenant: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();

            let devnet_config = Environment::Devnet.config().unwrap();
            validate_config_output(
                &output_str,
                &devnet_config.ledger_public_rpc_url,
                "1234567890",
            );
        });
    }

    fn new_test_config(mutator: impl Fn(&mut ClientConfig)) -> (TempDir, PathBuf, ClientConfig) {
        let tmp = TempDir::new().unwrap();
        let keypair_path = tmp.path().join("id.json");
        let config_path = tmp.path().join("config.yml");

        let devnet_config = Environment::Devnet.config().unwrap();

        let mut cfg = ClientConfig {
            json_rpc_url: devnet_config.ledger_public_rpc_url.clone(),
            websocket_url: Some(devnet_config.ledger_public_ws_rpc_url.clone()),
            keypair_path: keypair_path.clone(),
            program_id: Some(devnet_config.serviceability_program_id.to_string()),
            tenant: None,
            address_labels: Default::default(),
        };

        mutator(&mut cfg);

        (tmp, config_path, cfg)
    }

    #[test]
    #[serial]
    fn test_cli_config_set_tenant() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let client = create_test_client();

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: None,
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                tenant: Some("my-tenant".to_string()),
                no_tenant: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();
            assert!(output_str.contains("Tenant: my-tenant"));

            // Verify it was persisted
            let (_, saved_config) = read_doublezero_config().unwrap();
            assert_eq!(saved_config.tenant, Some("my-tenant".to_string()));
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_no_tenant() {
        let (_tmp, config_path, cfg) = new_test_config(|cfg| {
            cfg.tenant = Some("existing-tenant".to_string());
        });

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let client = create_test_client();

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: None,
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                tenant: None,
                no_tenant: true,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();
            assert!(output_str.contains("Tenant: (not set)"));

            // Verify it was persisted
            let (_, saved_config) = read_doublezero_config().unwrap();
            assert_eq!(saved_config.tenant, None);
        });
    }

    fn validate_config_output(output_str: &str, expected_rpc_url: &str, expected_program_id: &str) {
        let lines: Vec<&str> = output_str.lines().collect();

        // Check RPC URL
        let rpc_line = lines.iter().find(|line| line.starts_with("RPC URL:"));
        assert!(rpc_line.is_some(), "RPC URL line not found");
        assert!(
            rpc_line.unwrap().contains(expected_rpc_url),
            "RPC URL mismatch. Expected: {expected_rpc_url}, Found: {rpc_line:?}"
        );

        // Check Program ID
        let program_id_line = lines.iter().find(|line| line.starts_with("Program ID:"));
        assert!(program_id_line.is_some(), "Program ID line not found");
        assert!(
            program_id_line.unwrap().contains(expected_program_id),
            "Program ID mismatch. Expected: {expected_program_id}, Found: {program_id_line:?}"
        );

        // Verify the output contains expected structure
        assert!(
            output_str.contains("Config File:"),
            "Config File line missing"
        );
        assert!(
            output_str.contains("WebSocket URL:"),
            "WebSocket URL line missing"
        );
        assert!(
            output_str.contains("Keypair Path:"),
            "Keypair Path line missing"
        );
    }
}
