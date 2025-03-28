use clap::Args;
use double_zero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};

#[derive(Args, Debug)]
pub struct ListDeviceArgs {
    #[arg(long)]
    pub code: Option<String>,
}


impl ListDeviceArgs {
    pub async fn execute<T:DeviceService + LocationService + ExchangeService>(self, client: &T) -> eyre::Result<()> {
        let mut table = Table::new();
        table.add_row(row![
            "pubkey",
            "code",
            "location",
            "exchange",
            "device_type",
            "public_ip",
            "dz_ef_pools",
            "status",
            "owner"
        ]);

        let locations = client.get_locations()?;
        let exchanges = client.get_exchanges()?;

        for (pubkey, data) in client.get_devices()? {
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
                Cell::new(&networkv4_list_to_string(&data.dz_ef_pools)),
                Cell::new(&data.status.to_string()),
                Cell::new(&data.owner.to_string()),
            ]));
        }

        table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
        table.printstd();

        Ok(())
    }
}