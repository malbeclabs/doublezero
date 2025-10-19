use backon::{ExponentialBuilder, Retryable};
use indicatif::{ProgressBar, ProgressStyle};
use reqwest::Client;
use solana_sdk::pubkey::Pubkey;
use std::{error::Error, str, str::FromStr, time::Duration};

pub fn parse_pubkey(input: &str) -> Option<Pubkey> {
    if input.len() < 43 || input.len() > 44 {
        return None;
    }

    Pubkey::from_str(input).ok()
}

pub fn print_error(e: eyre::Report) {
    eprintln!("\nError: {e:?}\n");
}

pub async fn get_public_ipv4() -> Result<String, Box<dyn Error>> {
    let client = Client::builder().timeout(Duration::from_secs(5)).build()?;
    let policy = default_retry_policy();
    get_public_ipv4_with_policy(&client, "https://ifconfig.me/ip", policy).await
}

fn default_retry_policy() -> ExponentialBuilder {
    ExponentialBuilder::default()
        .with_min_delay(Duration::from_millis(200))
        .with_max_delay(Duration::from_secs(3))
        .with_max_times(5)
}

pub async fn get_public_ipv4_with_policy(
    client: &Client,
    url: &str,
    policy: ExponentialBuilder,
) -> Result<String, Box<dyn Error>> {
    (|| async {
        let r = client
            .get(url)
            .header("User-Agent", "curl/8.0")
            .send()
            .await
            .map_err(|e| format!("Failed to send request: {e}"))?;
        if !r.status().is_success() {
            return Err(format!("Received non-success status: {}", r.status()).into());
        }
        let text = r
            .text()
            .await
            .map_err(|e| format!("Failed to read response body: {e}"))?;
        let ip = text.trim();
        if ip.is_empty() {
            return Err("Empty IP response from server".into());
        }
        Ok::<_, Box<dyn Error>>(ip.to_string())
    })
    .retry(&policy)
    .await
}

pub fn init_command(len: u64) -> ProgressBar {
    let spinner = ProgressBar::new(len);

    spinner.set_style(
        ProgressStyle::default_spinner()
            .template("{spinner:.green} [{elapsed_precise}] [{bar:40.cyan/blue}] {pos}/{len} {msg}")
            .expect("Failed to set template")
            .progress_chars("#>-")
            .tick_strings(&["-", "\\", "|", "/"]),
    );
    spinner.enable_steady_tick(Duration::from_millis(100));

    spinner.println("DoubleZero Service Provisioning");

    spinner
}

#[cfg(test)]
mod get_public_ipv4_tests {
    use super::*;
    use std::{
        io::{Read, Write},
        net::{Shutdown, TcpListener},
        thread,
        time::Duration,
    };

    fn start_http_server<F>(accepts: usize, handler: F) -> (String, thread::JoinHandle<()>)
    where
        F: Fn(usize, &mut std::net::TcpStream) + Send + 'static + Clone,
    {
        let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
        let addr = listener.local_addr().unwrap();
        let handle = thread::spawn(move || {
            for i in 0..accepts {
                let (mut s, _) = listener.accept().unwrap();
                let _ = s.set_read_timeout(Some(Duration::from_millis(200)));
                let mut buf = [0u8; 2048];
                let _ = s.read(&mut buf);
                handler.clone()(i, &mut s);
                let _ = s.flush();
                let _ = s.shutdown(Shutdown::Both);
            }
        });
        (format!("http://{}/ip", addr), handle)
    }

    fn fast_policy(times: usize) -> ExponentialBuilder {
        ExponentialBuilder::default()
            .with_min_delay(Duration::from_millis(10))
            .with_max_delay(Duration::from_millis(40))
            .with_max_times(times)
    }

    #[tokio::test]
    async fn returns_immediately_on_success() {
        let (url, handle) = start_http_server(1, |_i, s| {
            let body = b"203.0.113.5\n";
            let hdr = format!(
                "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
                body.len()
            );
            let _ = s.write_all(hdr.as_bytes());
            let _ = s.write_all(body);
        });

        let client = reqwest::Client::builder()
            .timeout(Duration::from_millis(500))
            .build()
            .unwrap();

        let ip = get_public_ipv4_with_policy(&client, &url, fast_policy(3))
            .await
            .unwrap();
        assert_eq!(ip, "203.0.113.5");
        handle.join().ok();
    }

    #[tokio::test]
    async fn retries_then_succeeds() {
        let (url, handle) = start_http_server(2, |i, s| {
            if i == 0 {
                let resp = b"HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\nConnection: close\r\n\r\n";
                let _ = s.write_all(resp);
            } else {
                let body = b"198.51.100.7\n";
                let hdr = format!(
                    "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
                    body.len()
                );
                let _ = s.write_all(hdr.as_bytes());
                let _ = s.write_all(body);
            }
        });

        let client = reqwest::Client::builder()
            .timeout(Duration::from_millis(500))
            .build()
            .unwrap();

        let ip = get_public_ipv4_with_policy(&client, &url, fast_policy(3))
            .await
            .unwrap();
        assert_eq!(ip, "198.51.100.7");
        handle.join().ok();
    }

    #[tokio::test]
    async fn fails_after_max_retries_on_permanent_500() {
        let attempts = 3;
        let (url, handle) = start_http_server(attempts, |_i, s| {
            let resp = b"HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\nConnection: close\r\n\r\n";
            let _ = s.write_all(resp);
        });

        let client = reqwest::Client::builder()
            .timeout(Duration::from_millis(500))
            .build()
            .unwrap();

        let err = get_public_ipv4_with_policy(&client, &url, fast_policy(attempts))
            .await
            .unwrap_err()
            .to_string();
        let ok = err.contains("Received non-success status")
            || err.contains("500")
            || err.to_lowercase().contains("connection")
            || err.to_lowercase().contains("request")
            || err.to_lowercase().contains("protocol");
        assert!(ok, "unexpected error: {err}");
        handle.join().ok();
    }

    #[tokio::test]
    async fn empty_body_eventually_errors() {
        let attempts = 3;
        let (url, handle) = start_http_server(attempts, |_i, s| {
            let resp = b"HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n";
            let _ = s.write_all(resp);
        });

        let client = reqwest::Client::builder()
            .timeout(Duration::from_millis(500))
            .build()
            .unwrap();

        let err = get_public_ipv4_with_policy(&client, &url, fast_policy(attempts))
            .await
            .unwrap_err()
            .to_string();
        let ok = err.contains("Empty IP response from server")
            || err.to_lowercase().contains("body")
            || err.to_lowercase().contains("eof")
            || err.to_lowercase().contains("connection");
        assert!(ok, "unexpected error: {err}");
        handle.join().ok();
    }
}
