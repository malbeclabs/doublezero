use clap::Args;
use double_zero_sdk::*;
use double_zero_sdk::commands::device::list::ListDeviceCommand;
use double_zero_sdk::commands::tunnel::list::ListTunnelCommand;
use prettytable::{format, row, Cell, Row, Table};

#[derive(Args, Debug)]
pub struct ListTunnelArgs {
    #[arg(long)]
    pub code: Option<String>,
}

impl ListTunnelArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        let mut table = Table::new();
        table.add_row(row![
            "pubkey",
            "code",
            "location",
            "exchange",
            "device_type",
            "public_ip",
            "dz_prefixes",
            "status",
            "owner"
        ]);


        let devices = ListDeviceCommand{}.execute(client)?;
        let tunnels = ListTunnelCommand{}.execute(client)?;

        for (pubkey, data) in tunnels {
            let side_a_name = match &devices.get(&data.side_a_pk) {
                Some(device) => &device.code,
                None => &data.side_a_pk.to_string()
            };
            let side_z_name = match &devices.get(&data.side_z_pk) {
                Some(device) => &device.code,
                None => &data.side_z_pk.to_string()
            };

            table.add_row(Row::new(vec![
                Cell::new(&pubkey.to_string()),
                Cell::new(&data.code),
                Cell::new(side_a_name),
                Cell::new(side_z_name),
                Cell::new(&data.tunnel_type.to_string()),
                Cell::new(&bandwidth_to_string(data.bandwidth)),
                Cell::new_align(&data.mtu.to_string(), format::Alignment::RIGHT),
                Cell::new_align(&delay_to_string(data.delay_ns), format::Alignment::RIGHT),
                Cell::new_align(&jitter_to_string(data.jitter_ns), format::Alignment::RIGHT),
                Cell::new(&data.tunnel_id.to_string()),
                Cell::new(&networkv4_to_string(&data.tunnel_net)),
                Cell::new(&data.status.to_string()),
                Cell::new(&data.owner.to_string()),
            ]));
        }

        table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
        table.printstd();

        Ok(())
    }
}
