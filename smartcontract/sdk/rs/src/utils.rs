use solana_sdk::{pubkey::Pubkey, signature::Keypair};
use std::{error::Error, fs, str::FromStr};

pub fn read_keypair_from_file(file: String) -> eyre::Result<Keypair, Box<dyn Error>> {
    let file_content = fs::read_to_string(file)?;
    let secret_key_bytes: Vec<u8> = serde_json::from_str(&file_content)?;
    let keypair = Keypair::from_bytes(&secret_key_bytes)?;

    Ok(keypair)
}

pub fn parse_pubkey(input: &str) -> Option<Pubkey> {
    if input.len() < 40 || input.len() > 44 {
        return None;
    }

    Pubkey::from_str(input).ok()
}
