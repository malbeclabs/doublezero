use crate::{
    Error, Result, client::solana::SolRpcClient, error::rpc_with_retry, verify_access_request,
};
use doublezero_passport::instruction::AccessMode;
use solana_sdk::pubkey::Pubkey;
use std::net::Ipv4Addr;
use tracing::info;

/// Shared validator verification logic used by both WebSocket and polling modes
pub struct ValidatorVerifier<'a> {
    sol_rpc_client: &'a SolRpcClient,
    previous_leader_epochs: u8,
}

impl<'a> ValidatorVerifier<'a> {
    pub fn new(sol_rpc_client: &'a SolRpcClient, previous_leader_epochs: u8) -> Self {
        Self {
            sol_rpc_client,
            previous_leader_epochs,
        }
    }

    /// Verify access request qualifiers and return validated (validator_id, ip) pairs
    pub async fn verify_qualifiers(
        &self,
        access_mode: &AccessMode,
    ) -> Result<Vec<(Pubkey, Ipv4Addr)>> {
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
}
