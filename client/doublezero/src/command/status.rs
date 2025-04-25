use clap::Args;
use doublezero_sdk::{DZClient, ServiceController};

use doublezero_cli::{
    helpers::print_error,
};

use doublezero_cli::helpers::init_command;
use crate::requirements::check_doublezero;

#[derive(Args, Debug)]
pub struct StatusArgs {}

impl StatusArgs {
    pub async fn execute(self, _client: &DZClient) -> eyre::Result<()> {
        let spinner = init_command();
        let controller = ServiceController::new(None);

        // Check requirements
        check_doublezero(Some(&spinner))?;

        match controller.status().await {
            Ok(status) => {
                println!(
                    "Tunnel status: {}\nName: {}\nTunnel src: {}\nTunnel dst: {}\nDoublezero IP: {}",
                    status.status,
                    status.tunnel_name.unwrap_or_default(),
                    status.tunnel_src.unwrap_or_default(),
                    status.tunnel_dst.unwrap_or_default(),
                    status.doublezero_ip.unwrap_or_default()
                );
            }
            Err(e) => {
                print_error(e);
            }
        }

        Ok(())
    }
}
