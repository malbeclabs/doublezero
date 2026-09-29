pub mod instruction;
pub mod state;

use std::sync::LazyLock;

use solana_sdk::pubkey::Pubkey;

const DEFAULT_ID: Pubkey = solana_sdk::pubkey!("J9gupbyffs4XAoKn5NrJ4hrbdqW5ZfvMDaaas3FtH8yC");

/// Feed subscription program ID.
pub static ID: LazyLock<Pubkey> = LazyLock::new(|| {
    std::env::var("FEED_SUBSCRIPTION_PROGRAM_ID")
        .ok()
        .and_then(|s| s.parse().ok())
        .unwrap_or(DEFAULT_ID)
});
