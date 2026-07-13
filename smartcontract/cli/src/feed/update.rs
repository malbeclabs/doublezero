use crate::{
    doublezerocommand::CliCommand,
    helpers::{parse_or_resolve_exchange, parse_or_resolve_multicastgroup},
    validators::validate_pubkey_or_code,
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
    /// Metro (exchange) pubkey or code to disambiguate a code that exists in multiple metros
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub exchange: Option<String>,
    /// Updated name for the feed
    #[arg(long)]
    pub name: Option<String>,
    /// Replace the feed's multicast group set with these pubkeys or codes (repeatable). When
    /// omitted, the groups are left unchanged.
    #[arg(long = "group", value_parser = validate_pubkey_or_code, num_args = 1..)]
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

        let exchange = self
            .exchange
            .as_deref()
            .map(|e| parse_or_resolve_exchange(client, e))
            .transpose()?;
        let (pubkey, _feed) = client.get_feed(GetFeedCommand {
            pubkey_or_code: self.pubkey,
            exchange,
        })?;

        // An empty `--group` list leaves the groups unchanged; otherwise replace them.
        let groups = if self.groups.is_empty() {
            None
        } else {
            Some(
                self.groups
                    .iter()
                    .map(|g| parse_or_resolve_multicastgroup(client, g))
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

#[cfg(test)]
mod tests {
    use crate::{feed::update::UpdateFeedCliCommand, tests::utils::create_test_client};
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::{
            exchange::get::GetExchangeCommand,
            feed::{get::GetFeedCommand, update::UpdateFeedCommand},
            multicastgroup::get::GetMulticastGroupCommand,
        },
        AccountType, Exchange, ExchangeStatus, Feed, MulticastGroup, MulticastGroupStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_feed_update_with_codes() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let exchange_pk = Pubkey::new_unique();
        let group_pk = Pubkey::new_unique();
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

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

        let feed = Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
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

        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 255,
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 1, 1, 1].into(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            code: "mg01".to_string(),
            owner: Pubkey::new_unique(),
            publisher_count: 0,
            subscriber_count: 0,
        };
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: "mg01".to_string(),
            }))
            .times(1)
            .returning(move |_| Ok((group_pk, mgroup.clone())));

        client
            .expect_update_feed()
            .with(predicate::eq(UpdateFeedCommand {
                pubkey: feed_pk,
                name: Some("Feed v2".to_string()),
                groups: Some(vec![group_pk]),
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            UpdateFeedCliCommand {
                pubkey: "feed01".to_string(),
                exchange: Some("xchi".to_string()),
                name: Some("Feed v2".to_string()),
                groups: vec!["mg01".to_string()],
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        assert_eq!(
            String::from_utf8(output).unwrap(),
            format!("Signature: {signature}\n")
        );
    }
}
