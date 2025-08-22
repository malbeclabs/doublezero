use crate::doublezerocommand::CliCommand;
use ::serde::Serialize;
use clap::Args;
use doublezero_sdk::GetGlobalStateCommand;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct GetAirdropCliCommand;

#[derive(Tabled, Serialize)]
pub struct AirdropDisplay {
    pub contributor_airdrop_lamports: u64,
    pub user_airdrop_lamports: u64,
}

impl GetAirdropCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, gstate) = client.get_globalstate(GetGlobalStateCommand)?;

        let config_display = AirdropDisplay {
            contributor_airdrop_lamports: gstate.contributor_airdrop_lamports,
            user_airdrop_lamports: gstate.user_airdrop_lamports,
        };
        let config_displays = vec![config_display];
        let table = Table::new(config_displays)
            .with(Style::psql().remove_horizontals())
            .to_string();
        writeln!(out, "{table}")?;

        Ok(())
    }
}
