use clap::Args;
use doublezero_cli::doublezerocommand::CliCommand;
use doublezero_sdk::{commands::device::list::ListDeviceCommand, DeviceStatus};
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;
use tabled::{settings::Style, Table};

use crate::{requirements::check_doublezero, servicecontroller::ServiceController};

#[derive(Args, Debug)]
pub struct LatencyCliCommand {}

impl LatencyCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        check_doublezero(None)?;

        let controller = ServiceController::new(None);
        let devices = client.list_device(ListDeviceCommand {})?;
        let mut latencies = controller.latency().await.map_err(|e| eyre::eyre!(e))?;
        // Filter the active devices
        latencies.retain(
            |l| match devices.get(&Pubkey::from_str(&l.device_pk).unwrap()) {
                Some(device) => device.status == DeviceStatus::Activated,
                None => false,
            },
        );
        latencies.sort_by(|a, b| a.avg_latency_ns.cmp(&b.avg_latency_ns));

        let table = Table::new(latencies)
            .with(Style::psql().remove_horizontals())
            .to_string();
        println!("{}", table);

        Ok(())
    }
}
