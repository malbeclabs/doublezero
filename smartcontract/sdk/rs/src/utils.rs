use solana_sdk::{pubkey::Pubkey, signature::Keypair};
use std::{error::Error, path::PathBuf, str::FromStr};

use crate::keypair::parse_keypair_json;

/// Read a keypair from a JSON file.
///
/// # Deprecated
/// Use [`crate::keypair::load_keypair`] instead, which supports stdin and
/// environment variable input in addition to file paths.
#[deprecated(
    since = "0.8.0",
    note = "Use crate::keypair::load_keypair instead for stdin/env var support"
)]
pub fn read_keypair_from_file(file: PathBuf) -> eyre::Result<Keypair, Box<dyn Error>> {
    let file_content = std::fs::read_to_string(&file)?;
    let keypair = parse_keypair_json(&file_content, &file.display().to_string())
        .map_err(|e| Box::new(e) as Box<dyn Error>)?;
    Ok(keypair)
}

pub fn parse_pubkey(input: &str) -> Option<Pubkey> {
    if input.len() < 40 || input.len() > 44 {
        return None;
    }

    Pubkey::from_str(input).ok()
}
