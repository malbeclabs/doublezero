use crate::{
    client::{doublezero_ledger::DzRpcClient, solana::SolRpcClient},
    verify_access_request, AccessIds, Result,
};
use doublezero_passport::instruction::AccessMode;
use solana_sdk::signature::{Keypair, Signature};
use std::{sync::Arc, time::Duration};
use tokio::{sync::mpsc::UnboundedReceiver, time::interval};
use tokio_util::sync::CancellationToken;
use tracing::{error, info};
use url::Url;

const BACKFILL_TIMER: Duration = Duration::from_secs(60 * 60);

pub struct Sentinel {
    dz_rpc_client: DzRpcClient,
    sol_rpc_client: SolRpcClient,
    rx: UnboundedReceiver<Signature>,
    onboarding_lamports: u64,
    previous_leader_epochs: u8,
}

impl Sentinel {
    pub async fn new(
        dz_rpc: Url,
        sol_rpc: Url,
        keypair: Arc<Keypair>,
        rx: UnboundedReceiver<Signature>,
        onboarding_lamports: u64,
        previous_leader_epochs: u8,
    ) -> Result<Self> {
        Ok(Self {
            dz_rpc_client: DzRpcClient::new(dz_rpc, keypair.clone()),
            sol_rpc_client: SolRpcClient::new(sol_rpc, keypair),
            rx,
            onboarding_lamports,
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
                        let access_ids = self.sol_rpc_client.get_access_request_from_signature(signature).await?;
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
        let AccessMode::SolanaValidator {
            service_key,
            validator_id,
            ..
        } = access_ids.mode;
        if verify_access_request(&access_ids.mode).is_ok()
            && self
                .sol_rpc_client
                .check_leader_schedule(&validator_id, self.previous_leader_epochs)
                .await?
        {
            self.dz_rpc_client
                .fund_authorized_user(&service_key, self.onboarding_lamports)
                .await?;
            let signature = self
                .sol_rpc_client
                .grant_access(&access_ids.request_pda, &access_ids.rent_beneficiary_key)
                .await?;
            info!(%signature, user = %service_key, "access request granted");
            metrics::counter!("doublezero_sentinel_access_granted").increment(1);
        } else {
            let signature = self
                .sol_rpc_client
                .deny_access(&access_ids.request_pda)
                .await?;
            info!(%signature, user = %service_key, "access request denied");
            metrics::counter!("doublezero_sentinel_access_denied").increment(1);
        }

        Ok(())
    }
}
