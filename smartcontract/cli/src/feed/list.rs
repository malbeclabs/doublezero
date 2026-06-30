use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::{render_collection, CliContext, OutputFormat};
use doublezero_program_common::serializer;
use doublezero_sdk::commands::feed::list::ListFeedCommand;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct ListFeedCliCommand {
    /// Output in JSON format
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output in compact JSON format
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct FeedDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub name: String,
    pub metros: usize,
    pub reference_count: u32,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListFeedCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let feeds = client.list_feed(ListFeedCommand)?;

        let mut displays: Vec<FeedDisplay> = feeds
            .into_iter()
            .map(|(pubkey, feed)| FeedDisplay {
                account: pubkey,
                code: feed.code,
                name: feed.name,
                metros: feed.metros.len(),
                reference_count: feed.reference_count,
                owner: feed.owner,
            })
            .collect();

        displays.sort_by(|a, b| a.code.cmp(&b.code));

        render_collection(
            out,
            displays,
            OutputFormat::from_flags(self.json, self.json_compact),
        )
    }
}
