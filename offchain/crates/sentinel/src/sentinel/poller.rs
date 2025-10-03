use crate::{
    AccessId, Result,
    client::{doublezero_ledger::DzRpcClient, solana::SolRpcClient},
    error::rpc_with_retry,
    sentinel::ValidatorVerifier,
};
use doublezero_passport::instruction::AccessMode;
use retainer::Cache;
use solana_sdk::{pubkey::Pubkey, signature::Keypair};
use std::{
    net::Ipv4Addr,
    sync::Arc,
    time::{Duration, Instant},
};
use tokio::time::interval;
use tokio_util::sync::CancellationToken;
use tracing::{error, info};
use url::Url;

// cache ttl: 5 minutes
const CACHE_TTL: Duration = Duration::from_secs(300);
// cache monitoring interval, every 60s
const CACHE_MONITOR_INTERVAL: Duration = Duration::from_secs(60);

pub struct PollingSentinel {
    dz_rpc_client: DzRpcClient,
    sol_rpc_client: SolRpcClient,
    processed_cache: Arc<Cache<Pubkey, Instant>>,
    poll_interval: Duration,
    previous_leader_epochs: u8,
}

impl PollingSentinel {
    pub async fn new(
        dz_rpc: Url,
        sol_rpc: Url,
        keypair: Arc<Keypair>,
        serviceability_id: Pubkey,
        poll_interval_secs: u64,
        previous_leader_epochs: u8,
    ) -> Result<Self> {
        // Create cache with automatic background cleanup
        let processed_cache = Arc::new(Cache::new());

        // Spawn background task to monitor cache
        // every 60s, removing entries older than 300s (5 intervals of 60s)
        let cache_clone = processed_cache.clone();
        tokio::spawn(async move {
            cache_clone.monitor(5, 0.25, CACHE_MONITOR_INTERVAL).await;
        });

        Ok(Self {
            dz_rpc_client: DzRpcClient::new(dz_rpc, keypair.clone(), serviceability_id),
            sol_rpc_client: SolRpcClient::new(sol_rpc, keypair),
            processed_cache,
            poll_interval: Duration::from_secs(poll_interval_secs),
            previous_leader_epochs,
        })
    }

    pub async fn run(&mut self, shutdown_listener: CancellationToken) -> Result<()> {
        let mut poll_timer = interval(self.poll_interval);

        loop {
            tokio::select! {
                biased;
                _ = shutdown_listener.cancelled() => {
                    info!("shutdown signal received");
                    break;
                }
                _ = poll_timer.tick() => {
                    let access_ids = match rpc_with_retry(
                        || async {
                            self.sol_rpc_client.get_access_requests().await
                        },
                        "get_access_requests",
                    ).await {
                        Ok(ids) => ids,
                        Err(err) => {
                            error!(?err, "failed to fetch access requests after retries; will retry in next cycle");
                            metrics::counter!("doublezero_sentinel_poll_failed").increment(1);
                            continue;
                        }
                    };

                    // Filter out already-processed requests
                    let mut new_requests = Vec::new();
                    let mut duplicate_count = 0;

                    for access_id in access_ids {
                        if let Some(processed_at) = self.processed_cache.get(&access_id.request_pda).await {
                            duplicate_count += 1;
                            let age = processed_at.elapsed();
                            metrics::counter!("doublezero_sentinel_duplicate_request_filtered").increment(1);
                            metrics::histogram!("doublezero_sentinel_duplicate_age_seconds").record(age.as_secs_f64());
                        } else {
                            new_requests.push(access_id);
                        }
                    }

                    if duplicate_count > 0 {
                        info!(
                            duplicates = duplicate_count,
                            "filtered out recently processed requests"
                        );
                    }

                    info!(count = new_requests.len(), "processing unhandled access requests");

                    for access_id in new_requests {
                        let request_pda = access_id.request_pda;
                        match self.handle_access_request(access_id).await {
                            Ok(_) => {
                                // Only cache after successful processing
                                self.processed_cache.insert(request_pda, Instant::now(), CACHE_TTL).await;
                            }
                            Err(err) => {
                                error!(?err, "error encountered validating network access request; will retry on next poll");
                                // Don't cache failures - allow retry on next poll cycle
                            }
                        }
                    }
                }
            }
        }

        Ok(())
    }

    async fn handle_access_request(&self, access_id: AccessId) -> Result<()> {
        let service_key = match &access_id.mode {
            AccessMode::SolanaValidator(a) => a.service_key,
            AccessMode::SolanaValidatorWithBackupIds { attestation, .. } => attestation.service_key,
        };

        info!(%service_key, request_pda = %access_id.request_pda, "handling access request");

        let validator_ips = self.verify_qualifiers(&access_id.mode).await?;

        if !validator_ips.is_empty() {
            // Issue access passes for all validators (primary + backups)
            for (validator_id, validator_ip) in validator_ips {
                rpc_with_retry(
                    || async {
                        self.dz_rpc_client
                            .issue_access_pass(&service_key, &validator_ip, &validator_id)
                            .await
                    },
                    "issue_access_pass",
                )
                .await?;
                info!(%validator_id, %validator_ip, user = %service_key, "access pass issued");
            }

            let signature = rpc_with_retry(
                || async {
                    self.sol_rpc_client
                        .grant_access(&access_id.request_pda, &access_id.rent_beneficiary_key)
                        .await
                },
                "grant_access",
            )
            .await?;
            info!(%signature, user = %service_key, "access request granted");
            metrics::counter!("doublezero_sentinel_access_granted").increment(1);
        } else {
            let signature = rpc_with_retry(
                || async {
                    self.sol_rpc_client
                        .deny_access(&access_id.request_pda)
                        .await
                },
                "deny_access",
            )
            .await?;
            info!(%signature, user = %service_key, "access request denied");
            metrics::counter!("doublezero_sentinel_access_denied").increment(1);
        }

        Ok(())
    }

    async fn verify_qualifiers(&self, access_mode: &AccessMode) -> Result<Vec<(Pubkey, Ipv4Addr)>> {
        let verifier = ValidatorVerifier::new(&self.sol_rpc_client, self.previous_leader_epochs);
        verifier.verify_qualifiers(access_mode).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_passport::instruction::SolanaValidatorAttestation;
    use solana_sdk::pubkey::Pubkey;

    #[tokio::test]
    async fn test_cache_prevents_duplicate_processing() {
        // Test that cache correctly identifies already-processed requests
        let cache = Cache::new();
        let request_pda = Pubkey::new_unique();

        // Initially, request should not be in cache
        assert!(
            cache.get(&request_pda).await.is_none(),
            "new request should not be in cache"
        );

        // Insert request into cache
        cache.insert(request_pda, Instant::now(), CACHE_TTL).await;

        // Now it should be found
        assert!(
            cache.get(&request_pda).await.is_some(),
            "request should be in cache after insertion"
        );
    }

    #[tokio::test]
    async fn test_cache_ttl_expiration() {
        // Test that cache entries expire after TTL
        let cache = Cache::new();
        let request_pda = Pubkey::new_unique();

        // Use very short TTL for testing (100ms)
        let short_ttl = Duration::from_millis(100);
        cache.insert(request_pda, Instant::now(), short_ttl).await;

        // Should be in cache immediately
        assert!(
            cache.get(&request_pda).await.is_some(),
            "request should be in cache immediately after insertion"
        );

        // Wait for TTL to expire plus buffer
        tokio::time::sleep(Duration::from_millis(150)).await;

        // Should be expired and removed
        assert!(
            cache.get(&request_pda).await.is_none(),
            "request should be removed from cache after TTL expires"
        );
    }

    #[tokio::test]
    async fn test_cache_handles_multiple_requests() {
        // Test that cache can track multiple different requests
        let cache = Cache::new();
        let pda1 = Pubkey::new_unique();
        let pda2 = Pubkey::new_unique();
        let pda3 = Pubkey::new_unique();

        // Insert multiple requests
        cache.insert(pda1, Instant::now(), CACHE_TTL).await;
        cache.insert(pda2, Instant::now(), CACHE_TTL).await;

        // Both should be in cache
        assert!(cache.get(&pda1).await.is_some());
        assert!(cache.get(&pda2).await.is_some());

        // pda3 not inserted, should not be in cache
        assert!(cache.get(&pda3).await.is_none());
    }

    #[tokio::test]
    async fn test_verify_qualifiers_signature_verify_error_returns_empty() {
        // Build a real PollingSentinel; it won't hit network because we short-circuit on signature
        let keypair = Arc::new(Keypair::new());
        let dz_rpc = Url::parse("http://127.0.0.1:1234").unwrap();
        let sol_rpc = Url::parse("http://127.0.0.1:1235").unwrap();
        let serviceability_id = Pubkey::new_unique();

        let sentinel = PollingSentinel {
            dz_rpc_client: DzRpcClient::new(dz_rpc, keypair.clone(), serviceability_id),
            sol_rpc_client: SolRpcClient::new(sol_rpc, keypair),
            processed_cache: Arc::new(Cache::new()),
            poll_interval: Duration::from_secs(15),
            previous_leader_epochs: 0,
        };

        // Invalid signature -> verify_access_request(...) should return Error::SignatureVerify
        let attestation = SolanaValidatorAttestation {
            validator_id: Pubkey::new_unique(),
            service_key: Pubkey::new_unique(),
            ed25519_signature: [0u8; 64],
        };
        let access_mode = AccessMode::SolanaValidator(attestation);

        let result = sentinel.verify_qualifiers(&access_mode).await.unwrap();
        assert!(
            result.is_empty(),
            "expected empty vec when signature verification fails"
        );
    }
}
