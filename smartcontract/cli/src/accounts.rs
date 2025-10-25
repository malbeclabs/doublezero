use clap::Args;
use doublezero_config::Environment;
use doublezero_sdk::*;
use serde_json::to_writer_pretty;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetAccountsCliCommand {
    // Filter by account type
    #[arg(long)]
    pub account_type: Option<String>,
}

#[derive(serde::Serialize, serde::Deserialize)]
pub struct GetAccountsCliResponse {
    pub env: String,
    pub accounts: Vec<AccountsCliResponse>,
}

#[derive(serde::Serialize, serde::Deserialize)]
pub struct AccountsCliResponse {
    pub pubkey: String,
    pub account_type: String,
    pub account: Box<AccountData>,
}

impl GetAccountsCliCommand {
    pub fn execute<W: Write>(self, client: &DZClient, out: &mut W) -> eyre::Result<()> {
        let mut accounts: Vec<(String, Box<AccountData>)> = client
            .get_all()?
            .into_iter()
            .map(|acc| (acc.0.to_string(), acc.1))
            .collect::<Vec<_>>();

        if let Some(ref account_type) = self.account_type {
            let account_type = account_type.to_lowercase();
            accounts.retain(|(_, acc)| acc.get_name().to_lowercase() == account_type);
        }

        let res = GetAccountsCliResponse {
            env: Environment::from_program_id(&client.get_program_id().to_string())?.to_string(),
            accounts: accounts
                .into_iter()
                .map(|(pubkey, account)| AccountsCliResponse {
                    pubkey,
                    account_type: account.get_name().to_string(),
                    account,
                })
                .collect(),
        };

        to_writer_pretty(&mut *out, &res)?;
        writeln!(out)?;

        Ok(())
    }
}
