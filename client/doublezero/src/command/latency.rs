use clap::Args;
use doublezero_cli::doublezerocommand::CliCommand;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use doublezero_sdk::DeviceStatus;
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;
use tabled::builder::Builder;
use tabled::settings::Style;

use crate::requirements::check_doublezero;
use crate::servicecontroller::ServiceController;

#[derive(Args, Debug)]
pub struct LatencyCliCommand {}

impl LatencyCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        check_doublezero(None)?;

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

        let mut builder = Builder::new();
        // TODO: proper header?
        builder.push_record(["pubkey", "name", "ip", "min", "max", "avg", "teachable"]);


        for data in latencies {
            let device_name =
                match devices.get(&Pubkey::from_str(&data.device_pk).expect("Invalid pubkey")) {
                    Some(device) => &device.code,
                    None => &"".to_string(),
                };

            builder.push_record([
                data.device_pk.as_str(),
                device_name,
                data.device_ip.as_str(),
                &format!("{:.2}ms", (data.min_latency_ns as f32 / 1000000.0)),
                &format!("{:.2}ms", (data.max_latency_ns as f32 / 1000000.0)),
                &format!("{:.2}ms", (data.avg_latency_ns as f32 / 1000000.0)),
                &data.reachable.to_string(),
            ]);
        }

        let mut table = builder.build();
        table.with(Style::psql().remove_horizontals());
        println!("{}", table);

        Ok(())
    }
}
