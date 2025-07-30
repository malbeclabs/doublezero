use crate::settings::RpcSettings;
use anyhow::Result;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::commitment_config::CommitmentConfig;
use std::time::Duration;

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
        };

        let client = create_client(&settings).unwrap();
        assert_eq!(client.commitment().commitment, CommitmentLevel::Finalized);
    }
}
