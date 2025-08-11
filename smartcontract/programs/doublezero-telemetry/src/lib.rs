#![allow(unexpected_cfgs)]

#[cfg(any(not(feature = "no-entrypoint"), test))]
pub mod entrypoint;

pub mod error;
pub mod instructions;
pub mod pda;
pub mod processors;
pub mod seeds;
pub mod state;

use doublezero_config::Environment;
use solana_program::pubkey::Pubkey;
use std::str::FromStr;

const LOCAL_SERVICEABILITY_PROGRAM_ID: &str = "7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX";

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
        "devnet" => &Environment::Devnet
            .config()
            .unwrap()
            .serviceability_program_id
            .to_string(),
        "testnet" => &Environment::Testnet
            .config()
            .unwrap()
            .serviceability_program_id
            .to_string(),
        "mainnet" => &Environment::Mainnet
            .config()
            .unwrap()
            .serviceability_program_id
            .to_string(),
        other => other,
    };

    let error_msg = format!("Invalid SERVICEABILITY_PROGRAM_ID=\"{raw}\"");
    Pubkey::from_str(raw).expect(&error_msg)
}

#[cfg(test)]
mod build_constants {
    pub const RAW_SERVICEABILITY_PROGRAM_ID: &str = "local";
}
