use crate::doublezerocommand::CliCommand;
use clap::{ArgGroup, Args};
use doublezero_config::Environment;
use doublezero_sdk::{
    commands::user::get::GetUserCommand, convert_program_moniker, convert_url_moniker,
    convert_url_to_ws, convert_ws_moniker, read_doublezero_config, utils::read_keypair_from_file,
    write_doublezero_config,
};
use solana_sdk::signature::Signer;
use std::{io::Write, path::PathBuf};

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
    /// Force the config to be set even if the user is connected
    #[arg(long)]
    pub force: bool,
}

impl SetConfigCliCommand {
    pub fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<bool> {
        let (ledger_url, ledger_ws, program_id) = if let Some(env) = self.env {
            if self.url.is_some() || self.ws.is_some() || self.program_id.is_some() {
                writeln!(
                    out,
                    "Invalid flag combination: Use either --env for environment shortcuts OR individual --url/--ws/--program-id flags, but not both."
                )?;
                return Ok(false);
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
        {
            writeln!(out, "No arguments provided")?;
            return Ok(false);
        }

        let (filename, mut config) = read_doublezero_config()?;

        // Before the config can be changed, we need to check if the user has any open connections
        // and refuse to make any changes if so. Otherwise, the doublezerod state will reflect the
        // wrong environment.
        let keypair = read_keypair_from_file(config.keypair_path.clone()).map_err(|e| {
            eyre::eyre!(
                "Failed to read keypair from file: {}\nPlease check that the keypair exists and is valid",
                e
            )
        })?;

        if let Ok((_, user)) = client.get_user(GetUserCommand {
            pubkey: keypair.pubkey(),
        }) {
            if !self.force {
                writeln!(
                out,
                "Cannot change network config while connected as user. Please disconnect first. Current status: {}",
                user.status
                )?;
                return Ok(false);
            } else {
                writeln!(out, "WARNING: Changing network config while connected as user because --force was provided. This will disconnect you from the current network. Current status: {}", user.status)?;
            }
        }

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

        write_doublezero_config(&config)?;

        writeln!(
            out,
            "Config File: {}\nRPC URL: {}\nWebSocket URL: {}\nKeypair Path: {}\nProgram ID: {}\n",
            filename.display(),
            config.json_rpc_url,
            config.websocket_url.unwrap_or(format!(
                "{} (computed)",
                convert_url_to_ws(&config.json_rpc_url)?
            )),
            config.keypair_path.display(),
            config.program_id.unwrap_or(format!(
                "{} (computed)",
                doublezero_sdk::testnet::program_id::id()
            ))
        )?;

        Ok(true)
    }
}

#[cfg(test)]
mod tests {
    use serial_test::serial;
    use std::net::Ipv4Addr;
    use tempfile::TempDir;

    use doublezero_sdk::{
        create_new_pubkey_user, AccountType, ClientConfig, NetworkV4, User, UserCYOA, UserStatus,
        UserType,
    };
    use solana_sdk::pubkey::Pubkey;

    use crate::tests::utils::create_test_client;

    use super::*;

    const CONFIG_ENV_VAR: &str = "DOUBLEZERO_CONFIG_FILE";

    #[test]
    #[serial]
    fn test_cli_config_set_missing_keypair_file() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            let mut client = create_test_client();
            write_doublezero_config(&cfg).unwrap();

            client.expect_get_user().times(0);

            let mut output = Vec::new();
            let res = SetConfigCliCommand {
                env: Some(Environment::Devnet.to_string()),
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                force: false,
            }
            .execute(&client, &mut output);
            assert_eq!(
                res.err().unwrap().to_string(),
                "Failed to read keypair from file: No such file or directory (os error 2)\nPlease check that the keypair exists and is valid"
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

            let mut client = create_test_client();
            client.expect_get_user().times(0);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: None,
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                force: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();
            assert_eq!(output_str, "No arguments provided\n");
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_env_while_connected_pending() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            let keypair = create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut client = create_test_client();
            client
                .expect_get_user()
                .returning(move |_| {
                    Ok((
                        keypair.pubkey(),
                        new_test_user(|user| {
                            user.status = UserStatus::Pending;
                        }),
                    ))
                })
                .times(1);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: Some(Environment::Devnet.to_string()),
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                force: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();
            assert_eq!(output_str, "Cannot change network config while connected as user. Please disconnect first. Current status: pending\n");
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_env_while_connected_activated() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            let keypair = create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut client = create_test_client();
            client
                .expect_get_user()
                .returning(move |_| {
                    Ok((
                        keypair.pubkey(),
                        new_test_user(|user| {
                            user.status = UserStatus::Activated;
                        }),
                    ))
                })
                .times(1);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: Some(Environment::Devnet.to_string()),
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                force: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();
            assert_eq!(output_str, "Cannot change network config while connected as user. Please disconnect first. Current status: activated\n");
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_env_while_connected_suspended() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            let keypair = create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut client = create_test_client();
            client
                .expect_get_user()
                .returning(move |_| {
                    Ok((
                        keypair.pubkey(),
                        new_test_user(|user| {
                            user.status = UserStatus::Suspended;
                        }),
                    ))
                })
                .times(1);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: Some(Environment::Devnet.to_string()),
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                force: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();
            assert_eq!(output_str, "Cannot change network config while connected as user. Please disconnect first. Current status: suspended\n");
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_env_while_connected_deleting() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            let keypair = create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut client = create_test_client();
            client
                .expect_get_user()
                .returning(move |_| {
                    Ok((
                        keypair.pubkey(),
                        new_test_user(|user| {
                            user.status = UserStatus::Deleting;
                        }),
                    ))
                })
                .times(1);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: Some(Environment::Devnet.to_string()),
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                force: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();
            assert_eq!(output_str, "Cannot change network config while connected as user. Please disconnect first. Current status: deleting\n");
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_env_while_not_connected() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut client = create_test_client();
            client
                .expect_get_user()
                .returning(|_| Err(eyre::eyre!("User not found")))
                .times(1);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: Some(Environment::Devnet.to_string()),
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                force: false,
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
    fn test_cli_config_set_rpc_url_while_connected() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            let keypair = create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut client = create_test_client();
            client
                .expect_get_user()
                .returning(move |_| {
                    Ok((
                        keypair.pubkey(),
                        new_test_user(|user| {
                            user.status = UserStatus::Activated;
                        }),
                    ))
                })
                .times(1);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: None,
                url: Some("https://example.com".to_string()),
                ws: None,
                keypair: None,
                program_id: None,
                force: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();

            assert_eq!(output_str, "Cannot change network config while connected as user. Please disconnect first. Current status: activated\n");
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_rpc_url_while_not_connected() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut client = create_test_client();
            client
                .expect_get_user()
                .returning(|_| Err(eyre::eyre!("User not found")))
                .times(1);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: None,
                url: Some("https://example.com".to_string()),
                ws: None,
                keypair: None,
                program_id: None,
                force: false,
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
    fn test_cli_config_set_program_id_while_connected() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            let keypair = create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut client = create_test_client();
            client
                .expect_get_user()
                .returning(move |_| {
                    Ok((
                        keypair.pubkey(),
                        new_test_user(|user| {
                            user.status = UserStatus::Activated;
                        }),
                    ))
                })
                .times(1);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: None,
                url: None,
                ws: None,
                keypair: None,
                program_id: Some("1234567890".to_string()),
                force: false,
            }
            .execute(&client, &mut output)
            .unwrap();
            let output_str = String::from_utf8(output).unwrap();

            assert_eq!(output_str, "Cannot change network config while connected as user. Please disconnect first. Current status: activated\n");
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_program_id_while_not_connected() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut client = create_test_client();
            client
                .expect_get_user()
                .returning(|_| Err(eyre::eyre!("User not found")))
                .times(1);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: None,
                url: None,
                ws: None,
                keypair: None,
                program_id: Some("1234567890".to_string()),
                force: false,
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

    #[test]
    #[serial]
    fn test_cli_config_set_env_while_connected_force_activated() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            let keypair = create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut client = create_test_client();
            client
                .expect_get_user()
                .returning(move |_| {
                    Ok((
                        keypair.pubkey(),
                        new_test_user(|u| {
                            u.status = UserStatus::Activated;
                        }),
                    ))
                })
                .times(1);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: Some(Environment::Devnet.to_string()),
                url: None,
                ws: None,
                keypair: None,
                program_id: None,
                force: true,
            }
            .execute(&client, &mut output)
            .unwrap();

            let output_str = String::from_utf8(output).unwrap();
            assert!(
                output_str.contains("WARNING: Changing network config while connected as user because --force was provided. This will disconnect you from the current network. Current status: activated"),
                "Force warning not printed:\n{}",
                output_str
            );

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
    fn test_cli_config_set_rpc_url_while_connected_force() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            let keypair = create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut client = create_test_client();
            client
                .expect_get_user()
                .returning(move |_| {
                    Ok((
                        keypair.pubkey(),
                        new_test_user(|u| {
                            u.status = UserStatus::Activated;
                        }),
                    ))
                })
                .times(1);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: None,
                url: Some("https://example.com".to_string()),
                ws: None,
                keypair: None,
                program_id: None,
                force: true,
            }
            .execute(&client, &mut output)
            .unwrap();

            let output_str = String::from_utf8(output).unwrap();
            assert!(
                output_str.contains("WARNING: Changing network config while connected as user because --force was provided. This will disconnect you from the current network. Current status: activated"),
                "Force warning not printed:\n{}",
                output_str
            );

            let devnet_config = Environment::Devnet.config().unwrap();
            validate_config_output(
                &output_str,
                "https://example.com",
                &devnet_config.serviceability_program_id.to_string(),
            );
            // Note: when only URL is set, websocket is cleared and shown as "(computed)".
            // validate_config_output already ensures the WebSocket URL line exists.
        });
    }

    #[test]
    #[serial]
    fn test_cli_config_set_program_id_while_connected_force() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_var(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()), || {
            write_doublezero_config(&cfg).unwrap();
            let keypair = create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

            let mut client = create_test_client();
            client
                .expect_get_user()
                .returning(move |_| {
                    Ok((
                        keypair.pubkey(),
                        new_test_user(|u| {
                            u.status = UserStatus::Activated;
                        }),
                    ))
                })
                .times(1);

            let mut output = Vec::new();
            SetConfigCliCommand {
                env: None,
                url: None,
                ws: None,
                keypair: None,
                program_id: Some("1234567890".to_string()),
                force: true,
            }
            .execute(&client, &mut output)
            .unwrap();

            let output_str = String::from_utf8(output).unwrap();
            assert!(
                output_str.contains("WARNING: Changing network config while connected as user because --force was provided. This will disconnect you from the current network. Current status: activated"),
                "Force warning not printed:\n{}",
                output_str
            );

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
            address_labels: Default::default(),
        };

        mutator(&mut cfg);

        (tmp, config_path, cfg)
    }

    fn new_test_user(mutator: impl Fn(&mut User)) -> User {
        let mut user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            user_type: UserType::IBRL,
            tenant_pk: Pubkey::new_unique(),
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::None,
            client_ip: Ipv4Addr::new(127, 0, 0, 1),
            dz_ip: Ipv4Addr::new(127, 0, 0, 1),
            tunnel_id: 0,
            tunnel_net: NetworkV4::new(Ipv4Addr::new(127, 0, 0, 1), 32).unwrap(),
            status: UserStatus::Pending,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::new_unique(),
        };
        mutator(&mut user);
        user
    }

    fn validate_config_output(output_str: &str, expected_rpc_url: &str, expected_program_id: &str) {
        let lines: Vec<&str> = output_str.lines().collect();

        // Check RPC URL
        let rpc_line = lines.iter().find(|line| line.starts_with("RPC URL:"));
        assert!(rpc_line.is_some(), "RPC URL line not found");
        assert!(
            rpc_line.unwrap().contains(expected_rpc_url),
            "RPC URL mismatch. Expected: {}, Found: {:?}",
            expected_rpc_url,
            rpc_line
        );

        // Check Program ID
        let program_id_line = lines.iter().find(|line| line.starts_with("Program ID:"));
        assert!(program_id_line.is_some(), "Program ID line not found");
        assert!(
            program_id_line.unwrap().contains(expected_program_id),
            "Program ID mismatch. Expected: {}, Found: {:?}",
            expected_program_id,
            program_id_line
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
