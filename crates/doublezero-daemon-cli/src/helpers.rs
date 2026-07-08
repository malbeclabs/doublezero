//! Shared output helpers for daemon-control verbs.

use std::{io::Write, net::Ipv4Addr};

use tabled::{settings::Style, Table, Tabled};

use crate::client::DaemonClient;

/// Resolve the daemon-discovered client IP.
///
/// Groundwork for `connect`/`disconnect`/`multicast` (PRs 5–7); written against
/// the crate's `DaemonClient` trait. The binary retains its own copy until
/// those verbs migrate.
pub async fn resolve_client_ip<D: DaemonClient>(daemon: &D) -> eyre::Result<Ipv4Addr> {
    let v2_status = daemon.v2_status().await?;
    if v2_status.client_ip.is_empty() {
        return Err(eyre::eyre!(
            "Daemon has not discovered its client IP. Ensure the daemon is running \
             and has started up successfully, or set --client-ip on the daemon."
        ));
    }
    v2_status.client_ip.parse().map_err(|e| {
        eyre::eyre!(
            "Daemon returned invalid client IP '{}': {e}",
            v2_status.client_ip
        )
    })
}

/// Render a list of records as either pretty-printed JSON or a psql-style table.
pub fn show_output<T, W: Write>(data: Vec<T>, is_output_json: bool, out: &mut W) -> eyre::Result<()>
where
    T: serde::Serialize + Tabled,
{
    let output = if is_output_json {
        serde_json::to_string_pretty(&data)?
    } else {
        Table::new(data)
            .with(Style::psql().remove_horizontals())
            .to_string()
    };
    writeln!(out, "{output}")?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::client::{MockDaemonClient, V2StatusResponse};
    use doublezero_cli_core::testing::block_on;

    fn v2_status_with_client_ip(client_ip: &str) -> V2StatusResponse {
        V2StatusResponse {
            reconciler_enabled: true,
            client_ip: client_ip.to_string(),
            network: String::new(),
            services: vec![],
        }
    }

    #[test]
    fn test_resolve_client_ip_success() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon
                .expect_v2_status()
                .returning(|| Ok(v2_status_with_client_ip("1.2.3.4")));
            let ip = resolve_client_ip(&daemon).await.unwrap();
            assert_eq!(ip, Ipv4Addr::new(1, 2, 3, 4));
        });
    }

    #[test]
    fn test_resolve_client_ip_empty() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon
                .expect_v2_status()
                .returning(|| Ok(v2_status_with_client_ip("")));
            let err = resolve_client_ip(&daemon).await.unwrap_err();
            assert!(err.to_string().contains("has not discovered its client IP"));
        });
    }

    #[test]
    fn test_resolve_client_ip_invalid() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon
                .expect_v2_status()
                .returning(|| Ok(v2_status_with_client_ip("not-an-ip")));
            let err = resolve_client_ip(&daemon).await.unwrap_err();
            assert!(err.to_string().contains("invalid client IP"));
        });
    }

    #[test]
    fn test_resolve_client_ip_daemon_unreachable() {
        block_on(async {
            let mut daemon = MockDaemonClient::new();
            daemon
                .expect_v2_status()
                .returning(|| Err(eyre::eyre!("Unable to connect to doublezero daemon")));
            let err = resolve_client_ip(&daemon).await.unwrap_err();
            assert!(err.to_string().contains("Unable to connect"));
        });
    }
}
