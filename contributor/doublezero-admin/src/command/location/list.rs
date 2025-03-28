use clap::Args;
use double_zero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};

#[derive(Args, Debug)]
pub struct ListLocationArgs {
    #[arg(long)]
    pub code: Option<String>,
}

impl ListLocationArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        let mut table = Table::new();
        table.add_row(row![
            "pubkey", "code", "name", "country", "lat", "lng", "loc_id", "status", "owner"
        ]);

        for (pubkey, data) in client.get_locations()? {
            table.add_row(Row::new(vec![
                Cell::new(&pubkey.to_string()),
                Cell::new(&data.code),
                Cell::new(&data.name),
                Cell::new(&data.country),
                Cell::new(&data.lat.to_string()),
                Cell::new(&data.lng.to_string()),
                Cell::new(&data.loc_id.to_string()),
                Cell::new(&data.status.to_string()),
                Cell::new(&data.owner.to_string()),
            ]));
        }

        table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
        table.printstd();

        Ok(())
    }
}
