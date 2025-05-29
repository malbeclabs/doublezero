use clap::Args;
use doublezero_cli::doublezerocommand::CliCommand;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use doublezero_sdk::DeviceStatus;
use prettytable::{format, row, Cell, Row, Table};
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;

use crate::requirements::check_doublezero;
use crate::servicecontroller::ServiceController;

#[derive(Args, Debug)]
pub struct LatencyCliCommand {}

impl LatencyCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        check_doublezero(None)?;

        let mut table = Table::new();
        table.add_row(row![
            "pubkey",
            "name",
            "ip",
            "min",
            "max",
            "avg",
            "reachable"
        ]);

        let controller = ServiceController::new(None);
        let devices = client.list_device(ListDeviceCommand {})?;
        let mut latencies = controller.latency().await.map_err(|e| eyre::eyre!(e))?;
        latencies.retain(
            |l| match devices.get(&Pubkey::from_str(&l.device_pk).unwrap()) {
                Some(device) => device.status == DeviceStatus::Activated,
                None => false,
            },
        ); // Filter the active devices
        latencies.sort_by(|a, b| a.avg_latency_ns.cmp(&b.avg_latency_ns));

        for data in latencies {
            let device_name =
                match devices.get(&Pubkey::from_str(&data.device_pk).expect("Invalid pubkey")) {
                    Some(device) => &device.code,
                    None => &"".to_string(),
                };

            table.add_row(Row::new(vec![
                Cell::new(&data.device_pk.to_string()),
                Cell::new(device_name),
                Cell::new(&data.device_ip.to_string()),
                Cell::new_align(
                    &format!("{:.2}ms", (data.min_latency_ns as f32 / 1000000.0)),
                    format::Alignment::RIGHT,
                ),
                Cell::new_align(
                    &format!("{:.2}ms", (data.max_latency_ns as f32 / 1000000.0)),
                    format::Alignment::RIGHT,
                ),
                Cell::new_align(
                    &format!("{:.2}ms", (data.avg_latency_ns as f32 / 1000000.0)),
                    format::Alignment::RIGHT,
                ),
                Cell::new(&data.reachable.to_string()),
            ]));
        }

        table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
        table.printstd();

        Ok(())
    }
}
