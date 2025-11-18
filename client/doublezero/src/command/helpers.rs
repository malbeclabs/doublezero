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
