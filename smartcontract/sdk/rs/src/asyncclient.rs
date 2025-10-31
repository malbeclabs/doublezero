use futures::{future::BoxFuture, stream::BoxStream};
use solana_account_decoder::UiAccountEncoding;
use solana_sdk::{
    commitment_config::CommitmentConfig,
    // Note: Removed unused imports like Message, Transaction, Keypair
    pubkey::Pubkey,
};
// Import the non-blocking PubsubClient
use solana_client::nonblocking::pubsub_client::{PubsubClient, PubsubClientResult};
use solana_rpc_client_api::{
    config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    response::{Response as RpcResponse, RpcKeyedAccount},
};

pub type UnsubscribeFn = Box<dyn FnOnce() -> BoxFuture<'static, ()> + Send>;

pub struct AsyncDZClient {
    program_id: Pubkey,
    client: PubsubClient,
}

impl AsyncDZClient {
    pub async fn new(ws_url: &str, program_id: Pubkey) -> eyre::Result<Self> {
        let client = PubsubClient::new(ws_url).await?;
        Ok(AsyncDZClient { program_id, client })
    }

    pub async fn subscribe(
        &self,
    ) -> PubsubClientResult<(BoxStream<'_, RpcResponse<RpcKeyedAccount>>, UnsubscribeFn)> {
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
