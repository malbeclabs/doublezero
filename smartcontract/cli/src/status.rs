use clap::Args;
use doublezero_sdk::{DZClient, ServiceController};

use crate::{
    helpers::print_error,
    requirements::{check_requirements, CHECK_DOUBLEZEROD},
};

use super::helpers::init_command;

#[derive(Args, Debug)]
pub struct StatusArgs {}

impl StatusArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        let spinner = init_command();
        let controller = ServiceController::new(None);

        // Check requirements
        check_requirements(client, Some(&spinner), CHECK_DOUBLEZEROD)?;
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
