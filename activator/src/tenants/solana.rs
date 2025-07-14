use eyre::eyre;
use solana_client::{
    rpc_client::RpcClient,
    rpc_response::{RpcContactInfo, RpcVoteAccountInfo},
};
use solana_sdk::commitment_config::CommitmentConfig;

#[allow(dead_code)]
pub trait SolanaInfo {
    fn get_vote_accounts(&self) -> eyre::Result<Vec<RpcVoteAccountInfo>>;
    fn get_cluster_nodes(&self) -> eyre::Result<Vec<RpcContactInfo>>;
}

pub struct SolanaRpcInfo {
    pub url: String,
}

impl SolanaInfo for SolanaRpcInfo {
    fn get_vote_accounts(&self) -> eyre::Result<Vec<RpcVoteAccountInfo>> {
        let client =
            RpcClient::new_with_commitment(self.url.to_string(), CommitmentConfig::confirmed());
        Ok(client.get_vote_accounts().map_err(|e| eyre!(e))?.current)
    }

    fn get_cluster_nodes(&self) -> eyre::Result<Vec<RpcContactInfo>> {
        let client =
            RpcClient::new_with_commitment(self.url.to_string(), CommitmentConfig::confirmed());
        client.get_cluster_nodes().map_err(|e| eyre!(e))
    }
}
