use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_pubkey,
    validators::{validate_code, validate_pubkey},
};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::create::CreateFeedCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateFeedCliCommand {
    /// Unique code for the feed (immutable; part of the PDA seed)
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Human-readable name for the feed
    #[arg(long)]
    pub name: String,
    /// Metro (exchange) pubkey this feed serves (immutable; part of the PDA seed)
    #[arg(long, value_parser = validate_pubkey)]
    pub exchange: String,
    /// Multicast group pubkey joinable in this metro (repeatable)
    #[arg(long = "group", value_parser = validate_pubkey, num_args = 1..)]
    pub groups: Vec<String>,
}

impl CreateFeedCliCommand {
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

        let exchange =
            parse_pubkey(&self.exchange).ok_or_else(|| eyre::eyre!("Invalid exchange pubkey"))?;
        let groups = self
            .groups
            .iter()
            .map(|g| parse_pubkey(g).ok_or_else(|| eyre::eyre!("Invalid group pubkey: {g}")))
            .collect::<eyre::Result<Vec<_>>>()?;

        let (signature, _pubkey) = client.create_feed(CreateFeedCommand {
            code: self.code,
            name: self.name,
            exchange,
            groups,
        })?;

        print_signature(out, &signature)
    }
}
