use backon::{BlockingRetryable, ExponentialBuilder};
use doublezero_cli::helpers::get_public_ipv4;
use indicatif::ProgressBar;
use std::{net::Ipv4Addr, time::Duration};

pub async fn look_for_ip(
    client_ip: &Option<String>,
    spinner: &ProgressBar,
) -> eyre::Result<(Ipv4Addr, String)> {
    look_for_ip_with(client_ip, spinner, get_public_ipv4).await
}

async fn look_for_ip_with(
    client_ip: &Option<String>,
    spinner: &ProgressBar,
    ip_fetch_func: impl FnMut() -> eyre::Result<String, Box<dyn std::error::Error>>,
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

    if let Some(reason) = is_bgp_martian(ip) {
        eyre::bail!(
            "Client IP {} is a BGP martian address ({}). A publicly routable IP address is required.",
            ip,
            reason
        );
    }

    Ok((ip, client_ip.to_string()))
}

/// Returns `Some(reason)` if the given IPv4 address is a BGP martian (should
/// never appear as a source in the global routing table), or `None` if the
/// address is publicly routable.
fn is_bgp_martian(ip: Ipv4Addr) -> Option<&'static str> {
    let octets = ip.octets();

    // 0.0.0.0/8 — "this" network (RFC 791)
    if octets[0] == 0 {
        return Some("\"this\" network (0.0.0.0/8)");
    }
    // 10.0.0.0/8 — private (RFC 1918)
    if octets[0] == 10 {
        return Some("private (10.0.0.0/8)");
    }
    // 100.64.0.0/10 — shared / CGNAT (RFC 6598)
    if octets[0] == 100 && (octets[1] & 0xC0) == 64 {
        return Some("shared/CGNAT (100.64.0.0/10)");
    }
    // 127.0.0.0/8 — loopback (RFC 1122)
    if octets[0] == 127 {
        return Some("loopback (127.0.0.0/8)");
    }
    // 169.254.0.0/16 — link-local (RFC 3927)
    if octets[0] == 169 && octets[1] == 254 {
        return Some("link-local (169.254.0.0/16)");
    }
    // 172.16.0.0/12 — private (RFC 1918)
    if octets[0] == 172 && (octets[1] & 0xF0) == 16 {
        return Some("private (172.16.0.0/12)");
    }
    // 192.0.0.0/24 — IETF protocol assignments (RFC 6890)
    if octets[0] == 192 && octets[1] == 0 && octets[2] == 0 {
        return Some("IETF protocol assignments (192.0.0.0/24)");
    }
    // 192.0.2.0/24 — documentation TEST-NET-1 (RFC 5737)
    if octets[0] == 192 && octets[1] == 0 && octets[2] == 2 {
        return Some("documentation TEST-NET-1 (192.0.2.0/24)");
    }
    // 192.168.0.0/16 — private (RFC 1918)
    if octets[0] == 192 && octets[1] == 168 {
        return Some("private (192.168.0.0/16)");
    }
    // 198.18.0.0/15 — benchmarking (RFC 2544) — allowed for DZ use
    // 198.51.100.0/24 — documentation TEST-NET-2 (RFC 5737)
    if octets[0] == 198 && octets[1] == 51 && octets[2] == 100 {
        return Some("documentation TEST-NET-2 (198.51.100.0/24)");
    }
    // 203.0.113.0/24 — documentation TEST-NET-3 (RFC 5737)
    if octets[0] == 203 && octets[1] == 0 && octets[2] == 113 {
        return Some("documentation TEST-NET-3 (203.0.113.0/24)");
    }
    // 224.0.0.0/4 — multicast (RFC 5771)
    if (octets[0] & 0xF0) == 224 {
        return Some("multicast (224.0.0.0/4)");
    }
    // 240.0.0.0/4 — reserved for future use (RFC 1112) + broadcast
    if octets[0] >= 240 {
        return Some("reserved (240.0.0.0/4)");
    }

    None
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
        let client_ip = Some("203.0.114.1".to_string());
        let spinner = dummy_spinner();

        let mock_ip_fetch = || Ok("8.8.8.8".to_string());

        let result = look_for_ip_with(&client_ip, &spinner, mock_ip_fetch)
            .await
            .unwrap();
        assert_eq!(result.0, Ipv4Addr::new(203, 0, 114, 1));
        assert_eq!(result.1, "203.0.114.1");
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
    async fn test_look_for_ip_rejects_martian_address() {
        let spinner = dummy_spinner();
        let mock_ip_fetch = || Ok("8.8.8.8".to_string());

        // Private (RFC 1918)
        let result =
            look_for_ip_with(&Some("192.168.1.1".to_string()), &spinner, mock_ip_fetch).await;
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("BGP martian"));
    }

    #[tokio::test]
    async fn test_look_for_ip_rejects_auto_detected_martian() {
        let spinner = dummy_spinner();
        // Simulate auto-detection returning a private IP
        let mock_ip_fetch = || Ok("10.0.0.1".to_string());

        let result = look_for_ip_with(&None, &spinner, mock_ip_fetch).await;
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("BGP martian"));
    }

    #[test]
    fn test_is_bgp_martian() {
        // Martian addresses
        assert!(is_bgp_martian(Ipv4Addr::new(0, 0, 0, 0)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(0, 1, 2, 3)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(10, 0, 0, 1)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(10, 255, 255, 255)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(100, 64, 0, 1)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(100, 127, 255, 255)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(127, 0, 0, 1)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(169, 254, 1, 1)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(172, 16, 0, 1)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(172, 31, 255, 255)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(192, 0, 0, 1)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(192, 0, 2, 1)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(192, 168, 0, 1)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(198, 51, 100, 1)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(203, 0, 113, 1)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(224, 0, 0, 1)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(239, 255, 255, 255)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(240, 0, 0, 1)).is_some());
        assert!(is_bgp_martian(Ipv4Addr::new(255, 255, 255, 255)).is_some());

        // Non-martian (publicly routable)
        assert!(is_bgp_martian(Ipv4Addr::new(1, 1, 1, 1)).is_none());
        assert!(is_bgp_martian(Ipv4Addr::new(8, 8, 8, 8)).is_none());
        assert!(is_bgp_martian(Ipv4Addr::new(100, 63, 255, 255)).is_none()); // just below CGNAT
        assert!(is_bgp_martian(Ipv4Addr::new(100, 128, 0, 0)).is_none()); // just above CGNAT
        assert!(is_bgp_martian(Ipv4Addr::new(172, 15, 255, 255)).is_none()); // just below 172.16/12
        assert!(is_bgp_martian(Ipv4Addr::new(172, 32, 0, 0)).is_none()); // just above 172.16/12
        assert!(is_bgp_martian(Ipv4Addr::new(198, 18, 0, 1)).is_none()); // benchmarking — allowed
        assert!(is_bgp_martian(Ipv4Addr::new(198, 19, 0, 1)).is_none()); // benchmarking — allowed
        assert!(is_bgp_martian(Ipv4Addr::new(203, 0, 114, 1)).is_none()); // just above TEST-NET-3
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
