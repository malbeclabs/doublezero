pub mod instruction;
#[cfg(not(feature = "no-entrypoint"))]
mod processor;
pub mod state;
pub mod types;

//

solana_pubkey::declare_id!("ARu1CZsVpgq1j3Mw89F3PwfUcFxXWeBpbLteNpT37juR");

// TODO: Put somewhere else.
pub const DOUBLEZERO_MINT: solana_pubkey::Pubkey =
    solana_pubkey::pubkey!("F9m4F8TK8tXHnfaCV42mT9bDrC2EsxwUsKiWtjkUDZ2z");
