use clap::{ArgGroup, Args};
use double_zero_sdk::{convert_url_to_ws, read_doublezero_config, write_doublezero_config, DZClient};

#[derive(Args, Debug)]
#[clap(group(
    ArgGroup::new("mandatory")
        .args(&["url", "ws", "keypair", "program"])
        .required(true)
        .multiple(true)
))]
pub struct SetConfigArgs {
    #[arg(long)]
    url: Option<String>,
    #[arg(long)]
    ws: Option<String>,
    #[arg(long)]
    keypair: Option<String>,
    #[arg(long)]
    program: Option<String>,
}

impl SetConfigArgs {
    pub async fn execute(self, _: &DZClient) -> eyre::Result<()> {
        if self.url.is_none() && self.ws.is_none() && self.keypair.is_none() && self.program.is_none() {
            eprintln!("No arguments provided");
            return Ok(());
        }

        let (filename, mut config) = read_doublezero_config();
        if let Some(url) = self.url {
            config.json_rpc_url = convert_url_moniker(url);
        }
        if let Some(ws) = self.ws {
            config.websocket_url = Some(convert_ws_moniker(ws));
        }
        if let Some(keypair) = self.keypair {
            config.keypair_path = keypair;
        }
        if let Some(program_id) = self.program {
            config.program_id = Some(convert_program_moniker(program_id));
        }

        write_doublezero_config(&config);

        println!("Config File: {}\nRPC URL: {}\nWebSocket URL: {}\nKeypair Path: {}\nProgram ID: {}\n",
            filename,
            config.json_rpc_url,
            config.websocket_url.unwrap_or(format!("{} (computed)", convert_url_to_ws(&config.json_rpc_url))),
            config.keypair_path,
            config.program_id.unwrap_or(format!("{} (computed)",  double_zero_sdk::testnet::program_id::id())));

        Ok(())
    }
}


fn convert_url_moniker(url: String) -> String {
    match url.as_str() {
        "localhost" => "http://localhost:8899".to_string(),
        "devnet" => "https://api.devnet.solana.com".to_string(),
        "testnet" => "https://api.testnet.solana.com".to_string(),
        "mainnet" => "https://api.mainnet-beta.solana.com".to_string(),
        _ => url,
    }
}

fn convert_ws_moniker(url: String) -> String {
    match url.as_str() {
        "localhost" => "ws://localhost:8899".to_string(),
        "devnet" => "wss://api.devnet.solana.com".to_string(),
        "testnet" => "wss://api.testnet.solana.com".to_string(),
        "mainnet" => "wss://api.mainnet-beta.solana.com".to_string(),
        _ => url,
    }
}

fn convert_program_moniker(pubkey: String) -> String {
    match pubkey.as_str() {
        "devnet" => double_zero_sdk::devnet::program_id::id().to_string(),
        "testnet" => double_zero_sdk::testnet::program_id::id().to_string(),
        _ => pubkey,
    }
}
