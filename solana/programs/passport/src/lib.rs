pub mod instruction;
#[cfg(not(feature = "no-entrypoint"))]
mod processor;
pub mod state;

//

solana_pubkey::declare_id!("dzpt2dM8g9qsLxpdddnVvKfjkCLVXd82jrrQVJigCPV");
