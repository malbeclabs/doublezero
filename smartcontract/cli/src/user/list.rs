use clap::Args;
use doublezero_sdk::*;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use doublezero_sdk::commands::user::list::ListUserCommand;
use prettytable::{format, row, Cell, Row, Table};

#[derive(Args, Debug)]
pub struct ListUserArgs {
    #[arg(long)]
    pub code: Option<String>,
}

impl ListUserArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        let mut table = Table::new();
        table.add_row(row![
            "pubkey",
            "user_type",
            "device",
            "cyoa_type",
            "client_ip",
            "tunnel_id",
            "tunnel_net",
            "dz_ip",
            "status",
            "owner"
        ]);

        let devices = ListDeviceCommand{}.execute(client)?;

        let users = ListUserCommand{}.execute(client)?;

        for (pubkey, data) in users {
            let device_name = match &devices.get(&data.device_pk) {
                Some(device) => &device.code,
                None => &data.device_pk.to_string()
            };

            table.add_row(Row::new(vec![
                Cell::new(&pubkey.to_string()),
                Cell::new(&data.user_type.to_string()),
                Cell::new(device_name),
                Cell::new(&data.cyoa_type.to_string()),
                Cell::new(&ipv4_to_string(&data.client_ip)),
                Cell::new(&data.tunnel_id.to_string()),
                Cell::new(&networkv4_to_string(&data.tunnel_net)),
                Cell::new(&ipv4_to_string(&data.dz_ip)),
                Cell::new(&data.status.to_string()),
                Cell::new(&data.owner.to_string()),
            ]));
        }

        table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
        table.printstd();

        Ok(())
    }
}
