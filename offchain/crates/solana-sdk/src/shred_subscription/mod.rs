pub mod instruction;
pub mod state;

use std::sync::LazyLock;

use solana_sdk::pubkey::Pubkey;

/// Placeholder program ID — used when `SHRED_SUBSCRIPTION_PROGRAM_ID` env var is not set.
const DEFAULT_ID: Pubkey = Pubkey::new_from_array([
    0xFF, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
    0,
]);

/// Shred subscription program ID.
pub static ID: LazyLock<Pubkey> = LazyLock::new(|| {
    std::env::var("SHRED_SUBSCRIPTION_PROGRAM_ID")
        .ok()
        .and_then(|s| s.parse().ok())
        .unwrap_or(DEFAULT_ID)
});
