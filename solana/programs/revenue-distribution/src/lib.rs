pub mod instruction;
#[cfg(not(feature = "no-entrypoint"))]
mod processor;
pub mod state;
pub mod types;
//

solana_pubkey::declare_id!("dzrevZC94tBLwuHw1dyynZxaXTWyp7yocsinyEVPtt4");

#[cfg(not(feature = "development"))]
pub const DOUBLEZERO_MINT_KEY: solana_pubkey::Pubkey =
    solana_pubkey::pubkey!("F9m4F8TK8tXHnfaCV42mT9bDrC2EsxwUsKiWtjkUDZ2z");
#[cfg(feature = "development")]
pub const DOUBLEZERO_MINT_KEY: solana_pubkey::Pubkey =
    solana_pubkey::pubkey!("devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj");

pub const DOUBLEZERO_MINT_DECIMALS: u8 = 8;
