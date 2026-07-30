use crate::{
    doublezerocommand::CliCommand, feed::guard::unsubscribe_orphans,
    helpers::parse_or_resolve_exchange, validators::validate_pubkey_or_code,
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
    /// Metro (exchange) pubkey or code to disambiguate a code that exists in multiple metros
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub exchange: Option<String>,
    /// Unsubscribe EdgeSeat users from the deleted feed's groups, instead of refusing the delete.
    /// Without it, a delete that would leave a user subscribed to a group outside their access
    /// pass's feeds fails and changes nothing.
    #[arg(long, default_value_t = false)]
    pub force_unsubscribe: bool,
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
            .map(|e| parse_or_resolve_exchange(client, e))
            .transpose()?;
        let (pubkey, feed) = client.get_feed(GetFeedCommand {
            pubkey_or_code: self.pubkey,
            exchange,
        })?;

        // Deleting the feed drops every group it carried.
        unsubscribe_orphans(
            client,
            out,
            &pubkey,
            &feed.code,
            &feed.groups,
            self.force_unsubscribe,
        )?;

        let signature = client.delete_feed(DeleteFeedCommand { pubkey })?;
        print_signature(out, &signature)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::MockCliCommand,
        feed::{
            delete::DeleteFeedCliCommand,
            guard::fixtures::{device, feed as feed_account, pass, seat, user},
        },
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::{
            exchange::get::GetExchangeCommand,
            feed::{delete::DeleteFeedCommand, get::GetFeedCommand},
            multicastgroup::subscribe::UpdateMulticastGroupRolesCommand,
        },
        AccountType, Exchange, ExchangeStatus, Feed,
    };
    use doublezero_serviceability::state::accesspass::AccessPassType;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::{collections::HashMap, net::Ipv4Addr};

    /// A feed carrying `[group]` and one EdgeSeat user in the feed's exchange, seated on it.
    struct GuardFixture {
        exchange_pk: Pubkey,
        feed_pk: Pubkey,
        group: Pubkey,
        device_pk: Pubkey,
        user_pk: Pubkey,
        owner: Pubkey,
        client_ip: Ipv4Addr,
    }

    impl GuardFixture {
        fn new() -> Self {
            Self {
                exchange_pk: Pubkey::new_unique(),
                feed_pk: Pubkey::new_unique(),
                group: Pubkey::new_unique(),
                device_pk: Pubkey::new_unique(),
                user_pk: Pubkey::new_unique(),
                owner: Pubkey::new_unique(),
                client_ip: [10, 1, 1, 1].into(),
            }
        }

        /// Stub `get_feed` for a `--pubkey <feed_pk>` lookup.
        fn expect_get_feed(&self, client: &mut MockCliCommand) {
            let feed_pk = self.feed_pk;
            let feed = feed_account(self.exchange_pk, vec![self.group]);
            client
                .expect_get_feed()
                .with(predicate::eq(GetFeedCommand {
                    pubkey_or_code: feed_pk.to_string(),
                    exchange: None,
                }))
                .times(1)
                .returning(move |_| Ok((feed_pk, feed.clone())));
        }

        /// Stub one guard scan whose user holds `subscribers`. Stacked expectations match FIFO,
        /// so calling this twice lets a second scan see a different snapshot.
        fn expect_scan(&self, client: &mut MockCliCommand, subscribers: Vec<Pubkey>) {
            let user_pk = self.user_pk;
            let user_acct = user(self.owner, self.device_pk, self.client_ip, subscribers);
            client
                .expect_list_user()
                .times(1)
                .returning(move |_| Ok(HashMap::from([(user_pk, user_acct.clone())])));
            let pass_acct = pass(
                self.owner,
                self.client_ip,
                AccessPassType::EdgeSeat(vec![seat(self.feed_pk)]),
            );
            client
                .expect_list_accesspass()
                .times(1)
                .returning(move |_| Ok(HashMap::from([(Pubkey::new_unique(), pass_acct.clone())])));
            let device_pk = self.device_pk;
            let device_acct = device(self.exchange_pk);
            client
                .expect_list_device()
                .times(1)
                .returning(move |_| Ok(HashMap::from([(device_pk, device_acct.clone())])));
            let feed_pk = self.feed_pk;
            let feed_acct = feed_account(self.exchange_pk, vec![self.group]);
            client
                .expect_list_feed()
                .times(1)
                .returning(move |_| Ok(HashMap::from([(feed_pk, feed_acct.clone())])));
        }
    }

    #[test]
    fn test_cli_feed_delete_fails_closed_when_it_would_orphan_a_subscriber() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let f = GuardFixture::new();
        f.expect_get_feed(&mut client);
        f.expect_scan(&mut client, vec![f.group]);
        client.expect_delete_feed().times(0);
        client.expect_update_multicastgroup_roles().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            DeleteFeedCliCommand {
                pubkey: f.feed_pk.to_string(),
                exchange: None,
                force_unsubscribe: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("--force-unsubscribe"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn test_cli_feed_delete_force_unsubscribes_then_deletes() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let f = GuardFixture::new();
        let signature = Signature::new_unique();
        f.expect_get_feed(&mut client);
        f.expect_scan(&mut client, vec![f.group]);
        // The mock does not mutate state, so the post-unsubscribe re-scan needs its own snapshot
        // with the membership gone.
        f.expect_scan(&mut client, vec![]);
        client
            .expect_update_multicastgroup_roles()
            .with(predicate::eq(UpdateMulticastGroupRolesCommand {
                user_pk: f.user_pk,
                group_pk: f.group,
                client_ip: f.client_ip,
                publisher: false,
                subscriber: false,
                device_pk: None,
                feed_pk: None,
            }))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));
        client
            .expect_delete_feed()
            .with(predicate::eq(DeleteFeedCommand { pubkey: f.feed_pk }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            DeleteFeedCliCommand {
                pubkey: f.feed_pk.to_string(),
                exchange: None,
                force_unsubscribe: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        assert!(String::from_utf8(output)
            .unwrap()
            .contains(&format!("Signature: {signature}")));
    }

    #[test]
    fn test_cli_feed_delete_with_exchange_code() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let exchange_pk = Pubkey::new_unique();
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

        client
            .expect_delete_feed()
            .with(predicate::eq(DeleteFeedCommand { pubkey: feed_pk }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            DeleteFeedCliCommand {
                pubkey: "feed01".to_string(),
                exchange: Some("xchi".to_string()),
                force_unsubscribe: false,
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
