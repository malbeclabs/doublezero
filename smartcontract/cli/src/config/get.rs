use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::{convert_url_to_ws, read_doublezero_config};
use std::{collections::HashMap, io::Write};

#[derive(Args, Debug)]
pub struct GetConfigCliCommand;

impl GetConfigCliCommand {
    pub async fn execute<W: Write>(
        self,
        _ctx: &CliContext,
        _client: &dyn CliCommand,
        out: &mut W,
    ) -> eyre::Result<()> {
        let (filename, config) = read_doublezero_config()?;

        writeln!(
            out,
            "Config File: {}\nRPC URL: {}\nWebSocket URL: {}\nKeypair Path: {}\nProgram ID: {}\nGeolocation Program ID: {}\nTenant: {}\n{}",
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
            config.tenant.unwrap_or("(not set)".to_string()),
            format_address_labels(&config.address_labels),
        )?;

        Ok(())
    }
}

/// The `Address Labels:` section of `config get`: one `pubkey: label` line
/// per label, sorted by pubkey, or `(none)`.
fn format_address_labels(labels: &HashMap<String, String>) -> String {
    if labels.is_empty() {
        return "Address Labels: (none)\n".to_string();
    }
    let mut entries: Vec<_> = labels.iter().collect();
    entries.sort();
    let mut out = "Address Labels:\n".to_string();
    for (pubkey, label) in entries {
        out.push_str(&format!("  {pubkey}: {label}\n"));
    }
    out
}

#[cfg(test)]
mod tests {
    use serial_test::serial;
    use tempfile::TempDir;

    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{create_new_pubkey_user, write_doublezero_config, ClientConfig};

    use crate::tests::utils::create_test_client;
    use doublezero_config::Environment;

    use super::*;

    const CONFIG_ENV_VAR: &str = "DOUBLEZERO_CONFIG_FILE";

    #[test]
    #[serial]
    fn test_cli_config_get() {
        let (tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let client = create_test_client();
            let ctx = cli_context_default_for_tests();

            let mut output = Vec::new();
            block_on(GetConfigCliCommand.execute(&ctx, &client, &mut output)).unwrap();
            let output_str = String::from_utf8(output).unwrap();

            assert!(output_str.contains("Config File:"));
            assert!(output_str.contains("RPC URL:"));
            assert!(output_str.contains("WebSocket URL:"));
            assert!(output_str.contains("Keypair Path:"));
            assert!(output_str.contains("Program ID:"));
            assert!(output_str.contains("Geolocation Program ID:"));
            assert!(output_str.contains("Tenant: (not set)"));
            assert!(output_str.contains("Address Labels: (none)"));

            let devnet_config = Environment::Devnet.config().unwrap();
            assert!(output_str.contains(&devnet_config.ledger_public_rpc_url));
        });

        drop(tmp);
    }

    #[test]
    #[serial]
    fn test_cli_config_get_with_tenant() {
        let (_tmp, config_path, cfg) = new_test_config(|cfg| {
            cfg.tenant = Some("my-tenant".to_string());
        });

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let client = create_test_client();
            let ctx = cli_context_default_for_tests();

            let mut output = Vec::new();
            block_on(GetConfigCliCommand.execute(&ctx, &client, &mut output)).unwrap();
            let output_str = String::from_utf8(output).unwrap();
            assert!(output_str.contains("Tenant: my-tenant"));
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_get_with_address_labels() {
        let (_tmp, config_path, cfg) = new_test_config(|cfg| {
            cfg.address_labels.insert(
                "DZfLKFDgLShjY34WqXdVVzHUvVtrYXb7UtdrALnGa8jw".to_string(),
                "Jared".to_string(),
            );
            cfg.address_labels.insert(
                "11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo".to_string(),
                "Ops".to_string(),
            );
        });

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let client = create_test_client();
            let ctx = cli_context_default_for_tests();

            let mut output = Vec::new();
            block_on(GetConfigCliCommand.execute(&ctx, &client, &mut output)).unwrap();
            let output_str = String::from_utf8(output).unwrap();
            assert!(output_str.ends_with(
                "Address Labels:\n  11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo: Ops\n  DZfLKFDgLShjY34WqXdVVzHUvVtrYXb7UtdrALnGa8jw: Jared\n\n"
            ));
        });
    }

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
            keypair_path: keypair_path.clone(),
            program_id: Some(devnet_config.serviceability_program_id.to_string()),
            geo_program_id: None,
            tenant: None,
            address_labels: Default::default(),
        };

        mutator(&mut cfg);

        (tmp, config_path, cfg)
    }
}
