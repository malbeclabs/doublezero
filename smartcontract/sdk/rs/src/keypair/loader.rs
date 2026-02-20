use solana_sdk::signature::Keypair;
use std::{
    env, fs,
    io::{IsTerminal, Read},
    path::PathBuf,
};

use super::{error::KeypairLoadError, source::KeypairSource};

/// Environment variable name for keypair (can be JSON content or file path)
pub const ENV_KEYPAIR: &str = "DOUBLEZERO_KEYPAIR";

/// Result of loading a keypair, including provenance information
pub struct KeypairLoadResult {
    /// The loaded keypair
    pub keypair: Keypair,
    /// The source from which the keypair was loaded
    pub source: KeypairSource,
}

/// Check if a string value looks like JSON keypair content (starts with '[' and ends with ']')
pub fn is_keypair_json_content(value: &str) -> bool {
    let trimmed = value.trim();
    trimmed.starts_with('[') && trimmed.ends_with(']')
}

/// Parse keypair from JSON string
pub fn parse_keypair_json(json_str: &str, source_desc: &str) -> Result<Keypair, KeypairLoadError> {
    let secret_key_bytes: Vec<u8> =
        serde_json::from_str(json_str).map_err(|e| KeypairLoadError::InvalidJsonFormat {
            origin: source_desc.to_string(),
            message: e.to_string(),
        })?;

    let secret: [u8; 32] =
        secret_key_bytes[..32]
            .try_into()
            .map_err(|_| KeypairLoadError::InvalidKeypairBytes {
                origin: source_desc.to_string(),
            })?;
    Ok(Keypair::new_from_array(secret))
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

/// Read keypair from environment variable
fn read_keypair_from_env() -> Result<(Keypair, bool), KeypairLoadError> {
    let value = env::var(ENV_KEYPAIR).map_err(|_| KeypairLoadError::NoSourceAvailable {
        attempted: vec![format!("Env {} not set", ENV_KEYPAIR)],
    })?;

    if is_keypair_json_content(&value) {
        let keypair = parse_keypair_json(&value, &format!("{} (JSON)", ENV_KEYPAIR))?;
        Ok((keypair, true))
    } else {
        let path = PathBuf::from(&value);
        let keypair = read_keypair_from_path(&path)?;
        Ok((keypair, false))
    }
}

/// Load keypair following the precedence chain:
/// 1. CLI argument (--keypair)
/// 2. Environment variable (DOUBLEZERO_KEYPAIR)
/// 3. Stdin (if not a TTY)
/// 4. Config file keypair_path
/// 5. Default path (~/.config/doublezero/id.json)
///
/// # Arguments
/// * `cli_path` - Optional path from CLI --keypair argument
/// * `config_path` - Optional path from config file
/// * `default_path` - Default path if no other source available
///
/// # Returns
/// * `Ok(KeypairLoadResult)` - Successfully loaded keypair with source
/// * `Err(KeypairLoadError)` - Failed to load keypair from any source
pub fn load_keypair(
    cli_path: Option<PathBuf>,
    config_path: Option<PathBuf>,
    default_path: PathBuf,
) -> Result<KeypairLoadResult, KeypairLoadError> {
    let mut attempted: Vec<String> = Vec::new();

    // 1. Try CLI argument (highest precedence)
    if let Some(path) = cli_path {
        match read_keypair_from_path(&path) {
            Ok(keypair) => {
                return Ok(KeypairLoadResult {
                    keypair,
                    source: KeypairSource::CliArgument(path),
                });
            }
            Err(e) => {
                attempted.push(format!("CLI --keypair ({}): {}", path.display(), e));
            }
        }
    } else {
        attempted.push("CLI --keypair: not provided".to_string());
    }

    // 2. Try environment variable
    match read_keypair_from_env() {
        Ok((keypair, is_json)) => {
            return Ok(KeypairLoadResult {
                keypair,
                source: KeypairSource::EnvVar { is_json },
            });
        }
        Err(KeypairLoadError::NoSourceAvailable { .. }) => {
            attempted.push(format!("Env {}: not set", ENV_KEYPAIR));
        }
        Err(e) => {
            attempted.push(format!("Env {}: {}", ENV_KEYPAIR, e));
        }
    }

    // 3. Try stdin (if not a TTY)
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

    // 4. Try config file path
    if let Some(path) = config_path {
        match read_keypair_from_path(&path) {
            Ok(keypair) => {
                return Ok(KeypairLoadResult {
                    keypair,
                    source: KeypairSource::ConfigFile(path),
                });
            }
            Err(e) => {
                attempted.push(format!("Config keypair_path ({}): {}", path.display(), e));
            }
        }
    }

    // 5. Try default path
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

#[cfg(test)]
mod tests {
    use super::*;
    use solana_sdk::signer::Signer;
    use std::io::Write;
    use tempfile::TempDir;

    fn create_test_keypair_file(dir: &TempDir) -> (PathBuf, Keypair) {
        let keypair = Keypair::new();
        let path = dir.path().join("test-keypair.json");
        let json = serde_json::to_string(&keypair.to_bytes().to_vec()).unwrap();
        let mut file = fs::File::create(&path).unwrap();
        file.write_all(json.as_bytes()).unwrap();
        (path, keypair)
    }

    #[test]
    fn test_is_keypair_json_content() {
        assert!(is_keypair_json_content("[1,2,3,4,5]"));
        assert!(is_keypair_json_content("  [1,2,3,4,5]  "));
        assert!(is_keypair_json_content("\n[1,2,3]\n"));
        assert!(!is_keypair_json_content("/path/to/file.json"));
        assert!(!is_keypair_json_content("~/.config/solana/id.json"));
        assert!(!is_keypair_json_content(""));
    }

    #[test]
    fn test_parse_keypair_json_valid() {
        let keypair = Keypair::new();
        let json = serde_json::to_string(&keypair.to_bytes().to_vec()).unwrap();
        let parsed = parse_keypair_json(&json, "test").unwrap();
        assert_eq!(parsed.pubkey(), keypair.pubkey());
    }

    #[test]
    fn test_parse_keypair_json_invalid() {
        let result = parse_keypair_json("not json", "test");
        assert!(matches!(
            result,
            Err(KeypairLoadError::InvalidJsonFormat { .. })
        ));
    }

    #[test]
    fn test_read_keypair_from_path() {
        let tmp = TempDir::new().unwrap();
        let (path, original) = create_test_keypair_file(&tmp);

        let loaded = read_keypair_from_path(&path).unwrap();
        assert_eq!(loaded.pubkey(), original.pubkey());
    }

    #[test]
    fn test_read_keypair_from_path_not_found() {
        let path = PathBuf::from("/nonexistent/path/keypair.json");
        let result = read_keypair_from_path(&path);
        assert!(matches!(
            result,
            Err(KeypairLoadError::FileReadError { .. })
        ));
    }

    #[test]
    fn test_load_keypair_cli_path_precedence() {
        let tmp = TempDir::new().unwrap();
        let (cli_path, cli_keypair) = create_test_keypair_file(&tmp);

        let config_path = tmp.path().join("config-keypair.json");
        let default_path = tmp.path().join("default-keypair.json");

        let result = load_keypair(Some(cli_path.clone()), Some(config_path), default_path).unwrap();

        assert_eq!(result.keypair.pubkey(), cli_keypair.pubkey());
        assert!(matches!(result.source, KeypairSource::CliArgument(_)));
    }

    #[test]
    fn test_load_keypair_default_fallback() {
        let tmp = TempDir::new().unwrap();
        let (default_path, default_keypair) = create_test_keypair_file(&tmp);

        // Clear env var to ensure it doesn't interfere
        env::remove_var(ENV_KEYPAIR);

        let result = load_keypair(None, None, default_path).unwrap();

        assert_eq!(result.keypair.pubkey(), default_keypair.pubkey());
        assert!(matches!(result.source, KeypairSource::DefaultPath(_)));
    }

    #[test]
    fn test_load_keypair_no_source_available() {
        let tmp = TempDir::new().unwrap();

        // Clear env var
        env::remove_var(ENV_KEYPAIR);

        let nonexistent = tmp.path().join("nonexistent.json");
        let result = load_keypair(None, None, nonexistent);

        assert!(matches!(
            result,
            Err(KeypairLoadError::NoSourceAvailable { .. })
        ));
    }
}
