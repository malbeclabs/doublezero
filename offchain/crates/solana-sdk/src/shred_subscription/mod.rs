pub mod instruction;
pub mod state;

use std::sync::LazyLock;

use solana_sdk::pubkey::Pubkey;

const DEFAULT_ID: Pubkey = solana_sdk::pubkey!("dzshrr3yL57SB13sJPYHYo3TV8Bo1i1FxkyrZr3bKNE");

/// Shred subscription program ID.
pub static ID: LazyLock<Pubkey> = LazyLock::new(|| {
    std::env::var("SHRED_SUBSCRIPTION_PROGRAM_ID")
        .ok()
        .and_then(|s| s.parse().ok())
        .unwrap_or(DEFAULT_ID)
});
