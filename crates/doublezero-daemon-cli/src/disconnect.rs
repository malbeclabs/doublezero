//! `doublezero disconnect` — tear down the operator's DoubleZero user(s).
//!
//! Deletes matching onchain user accounts and waits for the daemon to
//! deprovision the corresponding tunnel(s). Progress animation is rendered on a
//! stderr spinner (transient UI); informational and result lines route through
//! the shared writer.

use std::{io::Write, time::Duration};

use backon::{BlockingRetryable, ExponentialBuilder};
use clap::{Args, ValueEnum};
use doublezero_cli_core::CliContext;
use doublezero_sdk::UserType;
use indicatif::ProgressBar;
use solana_sdk::pubkey::Pubkey;

use crate::{
    client::DaemonClient,
    helpers::{init_spinner, resolve_client_ip},
    ledger::LedgerClient,
    requirements::check_daemon,
};

#[allow(clippy::upper_case_acronyms)]
#[derive(Clone, Debug, ValueEnum)]
pub enum DzMode {
    IBRL,
    Multicast,
}

/// Disconnect your server from the doublezero network
#[derive(Args, Debug)]
pub struct Disconnect {
    /// Device Pubkey or code to associate with the user
    #[arg(long)]
    pub device: Option<String>,
    /// [deprecated] Client IP address — ignored; set --client-ip on the daemon (doublezerod) instead
    #[arg(long)]
    pub client_ip: Option<String>,
    /// Allocate a new address for the user
    #[arg(short, long, default_value_t = false)]
    pub verbose: bool,
    /// Skip waiting for the daemon to tear down the tunnel(s). The onchain user
    /// deletion is still awaited (and can block up to ~127s per user when the RPC
    /// is slow to reflect it); only the local tunnel-teardown wait is skipped, so
    /// traffic may still route over DoubleZero briefly after this returns.
    #[arg(long, default_value_t = false)]
    pub no_wait: bool,
    #[arg(value_enum)]
    pub dz_mode: Option<DzMode>,
}

impl Disconnect {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        _ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        let spinner = init_spinner(4);

        // Check that we have a keypair + balance, and that the daemon is
        // reachable and on the same environment as the client.
        ledger.check_requirements()?;
        check_daemon(daemon, ledger).await?;
        // READY
        writeln!(out, "⚡  Disconnecting...")?;

        // Get client IP from daemon (same source as connect)
        let client_ip = resolve_client_ip(daemon).await?;
        writeln!(out, "    Client IP: {client_ip}")?;

        let gstate = ledger.get_globalstate()?;
        self.delete_users(ledger, client_ip, gstate.feed_authority_pk, &spinner, out)?;

        if self.no_wait {
            writeln!(
                out,
                "    Onchain user deletion confirmed. The daemon will tear down the \
                 tunnel(s) shortly; traffic may still route over DoubleZero until then."
            )?;
            writeln!(
                out,
                "✅  Onchain deletion complete (tunnel teardown pending)"
            )?;
        } else {
            // Wait for daemon to deprovision the tunnel(s)
            let user_type_filter: Option<&str> = match self.dz_mode {
                Some(DzMode::IBRL) => Some("IBRL"),
                Some(DzMode::Multicast) => Some("Multicast"),
                None => None,
            };
            match self
                .poll_for_daemon_deprovisioned(daemon, user_type_filter, &spinner)
                .await
            {
                Ok(()) => {
                    writeln!(out, "    Tunnel confirmed removed")?;
                }
                Err(e) => {
                    writeln!(
                        out,
                        "    Daemon deprovisioning in progress (will complete automatically): {e}"
                    )?;
                }
            }
            writeln!(out, "✅  Deprovisioning Complete")?;
        }

        spinner.finish_and_clear();

        Ok(())
    }

    /// Delete DZ Ledger users matching `client_ip`, skipping any that are
    /// owned by a different keypair (e.g. the shred oracle). Extracted from
    /// `execute` so it can be tested without filesystem/daemon dependencies.
    fn delete_users<L: LedgerClient, W: Write>(
        &self,
        ledger: &L,
        client_ip: std::net::Ipv4Addr,
        feed_authority: Pubkey,
        spinner: &ProgressBar,
        out: &mut W,
    ) -> eyre::Result<()> {
        spinner.inc(1);
        spinner.set_message("deleting user account...");

        let users = ledger.list_user()?;
        let payer = ledger.get_payer();

        for (pubkey, user) in users.iter().filter(|(_, u)| u.client_ip == client_ip) {
            match self.dz_mode {
                Some(DzMode::IBRL) => {
                    if user.user_type != UserType::IBRL
                        && user.user_type != UserType::IBRLWithAllocatedIP
                    {
                        continue;
                    }
                }
                Some(DzMode::Multicast) => {
                    if user.user_type != UserType::Multicast {
                        continue;
                    }
                }
                None => {}
            }

            // Skip users owned by a different keypair — only the owner can delete them.
            if user.owner != payer {
                if user.owner == feed_authority {
                    // User is managed by the shred oracle.
                    // The validator must withdraw via doublezero-solana first.
                    writeln!(
                        out,
                        "⚠️  User {pubkey} is managed by the shred oracle (owner: {}). \
                         Use `doublezero-solana shreds withdraw` to disconnect.",
                        user.owner,
                    )?;
                } else {
                    writeln!(
                        out,
                        "⚠️  User {pubkey} is managed by an external service (owner: {}). \
                         It will be cleaned up automatically.",
                        user.owner,
                    )?;
                }
                continue;
            }

            spinner.inc(1);
            writeln!(out, "⚡  Removing account: {pubkey}")?;
            match ledger.delete_user(*pubkey) {
                Ok(_) => {
                    writeln!(out, "    Account deletion submitted")?;
                }
                Err(e) => {
                    writeln!(out, "❌  Failed to remove account: {e}")?;
                }
            }

            self.poll_for_user_closed(ledger, pubkey, spinner)?;
        }

        Ok(())
    }

    async fn poll_for_daemon_deprovisioned<D: DaemonClient>(
        &self,
        daemon: &D,
        user_type_filter: Option<&str>,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        let max_attempts = 12;
        let delay = Duration::from_secs(5);

        for attempt in 0..max_attempts {
            if attempt > 0 {
                spinner.set_message("waiting for tunnel removal...");
                tokio::time::sleep(delay).await;
            }

            match daemon.status().await {
                Ok(statuses) => {
                    // Filter to only active services (those with a user_type).
                    // The daemon returns a synthetic "disconnected" entry with no
                    // user_type when nothing is provisioned, so we must ignore it.
                    let active: Vec<_> =
                        statuses.iter().filter(|s| s.user_type.is_some()).collect();
                    let has_matching = match user_type_filter {
                        Some(filter) => active.iter().any(|s| {
                            s.user_type.as_ref().is_some_and(|ut| {
                                if filter == "IBRL" {
                                    ut == "IBRL" || ut == "IBRLWithAllocatedIP"
                                } else {
                                    ut == filter
                                }
                            })
                        }),
                        // No filter: wait for all active services to be gone
                        None => !active.is_empty(),
                    };
                    if !has_matching {
                        return Ok(());
                    }
                }
                Err(_) => {
                    // Daemon might not be reachable, that's OK for disconnect
                    return Ok(());
                }
            }
        }

        eyre::bail!("timed out waiting for daemon to remove tunnel")
    }

    fn poll_for_user_closed<L: LedgerClient>(
        &self,
        ledger: &L,
        user_pubkey: &Pubkey,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        spinner.set_message("Waiting for user deletion...");

        let builder = ExponentialBuilder::new()
            .with_max_times(8) // 1+2+4+8+16+32+32+32 = 127 seconds max
            .with_min_delay(Duration::from_secs(1))
            .with_max_delay(Duration::from_secs(32));

        let get_user = || {
            match ledger.get_user(*user_pubkey) {
                Ok(user) => Err(user), // User still exists, keep retrying
                Err(e) => {
                    Ok(if e.to_string().contains("User not found") {
                        Ok(()) // User deleted, stop retrying
                    } else {
                        Err(e) // Other error, keep retrying
                    })
                }
            }
        };

        let _ = get_user
            .retry(builder)
            .notify(|_, dur| {
                spinner.set_message(format!(
                    "Waiting for user deletion (checking in {dur:?})..."
                ))
            })
            .call()
            .map_err(|_| eyre::eyre!("Timeout waiting for user deletion"))?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{collections::HashMap, net::Ipv4Addr};

    use doublezero_cli_core::testing::block_on;
    use doublezero_sdk::{AccountType, GlobalState, User, UserCYOA, UserStatus};

    use crate::{
        client::{DoubleZeroStatus, MockDaemonClient, StatusResponse},
        ledger::MockLedgerClient,
    };

    fn test_cmd() -> Disconnect {
        Disconnect {
            device: None,
            client_ip: None,
            verbose: false,
            no_wait: false,
            dz_mode: None,
        }
    }

    fn hidden_spinner() -> ProgressBar {
        ProgressBar::hidden()
    }

    fn disconnected_status() -> StatusResponse {
        StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "disconnected".to_string(),
                last_session_update: None,
            },
            tunnel_name: None,
            tunnel_src: None,
            tunnel_dst: None,
            doublezero_ip: None,
            user_type: None,
        }
    }

    fn active_status(user_type: &str) -> StatusResponse {
        StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "established".to_string(),
                last_session_update: Some(1_234_567_890),
            },
            tunnel_name: Some("doublezero1".to_string()),
            tunnel_src: Some("1.2.3.4".to_string()),
            tunnel_dst: Some("5.6.7.8".to_string()),
            doublezero_ip: Some("10.0.0.1".to_string()),
            user_type: Some(user_type.to_string()),
        }
    }

    fn make_test_user(client_ip: Ipv4Addr, owner: Pubkey, user_type: UserType) -> User {
        User {
            account_type: AccountType::User,
            owner,
            index: 0,
            bump_seed: 0,
            user_type,
            tenant_pk: Pubkey::default(),
            device_pk: Pubkey::default(),
            cyoa_type: UserCYOA::None,
            client_ip,
            dz_ip: Ipv4Addr::UNSPECIFIED,
            tunnel_id: 0,
            tunnel_net: Default::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        }
    }

    // --- poll_for_daemon_deprovisioned tests ---

    #[test]
    fn test_poll_succeeds_with_synthetic_disconnected_entry() {
        // Daemon returns the synthetic "disconnected" entry (no user_type).
        // The poll should recognize this as "no active services" and succeed.
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon
                .expect_status()
                .returning(|| Ok(vec![disconnected_status()]));

            let cmd = test_cmd();
            let spinner = hidden_spinner();
            let result = cmd
                .poll_for_daemon_deprovisioned(&daemon, None, &spinner)
                .await;
            assert!(result.is_ok());
        });
    }

    #[test]
    fn test_poll_succeeds_with_empty_status() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon.expect_status().returning(|| Ok(vec![]));

            let cmd = test_cmd();
            let spinner = hidden_spinner();
            let result = cmd
                .poll_for_daemon_deprovisioned(&daemon, None, &spinner)
                .await;
            assert!(result.is_ok());
        });
    }

    #[test]
    fn test_poll_succeeds_when_daemon_unreachable() {
        // Daemon not reachable is OK for disconnect — treated as success.
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon
                .expect_status()
                .returning(|| Err(eyre::eyre!("connection refused")));

            let cmd = test_cmd();
            let spinner = hidden_spinner();
            let result = cmd
                .poll_for_daemon_deprovisioned(&daemon, None, &spinner)
                .await;
            assert!(result.is_ok());
        });
    }

    #[test]
    fn test_poll_with_ibrl_filter_ignores_synthetic_entry() {
        // Synthetic disconnected entry has no user_type, so IBRL filter should
        // see no matching services and succeed immediately.
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon
                .expect_status()
                .returning(|| Ok(vec![disconnected_status()]));

            let cmd = test_cmd();
            let spinner = hidden_spinner();
            let result = cmd
                .poll_for_daemon_deprovisioned(&daemon, Some("IBRL"), &spinner)
                .await;
            assert!(result.is_ok());
        });
    }

    #[test]
    fn test_poll_with_filter_waits_for_matching_service_only() {
        // Multicast is still active, but we're filtering for IBRL only.
        // Should succeed because no IBRL service is present.
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon
                .expect_status()
                .returning(|| Ok(vec![active_status("Multicast")]));

            let cmd = test_cmd();
            let spinner = hidden_spinner();
            let result = cmd
                .poll_for_daemon_deprovisioned(&daemon, Some("IBRL"), &spinner)
                .await;
            assert!(result.is_ok());
        });
    }

    // --- delete_users tests ---

    #[test]
    fn test_delete_users_skips_oracle_owned_user() {
        let mut ledger = MockLedgerClient::new();
        let payer = Pubkey::new_unique();
        let oracle_key = Pubkey::new_unique();
        let ip = Ipv4Addr::new(10, 0, 0, 1);

        let user_pk = Pubkey::new_unique();
        let user = make_test_user(ip, oracle_key, UserType::Multicast);

        let mut users = HashMap::new();
        users.insert(user_pk, user);

        ledger.expect_get_payer().return_const(payer);
        ledger
            .expect_list_user()
            .returning(move || Ok(users.clone()));
        // delete_user should NOT be called for oracle-owned user.
        ledger.expect_delete_user().never();

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let mut out = Vec::new();
        let result = cmd.delete_users(&ledger, ip, oracle_key, &spinner, &mut out);
        assert!(result.is_ok());

        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("managed by the shred oracle"));
        assert!(!output.contains("Removing account"));

        // Verify payer != oracle_key to confirm the test is meaningful.
        assert_ne!(payer, oracle_key);
    }

    #[test]
    fn test_delete_users_deletes_self_owned_user() {
        let mut ledger = MockLedgerClient::new();
        let payer = Pubkey::new_unique();
        let feed_authority = Pubkey::new_unique();
        let ip = Ipv4Addr::new(10, 0, 0, 1);

        let user_pk = Pubkey::new_unique();
        let user = make_test_user(ip, payer, UserType::Multicast);

        let mut users = HashMap::new();
        users.insert(user_pk, user);

        ledger.expect_get_payer().return_const(payer);
        ledger
            .expect_list_user()
            .returning(move || Ok(users.clone()));
        // delete_user SHOULD be called for self-owned user.
        ledger
            .expect_delete_user()
            .once()
            .returning(|_| Err(eyre::eyre!("simulated not found")));
        // get_user for poll_for_user_closed — return "not found" immediately.
        ledger
            .expect_get_user()
            .returning(|_| Err(eyre::eyre!("User not found")));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let mut out = Vec::new();
        let result = cmd.delete_users(&ledger, ip, feed_authority, &spinner, &mut out);
        assert!(result.is_ok());

        let output = String::from_utf8(out).unwrap();
        assert!(output.contains(&format!("Removing account: {user_pk}")));
    }

    #[test]
    fn test_delete_users_skips_externally_owned_non_oracle_user() {
        let mut ledger = MockLedgerClient::new();
        let payer = Pubkey::new_unique();
        let external_owner = Pubkey::new_unique();
        let feed_authority = Pubkey::new_unique();
        let ip = Ipv4Addr::new(10, 0, 0, 1);

        let user_pk = Pubkey::new_unique();
        let user = make_test_user(ip, external_owner, UserType::IBRL);

        let mut users = HashMap::new();
        users.insert(user_pk, user);

        ledger.expect_get_payer().return_const(payer);
        ledger
            .expect_list_user()
            .returning(move || Ok(users.clone()));
        // delete_user should NOT be called for externally-owned user.
        ledger.expect_delete_user().never();

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let mut out = Vec::new();
        let result = cmd.delete_users(&ledger, ip, feed_authority, &spinner, &mut out);
        assert!(result.is_ok());

        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("managed by an external service"));

        assert_ne!(payer, external_owner);
        assert_ne!(external_owner, feed_authority);
    }

    #[test]
    fn test_delete_users_mixed_ownership() {
        let mut ledger = MockLedgerClient::new();
        let payer = Pubkey::new_unique();
        let oracle_key = Pubkey::new_unique();
        let ip = Ipv4Addr::new(10, 0, 0, 1);

        let self_owned_pk = Pubkey::new_unique();
        let self_owned = make_test_user(ip, payer, UserType::IBRL);

        let oracle_owned_pk = Pubkey::new_unique();
        let oracle_owned = make_test_user(ip, oracle_key, UserType::Multicast);

        let mut users = HashMap::new();
        users.insert(self_owned_pk, self_owned);
        users.insert(oracle_owned_pk, oracle_owned);

        ledger.expect_get_payer().return_const(payer);
        ledger
            .expect_list_user()
            .returning(move || Ok(users.clone()));
        // delete_user should be called exactly once (for the self-owned user only).
        ledger
            .expect_delete_user()
            .once()
            .returning(|_| Err(eyre::eyre!("simulated not found")));
        ledger
            .expect_get_user()
            .returning(|_| Err(eyre::eyre!("User not found")));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let mut out = Vec::new();
        let result = cmd.delete_users(&ledger, ip, oracle_key, &spinner, &mut out);
        assert!(result.is_ok());
    }

    // --- execute tests: user-exists and user-doesn't-exist decommissioning ---

    fn setup_daemon_checks(daemon: &mut MockDaemonClient) {
        daemon.expect_daemon_check().return_const(true);
        daemon.expect_daemon_can_open().return_const(true);
        daemon
            .expect_get_env()
            .returning(|| Ok(doublezero_config::Environment::default()));
    }

    /// User exists and is self-owned: it is deleted, and the no_wait path
    /// reports onchain deletion complete.
    #[test]
    fn test_execute_user_exists() {
        block_on(async {
            let payer = Pubkey::new_unique();
            let ip = Ipv4Addr::new(1, 2, 3, 4);
            let user_pk = Pubkey::new_unique();
            let user = make_test_user(ip, payer, UserType::IBRL);
            let mut users = HashMap::new();
            users.insert(user_pk, user);

            let mut daemon = MockDaemonClient::new();
            setup_daemon_checks(&mut daemon);
            daemon.expect_v2_status().returning(move || {
                Ok(crate::client::V2StatusResponse {
                    reconciler_enabled: true,
                    client_ip: "1.2.3.4".to_string(),
                    network: String::new(),
                    services: vec![],
                })
            });

            let mut ledger = MockLedgerClient::new();
            ledger
                .expect_get_environment()
                .returning(doublezero_config::Environment::default);
            ledger.expect_check_requirements().returning(|| Ok(()));
            ledger.expect_get_payer().return_const(payer);
            ledger
                .expect_get_globalstate()
                .returning(|| Ok(GlobalState::default()));
            ledger
                .expect_list_user()
                .returning(move || Ok(users.clone()));
            ledger.expect_delete_user().once().returning(|_| Ok(()));
            ledger
                .expect_get_user()
                .returning(|_| Err(eyre::eyre!("User not found")));

            let ctx = doublezero_cli_core::testing::cli_context_default_for_tests();
            let mut out = Vec::new();
            let cmd = Disconnect {
                no_wait: true,
                ..test_cmd()
            };
            let result = cmd.execute(&ctx, &daemon, &ledger, &mut out).await;
            assert!(result.is_ok(), "{result:?}");

            let output = String::from_utf8(out).unwrap();
            assert!(output.contains("Disconnecting"));
            assert!(output.contains(&format!("Removing account: {user_pk}")));
            assert!(output.contains("Account deletion submitted"));
            assert!(output.contains("Onchain deletion complete"));
        });
    }

    /// No user matches the client IP: nothing is deleted, and the command still
    /// completes successfully.
    #[test]
    fn test_execute_user_does_not_exist() {
        block_on(async {
            let payer = Pubkey::new_unique();

            let mut daemon = MockDaemonClient::new();
            setup_daemon_checks(&mut daemon);
            daemon.expect_v2_status().returning(move || {
                Ok(crate::client::V2StatusResponse {
                    reconciler_enabled: true,
                    client_ip: "1.2.3.4".to_string(),
                    network: String::new(),
                    services: vec![],
                })
            });

            let mut ledger = MockLedgerClient::new();
            ledger
                .expect_get_environment()
                .returning(doublezero_config::Environment::default);
            ledger.expect_check_requirements().returning(|| Ok(()));
            ledger.expect_get_payer().return_const(payer);
            ledger
                .expect_get_globalstate()
                .returning(|| Ok(GlobalState::default()));
            // Empty user set — no matching users to delete.
            ledger.expect_list_user().returning(|| Ok(HashMap::new()));
            ledger.expect_delete_user().never();

            let ctx = doublezero_cli_core::testing::cli_context_default_for_tests();
            let mut out = Vec::new();
            let cmd = Disconnect {
                no_wait: true,
                ..test_cmd()
            };
            let result = cmd.execute(&ctx, &daemon, &ledger, &mut out).await;
            assert!(result.is_ok(), "{result:?}");

            let output = String::from_utf8(out).unwrap();
            assert!(output.contains("Disconnecting"));
            assert!(!output.contains("Removing account"));
            assert!(output.contains("Onchain deletion complete"));
        });
    }
}
