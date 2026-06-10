use std::net::Ipv4Addr;

use crate::servicecontroller::ServiceController;

pub async fn resolve_client_ip<T: ServiceController>(controller: &T) -> eyre::Result<Ipv4Addr> {
    let v2_status = controller.v2_status().await?;
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
