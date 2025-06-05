use serde::{Deserialize, Serialize};
use solana_client::client_error::reqwest::Url;
use solana_sdk::signature::Keypair;
use std::{
    collections::HashMap,
    fs::{self, File},
    io::{self, Write},
    path::{Path, PathBuf},
    sync::OnceLock,
};

static CONFIG_FILE: OnceLock<String> = OnceLock::new();

/// The default path to the CLI configuration file.
///
/// > `~/.config/doublezero/cli/config.yml`
///
/// Falls back to `./config.yml` if unable to identify the user's home directory.
fn get_cfg_filename() -> &'static String {
    CONFIG_FILE.get_or_init(|| match directories_next::UserDirs::new() {
        None => {
            // Fallback to current dir
            "./config.yml".to_string()
        }
        Some(dirs) => {
            let mut buf = PathBuf::new();
            buf.push(dirs.home_dir().to_str().unwrap());
            buf.extend([".config", "doublezero", "cli", "config.yml"]);
            buf.to_string_lossy().to_string()
        }
    })
}

#[derive(Debug, Serialize, Deserialize)]
pub struct ClientConfig {
    pub json_rpc_url: String,
    pub websocket_url: Option<String>,
    pub keypair_path: String,
    pub program_id: Option<String>,
    pub address_labels: HashMap<String, String>,
}

pub fn read_doublezero_config() -> eyre::Result<(String, ClientConfig)> {
    let filename = get_cfg_filename();

    match fs::read_to_string(filename) {
        Err(_) => Ok((
            filename.to_string(),
            ClientConfig {
                json_rpc_url: crate::consts::DOUBLEZERO_URL.to_string(),
                websocket_url: None,
                keypair_path: {
                    let mut keypair_path = dirs_next::home_dir().unwrap_or_default();
                    keypair_path.extend([".config", "doublezero", "id.json"]);
                    keypair_path.to_str().unwrap().to_string()
                },
                program_id: None,
                address_labels: HashMap::new(),
            },
        )),
        Ok(config_content) => {
            let config: ClientConfig = serde_yaml::from_str(&config_content)?;
            Ok((filename.to_string(), config))
        }
    }
}

pub fn write_doublezero_config(config: &ClientConfig) -> eyre::Result<()> {
    let config_file = get_cfg_filename();
    let path = Path::new(config_file);

    // Create parent directories if they don't exist
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }

    // Serialize and write the config
    let yaml_content = serde_yaml::to_string(config)?;
    fs::write(config_file, yaml_content)?;

    Ok(())
}

pub fn convert_url_moniker(url: String) -> String {
    match url.as_str() {
        "doublezero" => crate::consts::DOUBLEZERO_URL.to_string(),
        "localhost" => crate::consts::LOCALHOST_URL.to_string(),
        "devnet" => crate::consts::DEVNET_URL.to_string(),
        "testnet" => crate::consts::TESTNET_URL.to_string(),
        "mainnet" => crate::consts::MAINNET_BETA_URL.to_string(),
        _ => url,
    }
}

pub fn convert_ws_moniker(url: String) -> String {
    match url.as_str() {
        "doublezero" => crate::consts::DOUBLEZERO_WS.to_string(),
        "localhost" => crate::consts::LOCALHOST_WS.to_string(),
        "devnet" => crate::consts::DEVNET_WS.to_string(),
        "testnet" => crate::consts::TESTNET_WS.to_string(),
        "mainnet" => crate::consts::MAINNET_BETA_WS.to_string(),
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

pub fn convert_url_to_ws(url: &str) -> String {
    if url == crate::consts::DOUBLEZERO_URL {
        return crate::consts::DOUBLEZERO_WS.to_string();
    }

    let mut url = Url::parse(url).map_err(|_| "Invalid URL").unwrap();
    if url.scheme() == "https" {
        url.set_scheme("wss").ok();
    } else {
        url.set_scheme("ws").ok();
    }
    url.to_string()
}

pub fn create_new_pubkey_user(force: bool) -> std::io::Result<Keypair> {
    let key = Keypair::new();

    if let Some(home_dir) = dirs_next::home_dir() {
        let dir_path = home_dir.join(".config/doublezero");
        if !Path::new(&dir_path).exists() {
            fs::create_dir_all(&dir_path)?;
        }

        let file_path = home_dir.join(".config/doublezero/id.json");

        if !force && Path::new(&file_path).exists() {
            return Err(io::Error::new(
                io::ErrorKind::AlreadyExists,
                format!(
                    "The file {} already exists (use doublezero keygen -f)",
                    file_path.to_str().unwrap()
                ),
            ));
        }

        let data = key.to_bytes().to_vec();
        let json = serde_json::to_string(&data).ok().unwrap();
        let mut file = File::create(&file_path)?;
        file.write_all(json.as_bytes())?;
    };

    Ok(key)
}

pub fn get_doublezero_pubkey() -> Option<Keypair> {
    match dirs_next::home_dir() {
        Some(home_dir) => {
            let dir_path = home_dir.join(".config/doublezero");
            if !Path::new(&dir_path).exists() {
                fs::create_dir_all(&dir_path).unwrap();
            }

            let file_path = home_dir.join(".config/doublezero/id.json");
            match fs::read_to_string(file_path) {
                Ok(key_content) => {
                    let key_bytes: Vec<u8> = serde_json::from_str(&key_content).unwrap();
                    let key = Keypair::from_bytes(&key_bytes).unwrap();

                    Some(key)
                }
                Err(_) => None,
            }
        }
        None => None,
    }
}
