use crate::command::util;
use clap::Args;
use doublezero_cli::doublezerocommand::CliCommand;
use doublezero_sdk::{commands::device::list::ListDeviceCommand, DeviceStatus};
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;

use crate::{
    requirements::check_doublezero,
    servicecontroller::{ServiceController, ServiceControllerImpl},
};

#[derive(Args, Debug)]
pub struct LatencyCliCommand {
    /// Output as json
    #[arg(long, default_value = "false")]
    json: bool,
}

impl LatencyCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        check_doublezero(&controller, client, None).await?;

        let devices = client.list_device(ListDeviceCommand)?;
        let mut latencies = controller.latency().await.map_err(|e| eyre::eyre!(e))?;

        // Filter the active devices
        latencies.retain(|l| {
            Pubkey::from_str(&l.device_pk)
                .ok()
                .and_then(|pubkey| devices.get(&pubkey))
                .map(|device| device.status == DeviceStatus::Activated)
                .unwrap_or(false)
        });

        latencies.sort_by(|a, b| {
            let reachable_cmp = a.reachable.cmp(&b.reachable);
            if reachable_cmp != std::cmp::Ordering::Equal {
                return reachable_cmp;
            }
            a.avg_latency_ns
                .partial_cmp(&b.avg_latency_ns)
                .unwrap_or(std::cmp::Ordering::Equal)
        });

        util::show_output(latencies, self.json)?;

        Ok(())
    }
}
