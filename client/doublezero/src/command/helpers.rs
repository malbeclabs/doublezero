use std::net::Ipv4Addr;

use crate::servicecontroller::{ServiceController, V2StatusResponse};

pub async fn resolve_client_ip<T: ServiceController>(controller: &T) -> eyre::Result<Ipv4Addr> {
    let v2_status = controller.v2_status().await?;
    parse_client_ip(&v2_status)
}

/// Like [`resolve_client_ip`], but also returns the daemon's `behind_nat`
/// signal (true when the auto-discovered default-route source was a private
/// RFC1918 address). Fetches `/v2/status` once.
pub async fn resolve_client_ip_with_nat<T: ServiceController>(
    controller: &T,
) -> eyre::Result<(Ipv4Addr, bool)> {
    let v2_status = controller.v2_status().await?;
    let ip = parse_client_ip(&v2_status)?;
    Ok((ip, v2_status.behind_nat))
}

fn parse_client_ip(v2_status: &V2StatusResponse) -> eyre::Result<Ipv4Addr> {
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
