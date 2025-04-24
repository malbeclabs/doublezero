use clap::Args;
use doublezero_sdk::{convert_url_to_ws, read_doublezero_config, DZClient};

#[derive(Args, Debug)]
pub struct GetConfigArgs {}

impl GetConfigArgs {
    pub fn execute(self, _: &DZClient) -> eyre::Result<()> {
        let (filename, config) = read_doublezero_config();

        println!(
            "Config File: {}\nRPC URL: {}\nWebSocket URL: {}\nKeypair Path: {}\nProgram ID: {}\n",
            filename,
            config.json_rpc_url,
            config.websocket_url.unwrap_or(format!(
                "{} (computed)",
                convert_url_to_ws(&config.json_rpc_url)
            )),
            config.keypair_path,
            config.program_id.unwrap_or(format!(
                "{} (computed)",
                doublezero_sdk::testnet::program_id::id()
            ))
        );

        Ok(())
    }
}
