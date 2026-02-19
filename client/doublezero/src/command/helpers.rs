use backon::{BlockingRetryable, ExponentialBuilder};
use doublezero_cli::helpers::get_public_ipv4;
use indicatif::ProgressBar;
use std::{
    net::{Ipv4Addr, UdpSocket},
    time::Duration,
};

pub async fn look_for_ip(
    client_ip: &Option<String>,
    spinner: &ProgressBar,
) -> eyre::Result<(Ipv4Addr, String)> {
    look_for_ip_with(client_ip, spinner, discover_public_ip).await
}

/// Discovers the client's public IP address.
///
/// Resolution order:
///  1. Ask the kernel for the default route's source address (via a UDP
///     connect to 8.8.8.8 — no packets are sent). If the source is a
///     publicly routable IPv4 address, use it.
///  2. Fall back to querying ifconfig.me/ip.
///
/// This matches the daemon's discovery logic so both always agree on the IP.
fn discover_public_ip() -> Result<String, Box<dyn std::error::Error>> {
    // Try default route source hint first.
    if let Ok(ip) = discover_from_default_route() {
        return Ok(ip.to_string());
    }

    // Fall back to external discovery.
    get_public_ipv4()
}

/// Performs a kernel route lookup by binding a UDP socket to a well-known
/// public IP. The local address chosen by the kernel reflects the default
/// route's source hint. Returns the IP only if it's publicly routable.
fn discover_from_default_route() -> Result<Ipv4Addr, Box<dyn std::error::Error>> {
    let socket = UdpSocket::bind("0.0.0.0:0")?;
    socket.connect("8.8.8.8:80")?;
    let local_addr = socket.local_addr()?;
    let ip = match local_addr.ip() {
        std::net::IpAddr::V4(ip) => ip,
        _ => return Err("default route source is not IPv4".into()),
    };
    if ip.is_loopback()
        || ip.is_private()
        || ip.is_link_local()
        || ip.is_multicast()
        || ip.is_broadcast()
        || ip.is_unspecified()
    {
        return Err(format!("default route source {ip} is not publicly routable").into());
    }
    Ok(ip)
}

async fn look_for_ip_with(
    client_ip: &Option<String>,
    spinner: &ProgressBar,
    ip_fetch_func: impl FnMut() -> Result<String, Box<dyn std::error::Error>>,
) -> eyre::Result<(Ipv4Addr, String)> {
    let client_ip = match client_ip {
        Some(ip) => {
            spinner.println(format!("    Using Public IP: {ip}"));
            ip
        }
        None => &{
            spinner.set_message("Discovering your public IP...");

            let builder = ExponentialBuilder::new()
                .with_max_times(3)
                .with_min_delay(Duration::from_secs(1));

            let ipv4 = ip_fetch_func
                .retry(builder)
                .notify(|_, dur| {
                    spinner.set_message(format!("Fetching IP Address (checking in {dur:?})..."))
                })
                .call()
                .map_err(|_| eyre::eyre!("Timeout waiting for IP address"));

            match ipv4 {
                Ok(ip) => {
                    spinner.println(format!("Public IP detected: {ip} - If you want to use a different IP, you can specify it with `--client-ip x.x.x.x`"));
                    ip
                }
                Err(e) => {
                    eyre::bail!("Could not detect your public IP. Please provide the `--client-ip` argument. ({})", e.to_string());
                }
            }
        },
    };

    let ip: Ipv4Addr = client_ip
        .parse()
        .map_err(|_| eyre::eyre!("Invalid IPv4 address format: {}", client_ip))?;

    Ok((ip, client_ip.to_string()))
}

#[cfg(test)]
mod tests {
    use super::*;
    use indicatif::ProgressBar;
    use std::{error::Error, net::Ipv4Addr};

    // Dummy ProgressBar for testing
    fn dummy_spinner() -> ProgressBar {
        ProgressBar::hidden()
    }

    #[tokio::test]
    async fn test_look_for_ip_with_client_ip() {
        let client_ip = Some("192.168.1.1".to_string());
        let spinner = dummy_spinner();

        let mock_ip_fetch = || Ok("8.8.8.8".to_string());

        let result = look_for_ip_with(&client_ip, &spinner, mock_ip_fetch)
            .await
            .unwrap();
        assert_eq!(result.0, Ipv4Addr::new(192, 168, 1, 1));
        assert_eq!(result.1, "192.168.1.1");
    }

    #[tokio::test]
    async fn test_look_for_ip_with_fetch_success() {
        let client_ip = None;
        let spinner = dummy_spinner();

        let mock_ip_fetch = || Ok("8.8.8.8".to_string());

        let result = look_for_ip_with(&client_ip, &spinner, mock_ip_fetch)
            .await
            .unwrap();
        assert_eq!(result.0, Ipv4Addr::new(8, 8, 8, 8));
        assert_eq!(result.1, "8.8.8.8");
    }

    #[tokio::test]
    async fn test_look_for_ip_with_fetch_failure() {
        let client_ip = None;
        let spinner = dummy_spinner();

        let mock_ip_fetch =
            || Err::<String, Box<dyn Error>>(Box::<dyn Error>::from("fetch failed"));

        let result = look_for_ip_with(&client_ip, &spinner, mock_ip_fetch).await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_look_for_ip_with_invalid_ip() {
        let client_ip = Some("not_an_ip".to_string());
        let spinner = dummy_spinner();

        let mock_ip_fetch = || Ok("8.8.8.8".to_string());

        let result = look_for_ip_with(&client_ip, &spinner, mock_ip_fetch).await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_look_for_ip_with_retry_success() {
        let client_ip = None;
        let spinner = dummy_spinner();

        let mut first = true;
        let mock_ip_fetch = move || {
            if first {
                first = false;
                Err(Box::<dyn std::error::Error>::from("fetch failed"))
            } else {
                Ok("8.8.4.4".to_string())
            }
        };

        let result = look_for_ip_with(&client_ip, &spinner, mock_ip_fetch)
            .await
            .unwrap();
        assert_eq!(result.0, Ipv4Addr::new(8, 8, 4, 4));
        assert_eq!(result.1, "8.8.4.4");
    }
}
