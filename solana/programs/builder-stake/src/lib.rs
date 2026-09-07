//! Custody for the 2Z security deposit a builder posts before deploying a feed, per
//! [RFC-28](https://github.com/malbeclabs/edge-builder/blob/main/rfcs/rfc28-builder-deployed-edge.md).
//!
//! This program holds user deposits and will hold burn authority over them once slashing exists.
//! That blast radius is why it is a separate deployable from `revenue-distribution` rather than a
//! set of instructions inside it: a change here should not require redeploying the program that
//! moves validator revenue.

pub mod env;
pub mod instruction;
#[cfg(feature = "entrypoint")]
mod processor;
pub mod state;

//

solana_pubkey::declare_id!("dzbschFChpPoWihZFdnYjyzHJicZwPHb6QTntHjhLki");

#[cfg(feature = "development")]
pub use env::development::DOUBLEZERO_MINT_KEY;
#[cfg(not(feature = "development"))]
pub use env::mainnet::DOUBLEZERO_MINT_KEY;

pub const DOUBLEZERO_MINT_DECIMALS: u8 = 8;
