#![allow(unexpected_cfgs)]

#[cfg(any(not(feature = "no-entrypoint"), test))]
pub mod entrypoint;

pub mod error;
pub mod instructions;
pub mod pda;
pub mod processors;
pub mod seeds;
pub mod state;

use solana_program::pubkey::Pubkey;
use std::str::FromStr;

#[cfg(not(test))]
mod build_constants {
    include!(concat!(env!("OUT_DIR"), "/build_constants.rs"));
}

pub fn serviceability_program_id() -> Pubkey {
    Pubkey::from_str(crate::build_constants::SERVICEABILITY_PROGRAM_ID)
        .expect("SERVICEABILITY_PROGRAM_ID is not a valid Pubkey")
}

#[cfg(test)]
mod build_constants {
    pub const SERVICEABILITY_PROGRAM_ID: &str = "";
}
