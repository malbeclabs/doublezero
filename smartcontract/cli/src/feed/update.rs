use crate::{
    doublezerocommand::CliCommand, feed::parse_metro, validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::{get::GetFeedCommand, update::UpdateFeedCommand};
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateFeedCliCommand {
    /// Feed pubkey or code to update
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Updated name for the feed
    #[arg(long)]
    pub name: Option<String>,
    /// Replace the metro map with these `EXCHANGE_PK=GROUP_PK[,GROUP_PK...]` entries (repeatable).
    /// When omitted, the metro map is left unchanged.
    #[arg(long = "metro", value_parser = parse_metro)]
    pub metros: Vec<(Pubkey, Vec<Pubkey>)>,
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

        // An empty `--metro` list leaves the map unchanged; pass Some only when entries are given.
        let metros = if self.metros.is_empty() {
            None
        } else {
            Some(self.metros)
        };

        let signature = client.update_feed(UpdateFeedCommand {
            pubkey,
            name: self.name,
            metros,
        })?;

        print_signature(out, &signature)
    }
}
