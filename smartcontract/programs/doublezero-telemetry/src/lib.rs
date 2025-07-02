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

const LOCAL_SERVICEABILITY_PROGRAM_ID: &str = "7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX";
const DEVNET_SERVICEABILITY_PROGRAM_ID: &str = "GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah";
const TESTNET_SERVICEABILITY_PROGRAM_ID: &str = "DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb";

#[cfg(not(test))]
mod build_constants {
    include!(concat!(
        env!("OUT_DIR"),
        "/serviceability_program_id_input.rs"
    ));
}

pub fn serviceability_program_id() -> Pubkey {
    let raw = match build_constants::RAW_SERVICEABILITY_PROGRAM_ID {
        "local" => LOCAL_SERVICEABILITY_PROGRAM_ID,
        "devnet" => DEVNET_SERVICEABILITY_PROGRAM_ID,
        "testnet" => TESTNET_SERVICEABILITY_PROGRAM_ID,
        other => other,
    };

    let error_msg = format!("Invalid SERVICEABILITY_PROGRAM_ID=\"{raw}\"");
    Pubkey::from_str(raw).expect(&error_msg)
}

#[cfg(test)]
mod build_constants {
    pub const RAW_SERVICEABILITY_PROGRAM_ID: &str = "local";
}
