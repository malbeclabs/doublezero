use crate::{new_transaction, Result};

use solana_client::nonblocking::rpc_client::RpcClient;
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{
    pubkey::Pubkey,
    signature::{Keypair, Signature, Signer},
};
use solana_system_interface::instruction;
use std::sync::Arc;
use tracing::info;
use url::Url;

// Estimated size of a DZ User account with a single publisher and a single subscriber pubkey
const DZ_USER_SIZE_ESTIMATE: usize = 236;

pub struct DzRpcClient {
    client: RpcClient,
    payer: Arc<Keypair>,
}

impl DzRpcClient {
    pub fn new(rpc_url: Url, payer: Arc<Keypair>) -> Self {
        Self {
            client: RpcClient::new_with_commitment(
                rpc_url.clone().into(),
                CommitmentConfig::confirmed(),
            ),
            payer,
        }
    }

    pub async fn fund_authorized_user(
        &self,
        recipient_pubkey: &Pubkey,
        onboarding_lamports: u64,
    ) -> Result<Signature> {
        let recent_blockhash = self.client.get_latest_blockhash().await?;

        // TODO: Read the ledger for records to indicate if this user already exists and
        // only credit the onboarding lamports if it does
        let rent = self
            .client
            .get_minimum_balance_for_rent_exemption(DZ_USER_SIZE_ESTIMATE)
            .await?;

        let xfr = instruction::transfer(
            &self.payer.pubkey(),
            recipient_pubkey,
            onboarding_lamports + rent,
        );

        let txn = new_transaction(&[xfr], &[&self.payer], recent_blockhash);

        let signature = self.client.send_and_confirm_transaction(&txn).await?;
        info!(rent, onboarding_lamports, user = %recipient_pubkey, %signature, "successfully funded authorized user");

        Ok(signature)
    }
}
