use std::{net::Ipv4Addr, sync::Arc};

use async_trait::async_trait;
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_accesspass_pda, get_globalstate_pda},
    processors::accesspass::set::SetAccessPassArgs,
    state::accesspass::AccessPassType,
};
use mockall::automock;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signature, Signer},
};
use solana_system_interface::program as system_program;
use tracing::info;
use url::Url;

use crate::{Result, new_transaction};

#[automock]
#[async_trait]
pub trait DzRpcClientType {
    async fn issue_access_pass(
        &self,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
        validator_id: &Pubkey,
    ) -> Result<Signature>;
}

pub struct DzRpcClient {
    client: RpcClient,
    payer: Arc<Keypair>,
    serviceability_id: Pubkey,
}

#[async_trait]
impl DzRpcClientType for DzRpcClient {
    async fn issue_access_pass(
        &self,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
        validator_id: &Pubkey,
    ) -> Result<Signature> {
        self.issue_access_pass(service_key, client_ip, validator_id)
            .await
    }
}

impl DzRpcClient {
    pub fn new(rpc_url: Url, payer: Arc<Keypair>, serviceability_id: Pubkey) -> Self {
        Self {
            client: RpcClient::new_with_commitment(
                rpc_url.clone().into(),
                CommitmentConfig::confirmed(),
            ),
            payer,
            serviceability_id,
        }
    }

    pub async fn issue_access_pass(
        &self,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
        validator_id: &Pubkey,
    ) -> Result<Signature> {
        let (globalstate_pk, _) = get_globalstate_pda(&self.serviceability_id);
        let (pass_pk, _) = get_accesspass_pda(&self.serviceability_id, client_ip, service_key);
        let args = DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::SolanaValidator(*validator_id),
            client_ip: *client_ip,
            last_access_epoch: u64::MAX,
            // NOTE: Setting this to false by default
            allow_multiple_ip: false,
        });
        let accounts = vec![
            AccountMeta::new(pass_pk, false),
            AccountMeta::new_readonly(globalstate_pk, false),
            AccountMeta::new(*service_key, false),
            AccountMeta::new(self.payer.pubkey(), true),
            AccountMeta::new_readonly(system_program::id(), false),
        ];

        let set_pass_ix = try_build_instruction(&self.serviceability_id, accounts, &args)?;
        let signer = &self.payer;
        let recent_blockhash = self.client.get_latest_blockhash().await?;
        let transaction = new_transaction(&[set_pass_ix], &[signer], recent_blockhash);

        let signature = self
            .client
            .send_and_confirm_transaction(&transaction)
            .await?;
        info!(validator = %service_key, %signature, "issued validator access pass");

        Ok(signature)
    }
}
