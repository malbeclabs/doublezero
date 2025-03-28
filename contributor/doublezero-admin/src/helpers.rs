use std::str;

use std::str::FromStr;
use colored::Colorize;
use solana_sdk::pubkey::Pubkey;

pub fn parse_pubkey(input: &str) -> Option<Pubkey> {
    if input.len() < 43 || input.len() > 44 {
        return None;
    }

    match Pubkey::from_str(input) {
        Ok(pk) => Some(pk),
        Err(_) => None,
    }
}

pub fn print_error(e: eyre::Report) {
    eprintln!("\n{}: {:?}\n", "Error".red().bold(), e);
}
