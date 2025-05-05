use clap::Args;
use doublezero_sdk::commands::exchange::list::ListExchangeCommand;
use doublezero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListExchangeArgs {
    #[arg(long)]
    pub code: Option<String>,
}

impl ListExchangeArgs {



    pub fn execute<W: Write>(self, client: &DZClient, out: &mut W) -> eyre::Result<()> {
        let mut table = Table::new();
        table.add_row(row![
            "pubkey", "code", "name", "lat", "lng", "loc_id", "status", "owner"
        ]);

        let exchanges = ListExchangeCommand {}.execute(client)?;

        let mut exchanges: Vec<(Pubkey, Exchange)> = exchanges.into_iter().collect();
        exchanges.sort_by(|(_, a), (_, b)| {
            a.owner.cmp(&b.owner)
        });

        for (pubkey, data) in exchanges {
            table.add_row(Row::new(vec![
                Cell::new(&pubkey.to_string()),
                Cell::new(&data.code),
                Cell::new(&data.name),
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
