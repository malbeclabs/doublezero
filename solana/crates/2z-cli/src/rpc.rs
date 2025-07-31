use clap::Args;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_commitment_config::CommitmentConfig;

// TODO: Read from Solana config for default.
fn unwrap_rpc_url(url: Option<String>) -> String {
    url.unwrap_or("https://api.mainnet-beta.solana.com".to_string())
}

// TODO: Read from Solana config for default.
fn unwrap_ws_url(ws: Option<String>) -> String {
    ws.unwrap_or("wss://api.mainnet-beta.solana.com".to_string())
}

#[derive(Debug, Args)]
pub struct DoubleZeroLedgerRpcOptions {
    /// URL for DoubleZero Ledger's JSON RPC. Required.
    #[arg(long, required = true)]
    pub dz_ledger_url: String,
}

#[derive(Debug, Args)]
pub struct SolanaRpcOptions {
    /// URL for Solana's JSON RPC or moniker (or their first letter):
    /// [mainnet-beta, testnet, devnet, localhost].
    #[arg(long = "url", short = 'u')]
    pub url_or_moniker: Option<String>,
}

#[derive(Debug, Args)]
pub struct SolanaWebsocketOptions {
    #[command(flatten)]
    pub rpc_options: SolanaRpcOptions,

    /// WebSocket URL for the solana cluster.
    #[arg(long = "ws", value_name = "WEBSOCKET_URL")]
    pub ws: Option<String>,
}

impl SolanaWebsocketOptions {
    pub fn into_clients(self) -> (RpcClient, String) {
        (
            RpcClient::new_with_commitment(
                unwrap_rpc_url(self.rpc_options.url_or_moniker),
                CommitmentConfig::confirmed(),
            ),
            unwrap_ws_url(self.ws), // TODO
        )
    }
}
