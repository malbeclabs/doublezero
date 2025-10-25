use clap::Args;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetAccountsCliCommand {
    // Filter by account type
    #[arg(long)]
    pub account_type: Option<String>,
}

impl GetAccountsCliCommand {
    pub fn execute<W: Write>(self, client: &DZClient, out: &mut W) -> eyre::Result<()> {
        let mut accounts = client
            .get_all()?
            .into_iter()
            .map(|acc| (acc.0.to_string(), acc.1))
            .collect::<Vec<_>>();

        if let Some(ref account_type) = self.account_type {
            let account_type = account_type.to_lowercase();
            accounts.retain(|(_, acc)| acc.get_name().to_lowercase() == account_type);
        }

        for (i, (pk, account)) in accounts.into_iter().enumerate() {
            writeln!(out, "{i} {pk}: {:?}", account)?;
        }

        Ok(())
    }
}
