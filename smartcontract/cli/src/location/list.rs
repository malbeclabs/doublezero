use clap::Args;
use doublezero_sdk::commands::location::list::ListLocationCommand;
use doublezero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListLocationArgs {
    #[arg(long)]
    pub code: Option<String>,
}

impl ListLocationArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let mut table = Table::new();
        table.add_row(row![
            "pubkey", "code", "name", "country", "lat", "lng", "loc_id", "status", "owner"
        ]);

        let locations = ListLocationCommand {}.execute(client)?;

        let mut locations: Vec<(Pubkey, Location)> = locations.into_iter().collect();

        locations.sort_by(|(_, a), (_, b)| {
            a.owner.cmp(&b.owner)
            });

        for (pubkey, data) in locations {
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
        table.print(out);

        Ok(())
    }
}
