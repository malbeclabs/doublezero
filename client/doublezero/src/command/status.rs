use crate::{
    command::util,
    dzd_latency::best_latency,
    requirements::check_doublezero,
    servicecontroller::{ServiceController, ServiceControllerImpl, StatusResponse},
};
use clap::Args;
use doublezero_cli::{doublezerocommand::CliCommand, helpers::print_error};
use doublezero_sdk::commands::{
    device::list::ListDeviceCommand, exchange::list::ListExchangeCommand,
    user::list::ListUserCommand,
};
use serde::{Deserialize, Serialize};
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct StatusCliCommand {
    /// Output as json
    #[arg(long, default_value = "false")]
    json: bool,
}

#[derive(Tabled, Debug, Deserialize, Serialize)]
struct AppendedStatusResponse {
    #[tabled(inline)]
    response: StatusResponse,
    current_device: String,
    best_device: String,
    metro: String,
    network: String,
}

impl StatusCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);

        // Check requirements
        check_doublezero(&controller, client, None).await?;

        let devices = client.list_device(ListDeviceCommand)?;
        let users = client.list_user(ListUserCommand)?;
        let exchanges = client.list_exchange(ListExchangeCommand)?;

        match controller.status().await {
            Err(e) => print_error(e),
            Ok(status_responses) => {
                let mut responses = Vec::with_capacity(status_responses.len());
                if !status_responses.is_empty() {
                    for response in &status_responses {
                        let user = users
                            .iter()
                            .find(|(_, u)| Some(u.dz_ip.to_string()) == response.doublezero_ip)
                            .expect("user not found by dz_ip")
                            .1;
                        let opt_device = devices.get(&user.device_pk);
                        let current_device = opt_device
                            .map(|d| d.code.clone())
                            .unwrap_or_else(|| "unknown".to_string());
                        let best =
                            best_latency(&controller, &devices, true, None, Some(&user.device_pk))
                                .await?;
                        let best_code;
                        if best.device_code != current_device {
                            best_code = format!("❌ {}", best.device_code);
                        } else {
                            best_code = format!("✅ {}", best.device_code);
                        }
                        let metro = if let Some(dev) = opt_device {
                            exchanges
                                .get(&dev.exchange_pk)
                                .map(|e| e.name.clone())
                                .unwrap_or_else(|| "unknown".to_string())
                        } else {
                            "unknown".to_string()
                        };
                        let network = format!("{}", client.get_environment());
                        responses.push(AppendedStatusResponse {
                            response: response.clone(),
                            current_device,
                            best_device: best_code,
                            metro,
                            network,
                        })
                    }
                    util::show_output(responses, self.json)?
                }
            }
        }

        Ok(())
    }
}
