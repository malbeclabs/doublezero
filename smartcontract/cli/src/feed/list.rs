use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::{render_collection, CliContext, OutputFormat};
use doublezero_program_common::serializer;
use doublezero_sdk::commands::{
    feed::list::ListFeedCommand, multicastgroup::list::ListMulticastGroupCommand,
};
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
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub exchange: Pubkey,
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
        let feeds = client.list_feed(ListFeedCommand)?;
        let mgroups = client.list_multicastgroup(ListMulticastGroupCommand)?;

        let mut displays = feeds
            .into_iter()
            .map(|(pubkey, feed)| FeedDisplay {
                account: pubkey,
                code: feed.code,
                name: feed.name,
                exchange: feed.exchange,
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
    use doublezero_sdk::{AccountType, Feed, MulticastGroup, MulticastGroupStatus};
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

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ListFeedCliCommand {
                json: false,
                json_compact: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            " account                                   | code        | name        | exchange                                  | groups | group_codes                                     | owner                                     \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR | qa-payments | QA Payments | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3 | 2      | mg01, 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo4 | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 \n"
        );
    }
}
