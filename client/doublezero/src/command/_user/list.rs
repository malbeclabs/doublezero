use clap::Args;
use double_zero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};

#[derive(Args, Debug)]
pub struct ListUserArgs {
    #[arg(long)]
    pub code: Option<String>,    
    #[arg(long)]
    pub json: bool,
}

impl ListUserArgs {
    pub async fn execute<T:DoubleZeroClient>(self, client: &T) -> eyre::Result<()> {

        if self.json {
            println!("XX");
            let users = client.get_users()?;
            println!("{}", serde_json::to_string_pretty(&users)?);
            return Ok(());
        }


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

        let devices = client.get_devices()?;

        for (pubkey, data) in client.get_users()? {

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
