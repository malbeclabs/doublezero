use std::{
    fs,
    io::{IsTerminal, Read},
    path::PathBuf,
};

use solana_sdk::signature::Keypair;

use crate::keypair::{error::KeypairLoadError, source::KeypairSource};

/// Default keypair path relative to HOME
const DEFAULT_KEYPAIR_PATH: &str = ".config/solana/id.json";

/// Result of loading a keypair, including provenance information
#[derive(Debug)]
pub struct KeypairLoadResult {
    /// The loaded keypair
    pub keypair: Keypair,
    /// The source from which the keypair was loaded
    pub source: KeypairSource,
}

/// Parse keypair from JSON string
pub fn parse_keypair_json(json_str: &str, source_desc: &str) -> Result<Keypair, KeypairLoadError> {
    let secret_key_bytes: Vec<u8> =
        serde_json::from_str(json_str).map_err(|e| KeypairLoadError::InvalidJsonFormat {
            origin: source_desc.to_string(),
            message: e.to_string(),
        })?;

    Keypair::try_from(secret_key_bytes.as_slice()).map_err(|_| {
        KeypairLoadError::InvalidKeypairBytes {
            origin: source_desc.to_string(),
        }
    })
}

/// Read keypair from a file path
fn read_keypair_from_path(path: &PathBuf) -> Result<Keypair, KeypairLoadError> {
    let content = fs::read_to_string(path).map_err(|e| KeypairLoadError::FileReadError {
        path: path.display().to_string(),
        message: e.to_string(),
    })?;

    parse_keypair_json(&content, &path.display().to_string())
}

/// Read keypair from stdin
fn read_keypair_from_stdin() -> Result<Keypair, KeypairLoadError> {
    if std::io::stdin().is_terminal() {
        return Err(KeypairLoadError::StdinIsTty);
    }

    let mut buffer = String::new();
    std::io::stdin()
        .read_to_string(&mut buffer)
        .map_err(|e| KeypairLoadError::StdinReadError {
            message: e.to_string(),
        })?;

    if buffer.trim().is_empty() {
        return Err(KeypairLoadError::StdinReadError {
            message: "stdin was empty".to_string(),
        });
    }

    parse_keypair_json(&buffer, "stdin")
}

/// Load keypair following the precedence chain:
/// 1. CLI argument (--keypair)
/// 2. Stdin (if not a TTY)
/// 3. Default path (~/.config/solana/id.json)
///
/// # Arguments
/// * `cli_path` - Optional path from CLI --keypair argument
/// * `default_path` - Default path if no other source available
///
/// # Returns
/// * `Ok(KeypairLoadResult)` - Successfully loaded keypair with source
/// * `Err(KeypairLoadError)` - Failed to load keypair from any source
pub fn load_keypair(
    cli_path: Option<PathBuf>,
    default_path: PathBuf,
) -> Result<KeypairLoadResult, KeypairLoadError> {
    // 1. Try CLI argument (highest precedence)
    // If explicitly provided, fail immediately on error rather than silently
    // falling through to other sources (which could sign with the wrong key).
    if let Some(path) = cli_path {
        let keypair = read_keypair_from_path(&path)?;
        return Ok(KeypairLoadResult {
            keypair,
            source: KeypairSource::CliArgument(path),
        });
    }
    let mut attempted: Vec<String> = Vec::new();
    attempted.push("CLI --keypair: not provided".to_string());

    // 2. Try stdin (if not a TTY)
    match read_keypair_from_stdin() {
        Ok(keypair) => {
            return Ok(KeypairLoadResult {
                keypair,
                source: KeypairSource::Stdin,
            });
        }
        Err(KeypairLoadError::StdinIsTty) => {
            attempted.push("Stdin: is a TTY (not piped)".to_string());
        }
        Err(e) => {
            attempted.push(format!("Stdin: {}", e));
        }
    }

    // 3. Try default path
    match read_keypair_from_path(&default_path) {
        Ok(keypair) => {
            return Ok(KeypairLoadResult {
                keypair,
                source: KeypairSource::DefaultPath(default_path),
            });
        }
        Err(e) => {
            attempted.push(format!("Default path ({}): {}", default_path.display(), e));
        }
    }

    Err(KeypairLoadError::NoSourceAvailable { attempted })
}

/// Load keypair following the precedence chain:
/// 1. CLI argument (--keypair)
/// 2. Stdin (if not a TTY)
/// 3. Default path (~/.config/solana/id.json)
///
/// This is a convenience wrapper around [`load_keypair`] that automatically
/// computes the default path from the HOME environment variable.
///
/// # Arguments
/// * `cli_path` - Optional path from CLI --keypair argument
///
/// # Returns
/// * `Ok(Keypair)` - Successfully loaded keypair
/// * `Err(KeypairLoadError)` - Failed to load keypair from any source
pub fn try_load_keypair(cli_path: Option<PathBuf>) -> Result<Keypair, KeypairLoadError> {
    let home = home::home_dir().ok_or(KeypairLoadError::HomeDirNotFound)?;
    let default_path = home.join(DEFAULT_KEYPAIR_PATH);
    let result = load_keypair(cli_path, default_path)?;
    Ok(result.keypair)
}

#[cfg(test)]
mod tests {
    use std::io::Write;

    use solana_sdk::signer::Signer;
    use tempfile::NamedTempFile;

    use super::*;

    fn write_keypair_file(keypair: &Keypair) -> NamedTempFile {
        let mut file = NamedTempFile::new().unwrap();
        let bytes: Vec<u8> = keypair.to_bytes().to_vec();
        write!(file, "{}", serde_json::to_string(&bytes).unwrap()).unwrap();
        file
    }

    #[test]
    fn cli_path_valid_keypair_succeeds() {
        let keypair = Keypair::new();
        let file = write_keypair_file(&keypair);

        let result = load_keypair(Some(file.path().into()), PathBuf::from("/nonexistent")).unwrap();

        assert_eq!(result.keypair.pubkey(), keypair.pubkey());
        assert_eq!(
            result.source,
            KeypairSource::CliArgument(file.path().into())
        );
    }

    #[test]
    fn cli_path_missing_file_fails_immediately() {
        let valid_keypair = Keypair::new();
        let valid_file = write_keypair_file(&valid_keypair);

        // Even though default_path is a valid keypair, specifying a missing
        // --keypair must fail rather than falling through to the default.
        let result = load_keypair(
            Some(PathBuf::from("/nonexistent/keypair.json")),
            valid_file.path().into(),
        );

        assert!(result.is_err());
        assert!(matches!(
            result.unwrap_err(),
            KeypairLoadError::FileReadError { .. }
        ));
    }

    #[test]
    fn cli_path_invalid_json_fails_immediately() {
        let mut bad_file = NamedTempFile::new().unwrap();
        write!(bad_file, "not json").unwrap();

        let valid_keypair = Keypair::new();
        let valid_default = write_keypair_file(&valid_keypair);

        let result = load_keypair(Some(bad_file.path().into()), valid_default.path().into());

        assert!(result.is_err());
        assert!(matches!(
            result.unwrap_err(),
            KeypairLoadError::InvalidJsonFormat { .. }
        ));
    }

    #[test]
    fn cli_path_wrong_byte_length_fails_immediately() {
        let mut bad_file = NamedTempFile::new().unwrap();
        write!(bad_file, "[1, 2, 3]").unwrap();

        let valid_keypair = Keypair::new();
        let valid_default = write_keypair_file(&valid_keypair);

        let result = load_keypair(Some(bad_file.path().into()), valid_default.path().into());

        assert!(result.is_err());
        assert!(matches!(
            result.unwrap_err(),
            KeypairLoadError::InvalidKeypairBytes { .. }
        ));
    }

    #[test]
    fn no_cli_path_falls_back_to_default() {
        let keypair = Keypair::new();
        let file = write_keypair_file(&keypair);

        let result = load_keypair(None, file.path().into()).unwrap();

        assert_eq!(result.keypair.pubkey(), keypair.pubkey());
        assert_eq!(
            result.source,
            KeypairSource::DefaultPath(file.path().into())
        );
    }
}
