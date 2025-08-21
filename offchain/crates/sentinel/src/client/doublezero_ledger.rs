use crate::{Result, new_transaction};

use doublezero_program_common::types::NetworkV4;
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_serviceability::{
    pda::{get_accesspass_pda, get_globalstate_pda},
    processors::accesspass::set::SetAccessPassArgs,
    state::{
        accesspass::AccessPassType,
        accounttype::{AccountType, AccountTypeInfo},
        user::User,
    },
};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signature, Signer},
};
use solana_system_interface::{instruction, program as system_program};
use std::{net::Ipv4Addr, sync::Arc};
use tracing::info;
use url::Url;

pub struct DzRpcClient {
    client: RpcClient,
    payer: Arc<Keypair>,
    serviceability_id: Pubkey,
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

    pub async fn fund_authorized_user(
        &self,
        service_key: &Pubkey,
        onboarding_lamports: u64,
    ) -> Result<Signature> {
        let recent_blockhash = self.client.get_latest_blockhash().await?;

        let onboarding_balance = onboarding_lamports
            + self
                .client
                .get_minimum_balance_for_rent_exemption(user_size())
                .await?;
        let current_balance = self.client.get_balance(service_key).await?;
        let deposit = onboarding_balance.saturating_sub(current_balance);

        let xfr = instruction::transfer(&self.payer.pubkey(), service_key, deposit);

        let txn = new_transaction(&[xfr], &[&self.payer], recent_blockhash);

        let signature = self.client.send_and_confirm_transaction(&txn).await?;
        info!(deposit, validator = %service_key, %signature, "funded authorized validator");

        Ok(signature)
    }

    pub async fn issue_access_pass(
        &self,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
    ) -> Result<Signature> {
        let (globalstate_pk, _) = get_globalstate_pda(&self.serviceability_id);
        let (pass_pk, _) = get_accesspass_pda(&self.serviceability_id, client_ip, service_key);
        let args = SetAccessPassArgs {
            accesspass_type: AccessPassType::SolanaValidator,
            client_ip: *client_ip,
            user_payer: *service_key,
            last_access_epoch: u64::MAX,
        };
        let accounts = vec![
            AccountMeta::new(pass_pk, false),
            AccountMeta::new_readonly(globalstate_pk, false),
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

fn user_size() -> usize {
    let default_user = User {
        account_type: AccountType::User,
        owner: Default::default(),
        index: Default::default(),
        bump_seed: Default::default(),
        user_type: 0.into(),
        tenant_pk: Default::default(),
        device_pk: Default::default(),
        cyoa_type: 0.into(),
        client_ip: Ipv4Addr::UNSPECIFIED,
        dz_ip: Ipv4Addr::UNSPECIFIED,
        tunnel_id: Default::default(),
        tunnel_net: NetworkV4::new(Ipv4Addr::UNSPECIFIED, Default::default()).unwrap(),
        status: 0.into(),
        publishers: vec![Default::default()],
        subscribers: vec![Default::default()],
        validator_pubkey: Default::default(),
    };
    default_user.size()
}
