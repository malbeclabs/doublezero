use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_pubkey,
    validators::{validate_pubkey, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_cli_core::{render_record, CliContext, OutputFormat};
use doublezero_sdk::commands::feed::get::GetFeedCommand;
use serde::Serialize;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetFeedCliCommand {
    /// Feed pubkey or code to get details for
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Metro (exchange) pubkey to disambiguate a code that exists in multiple metros
    #[arg(long, value_parser = validate_pubkey)]
    pub exchange: Option<String>,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct FeedDisplay {
    pub account: String,
    pub code: String,
    pub name: String,
    /// The metro (exchange) this feed serves.
    pub exchange: String,
    /// Number of multicast groups joinable in this metro.
    pub groups: usize,
    pub reference_count: u32,
    pub owner: String,
}

impl GetFeedCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let exchange = self
            .exchange
            .as_deref()
            .map(|e| parse_pubkey(e).ok_or_else(|| eyre::eyre!("Invalid exchange pubkey")))
            .transpose()?;
        let (pubkey, feed) = client.get_feed(GetFeedCommand {
            pubkey_or_code: self.pubkey,
            exchange,
        })?;

        let display = FeedDisplay {
            account: pubkey.to_string(),
            code: feed.code,
            name: feed.name,
            exchange: feed.exchange.to_string(),
            groups: feed.groups.len(),
            reference_count: feed.reference_count,
            owner: feed.owner.to_string(),
        };

        render_record(out, &display, OutputFormat::from_flags(self.json, false))
    }
}
