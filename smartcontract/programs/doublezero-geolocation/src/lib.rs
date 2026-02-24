#![allow(unexpected_cfgs)]

#[cfg(any(not(feature = "no-entrypoint"), test))]
pub mod entrypoint;

pub mod error;
pub mod instructions;
pub mod pda;
pub mod processors;
pub mod seeds;
mod serializer;
pub mod state;

use solana_program::pubkey::Pubkey;

#[cfg(not(test))]
mod build_constants {
    include!(concat!(env!("OUT_DIR"), "/build_constants.rs"));
}

pub const fn serviceability_program_id() -> Pubkey {
    Pubkey::from_str_const(crate::build_constants::SERVICEABILITY_PROGRAM_ID)
}

#[cfg(test)]
mod build_constants {
    pub const SERVICEABILITY_PROGRAM_ID: &str = "";
}
