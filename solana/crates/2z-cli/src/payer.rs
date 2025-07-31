use clap::Args;

use crate::rpc::{SolanaRpcOptions, SolanaWebsocketOptions};

#[derive(Debug, Args)]
pub struct SolanaPayerOptions {
    #[command(flatten)]
    solana_rpc_options: SolanaRpcOptions,

    #[command(flatten)]
    pub keypair: SolanaKeypairOptions,
}

#[derive(Debug, Args)]
pub struct SolanaPayerWithWebsocketOptions {
    #[command(flatten)]
    pub solana_websocket_options: SolanaWebsocketOptions,

    #[command(flatten)]
    pub solana_keypair_options: SolanaKeypairOptions,
}

// TODO: Add fee-payer like in solana CLI?
#[derive(Debug, Args)]
pub struct SolanaKeypairOptions {
    /// Filepath or URL to a keypair.
    #[arg(long = "keypair", short = 'k', value_name = "KEYPAIR")]
    pub keypair: Option<String>,

    /// Set the compute unit price for transaction in increments of 0.000001 lamports per compute
    /// unit.
    #[arg(long, value_name = "COMPUTE_UNIT_PRICE")]
    pub with_compute_unit_price: Option<u64>,
}
