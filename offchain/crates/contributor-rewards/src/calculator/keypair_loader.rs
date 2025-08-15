use anyhow::Result;
use solana_sdk::signature::{Keypair, Signer};
use std::path::PathBuf;
use thiserror::Error;
use tracing::info;

#[derive(Error, Debug)]
pub enum KeypairError {
    #[error(
        "Keypair not provided. Please specify --keypair <path> or set REWARDER_KEYPAIR_PATH environment variable"
    )]
    NotProvided,

    #[error("Keypair file not found at path: {path}")]
    FileNotFound { path: String },

    #[error("Invalid keypair format in file: {path}")]
    InvalidFormat { path: String },

    #[error("IO error reading keypair file: {0}")]
    IoError(#[from] std::io::Error),

    #[error("JSON parsing error: {0}")]
    JsonError(#[from] serde_json::Error),
}

/// Load keypair from CLI argument or environment variable.
/// CLI argument takes precedence over environment variable.
/// No default fallback - keypair must be explicitly provided.
pub fn load_keypair(cli_path: &Option<PathBuf>) -> Result<Keypair> {
    let keypair_path = if let Some(path) = cli_path {
        info!("Using keypair from CLI argument");
        path
    } else if let Ok(path_str) = std::env::var("REWARDER_KEYPAIR_PATH") {
        info!("Using keypair from REWARDER_KEYPAIR_PATH environment variable");
        &PathBuf::from(path_str)
    } else {
        return Err(KeypairError::NotProvided.into());
    };

    if !keypair_path.exists() {
        return Err(KeypairError::FileNotFound {
            path: keypair_path.display().to_string(),
        }
        .into());
    }

    let keypair_file = std::fs::read_to_string(keypair_path).map_err(KeypairError::IoError)?;

    let keypair_bytes: Vec<u8> =
        serde_json::from_str(&keypair_file).map_err(|_e| KeypairError::InvalidFormat {
            path: keypair_path.display().to_string(),
        })?;

    let keypair =
        Keypair::try_from(keypair_bytes.as_slice()).map_err(|_| KeypairError::InvalidFormat {
            path: keypair_path.display().to_string(),
        })?;

    info!("Loaded keypair with pubkey: {}", keypair.pubkey());

    Ok(keypair)
}
