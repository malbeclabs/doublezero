use crate::requirements::check_doublezero;
use crate::servicecontroller::{ServiceController, ServiceControllerImpl};
use chrono::prelude::*;
use clap::Args;
use doublezero_cli::helpers::init_command;
use doublezero_cli::{doublezerocommand::CliCommand, helpers::print_error};

#[derive(Args, Debug)]
pub struct StatusCliCommand {}

impl StatusCliCommand {
    pub async fn execute(self, _client: &dyn CliCommand) -> eyre::Result<()> {
        let spinner = init_command();
        let controller = ServiceControllerImpl::new(None);

        // Check requirements
        check_doublezero(&controller, Some(&spinner))?;

        match controller.status().await {
            Ok(status) => {
                for status in status {
                    let last_session_update = status
                        .doublezero_status
                        .last_session_update
                        .unwrap_or_default();
                    let parsed_last_session_update = if last_session_update == 0 {
                        "no session data"
                    } else {
                        &DateTime::from_timestamp(last_session_update, 0)
                            .expect("invalid timestamp")
                            .to_string()
                    };

                    println!(
                    "Tunnel status: {}\nName: {}\nTunnel src: {}\nTunnel dst: {}\nDoublezero IP: {}\nUser type: {}\nLast Session Update: {}",
                    status.doublezero_status.session_status,
                    status.tunnel_name.unwrap_or_default(),
                    status.tunnel_src.unwrap_or_default(),
                    status.tunnel_dst.unwrap_or_default(),
                    status.doublezero_ip.unwrap_or_default(),
                    status.user_type.unwrap_or_default(),
                    parsed_last_session_update,
                );
                }
            }
            Err(e) => {
                print_error(e);
            }
        }

        Ok(())
    }
}
