use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_or_resolve_exchange,
    validators::{validate_code, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_cli_core::{render_collection, CliContext, OutputFormat};
use doublezero_program_common::serializer;
use doublezero_sdk::commands::{
    exchange::list::ListExchangeCommand, feed::list::ListFeedCommand,
    multicastgroup::list::ListMulticastGroupCommand,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct ListFeedCliCommand {
    /// Show only the feed with this code
    #[arg(long, value_parser = validate_code)]
    pub code: Option<String>,
    /// Show only the feeds serving this metro (exchange), by pubkey or code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub exchange: Option<String>,
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
    pub exchange: String,
    pub groups: usize,
    pub group_codes: String,
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
        let exchange = self
            .exchange
            .as_deref()
            .map(|e| parse_or_resolve_exchange(client, e))
            .transpose()?;

        let feeds = client.list_feed(ListFeedCommand)?;
        let mgroups = client.list_multicastgroup(ListMulticastGroupCommand)?;
        let exchanges = client.list_exchange(ListExchangeCommand)?;

        let mut displays = feeds
            .into_iter()
            .filter(|(_, feed)| {
                self.code.as_deref().is_none_or(|code| feed.code == code)
                    && exchange.is_none_or(|ex| feed.exchange == ex)
            })
            .map(|(pubkey, feed)| FeedDisplay {
                account: pubkey,
                code: feed.code,
                name: feed.name,
                exchange: exchanges
                    .get(&feed.exchange)
                    .map_or_else(|| feed.exchange.to_string(), |ex| ex.code.clone()),
                groups: feed.groups.len(),
                group_codes: feed
                    .groups
                    .iter()
                    .map(|mg_pk| {
                        mgroups
                            .get(mg_pk)
                            .map_or_else(|| mg_pk.to_string(), |mg| mg.code.clone())
                    })
                    .collect::<Vec<_>>()
                    .join(", "),
                owner: feed.owner,
            })
            .collect::<Vec<FeedDisplay>>();

        displays.sort_by(|a, b| a.code.cmp(&b.code));

        render_collection(
            out,
            displays,
            OutputFormat::from_flags(self.json, self.json_compact),
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{feed::list::ListFeedCliCommand, tests::utils::create_test_client};
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::exchange::get::GetExchangeCommand, AccountType, Exchange, ExchangeStatus, Feed,
        MulticastGroup, MulticastGroupStatus,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_cli_feed_list() {
        let mut client = create_test_client();

        let feed_pk = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR");
        let exchange_pk = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3");
        let owner_pk = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");
        let mgroup_pk = Pubkey::from_str_const("1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM");
        // Deliberately not in the multicast group map, so it renders as a raw pubkey.
        let unknown_mgroup_pk = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo4");

        let feed = Feed {
            account_type: AccountType::Feed,
            owner: owner_pk,
            bump_seed: 255,
            code: "qa-payments".to_string(),
            name: "QA Payments".to_string(),
            exchange: exchange_pk,
            groups: vec![mgroup_pk, unknown_mgroup_pk],
            ..Default::default()
        };
        client.expect_list_feed().returning(move |_| {
            let mut feeds = HashMap::new();
            feeds.insert(feed_pk, feed.clone());
            Ok(feeds)
        });

        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 2,
            tenant_pk: Pubkey::default(),
            code: "mg01".to_string(),
            multicast_ip: [1, 2, 3, 4].into(),
            max_bandwidth: 1234,
            status: MulticastGroupStatus::Activated,
            owner: owner_pk,
            publisher_count: 1,
            subscriber_count: 2,
        };
        client.expect_list_multicastgroup().returning(move |_| {
            let mut mgroups = HashMap::new();
            mgroups.insert(mgroup_pk, mgroup.clone());
            Ok(mgroups)
        });

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "xams".to_string(),
            name: "Amsterdam".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 52.37,
            lng: 4.89,
            bgp_community: 1,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: owner_pk,
        };
        client.expect_list_exchange().returning(move |_| {
            let mut exchanges = HashMap::new();
            exchanges.insert(exchange_pk, exchange.clone());
            Ok(exchanges)
        });

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ListFeedCliCommand {
                code: None,
                exchange: None,
                json: false,
                json_compact: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            " account                                   | code        | name        | exchange | groups | group_codes                                     | owner                                     \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR | qa-payments | QA Payments | xams     | 2      | mg01, 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo4 | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 \n"
        );
    }

    /// Three feeds, where two metros share the code `qa-payments`. The exchange `xams` resolves
    /// from its code, so a run that passes `--exchange xams` exercises the lookup.
    fn multi_metro_client(
        xams_pk: Pubkey,
        xfra_pk: Pubkey,
    ) -> crate::doublezerocommand::MockCliCommand {
        let mut client = create_test_client();

        let feed = |code: &str, exchange: Pubkey| Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            code: code.to_string(),
            name: code.to_string(),
            exchange,
            groups: vec![],
            ..Default::default()
        };

        client.expect_list_feed().returning(move |_| {
            let mut feeds = HashMap::new();
            feeds.insert(Pubkey::new_unique(), feed("qa-payments", xams_pk));
            feeds.insert(Pubkey::new_unique(), feed("qa-payments", xfra_pk));
            feeds.insert(Pubkey::new_unique(), feed("shreds", xams_pk));
            Ok(feeds)
        });
        client
            .expect_list_multicastgroup()
            .returning(|_| Ok(HashMap::new()));

        let xams = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "xams".to_string(),
            name: "Amsterdam".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 52.37,
            lng: 4.89,
            bgp_community: 1,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::new_unique(),
        };
        let xfra = Exchange {
            account_type: AccountType::Exchange,
            index: 2,
            bump_seed: 255,
            reference_count: 0,
            code: "xfra".to_string(),
            name: "Frankfurt".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 50.11,
            lng: 8.68,
            bgp_community: 2,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        let exchanges = HashMap::from([(xams_pk, xams.clone()), (xfra_pk, xfra)]);
        client
            .expect_list_exchange()
            .returning(move |_| Ok(exchanges.clone()));
        client
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: "xams".to_string(),
            }))
            .returning(move |_| Ok((xams_pk, xams.clone())));

        client
    }

    fn list_feeds(
        client: &crate::doublezerocommand::MockCliCommand,
        code: Option<&str>,
        exchange: Option<&str>,
    ) -> serde_json::Value {
        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ListFeedCliCommand {
                code: code.map(str::to_string),
                exchange: exchange.map(str::to_string),
                json: true,
                json_compact: false,
            }
            .execute(&ctx, client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        serde_json::from_slice(&output).unwrap()
    }

    #[test]
    fn test_cli_feed_list_filters_by_code() {
        let xams_pk = Pubkey::new_unique();
        let xfra_pk = Pubkey::new_unique();
        let client = multi_metro_client(xams_pk, xfra_pk);

        let rows = list_feeds(&client, Some("qa-payments"), None);
        let rows = rows.as_array().unwrap();
        assert_eq!(rows.len(), 2);
        assert_eq!(rows[0]["code"], "qa-payments");
        assert_eq!(rows[1]["code"], "qa-payments");
    }

    #[test]
    fn test_cli_feed_list_filters_by_exchange_code() {
        let xams_pk = Pubkey::new_unique();
        let xfra_pk = Pubkey::new_unique();
        let client = multi_metro_client(xams_pk, xfra_pk);

        let rows = list_feeds(&client, None, Some("xams"));
        let rows = rows.as_array().unwrap();
        assert_eq!(rows.len(), 2);
        assert_eq!(rows[0]["code"], "qa-payments");
        assert_eq!(rows[1]["code"], "shreds");
        assert_eq!(rows[0]["exchange"], "xams");
        assert_eq!(rows[1]["exchange"], "xams");
    }

    /// The pair of filters names exactly one feed, which is what `feed get` used to do.
    #[test]
    fn test_cli_feed_list_filters_by_code_and_exchange() {
        let xams_pk = Pubkey::new_unique();
        let xfra_pk = Pubkey::new_unique();
        let client = multi_metro_client(xams_pk, xfra_pk);

        let rows = list_feeds(&client, Some("qa-payments"), Some(&xfra_pk.to_string()));
        let rows = rows.as_array().unwrap();
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0]["code"], "qa-payments");
        assert_eq!(rows[0]["exchange"], "xfra");
    }

    #[test]
    fn test_cli_feed_list_renders_no_row_for_an_unknown_code() {
        let xams_pk = Pubkey::new_unique();
        let xfra_pk = Pubkey::new_unique();
        let client = multi_metro_client(xams_pk, xfra_pk);

        let rows = list_feeds(&client, Some("no-such-feed"), None);
        assert!(rows.as_array().unwrap().is_empty());
    }
}
