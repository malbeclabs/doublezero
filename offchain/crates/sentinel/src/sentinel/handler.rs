use crate::{
    AccessIds, Error, Result,
    client::{doublezero_ledger::DzRpcClient, solana::SolRpcClient},
    error::rpc_with_retry,
    verify_access_request,
};
use doublezero_passport::instruction::AccessMode;
use solana_sdk::{
    pubkey::Pubkey,
    signature::{Keypair, Signature},
};
use std::{net::Ipv4Addr, sync::Arc, time::Duration};
use tokio::{sync::mpsc::UnboundedReceiver, time::interval};
use tokio_util::sync::CancellationToken;
use tracing::{error, info};
use url::Url;

const BACKFILL_TIMER: Duration = Duration::from_secs(60 * 60);

pub struct Sentinel {
    dz_rpc_client: DzRpcClient,
    sol_rpc_client: SolRpcClient,
    rx: UnboundedReceiver<Signature>,
    #[allow(dead_code)]
    previous_leader_epochs: u8,
}

impl Sentinel {
    pub async fn new(
        dz_rpc: Url,
        sol_rpc: Url,
        keypair: Arc<Keypair>,
        serviceability_id: Pubkey,
        rx: UnboundedReceiver<Signature>,
        previous_leader_epochs: u8,
    ) -> Result<Self> {
        Ok(Self {
            dz_rpc_client: DzRpcClient::new(dz_rpc, keypair.clone(), serviceability_id),
            sol_rpc_client: SolRpcClient::new(sol_rpc, keypair),
            rx,
            previous_leader_epochs,
        })
    }

    pub async fn run(&mut self, shutdown_listener: CancellationToken) -> Result<()> {
        let mut backfill_timer = interval(BACKFILL_TIMER);

        loop {
            tokio::select! {
                biased;
                _ = shutdown_listener.cancelled() => break,
                _ = backfill_timer.tick() => {
                    info!("fetching outstanding access requests");
                    let access_ids = self.sol_rpc_client.get_access_requests().await?;

                    info!(count = access_ids.len(), "processing unhandled access requests");

                    for ids in access_ids {
                        if let Err(err) = self.handle_access_request(ids).await {
                            error!(?err, "error encountered validating network access request");
                        }
                    }
                }
                event = self.rx.recv() => {
                    if let Some(signature) = event {
                        info!(%signature, "received access request txn");
                        let access_ids = rpc_with_retry(
                            || async {
                                self.sol_rpc_client.get_access_request_from_signature(signature).await
                            },
                            "get_access_request_from_signature",
                        ).await?;
                        if let Err(err) = self.handle_access_request(access_ids).await {
                            error!(?err, "error encountered validating network access request");
                        }
                    }
                }
            }
        }

        Ok(())
    }

    async fn handle_access_request(&self, access_ids: AccessIds) -> Result<()> {
        // Get the service key.
        let service_key = access_ids.mode.service_key();

        let validator_ips = self.verify_qualifiers(&access_ids.mode).await?;

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
                        .grant_access(&access_ids.request_pda, &access_ids.rent_beneficiary_key)
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
                        .deny_access(&access_ids.request_pda)
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

    /// Check that a validator is in the leader schedule
    async fn check_validator_in_leader_schedule(&self, validator_id: &Pubkey) -> Result<bool> {
        rpc_with_retry(
            || async {
                self.sol_rpc_client
                    .check_leader_schedule(validator_id, self.previous_leader_epochs)
                    .await
            },
            "check_leader_schedule",
        )
        .await
    }

    /// Get and validate a validator's IP from gossip
    async fn get_and_validate_validator_ip(
        &self,
        validator_id: &Pubkey,
    ) -> Result<Option<Ipv4Addr>> {
        rpc_with_retry(
            || async { self.sol_rpc_client.get_validator_ip(validator_id).await },
            "get_validator_ip",
        )
        .await
    }

    async fn verify_qualifiers(&self, access_mode: &AccessMode) -> Result<Vec<(Pubkey, Ipv4Addr)>> {
        // Return early if sig verification fails
        let validator_id = match verify_access_request(access_mode) {
            Ok(v) => v,
            Err(e @ Error::SignatureVerify) => {
                return {
                    info!(error = %e, "signature verification failed");
                    Ok(vec![])
                };
            }
            Err(e) => return Err(e),
        };
        info!(%validator_id, "Validator passed signature validation");

        // Extract attestation and backup IDs
        let backup_ids = match access_mode {
            AccessMode::SolanaValidator(_) => None,
            AccessMode::SolanaValidatorWithBackupIds { backup_ids, .. } => Some(backup_ids),
        };

        // Check primary validator is in leader schedule
        if !self
            .check_validator_in_leader_schedule(&validator_id)
            .await?
        {
            info!(
                %validator_id,
                "Validator failed leader schedule qualification"
            );
            return Ok(vec![]);
        }

        // Get primary validator IP immediately after leader schedule check
        let validator_ip = match self.get_and_validate_validator_ip(&validator_id).await? {
            Some(ip) => ip,
            None => {
                info!(
                    %validator_id,
                    "Validator failed gossip protocol ip qualification"
                );
                return Ok(Default::default());
            }
        };

        // Collect all validated IPs (starting with primary)
        let mut ips = vec![(validator_id, validator_ip)];

        // If we have backup IDs, verify they are NOT in leader schedule but ARE in gossip
        if let Some(backup_ids) = backup_ids {
            for backup_id in backup_ids {
                // Backup should NOT be in leader schedule
                if self.check_validator_in_leader_schedule(backup_id).await? {
                    info!(
                        %backup_id,
                        "Backup validator is in leader schedule (should not be)"
                    );
                    return Ok(Default::default());
                }

                // Check backup ID is in gossip and store IP
                match self.get_and_validate_validator_ip(backup_id).await? {
                    Some(ip) => {
                        ips.push((*backup_id, ip));
                    }
                    None => {
                        info!(
                            %backup_id,
                            "Backup validator not found in gossip"
                        );
                        return Ok(Default::default());
                    }
                }
            }
        }

        Ok(ips)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_passport::instruction::SolanaValidatorAttestation;
    use solana_sdk::pubkey::Pubkey;
    use std::net::Ipv4Addr;
    use tokio::sync::mpsc::unbounded_channel;

    // Mock implementations for testing
    struct MockSentinel {
        leader_schedule_responses: std::collections::HashMap<Pubkey, bool>,
        gossip_ip_responses: std::collections::HashMap<Pubkey, Option<Ipv4Addr>>,
    }

    impl MockSentinel {
        fn new() -> Self {
            Self {
                leader_schedule_responses: std::collections::HashMap::new(),
                gossip_ip_responses: std::collections::HashMap::new(),
            }
        }

        fn set_leader_schedule(&mut self, pubkey: Pubkey, in_schedule: bool) {
            self.leader_schedule_responses.insert(pubkey, in_schedule);
        }

        fn set_gossip_ip(&mut self, pubkey: Pubkey, ip: Option<Ipv4Addr>) {
            self.gossip_ip_responses.insert(pubkey, ip);
        }

        async fn check_validator_in_leader_schedule(&self, validator_id: &Pubkey) -> Result<bool> {
            Ok(self
                .leader_schedule_responses
                .get(validator_id)
                .copied()
                .unwrap_or(false))
        }

        async fn check_validator_not_in_leader_schedule(
            &self,
            validator_id: &Pubkey,
        ) -> Result<bool> {
            Ok(!self
                .leader_schedule_responses
                .get(validator_id)
                .copied()
                .unwrap_or(false))
        }

        async fn get_and_validate_validator_ip(
            &self,
            validator_id: &Pubkey,
        ) -> Result<Option<Ipv4Addr>> {
            Ok(self
                .gossip_ip_responses
                .get(validator_id)
                .copied()
                .flatten())
        }

        async fn verify_qualifiers_mock(
            &self,
            access_mode: &AccessMode,
        ) -> Result<Vec<(Pubkey, Ipv4Addr)>> {
            // Note: Skipping signature verification in mock, but in real code it's done first

            // Extract attestation and backup IDs
            let (attestation, backup_ids) = match access_mode {
                AccessMode::SolanaValidator(attestation) => (attestation, None),
                AccessMode::SolanaValidatorWithBackupIds {
                    attestation,
                    backup_ids,
                } => (attestation, Some(backup_ids)),
            };

            // Check primary validator is in leader schedule
            if !self
                .check_validator_in_leader_schedule(&attestation.validator_id)
                .await?
            {
                return Ok(vec![]);
            }

            // Get primary validator IP immediately after leader schedule check
            let validator_ip = match self
                .get_and_validate_validator_ip(&attestation.validator_id)
                .await?
            {
                Some(ip) => ip,
                None => return Ok(vec![]),
            };

            // Collect all validated IPs (starting with primary)
            let mut ips = vec![(attestation.validator_id, validator_ip)];

            // If we have backup IDs, verify they are NOT in leader schedule but ARE in gossip
            if let Some(backup_ids) = backup_ids {
                // Store backup IPs to avoid duplicate calls
                let mut backup_ips = Vec::new();

                for backup_id in backup_ids {
                    // Backup should NOT be in leader schedule
                    if !self
                        .check_validator_not_in_leader_schedule(backup_id)
                        .await?
                    {
                        return Ok(vec![]);
                    }

                    // Check backup ID is in gossip and store IP
                    match self.get_and_validate_validator_ip(backup_id).await? {
                        Some(backup_ip) => {
                            backup_ips.push((*backup_id, backup_ip));
                        }
                        None => {
                            return Ok(vec![]);
                        }
                    }
                }

                // Add all validated backup IPs to the result
                ips.extend(backup_ips);
            }

            Ok(ips)
        }
    }

    #[tokio::test]
    async fn test_verify_qualifiers_solana_validator_success() {
        let mut mock = MockSentinel::new();

        let validator_id = Pubkey::new_unique();
        let service_key = Pubkey::new_unique();
        let validator_ip = Ipv4Addr::new(192, 168, 1, 1);

        // Setup mock responses
        mock.set_leader_schedule(validator_id, true);
        mock.set_gossip_ip(validator_id, Some(validator_ip));

        let attestation = SolanaValidatorAttestation {
            validator_id,
            service_key,
            ed25519_signature: [0; 64],
        };

        let access_mode = AccessMode::SolanaValidator(attestation);
        let result = mock.verify_qualifiers_mock(&access_mode).await.unwrap();

        assert_eq!(result.len(), 1);
        assert_eq!(result[0].0, validator_id);
        assert_eq!(result[0].1, validator_ip);
    }

    #[tokio::test]
    async fn test_verify_qualifiers_solana_validator_not_in_schedule() {
        let mut mock = MockSentinel::new();

        let validator_id = Pubkey::new_unique();
        let service_key = Pubkey::new_unique();

        // Setup mock responses - validator not in leader schedule
        mock.set_leader_schedule(validator_id, false);
        mock.set_gossip_ip(validator_id, Some(Ipv4Addr::new(192, 168, 1, 1)));

        let attestation = SolanaValidatorAttestation {
            validator_id,
            service_key,
            ed25519_signature: [0; 64],
        };

        let access_mode = AccessMode::SolanaValidator(attestation);
        let result = mock.verify_qualifiers_mock(&access_mode).await.unwrap();

        assert_eq!(result.len(), 0);
    }

    #[tokio::test]
    async fn test_verify_qualifiers_with_backup_ids_success() {
        let mut mock = MockSentinel::new();

        let validator_id = Pubkey::new_unique();
        let backup_id_1 = Pubkey::new_unique();
        let backup_id_2 = Pubkey::new_unique();
        let service_key = Pubkey::new_unique();

        let validator_ip = Ipv4Addr::new(192, 168, 1, 1);
        let backup_ip_1 = Ipv4Addr::new(192, 168, 1, 2);
        let backup_ip_2 = Ipv4Addr::new(192, 168, 1, 3);

        // Setup mock responses
        mock.set_leader_schedule(validator_id, true); // Primary in schedule
        mock.set_leader_schedule(backup_id_1, false); // Backup NOT in schedule
        mock.set_leader_schedule(backup_id_2, false); // Backup NOT in schedule

        mock.set_gossip_ip(validator_id, Some(validator_ip));
        mock.set_gossip_ip(backup_id_1, Some(backup_ip_1));
        mock.set_gossip_ip(backup_id_2, Some(backup_ip_2));

        let attestation = SolanaValidatorAttestation {
            validator_id,
            service_key,
            ed25519_signature: [0; 64],
        };

        let access_mode = AccessMode::SolanaValidatorWithBackupIds {
            attestation,
            backup_ids: vec![backup_id_1, backup_id_2],
        };

        let result = mock.verify_qualifiers_mock(&access_mode).await.unwrap();

        assert_eq!(result.len(), 3);
        assert!(
            result
                .iter()
                .any(|(id, ip)| *id == validator_id && *ip == validator_ip)
        );
        assert!(
            result
                .iter()
                .any(|(id, ip)| *id == backup_id_1 && *ip == backup_ip_1)
        );
        assert!(
            result
                .iter()
                .any(|(id, ip)| *id == backup_id_2 && *ip == backup_ip_2)
        );
    }

    #[tokio::test]
    async fn test_verify_qualifiers_backup_in_leader_schedule_fails() {
        let mut mock = MockSentinel::new();

        let validator_id = Pubkey::new_unique();
        let backup_id = Pubkey::new_unique();
        let service_key = Pubkey::new_unique();

        // Setup mock responses - backup IS in leader schedule (should fail)
        mock.set_leader_schedule(validator_id, true);
        mock.set_leader_schedule(backup_id, true); // Backup IS in schedule - should fail

        mock.set_gossip_ip(validator_id, Some(Ipv4Addr::new(192, 168, 1, 1)));
        mock.set_gossip_ip(backup_id, Some(Ipv4Addr::new(192, 168, 1, 2)));

        let attestation = SolanaValidatorAttestation {
            validator_id,
            service_key,
            ed25519_signature: [0; 64],
        };

        let access_mode = AccessMode::SolanaValidatorWithBackupIds {
            attestation,
            backup_ids: vec![backup_id],
        };

        let result = mock.verify_qualifiers_mock(&access_mode).await.unwrap();

        assert_eq!(result.len(), 0);
    }

    #[tokio::test]
    async fn test_verify_qualifiers_backup_not_in_gossip_fails() {
        let mut mock = MockSentinel::new();

        let validator_id = Pubkey::new_unique();
        let backup_id = Pubkey::new_unique();
        let service_key = Pubkey::new_unique();

        // Setup mock responses - backup not in gossip
        mock.set_leader_schedule(validator_id, true);
        mock.set_leader_schedule(backup_id, false);

        mock.set_gossip_ip(validator_id, Some(Ipv4Addr::new(192, 168, 1, 1)));
        mock.set_gossip_ip(backup_id, None); // Backup not in gossip - should fail

        let attestation = SolanaValidatorAttestation {
            validator_id,
            service_key,
            ed25519_signature: [0; 64],
        };

        let access_mode = AccessMode::SolanaValidatorWithBackupIds {
            attestation,
            backup_ids: vec![backup_id],
        };

        let result = mock.verify_qualifiers_mock(&access_mode).await.unwrap();

        assert_eq!(result.len(), 0);
    }

    #[tokio::test]
    async fn test_verify_qualifiers_empty_backup_ids() {
        let mut mock = MockSentinel::new();

        let validator_id = Pubkey::new_unique();
        let service_key = Pubkey::new_unique();
        let validator_ip = Ipv4Addr::new(192, 168, 1, 1);

        // Setup mock responses
        mock.set_leader_schedule(validator_id, true);
        mock.set_gossip_ip(validator_id, Some(validator_ip));

        let attestation = SolanaValidatorAttestation {
            validator_id,
            service_key,
            ed25519_signature: [0; 64],
        };

        // Empty backup IDs list
        let access_mode = AccessMode::SolanaValidatorWithBackupIds {
            attestation,
            backup_ids: vec![],
        };

        let result = mock.verify_qualifiers_mock(&access_mode).await.unwrap();

        // Should work with just the primary validator
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].0, validator_id);
        assert_eq!(result[0].1, validator_ip);
    }

    #[tokio::test]
    async fn test_verify_qualifiers_signature_verify_error_returns_empty() {
        // Build a real Sentinel; it won't hit network because we short-circuit on signature
        let (_tx, rx) = unbounded_channel();
        let keypair = Arc::new(Keypair::new());
        let dz_rpc = Url::parse("http://127.0.0.1:1234").unwrap();
        let sol_rpc = Url::parse("http://127.0.0.1:1235").unwrap();
        let serviceability_id = Pubkey::new_unique();

        let sentinel = Sentinel {
            dz_rpc_client: DzRpcClient::new(dz_rpc, keypair.clone(), serviceability_id),
            sol_rpc_client: SolRpcClient::new(sol_rpc, keypair),
            rx,
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
