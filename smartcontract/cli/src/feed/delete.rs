use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_pubkey,
    validators::{validate_pubkey, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::{delete::DeleteFeedCommand, get::GetFeedCommand};
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteFeedCliCommand {
    /// Feed pubkey or code to delete
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Metro (exchange) pubkey to disambiguate a code that exists in multiple metros
    #[arg(long, value_parser = validate_pubkey)]
    pub exchange: Option<String>,
}

impl DeleteFeedCliCommand {
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

        let exchange = self
            .exchange
            .as_deref()
            .map(|e| parse_pubkey(e).ok_or_else(|| eyre::eyre!("Invalid exchange pubkey")))
            .transpose()?;
        let (pubkey, _feed) = client.get_feed(GetFeedCommand {
            pubkey_or_code: self.pubkey,
            exchange,
        })?;

        let signature = client.delete_feed(DeleteFeedCommand { pubkey })?;
        print_signature(out, &signature)
    }
}
