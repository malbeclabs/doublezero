use crate::settings::RpcSettings;
use anyhow::{Result, bail};
use solana_client::{client_error::ClientError, rpc_client::RpcClient};
use solana_sdk::commitment_config::CommitmentConfig;
use std::time::Duration;
use tracing::{debug, warn};

/// Create an RPC client with the given settings
pub fn create_client(settings: &RpcSettings) -> Result<RpcClient> {
    let commitment = match settings.commitment.as_str() {
        "processed" => CommitmentConfig::processed(),
        "confirmed" => CommitmentConfig::confirmed(),
        "finalized" => CommitmentConfig::finalized(),
        _ => CommitmentConfig::finalized(),
    };

    let timeout = Duration::from_secs(settings.timeout_secs);

    Ok(RpcClient::new_with_timeout_and_commitment(
        settings.url.clone(),
        timeout,
        commitment,
    ))
}

/// Retry an RPC operation with exponential backoff
pub async fn with_retry<T, F, Fut>(
    operation: F,
    max_retries: u32,
    operation_name: &str,
) -> Result<T>
where
    F: Fn() -> Fut,
    Fut: std::future::Future<Output = Result<T, ClientError>>,
{
    let mut retry_count = 0;
    let mut backoff = Duration::from_millis(100);

    loop {
        match operation().await {
            Ok(result) => {
                if retry_count > 0 {
                    debug!("{} succeeded after {} retries", operation_name, retry_count);
                }
                return Ok(result);
            }
            Err(e) => {
                retry_count += 1;
                if retry_count > max_retries {
                    bail!(
                        "{} failed after {} retries: {}",
                        operation_name,
                        max_retries,
                        e
                    );
                }

                warn!(
                    "{} failed (attempt {}/{}): {}. Retrying in {:?}",
                    operation_name, retry_count, max_retries, e, backoff
                );

                tokio::time::sleep(backoff).await;
                backoff = backoff.saturating_mul(2).min(Duration::from_secs(30));
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_sdk::commitment_config::CommitmentLevel;

    #[test]
    fn test_create_client_with_finalized_commitment() {
        let settings = RpcSettings {
            url: "https://api.mainnet-beta.solana.com".to_string(),
            commitment: "finalized".to_string(),
            timeout_secs: 60,
            max_retries: 3,
        };

        let client = create_client(&settings).unwrap();
        assert_eq!(client.commitment().commitment, CommitmentLevel::Finalized);
    }

    #[test]
    fn test_create_client_with_confirmed_commitment() {
        let settings = RpcSettings {
            url: "https://api.mainnet-beta.solana.com".to_string(),
            commitment: "confirmed".to_string(),
            timeout_secs: 60,
            max_retries: 3,
        };

        let client = create_client(&settings).unwrap();
        assert_eq!(client.commitment().commitment, CommitmentLevel::Confirmed);
    }

    #[test]
    fn test_create_client_with_processed_commitment() {
        let settings = RpcSettings {
            url: "https://api.mainnet-beta.solana.com".to_string(),
            commitment: "processed".to_string(),
            timeout_secs: 60,
            max_retries: 3,
        };

        let client = create_client(&settings).unwrap();
        assert_eq!(client.commitment().commitment, CommitmentLevel::Processed);
    }

    #[test]
    fn test_create_client_defaults_to_finalized() {
        let settings = RpcSettings {
            url: "https://api.mainnet-beta.solana.com".to_string(),
            commitment: "invalid".to_string(),
            timeout_secs: 60,
            max_retries: 3,
        };

        let client = create_client(&settings).unwrap();
        assert_eq!(client.commitment().commitment, CommitmentLevel::Finalized);
    }
}
