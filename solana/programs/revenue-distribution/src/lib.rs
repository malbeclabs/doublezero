pub mod env;
pub mod instruction;
#[cfg(feature = "entrypoint")]
mod processor;
pub mod state;
pub mod types;

//

solana_pubkey::declare_id!("dzrevZC94tBLwuHw1dyynZxaXTWyp7yocsinyEVPtt4");

#[cfg(feature = "development")]
pub use env::development::DOUBLEZERO_MINT_KEY;
#[cfg(not(feature = "development"))]
pub use env::mainnet::DOUBLEZERO_MINT_KEY;

pub const DOUBLEZERO_MINT_DECIMALS: u8 = 8;

// For the development environment, there is no swap program to determine the
// swap rate for a given SOL amount. This arbitrary constant rate is a
// placeholder.
//
// SOL is 9 decimals and 2Z is 8 decimals. This arbitrary rate fixes the 2Z to
// SOL rate to 100:1.
//
// For example, we want to swap 1 SOL to 100 2Z tokens. 1 * 10^9 SOL must equal
// 100 * 10^8 2Z.
#[cfg(feature = "development")]
pub const FIXED_SOL_2Z_SWAP_RATE_FOR_DEVELOPMENT: u64 = 10;
