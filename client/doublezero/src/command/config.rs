use crate::{
    requirements::check_doublezero,
    servicecontroller::{ServiceController, ServiceControllerImpl, UpdateConfigRequest},
};
use clap::{ArgGroup, Args};
use doublezero_cli::doublezerocommand::CliCommand;
use doublezero_sdk::read_doublezero_config;
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
    env: Option<String>,
    /// URL of the JSON RPC endpoint (devnet, testnet, mainnet, localhost)
    #[arg(long)]
    url: Option<String>,
    /// URL of the WS RPC endpoint (devnet, testnet, mainnet, localhost)
    #[arg(long)]
    ws: Option<String>,
    /// Keypair of the user
    #[arg(long)]
    keypair: Option<PathBuf>,
    /// Pubkey of the smart contract (devnet, testnet)
    #[arg(long)]
    program_id: Option<String>,
    /// Force the config to be set even if the user is connected
    #[arg(long)]
    force: bool,
}

impl SetConfigCliCommand {
    pub async fn execute<W: Write>(
        self,
        client: &dyn CliCommand,
        out: &mut W,
        socket_path: Option<String>,
    ) -> eyre::Result<()> {
        let updated = doublezero_cli::config::set::SetConfigCliCommand {
            env: self.env,
            url: self.url,
            ws: self.ws,
            keypair: self.keypair,
            program_id: self.program_id,
            force: self.force,
        }
        .execute(client, out)?;
        if !updated {
            return Ok(());
        }

        let (_, config) = read_doublezero_config()?;

        // Notify running daemon of the new config (best-effort)
        {
            let program_id = config
                .program_id
                .clone()
                .unwrap_or_else(|| doublezero_sdk::testnet::program_id::id().to_string());

            let controller = ServiceControllerImpl::new(socket_path);
            if let Err(e) = check_doublezero(&controller, None) {
                writeln!(out, "WARNING: could not update daemon config: {e}")?;
            } else {
                let req = UpdateConfigRequest {
                    ledger_rpc_url: config.json_rpc_url.clone(),
                    serviceability_program_id: program_id.clone(),
                };
                let result = controller.update_config(req).await;
                if let Err(e) = result {
                    writeln!(out, "WARNING: could not update daemon config: {e}")?;
                }
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use bytes::Bytes;
    use doublezero_cli::doublezerocommand::MockCliCommand;
    use doublezero_config::Environment;
    use doublezero_sdk::{convert_url_to_ws, write_doublezero_config};
    use futures::io;
    use http::{Method, StatusCode};
    use http_body_util::{BodyExt, Full};
    use hyper::{body::Incoming, service::service_fn, Request, Response};
    use hyper_util::rt::TokioIo;
    use serial_test::serial;
    use std::{net::Ipv4Addr, path::Path, sync::Arc};
    use tempfile::TempDir;
    use tokio::{
        net::UnixListener,
        sync::{Mutex, Notify},
        task::JoinHandle,
    };

    use doublezero_sdk::{
        create_new_pubkey_user, AccountType, ClientConfig, NetworkV4, User, UserCYOA, UserStatus,
        UserType,
    };
    use solana_sdk::{pubkey::Pubkey, signature::Signer};

    use super::*;

    const CONFIG_ENV_VAR: &str = "DOUBLEZERO_CONFIG_FILE";
    const SOCKET_ENV_VAR: &str = "DOUBLEZERO_SOCK";

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_missing_keypair_file() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_vars(
            [
                (CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap())),
            ],
            async || {
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
                .execute(&client, &mut output, None)
                .await;
                assert_eq!(
                res.err().unwrap().to_string(),
                "Failed to read keypair from file: No such file or directory (os error 2)\nPlease check that the keypair exists and is valid"
            );
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_no_flags() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_vars(
            [(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()))],
            async || {
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
                .execute(&client, &mut output, None)
                .await
                .unwrap();
                let output_str = String::from_utf8(output).unwrap();
                assert_eq!(output_str, "No arguments provided\n");
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_env_while_connected_pending() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_vars(
            [(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()))],
            async || {
                write_doublezero_config(&cfg).unwrap();
                let keypair =
                    create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

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
                .execute(&client, &mut output, None)
                .await
                .unwrap();
                let output_str = String::from_utf8(output).unwrap();
                assert_eq!(output_str, "Cannot change network config while connected as user. Please disconnect first. Current status: pending\n");
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_env_while_connected_activated() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_vars(
            [(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()))],
            async || {
                write_doublezero_config(&cfg).unwrap();
                let keypair =
                    create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

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
                .execute(&client, &mut output, None)
                .await
                .unwrap();
                let output_str = String::from_utf8(output).unwrap();
                assert_eq!(output_str, "Cannot change network config while connected as user. Please disconnect first. Current status: activated\n");
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_env_while_connected_suspended() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_vars(
            [(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()))],
            async || {
                write_doublezero_config(&cfg).unwrap();
                let keypair =
                    create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

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
                .execute(&client, &mut output, None)
                .await
                .unwrap();
                let output_str = String::from_utf8(output).unwrap();
                assert_eq!(output_str, "Cannot change network config while connected as user. Please disconnect first. Current status: suspended\n");
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_env_while_connected_deleting() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_vars(
            [(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()))],
            async || {
                write_doublezero_config(&cfg).unwrap();
                let keypair =
                    create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

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
                .execute(&client, &mut output, None)
                .await
                .unwrap();
                let output_str = String::from_utf8(output).unwrap();
                assert_eq!(output_str, "Cannot change network config while connected as user. Please disconnect first. Current status: deleting\n");
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_env_while_not_connected() {
        let (tmp, config_path, cfg) = new_test_config(|_cfg| {});
        let socket_path = tmp.path().join("doublezero.sock");

        temp_env::with_vars(
            [
                (CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap())),
                (SOCKET_ENV_VAR, Some(&socket_path.to_str().unwrap())),
            ],
            async || {
                write_doublezero_config(&cfg).unwrap();
                create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

                spawn_config_server_ok(socket_path.clone()).await.unwrap();

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
                .execute(
                    &client,
                    &mut output,
                    Some(socket_path.to_str().unwrap().to_string()),
                )
                .await
                .unwrap();
                let output_str = String::from_utf8(output).unwrap();

                let devnet_config = Environment::Devnet.config().unwrap();
                validate_config_output(
                    &output_str,
                    &devnet_config.ledger_public_rpc_url,
                    &devnet_config.serviceability_program_id.to_string(),
                );
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_rpc_url_while_connected() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_vars(
            [(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()))],
            async || {
                write_doublezero_config(&cfg).unwrap();
                let keypair =
                    create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

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
                .execute(&client, &mut output, None)
                .await
                .unwrap();
                let output_str = String::from_utf8(output).unwrap();

                assert_eq!(output_str, "Cannot change network config while connected as user. Please disconnect first. Current status: activated\n");
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_rpc_url_while_not_connected() {
        let (tmp, config_path, cfg) = new_test_config(|_cfg| {});
        let socket_path = tmp.path().join("doublezero.sock");

        temp_env::with_vars(
            [
                (CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap())),
                (SOCKET_ENV_VAR, Some(&socket_path.to_str().unwrap())),
            ],
            async || {
                write_doublezero_config(&cfg).unwrap();
                create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

                spawn_config_server_ok(socket_path.clone()).await.unwrap();

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
                .execute(
                    &client,
                    &mut output,
                    Some(socket_path.to_str().unwrap().to_string()),
                )
                .await
                .unwrap();
                let output_str = String::from_utf8(output).unwrap();

                let devnet_config = Environment::Devnet.config().unwrap();
                validate_config_output(
                    &output_str,
                    "https://example.com",
                    &devnet_config.serviceability_program_id.to_string(),
                );
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_program_id_while_connected() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_vars(
            [(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()))],
            async || {
                write_doublezero_config(&cfg).unwrap();
                let keypair =
                    create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

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
                .execute(&client, &mut output, None)
                .await
                .unwrap();
                let output_str = String::from_utf8(output).unwrap();

                assert_eq!(output_str, "Cannot change network config while connected as user. Please disconnect first. Current status: activated\n");
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_program_id_while_not_connected() {
        let (tmp, config_path, cfg) = new_test_config(|_cfg| {});
        let socket_path = tmp.path().join("doublezero.sock");

        temp_env::with_vars(
            [
                (CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap())),
                (SOCKET_ENV_VAR, Some(&socket_path.to_str().unwrap())),
            ],
            async || {
                write_doublezero_config(&cfg).unwrap();
                create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

                spawn_config_server_ok(socket_path.clone()).await.unwrap();

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
                .execute(
                    &client,
                    &mut output,
                    Some(socket_path.to_str().unwrap().to_string()),
                )
                .await
                .unwrap();
                let output_str = String::from_utf8(output).unwrap();

                let devnet_config = Environment::Devnet.config().unwrap();
                validate_config_output(
                    &output_str,
                    &devnet_config.ledger_public_rpc_url,
                    "1234567890",
                );
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_env_while_connected_force_activated() {
        let (tmp, config_path, cfg) = new_test_config(|_cfg| {});
        let socket_path = tmp.path().join("doublezero.sock");

        temp_env::with_vars(
            [
                (CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap())),
                (SOCKET_ENV_VAR, Some(&socket_path.to_str().unwrap())),
            ],
            async || {
                write_doublezero_config(&cfg).unwrap();
                let keypair =
                    create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

                spawn_config_server_ok(socket_path.clone()).await.unwrap();

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
                .execute(
                    &client,
                    &mut output,
                    Some(socket_path.to_str().unwrap().to_string()),
                )
                .await
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
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_rpc_url_while_connected_force() {
        let (tmp, config_path, cfg) = new_test_config(|_cfg| {});
        let socket_path = tmp.path().join("doublezero.sock");

        temp_env::with_vars(
            [
                (CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap())),
                (SOCKET_ENV_VAR, Some(&socket_path.to_str().unwrap())),
            ],
            async || {
                write_doublezero_config(&cfg).unwrap();
                let keypair =
                    create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

                spawn_config_server_ok(socket_path.clone()).await.unwrap();

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
                .execute(
                    &client,
                    &mut output,
                    Some(socket_path.to_str().unwrap().to_string()),
                )
                .await
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
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_program_id_while_connected_force() {
        let (tmp, config_path, cfg) = new_test_config(|_cfg| {});
        let socket_path = tmp.path().join("doublezero.sock");

        temp_env::with_vars(
            [
                (CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap())),
                (SOCKET_ENV_VAR, Some(&socket_path.to_str().unwrap())),
            ],
            async || {
                write_doublezero_config(&cfg).unwrap();
                let keypair =
                    create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

                spawn_config_server_ok(socket_path.clone()).await.unwrap();

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
                .execute(
                    &client,
                    &mut output,
                    Some(socket_path.to_str().unwrap().to_string()),
                )
                .await
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
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_invalid_flag_combo() {
        let (_tmp, config_path, cfg) = new_test_config(|_cfg| {});

        temp_env::with_vars(
            [(CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap()))],
            async || {
                write_doublezero_config(&cfg).unwrap();

                let mut client = create_test_client();
                client.expect_get_user().times(0);

                let mut output = Vec::new();
                SetConfigCliCommand {
                    env: Some(Environment::Devnet.to_string()),
                    url: Some("https://example.com".into()),
                    ws: None,
                    keypair: None,
                    program_id: None,
                    force: false,
                }
                .execute(&client, &mut output, None)
                .await
                .unwrap();

                let s = String::from_utf8(output).unwrap();
                assert!(s.starts_with("Invalid flag combination:"), "got: {s}");
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_ws_only_while_not_connected() {
        let (tmp, config_path, cfg) = new_test_config(|_cfg| {});
        let socket_path = tmp.path().join("doublezero.sock");

        temp_env::with_vars(
            [
                (CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap())),
                (SOCKET_ENV_VAR, Some(&socket_path.to_str().unwrap())),
            ],
            async || {
                write_doublezero_config(&cfg).unwrap();
                create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

                spawn_config_server_ok(socket_path.clone()).await.unwrap();

                let mut client = create_test_client();
                client
                    .expect_get_user()
                    .returning(|_| Err(eyre::eyre!("User not found")))
                    .times(1);

                let mut output = Vec::new();
                let new_ws = "wss://ws.example.com";
                SetConfigCliCommand {
                    env: None,
                    url: None,
                    ws: Some(new_ws.to_string()),
                    keypair: None,
                    program_id: None,
                    force: false,
                }
                .execute(
                    &client,
                    &mut output,
                    Some(socket_path.to_str().unwrap().to_string()),
                )
                .await
                .unwrap();

                let s = String::from_utf8(output).unwrap();
                // rpc should remain whatever was in cfg; ws should be exact match (not computed)
                assert!(
                    s.lines()
                        .any(|l| l.starts_with("RPC URL: ") && l.contains(&cfg.json_rpc_url)),
                    "{s}"
                );
                assert!(
                    s.lines().any(|l| l.starts_with("WebSocket URL: ")
                        && l.contains(new_ws)
                        && !l.contains("(computed)")),
                    "{s}"
                );
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_set_keypair_updates_path() {
        let (tmp, config_path, cfg) = new_test_config(|_cfg| {});
        let socket_path = tmp.path().join("doublezero.sock");
        let new_kp = tmp.path().join("new-id.json");

        temp_env::with_vars(
            [
                (CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap())),
                (SOCKET_ENV_VAR, Some(&socket_path.to_str().unwrap())),
            ],
            async || {
                write_doublezero_config(&cfg).unwrap();
                // must exist because execute() reads current keypair before changing config
                create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

                // also create the new keypair path file to look realistic
                std::fs::write(&new_kp, "[]").unwrap();

                spawn_config_server_ok(socket_path.clone()).await.unwrap();

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
                    keypair: Some(new_kp.clone()),
                    program_id: None,
                    force: false,
                }
                .execute(
                    &client,
                    &mut output,
                    Some(socket_path.to_str().unwrap().to_string()),
                )
                .await
                .unwrap();

                let s = String::from_utf8(output).unwrap();
                assert!(
                    s.lines()
                        .any(|l| l.starts_with("Keypair Path: ")
                            && l.contains(new_kp.to_str().unwrap())),
                    "{s}"
                );
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_daemon_update_warning_on_error() {
        let (tmp, config_path, cfg) = new_test_config(|_cfg| {});
        let socket_path = tmp.path().join("doublezero.sock");

        temp_env::with_vars(
            [
                (CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap())),
                (SOCKET_ENV_VAR, Some(&socket_path.to_str().unwrap())),
            ],
            async || {
                write_doublezero_config(&cfg).unwrap();
                create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

                spawn_config_server_error(
                    socket_path.clone(),
                    StatusCode::INTERNAL_SERVER_ERROR,
                    *b"oops",
                )
                .await
                .unwrap();

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
                .execute(
                    &client,
                    &mut output,
                    Some(socket_path.to_str().unwrap().to_string()),
                )
                .await
                .unwrap();

                let s = String::from_utf8(output).unwrap();
                assert!(
                    s.contains("WARNING: could not update daemon config:"),
                    "{s}"
                );
                assert!(s.contains("Config File:"), "{s}");
            },
        )
        .await;
    }

    #[tokio::test]
    #[serial]
    async fn test_cli_config_computed_ws_and_program_id() {
        let (tmp, config_path, cfg) = new_test_config(|c| {
            c.program_id = None;
        });
        let socket_path = tmp.path().join("doublezero.sock");

        temp_env::with_vars(
            [
                (CONFIG_ENV_VAR, Some(&config_path.to_str().unwrap())),
                (SOCKET_ENV_VAR, Some(&socket_path.to_str().unwrap())),
            ],
            async || {
                write_doublezero_config(&cfg).unwrap();
                create_new_pubkey_user(false, Some(cfg.keypair_path.clone())).unwrap();

                spawn_config_server_ok(socket_path.clone()).await.unwrap();

                let mut client = create_test_client();
                client
                    .expect_get_user()
                    .returning(|_| Err(eyre::eyre!("User not found")))
                    .times(1);

                let mut output = Vec::new();
                let new_rpc = "https://example.com";
                SetConfigCliCommand {
                    env: None,
                    url: Some(new_rpc.to_string()),
                    ws: None,
                    keypair: None,
                    program_id: None,
                    force: false,
                }
                .execute(
                    &client,
                    &mut output,
                    Some(socket_path.to_str().unwrap().to_string()),
                )
                .await
                .unwrap();

                let s = String::from_utf8(output).unwrap();
                let expected_ws = convert_url_to_ws(new_rpc).unwrap();
                let expected_pid = doublezero_sdk::testnet::program_id::id().to_string();

                assert!(
                    s.lines()
                        .any(|l| l.starts_with("RPC URL: ") && l.contains(new_rpc)),
                    "{s}"
                );
                assert!(
                    s.lines().any(|l| l.starts_with("WebSocket URL: ")
                        && l.contains(&expected_ws)
                        && l.contains("(computed)")),
                    "{s}"
                );
                assert!(
                    s.lines().any(|l| l.starts_with("Program ID: ")
                        && l.contains(&expected_pid)
                        && l.contains("(computed)")),
                    "{s}"
                );
            },
        )
        .await;
    }

    #[derive(Default, Clone)]
    pub struct Seen {
        body: Arc<Mutex<Vec<u8>>>,
        hit: Arc<Notify>,
    }

    pub async fn spawn_config_server<P, H, Fut>(
        socket_path: P,
        handler: H,
    ) -> io::Result<(JoinHandle<()>, Seen)>
    where
        P: AsRef<Path>,
        H: Fn(Request<Incoming>, Seen) -> Fut + Send + Sync + Clone + 'static,
        Fut: std::future::Future<Output = Result<Response<Full<Bytes>>, hyper::Error>>
            + Send
            + 'static,
    {
        // Clean up stale socket and bind
        let _ = tokio::fs::remove_file(&socket_path).await;
        let listener = UnixListener::bind(&socket_path)?;

        let seen = Seen::default();
        let seen_for_accept = seen.clone();

        let jh = tokio::spawn(async move {
            loop {
                let (stream, _) = match listener.accept().await {
                    Ok(x) => x,
                    Err(_) => break,
                };

                let io = TokioIo::new(stream);
                let svc_seen = seen_for_accept.clone();

                let h = handler.clone();

                let svc = service_fn(move |req| {
                    let s = svc_seen.clone();
                    h(req, s)
                });

                tokio::spawn(async move {
                    let _ = hyper::server::conn::http1::Builder::new()
                        .serve_connection(io, svc)
                        .await;
                });
            }
        });

        Ok((jh, seen))
    }

    pub async fn spawn_config_server_ok<P: AsRef<Path>>(
        socket_path: P,
    ) -> io::Result<(JoinHandle<()>, Seen)> {
        let ok_json_body = br#"{"status":"ok"}"#;
        spawn_config_server(socket_path, move |req, seen| async move {
            if req.method() == Method::PUT && req.uri().path() == "/config" {
                let bytes = req.into_body().collect().await.unwrap().to_bytes();
                {
                    let mut g = seen.body.lock().await;
                    g.extend_from_slice(&bytes);
                }
                seen.hit.notify_one();
                let resp = Response::builder()
                    .status(StatusCode::OK)
                    .header("content-type", "application/json")
                    .body(Full::new(Bytes::from_static(ok_json_body)))
                    .unwrap();
                Ok::<_, hyper::Error>(resp)
            } else {
                Ok(Response::builder()
                    .status(StatusCode::NOT_FOUND)
                    .body(Full::new(Bytes::new()))
                    .unwrap())
            }
        })
        .await
    }

    pub async fn spawn_config_server_error<P, const N: usize>(
        socket_path: P,
        status: StatusCode,
        body_bytes: [u8; N],
    ) -> std::io::Result<(tokio::task::JoinHandle<()>, Seen)>
    where
        P: AsRef<std::path::Path>,
    {
        // Pre-build a ref-counted body we can clone per request.
        // This avoids 'static and the borrow checker issue.
        let body = Bytes::from(body_bytes.to_vec());

        spawn_config_server(
            socket_path,
            move |_req: Request<hyper::body::Incoming>, _seen: Seen| {
                let body = body.clone(); // cheap; Bytes is Arc-backed
                async move {
                    Ok::<_, hyper::Error>(
                        Response::builder()
                            .status(status)
                            .header("content-type", "text/plain")
                            .body(Full::new(body))
                            .unwrap(),
                    )
                }
            },
        )
        .await
    }

    pub fn create_test_client() -> MockCliCommand {
        let mut client = MockCliCommand::new();
        // Payer
        let payer: Pubkey = Pubkey::from_str_const("DDddB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt");
        let program_id = Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");
        client.expect_get_payer().returning(move || payer);
        client.expect_get_balance().returning(|| Ok(10));
        client.expect_get_program_id().returning(move || program_id);

        client
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
