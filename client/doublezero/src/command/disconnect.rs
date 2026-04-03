use std::time::Duration;

use backon::{BlockingRetryable, ExponentialBuilder};
use clap::{Args, ValueEnum};
use indicatif::ProgressBar;
use solana_sdk::pubkey::Pubkey;

use crate::{
    requirements::check_doublezero,
    servicecontroller::{ServiceController, ServiceControllerImpl},
};
use doublezero_cli::{
    doublezerocommand::CliCommand,
    helpers::init_command,
    requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON},
};

use doublezero_sdk::{
    commands::user::{delete::DeleteUserCommand, get::GetUserCommand, list::ListUserCommand},
    UserType,
};

#[allow(clippy::upper_case_acronyms)]
#[derive(Clone, Debug, ValueEnum)]
pub enum DzMode {
    IBRL,
    Multicast,
}

#[derive(Args, Debug)]
pub struct DecommissioningCliCommand {
    /// Device Pubkey or code to associate with the user
    #[arg(long)]
    pub device: Option<String>,
    /// [deprecated] Client IP address — ignored; set --client-ip on the daemon (doublezerod) instead
    #[arg(long)]
    pub client_ip: Option<String>,
    /// Allocate a new address for the user
    #[arg(short, long, default_value_t = false)]
    pub verbose: bool,
    #[arg(value_enum)]
    pub dz_mode: Option<DzMode>,
}

impl DecommissioningCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let spinner = init_command(4);
        let controller = ServiceControllerImpl::new(None);

        // Check that have your id.json
        check_requirements(client, Some(&spinner), CHECK_ID_JSON | CHECK_BALANCE)?;
        check_doublezero(&controller, client, Some(&spinner)).await?;
        // READY
        spinner.println("🔍  Decommissioning User");

        // Get client IP from daemon (same source as connect)
        let client_ip = super::helpers::resolve_client_ip(&controller).await?;
        spinner.println(format!("    Client IP: {client_ip}"));

        self.delete_users(client, client_ip, &spinner)?;

        // Wait for daemon to deprovision the tunnel(s)
        let user_type_filter: Option<&str> = match self.dz_mode {
            Some(DzMode::IBRL) => Some("IBRL"),
            Some(DzMode::Multicast) => Some("Multicast"),
            None => None,
        };
        match self
            .poll_for_daemon_deprovisioned(&controller, user_type_filter, &spinner)
            .await
        {
            Ok(()) => {
                spinner.println("    Daemon confirmed tunnel(s) removed");
            }
            Err(e) => {
                spinner.println(format!(
                    "    Daemon deprovisioning in progress (will complete automatically): {e}"
                ));
            }
        }

        spinner.println("✅  Deprovisioning Complete");
        spinner.finish_and_clear();

        Ok(())
    }

    /// Delete DZ Ledger users matching `client_ip`, skipping any that are
    /// owned by a different keypair (e.g. the shred oracle). Extracted from
    /// `execute` so it can be tested without filesystem/daemon dependencies.
    fn delete_users(
        &self,
        client: &dyn CliCommand,
        client_ip: std::net::Ipv4Addr,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        spinner.inc(1);
        spinner.set_message("deleting user account...");

        let users = client.list_user(ListUserCommand)?;

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

            // Skip oracle-owned users — only the oracle can delete them.
            // The oracle's orphan cleanup will remove the DZ Ledger user
            // automatically once the onchain seat becomes inactive.
            if user.owner != client.get_payer() {
                spinner.println(format!(
                    "⚠️  User {pubkey} is managed by an external service (owner: {}). \
                     It will be cleaned up automatically.",
                    user.owner,
                ));
                continue;
            }

            spinner.inc(1);
            println!("🔍  Deleting User Account for: {pubkey}");
            let res = client.delete_user(DeleteUserCommand { pubkey: *pubkey });
            match res {
                Ok(_) => {
                    spinner.println("🔍  User Account deleting...");
                }
                Err(e) => {
                    spinner.println(format!("🔍  Failed to delete user account: {e}"));
                }
            }

            self.poll_for_user_closed(client, pubkey, spinner)?;
        }

        Ok(())
    }

    async fn poll_for_daemon_deprovisioned<T: ServiceController>(
        &self,
        controller: &T,
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

            match controller.status().await {
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

    fn poll_for_user_closed(
        &self,
        client: &dyn CliCommand,
        user_pubkey: &Pubkey,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        spinner.set_message("Waiting for user deletion...");

        let builder = ExponentialBuilder::new()
            .with_max_times(8) // 1+2+4+8+16+32+32+32 = 127 seconds max
            .with_min_delay(Duration::from_secs(1))
            .with_max_delay(Duration::from_secs(32));

        let get_user = || {
            match client.get_user(GetUserCommand {
                pubkey: *user_pubkey,
            }) {
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
    use std::net::Ipv4Addr;

    use crate::servicecontroller::{
        DoubleZeroStatus, MockServiceController, StatusResponse, V2StatusResponse,
    };

    fn test_cmd() -> DecommissioningCliCommand {
        DecommissioningCliCommand {
            device: None,
            client_ip: None,
            verbose: false,
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
                last_session_update: Some(1234567890),
            },
            tunnel_name: Some("doublezero1".to_string()),
            tunnel_src: Some("1.2.3.4".to_string()),
            tunnel_dst: Some("5.6.7.8".to_string()),
            doublezero_ip: Some("10.0.0.1".to_string()),
            user_type: Some(user_type.to_string()),
        }
    }

    #[tokio::test]
    async fn test_poll_succeeds_with_synthetic_disconnected_entry() {
        // Daemon returns the synthetic "disconnected" entry (no user_type).
        // The poll should recognize this as "no active services" and succeed.
        let mut mock = MockServiceController::new();
        mock.expect_status()
            .returning(|| Ok(vec![disconnected_status()]));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd
            .poll_for_daemon_deprovisioned(&mock, None, &spinner)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_poll_succeeds_with_empty_status() {
        let mut mock = MockServiceController::new();
        mock.expect_status().returning(|| Ok(vec![]));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd
            .poll_for_daemon_deprovisioned(&mock, None, &spinner)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_poll_succeeds_when_daemon_unreachable() {
        // Daemon not reachable is OK for disconnect — treated as success.
        let mut mock = MockServiceController::new();
        mock.expect_status()
            .returning(|| Err(eyre::eyre!("connection refused")));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd
            .poll_for_daemon_deprovisioned(&mock, None, &spinner)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_poll_with_ibrl_filter_ignores_synthetic_entry() {
        // Synthetic disconnected entry has no user_type, so IBRL filter should
        // see no matching services and succeed immediately.
        let mut mock = MockServiceController::new();
        mock.expect_status()
            .returning(|| Ok(vec![disconnected_status()]));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd
            .poll_for_daemon_deprovisioned(&mock, Some("IBRL"), &spinner)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_poll_with_filter_waits_for_matching_service_only() {
        // Multicast is still active, but we're filtering for IBRL only.
        // Should succeed because no IBRL service is present.
        let mut mock = MockServiceController::new();
        mock.expect_status()
            .returning(|| Ok(vec![active_status("Multicast")]));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd
            .poll_for_daemon_deprovisioned(&mock, Some("IBRL"), &spinner)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_poll_ibrl_filter_matches_ibrl_with_allocated_ip() {
        // IBRLWithAllocatedIP should also match the "IBRL" filter.
        use std::sync::atomic::{AtomicU32, Ordering};
        let call_count = std::sync::Arc::new(AtomicU32::new(0));
        let cc = call_count.clone();

        let mut mock = MockServiceController::new();
        mock.expect_status().returning(move || {
            let n = cc.fetch_add(1, Ordering::SeqCst);
            if n == 0 {
                Ok(vec![active_status("IBRLWithAllocatedIP")])
            } else {
                Ok(vec![disconnected_status()])
            }
        });

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd
            .poll_for_daemon_deprovisioned(&mock, Some("IBRL"), &spinner)
            .await;
        assert!(result.is_ok());
        assert!(
            call_count.load(Ordering::SeqCst) >= 2,
            "should have polled at least twice"
        );
    }

    fn v2_status_with_ip(client_ip: &str) -> V2StatusResponse {
        V2StatusResponse {
            reconciler_enabled: true,
            client_ip: client_ip.to_string(),
            network: "mainnet".to_string(),
            services: vec![],
        }
    }

    #[tokio::test]
    async fn test_resolve_client_ip_success() {
        let mut mock = MockServiceController::new();
        mock.expect_v2_status()
            .returning(|| Ok(v2_status_with_ip("1.2.3.4")));

        let ip = crate::command::helpers::resolve_client_ip(&mock)
            .await
            .unwrap();
        assert_eq!(ip, Ipv4Addr::new(1, 2, 3, 4));
    }

    #[tokio::test]
    async fn test_resolve_client_ip_empty() {
        let mut mock = MockServiceController::new();
        mock.expect_v2_status()
            .returning(|| Ok(v2_status_with_ip("")));

        let err = crate::command::helpers::resolve_client_ip(&mock)
            .await
            .unwrap_err();
        assert!(
            err.to_string().contains("has not discovered its client IP"),
            "unexpected error: {err}"
        );
    }

    #[tokio::test]
    async fn test_resolve_client_ip_invalid() {
        let mut mock = MockServiceController::new();
        mock.expect_v2_status()
            .returning(|| Ok(v2_status_with_ip("not-an-ip")));

        let err = crate::command::helpers::resolve_client_ip(&mock)
            .await
            .unwrap_err();
        assert!(
            err.to_string().contains("invalid client IP 'not-an-ip'"),
            "unexpected error: {err}"
        );
    }

    #[tokio::test]
    async fn test_resolve_client_ip_daemon_unreachable() {
        let mut mock = MockServiceController::new();
        mock.expect_v2_status()
            .returning(|| Err(eyre::eyre!("connection refused")));

        let err = crate::command::helpers::resolve_client_ip(&mock)
            .await
            .unwrap_err();
        assert!(
            err.to_string().contains("connection refused"),
            "unexpected error: {err}"
        );
    }

    // --- delete_users tests ---

    use doublezero_cli::tests::utils::create_test_client;
    use doublezero_sdk::{AccountType, User, UserCYOA, UserStatus};
    use std::collections::HashMap;

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
        }
    }

    #[test]
    fn test_delete_users_skips_oracle_owned_user() {
        let mut client = create_test_client();
        let payer = client.get_payer();
        let oracle_key = Pubkey::new_unique();
        let ip = Ipv4Addr::new(10, 0, 0, 1);

        let user_pk = Pubkey::new_unique();
        let user = make_test_user(ip, oracle_key, UserType::Multicast);

        let mut users = HashMap::new();
        users.insert(user_pk, user);

        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));
        // delete_user should NOT be called for oracle-owned user.
        client.expect_delete_user().never();

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd.delete_users(&client, ip, &spinner);
        assert!(result.is_ok());

        // Verify payer != oracle_key to confirm the test is meaningful.
        assert_ne!(payer, oracle_key);
    }

    #[test]
    fn test_delete_users_deletes_self_owned_user() {
        let mut client = create_test_client();
        let payer = client.get_payer();
        let ip = Ipv4Addr::new(10, 0, 0, 1);

        let user_pk = Pubkey::new_unique();
        let user = make_test_user(ip, payer, UserType::Multicast);

        let mut users = HashMap::new();
        users.insert(user_pk, user);

        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));
        // delete_user SHOULD be called for self-owned user.
        client
            .expect_delete_user()
            .once()
            .returning(|_| Err(eyre::eyre!("simulated not found")));
        // get_user for poll_for_user_closed — return "not found" immediately.
        client
            .expect_get_user()
            .returning(|_| Err(eyre::eyre!("User not found")));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd.delete_users(&client, ip, &spinner);
        assert!(result.is_ok());
    }

    #[test]
    fn test_delete_users_mixed_ownership() {
        let mut client = create_test_client();
        let payer = client.get_payer();
        let oracle_key = Pubkey::new_unique();
        let ip = Ipv4Addr::new(10, 0, 0, 1);

        let self_owned_pk = Pubkey::new_unique();
        let self_owned = make_test_user(ip, payer, UserType::IBRL);

        let oracle_owned_pk = Pubkey::new_unique();
        let oracle_owned = make_test_user(ip, oracle_key, UserType::Multicast);

        let mut users = HashMap::new();
        users.insert(self_owned_pk, self_owned);
        users.insert(oracle_owned_pk, oracle_owned);

        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));
        // delete_user should be called exactly once (for the self-owned user only).
        client
            .expect_delete_user()
            .once()
            .returning(|_| Err(eyre::eyre!("simulated not found")));
        client
            .expect_get_user()
            .returning(|_| Err(eyre::eyre!("User not found")));

        let cmd = test_cmd();
        let spinner = hidden_spinner();
        let result = cmd.delete_users(&client, ip, &spinner);
        assert!(result.is_ok());
    }
}
