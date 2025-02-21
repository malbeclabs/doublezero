use clap::Args;
use double_zero_sdk::{DZClient, ServiceController};

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
                    "Tunnel name: {}\nTunnel src: {}\nTunnel dst: {}\nDoublezero IP: {}",
                    status.tunnel_name, status.tunnel_src, status.tunnel_dst, status.doublezero_ip
                );
            }
            Err(e) => {
                print_error(e);
            }
        }

        Ok(())
    }
}
