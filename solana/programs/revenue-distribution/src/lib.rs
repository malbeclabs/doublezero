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
