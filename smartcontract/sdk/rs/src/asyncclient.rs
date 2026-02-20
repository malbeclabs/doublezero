use crate::doublezeroclient::{AsyncDoubleZeroClient, SubscribeResult};
use solana_account_decoder::UiAccountEncoding;
use solana_client::nonblocking::pubsub_client::PubsubClient;
use solana_commitment_config::CommitmentConfig;
use solana_rpc_client_api::config::{RpcAccountInfoConfig, RpcProgramAccountsConfig};
use solana_sdk::pubkey::Pubkey;

pub struct AsyncDZClient {
    program_id: Pubkey,
    client: PubsubClient,
}

impl AsyncDZClient {
    pub async fn new(_rpc_url: String, ws_url: String, program_id: Pubkey) -> eyre::Result<Self> {
        let client = PubsubClient::new(&ws_url).await?;
        Ok(AsyncDZClient { program_id, client })
    }
}

impl AsyncDoubleZeroClient for AsyncDZClient {
    async fn subscribe<'a>(&'a self) -> SubscribeResult<'a> {
        let options = RpcProgramAccountsConfig {
            filters: None,
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                data_slice: None,
                commitment: Some(CommitmentConfig::confirmed()),
                min_context_slot: None,
            },
            with_context: None,
            sort_results: None,
        };
        self.client
            .program_subscribe(&self.program_id, Some(options))
            .await
    }
}
