use crate::{doublezerocommand::CliCommand, feed::parse_metro, validators::validate_code};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::create::CreateFeedCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateFeedCliCommand {
    /// Unique code for the feed (immutable; used as the PDA seed)
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Human-readable name for the feed
    #[arg(long)]
    pub name: String,
    /// Metro mapping `EXCHANGE_PK=GROUP_PK[,GROUP_PK...]` (repeatable). Omit for a feed with no
    /// metro restriction (reachable from any exchange).
    #[arg(long = "metro", value_parser = parse_metro)]
    pub metros: Vec<(Pubkey, Vec<Pubkey>)>,
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

        let (signature, _pubkey) = client.create_feed(CreateFeedCommand {
            code: self.code,
            name: self.name,
            metros: self.metros,
        })?;

        print_signature(out, &signature)
    }
}
