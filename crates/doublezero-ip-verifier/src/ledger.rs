//! The DoubleZero Ledger, as this service reads it: the current epoch, and the verifier authority
//! the serviceability program will accept proofs from.

use crate::{authority::AuthoritySource, epoch::EpochSource};
use async_trait::async_trait;
use doublezero_serviceability::{pda::get_globalstate_pda, state::globalstate::GlobalState};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_commitment_config::CommitmentConfig;
use solana_program::pubkey::Pubkey;

pub struct Ledger {
    rpc: RpcClient,
    serviceability_program_id: Pubkey,
}

impl Ledger {
    pub fn new(ledger_rpc_url: String, serviceability_program_id: Pubkey) -> Self {
        Self {
            rpc: RpcClient::new_with_commitment(ledger_rpc_url, CommitmentConfig::confirmed()),
            serviceability_program_id,
        }
    }
}

#[async_trait]
impl EpochSource for Ledger {
    async fn current_epoch(&self) -> anyhow::Result<u64> {
        Ok(self.rpc.get_epoch_info().await?.epoch)
    }
}

#[async_trait]
impl AuthoritySource for Ledger {
    async fn ip_verifier_authority(&self) -> anyhow::Result<Pubkey> {
        let (globalstate_pubkey, _bump) = get_globalstate_pda(&self.serviceability_program_id);
        let data = self.rpc.get_account_data(&globalstate_pubkey).await?;

        Ok(GlobalState::try_from(&data[..])
            .map_err(|err| anyhow::anyhow!("could not deserialize GlobalState: {err}"))?
            .ip_verifier_authority_pk)
    }
}
