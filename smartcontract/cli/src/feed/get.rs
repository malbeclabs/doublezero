use crate::{
    doublezerocommand::CliCommand, helpers::parse_or_resolve_exchange,
    validators::validate_pubkey_or_code,
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
    /// Metro (exchange) pubkey or code to disambiguate a code that exists in multiple metros
    #[arg(long, value_parser = validate_pubkey_or_code)]
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
            .map(|e| parse_or_resolve_exchange(client, e))
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
            owner: feed.owner.to_string(),
        };

        render_record(out, &display, OutputFormat::from_flags(self.json, false))
    }
}

#[cfg(test)]
mod tests {
    use crate::{feed::get::GetFeedCliCommand, tests::utils::create_test_client};
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::{exchange::get::GetExchangeCommand, feed::get::GetFeedCommand},
        AccountType, Exchange, ExchangeStatus, Feed,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_feed_get_with_exchange_code() {
        let mut client = create_test_client();

        let exchange_pk = Pubkey::new_unique();
        let feed_pk = Pubkey::new_unique();

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "xchi".to_string(),
            name: "Test Exchange".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 12.34,
            lng: 56.78,
            bgp_community: 1,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::new_unique(),
        };
        client
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: "xchi".to_string(),
            }))
            .times(1)
            .returning(move |_| Ok((exchange_pk, exchange.clone())));

        let owner_pk = Pubkey::new_unique();
        let feed = Feed {
            account_type: AccountType::Feed,
            owner: owner_pk,
            bump_seed: 255,
            code: "feed01".to_string(),
            name: "Feed".to_string(),
            exchange: exchange_pk,
            groups: vec![],
        };
        client
            .expect_get_feed()
            .with(predicate::eq(GetFeedCommand {
                pubkey_or_code: "feed01".to_string(),
                exchange: Some(exchange_pk),
            }))
            .times(1)
            .returning(move |_| Ok((feed_pk, feed.clone())));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetFeedCliCommand {
                pubkey: "feed01".to_string(),
                exchange: Some("xchi".to_string()),
                json: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            format!(
                " account  | {feed_pk}\n code     | feed01\n name     | Feed\n exchange | {exchange_pk}\n groups   | 0\n owner    | {owner_pk}\n"
            )
        );
    }
}
