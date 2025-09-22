use clap::Args;
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Args)]
pub struct ServiceKey {
    /// Pubkey used on DoubleZero Ledger network to interact with the Serviceability program.
    pub service_key: Pubkey,
}
