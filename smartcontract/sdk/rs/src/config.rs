use serde::{Deserialize, Serialize};
use solana_client::client_error::reqwest::Url;
use solana_sdk::signature::Keypair;
use std::{
    collections::HashMap,
    default::Default,
    env, fs,
    io::Write,
    path::{Path, PathBuf},
    sync::OnceLock,
};

static CONFIG_FILE: OnceLock<Option<String>> = OnceLock::new();

/// The default path to the CLI configuration file.
///
/// > `~/.config/doublezero/cli/config.yml`
///
/// It will only be `None` if it is unable to identify the user's home
/// directory, which should not happen under typical OS environments.
fn get_cfg_filename() -> &'static Option<String> {
    CONFIG_FILE.get_or_init(|| match env::var_os("DOUBLEZERO_CONFIG_FILE") {
        Some(path) => Some(path.to_str().unwrap().to_string()),
        None => match directories_next::UserDirs::new() {
            Some(dirs) => {
                let mut buf = PathBuf::new();
                buf.push(dirs.home_dir().to_str().unwrap());
                buf.extend([".config", "doublezero", "cli", "config.yml"]);
                Some(buf.to_string_lossy().to_string())
            }
            None => None,
        },
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

impl Default for ClientConfig {
    fn default() -> Self {
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
        }
    }
}

pub fn read_doublezero_config() -> eyre::Result<(String, ClientConfig)> {
    match get_cfg_filename() {
        None => eyre::bail!("Unable to get_cfg_filename"),
        Some(filename) => match fs::read_to_string(filename) {
            Err(_) => Ok((filename.clone(), ClientConfig::default())),
            Ok(config_content) => {
                let config: ClientConfig = serde_yaml::from_str(&config_content).unwrap();

                Ok((filename.clone(), config))
            }
        },
    }
}

pub fn write_doublezero_config(config: &ClientConfig) -> eyre::Result<()> {
    match get_cfg_filename() {
        None => eyre::bail!("Unable to get_cfg_filename"),
        Some(filename) => {
            let path = Path::new(filename);

            if let Some(parent) = path.parent() {
                fs::create_dir_all(parent)?
            }

            let yaml_content = serde_yaml::to_string(config)?;
            fs::write(filename, yaml_content)?;
            Ok(())
        }
    }
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

pub fn create_new_pubkey_user(force: bool) -> eyre::Result<Keypair> {
    let (_, client_cfg) = read_doublezero_config()?;
    let file_path = client_cfg.keypair_path.clone();
    let dir_path = Path::new(&file_path)
        .parent()
        .unwrap()
        .to_str()
        .unwrap()
        .to_string();
    let key = Keypair::new();

    if !Path::new(&dir_path).exists() {
        fs::create_dir_all(&dir_path)?;
    }

    if !force && Path::new(&file_path).exists() {
        eyre::bail!(
            "The file {} already exists (use doublezero keygen -f)",
            file_path
        );
    }

    let data = key.to_bytes().to_vec();
    let json = serde_json::to_string(&data).ok().unwrap();
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
            #[allow(deprecated)] //TODO: not sure why this is being triggered
            let key = Keypair::from_bytes(&key_bytes)?;
            Ok(key)
        }
    }
}
