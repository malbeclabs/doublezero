use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_pubkey,
    validators::{validate_pubkey, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::{get::GetFeedCommand, update::UpdateFeedCommand};
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateFeedCliCommand {
    /// Feed pubkey or code to update
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Updated name for the feed
    #[arg(long)]
    pub name: Option<String>,
    /// Replace the feed's multicast group set with these pubkeys (repeatable). When omitted, the
    /// groups are left unchanged.
    #[arg(long = "group", value_parser = validate_pubkey, num_args = 1..)]
    pub groups: Vec<String>,
}

impl UpdateFeedCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        require!(
            client,
            RequirementCheck::KEYPAIR | RequirementCheck::BALANCE
        );

        let (pubkey, _feed) = client.get_feed(GetFeedCommand {
            pubkey_or_code: self.pubkey,
        })?;

        // An empty `--group` list leaves the groups unchanged; otherwise replace them.
        let groups = if self.groups.is_empty() {
            None
        } else {
            Some(
                self.groups
                    .iter()
                    .map(|g| {
                        parse_pubkey(g).ok_or_else(|| eyre::eyre!("Invalid group pubkey: {g}"))
                    })
                    .collect::<eyre::Result<Vec<_>>>()?,
            )
        };

        let signature = client.update_feed(UpdateFeedCommand {
            pubkey,
            name: self.name,
            groups,
        })?;

        print_signature(out, &signature)
    }
}
