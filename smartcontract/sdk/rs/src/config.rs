use doublezero_config::Environment;
use serde::{Deserialize, Serialize};
use solana_client::client_error::reqwest::Url;
use solana_sdk::{pubkey::Pubkey, signature::Keypair};
use std::{
    collections::HashMap,
    default::Default,
    env, fs,
    io::Write,
    path::{Path, PathBuf},
};

#[cfg(feature = "default-mainnet-beta")]
const DEFAULT_ENVIRONMENT: Environment = Environment::MainnetBeta;
#[cfg(not(feature = "default-mainnet-beta"))]
const DEFAULT_ENVIRONMENT: Environment = Environment::Testnet;

/// Returns the default program ID based on the compiled-in environment.
pub fn default_program_id() -> Pubkey {
    DEFAULT_ENVIRONMENT
        .config()
        .unwrap()
        .serviceability_program_id
}

/// The default path to the CLI configuration file.
///
/// > `~/.config/doublezero/cli/config.yml`
///
/// It will only be `None` if it is unable to identify the user's home
/// directory, which should not happen under typical OS environments.
fn get_cfg_filename() -> Option<PathBuf> {
    match env::var_os("DOUBLEZERO_CONFIG_FILE") {
        Some(path) => Some(PathBuf::from(path)),
        None => directories_next::UserDirs::new().map(|dirs| {
            let mut buf = dirs.home_dir().to_path_buf();
            buf.extend([".config", "doublezero", "cli", "config.yml"]);
            buf
        }),
    }
}

#[derive(Debug, Serialize, Deserialize)]
pub struct ClientConfig {
    pub json_rpc_url: String,
    pub websocket_url: Option<String>,
    #[serde(default = "default_keypair_path")]
    pub keypair_path: PathBuf,
    pub program_id: Option<String>,
    #[serde(default)]
    pub tenant: Option<String>,
    #[serde(default)]
    pub address_labels: HashMap<String, String>,
}

fn default_keypair_path() -> PathBuf {
    let mut keypair_path = dirs_next::home_dir().unwrap_or_default();
    keypair_path.extend([".config", "doublezero", "id.json"]);
    keypair_path
}

impl Default for ClientConfig {
    fn default() -> Self {
        ClientConfig {
            json_rpc_url: DEFAULT_ENVIRONMENT.config().unwrap().ledger_public_rpc_url,
            websocket_url: None,
            keypair_path: default_keypair_path(),
            program_id: None,
            tenant: None,
            address_labels: HashMap::new(),
        }
    }
}

pub fn read_doublezero_config() -> eyre::Result<(PathBuf, ClientConfig)> {
    match get_cfg_filename() {
        None => eyre::bail!("Unable to get_cfg_filename"),
        Some(filename) => match fs::read_to_string(&filename) {
            Err(_) => Ok((filename, ClientConfig::default())),
            Ok(config_content) => {
                let config: ClientConfig = serde_yaml::from_str(&config_content)?;
                Ok((filename, config))
            }
        },
    }
}

pub fn write_doublezero_config(config: &ClientConfig) -> eyre::Result<()> {
    match get_cfg_filename() {
        None => eyre::bail!("Unable to get_cfg_filename"),
        Some(filename) => {
            if let Some(parent) = filename.parent() {
                fs::create_dir_all(parent)?
            }

            let yaml_content = serde_yaml::to_string(config)?;
            fs::write(&filename, yaml_content)?;
            Ok(())
        }
    }
}

pub fn convert_url_moniker(url: String) -> String {
    match url.as_str() {
        "doublezero" => DEFAULT_ENVIRONMENT.config().unwrap().ledger_public_rpc_url,
        "localhost" => crate::consts::LOCALHOST_URL.to_string(),
        "devnet" => crate::consts::DEVNET_URL.to_string(),
        "testnet" => crate::consts::TESTNET_URL.to_string(),
        "mainnet-beta" => crate::consts::MAINNET_BETA_URL.to_string(),
        _ => url,
    }
}

pub fn convert_ws_moniker(url: String) -> String {
    match url.as_str() {
        "doublezero" => {
            DEFAULT_ENVIRONMENT
                .config()
                .unwrap()
                .ledger_public_ws_rpc_url
        }
        "localhost" => crate::consts::LOCALHOST_WS.to_string(),
        "devnet" => crate::consts::DEVNET_WS.to_string(),
        "testnet" => crate::consts::TESTNET_WS.to_string(),
        "mainnet-beta" => crate::consts::MAINNET_BETA_WS.to_string(),
        _ => url,
    }
}

pub fn convert_program_moniker(pubkey: String) -> String {
    match pubkey.as_str() {
        "devnet" => crate::devnet::program_id::id().to_string(),
        "testnet" => crate::testnet::program_id::id().to_string(),
        _ => pubkey,
    }
}

pub fn convert_url_to_ws(url: &str) -> eyre::Result<String> {
    if url == DEFAULT_ENVIRONMENT.config()?.ledger_public_rpc_url {
        return Ok(DEFAULT_ENVIRONMENT.config()?.ledger_public_ws_rpc_url);
    }

    let mut url = Url::parse(url)?;
    if url.scheme() == "https" {
        url.set_scheme("wss").ok();
    } else {
        url.set_scheme("ws").ok();
    }
    Ok(url.to_string())
}

pub fn create_new_pubkey_user(force: bool, outfile: Option<PathBuf>) -> eyre::Result<Keypair> {
    let file_path = match outfile {
        Some(path) => path,
        None => {
            let (_, client_cfg) = read_doublezero_config()?;
            client_cfg.keypair_path
        }
    };

    let dir_path = Path::new(&file_path)
        .parent()
        .ok_or_else(|| eyre::eyre!("Invalid keypair path: no parent directory"))?
        .to_str()
        .ok_or_else(|| eyre::eyre!("Invalid keypair path: contains invalid UTF-8"))?
        .to_string();

    let key = Keypair::new();

    if !Path::new(&dir_path).exists() {
        fs::create_dir_all(&dir_path)?;
    }

    if !force && Path::new(&file_path).exists() {
        eyre::bail!(
            "The file {} already exists (use doublezero keygen -f)",
            file_path.display()
        );
    }

    let data = key.to_bytes().to_vec();
    let json = serde_json::to_string(&data)?;
    let mut file = fs::File::create(&file_path)?;
    file.write_all(json.as_bytes())?;

    Ok(key)
}

pub fn get_doublezero_pubkey() -> eyre::Result<Keypair> {
    let (_, client_cfg) = read_doublezero_config()?;
    match fs::read_to_string(client_cfg.keypair_path) {
        Err(_) => eyre::bail!("Unable to read configured keypair_path"),
        Ok(key_content) => {
            let key_bytes: Vec<u8> = serde_json::from_str(&key_content)?;
            let key = Keypair::from_bytes(&key_bytes)?;
            Ok(key)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serial_test::serial;
    use solana_sdk::signature::Signer;
    use std::{env, fs};
    use tempfile::TempDir;

    #[test]
    #[serial]
    fn test_create_new_pubkey_user_creates_keypair_and_writes_file() {
        let tmp = TempDir::new().unwrap();
        let keypair_path = tmp.path().join("id.json");
        let config_path = tmp.path().join("config.yml");

        // Needs to be in a serial test.
        env::set_var("DOUBLEZERO_CONFIG_FILE", &config_path);

        let cfg = ClientConfig {
            json_rpc_url: "http://localhost:8899".into(),
            websocket_url: None,
            keypair_path: keypair_path.clone(),
            program_id: None,
            tenant: None,
            address_labels: Default::default(),
        };

        write_doublezero_config(&cfg).unwrap();

        let key = create_new_pubkey_user(false, None).unwrap();
        assert!(keypair_path.exists());

        let contents = fs::read_to_string(&keypair_path).unwrap();
        let bytes: Vec<u8> = serde_json::from_str(&contents).unwrap();
        let deserialized = Keypair::from_bytes(&bytes).unwrap();
        assert_eq!(deserialized.pubkey(), key.pubkey());
    }

    #[test]
    #[serial]
    fn test_create_new_pubkey_user_fails_if_exists_without_force() {
        let tmp = TempDir::new().unwrap();
        let keypair_path = tmp.path().join("id.json");
        let config_path = tmp.path().join("config.yml");

        // Needs to be in a serial test.
        env::set_var("DOUBLEZERO_CONFIG_FILE", &config_path);

        let cfg = ClientConfig {
            json_rpc_url: "http://localhost:8899".into(),
            websocket_url: None,
            keypair_path: keypair_path.clone(),
            program_id: None,
            tenant: None,
            address_labels: Default::default(),
        };

        write_doublezero_config(&cfg).unwrap();
        let _ = create_new_pubkey_user(false, None).unwrap();

        let err = create_new_pubkey_user(false, None).unwrap_err();
        assert!(err.to_string().contains("already exists"));
    }

    #[test]
    #[serial]
    fn test_create_new_pubkey_user_overwrites_with_force() {
        let tmp = TempDir::new().unwrap();
        let keypair_path = tmp.path().join("id.json");
        let config_path = tmp.path().join("config.yml");

        // Needs to be in a serial test.
        env::set_var("DOUBLEZERO_CONFIG_FILE", &config_path);

        let cfg = ClientConfig {
            json_rpc_url: "http://localhost:8899".into(),
            websocket_url: None,
            keypair_path: keypair_path.clone(),
            program_id: None,
            tenant: None,
            address_labels: Default::default(),
        };

        write_doublezero_config(&cfg).unwrap();
        let first = create_new_pubkey_user(false, None).unwrap();
        let second = create_new_pubkey_user(true, None).unwrap();
        assert_ne!(first.pubkey(), second.pubkey());
    }

    #[test]
    fn test_create_new_pubkey_user_with_explicit_outfile() {
        let tmp = TempDir::new().unwrap();
        let outfile_path = tmp.path().join("my-keypair.json");

        let result = create_new_pubkey_user(false, Some(outfile_path.clone()));
        assert!(result.is_ok());

        let key = result.unwrap();
        assert!(outfile_path.exists());

        let contents = fs::read_to_string(&outfile_path).unwrap();
        let bytes: Vec<u8> = serde_json::from_str(&contents).unwrap();
        let restored = Keypair::from_bytes(&bytes).unwrap();
        assert_eq!(key.pubkey(), restored.pubkey());
    }

    #[test]
    fn test_create_new_pubkey_user_outfile_exists_fails_without_force() {
        let tmp = TempDir::new().unwrap();
        let outfile_path = tmp.path().join("my-keypair.json");

        let first = create_new_pubkey_user(false, Some(outfile_path.clone())).unwrap();
        assert!(outfile_path.exists());

        let err = create_new_pubkey_user(false, Some(outfile_path.clone())).unwrap_err();
        assert!(err.to_string().contains("already exists"));

        let second = create_new_pubkey_user(true, Some(outfile_path.clone())).unwrap();
        assert_ne!(first.pubkey(), second.pubkey());
    }
}
