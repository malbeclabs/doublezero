use clap::Args;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use doublezero_sdk::commands::exchange::list::ListExchangeCommand;
use doublezero_sdk::commands::location::list::ListLocationCommand;
use doublezero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};
use solana_sdk::pubkey::Pubkey;

#[derive(Args, Debug)]
pub struct ListDeviceArgs {
    #[arg(long)]
    pub code: Option<String>,
}

impl ListDeviceArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
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

        let locations = ListLocationCommand {}.execute(client)?;
        let exchanges = ListExchangeCommand {}.execute(client)?;

        let devices = ListDeviceCommand {}.execute(client)?;

        let mut  devices: Vec<(Pubkey, Device)> = devices.into_iter().collect();
        devices.sort_by(|(_,a ), (_, b)| {
            a.owner.cmp(&b.owner)
            });

        for (pubkey, data) in devices {
            let loc_name = match &locations.get(&data.location_pk) {
                Some(location) => &location.code,
                None => &data.location_pk.to_string(),
            };
            let exch_name = match &exchanges.get(&data.exchange_pk) {
                Some(exchange) => &exchange.code,
                None => &data.exchange_pk.to_string(),
            };

            table.add_row(Row::new(vec![
                Cell::new(&pubkey.to_string()),
                Cell::new(&data.code),
                Cell::new(loc_name),
                Cell::new(exch_name),
                Cell::new(&data.device_type.to_string()),
                Cell::new(&ipv4_to_string(&data.public_ip)),
                Cell::new(&networkv4_list_to_string(&data.dz_prefixes)),
                Cell::new(&data.status.to_string()),
                Cell::new(&data.owner.to_string()),
            ]));
        }

        table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
        table.printstd();

        Ok(())
    }
}
