use crate::{
    doublezerocommand::CliCommand,
    feed::{guard::unsubscribe_orphans, resolve::pubkey_or_code},
    helpers::{parse_or_resolve_exchange, resolve_multicastgroup_pk},
    validators::{validate_code, validate_pubkey, validate_pubkey_or_code},
};
use clap::{ArgGroup, Args};
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::{get::GetFeedCommand, update::UpdateFeedCommand};
use std::io::Write;

#[derive(Args, Debug)]
#[clap(group(ArgGroup::new("target").args(&["pubkey", "code"]).required(true)))]
pub struct UpdateFeedCliCommand {
    /// Feed pubkey to update
    #[arg(long, value_parser = validate_pubkey, conflicts_with = "exchange")]
    pub pubkey: Option<String>,
    /// Feed code to update, which names one feed only together with its metro
    #[arg(long, value_parser = validate_code, requires = "exchange")]
    pub code: Option<String>,
    /// Metro (exchange) pubkey or code carrying the feed named by --code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub exchange: Option<String>,
    /// Updated name for the feed
    #[arg(long)]
    pub name: Option<String>,
    /// Replace the feed's multicast group set with these pubkeys or codes (repeatable). When
    /// omitted, the groups are left unchanged.
    #[arg(long = "group", value_parser = validate_pubkey_or_code, num_args = 1..)]
    pub groups: Vec<String>,
    /// Unsubscribe EdgeSeat users from any group this update drops from the feed, instead of
    /// refusing the update. Without it, an update that would leave a user subscribed to a group
    /// outside their access pass's feeds fails and changes nothing.
    #[arg(long, default_value_t = false)]
    pub force_unsubscribe: bool,
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
        let (pubkey, feed) = client.get_feed(GetFeedCommand {
            pubkey_or_code: pubkey_or_code(self.pubkey, self.code)?,
            exchange,
        })?;

        // An empty `--group` list leaves the groups unchanged; otherwise replace them.
        let groups = if self.groups.is_empty() {
            None
        } else {
            Some(
                self.groups
                    .iter()
                    .map(|g| resolve_multicastgroup_pk(client, g))
                    .collect::<eyre::Result<Vec<_>>>()?,
            )
        };

        // Only a group replacement can orphan a subscriber, so a rename leaves the groups alone
        // and scans nothing. Whenever `--group` is given the guard runs — even for an apparently
        // additive change — because it re-derives the dropped set from its own fresh scan, and
        // deciding from the `get_feed` read above would miss a group added in between.
        if let Some(new_groups) = &groups {
            unsubscribe_orphans(
                client,
                out,
                &pubkey,
                &feed.code,
                new_groups,
                self.force_unsubscribe,
            )?;
        }

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
    use crate::{
        feed::{
            guard::fixtures::{device, feed as feed_account, pass, seat, user, GuardFixture},
            update::UpdateFeedCliCommand,
        },
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::{
            exchange::get::GetExchangeCommand,
            feed::{get::GetFeedCommand, update::UpdateFeedCommand},
            multicastgroup::{
                get::GetMulticastGroupCommand, subscribe::UpdateMulticastGroupRolesCommand,
            },
        },
        AccountType, Exchange, ExchangeStatus, Feed, MulticastGroup, MulticastGroupStatus,
    };
    use doublezero_serviceability::state::accesspass::AccessPassType;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::collections::HashMap;

    #[test]
    fn test_cli_feed_update_fails_closed_when_it_would_orphan_a_subscriber() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let f = GuardFixture::new(2);
        let (g1, g2) = (f.groups[0], f.groups[1]);
        f.expect_get_feed(&mut client, vec![g1, g2]);
        f.expect_get_groups(&mut client);
        f.expect_scan(&mut client, vec![g2]);
        client.expect_update_feed().times(0);
        client.expect_update_multicastgroup_roles().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            UpdateFeedCliCommand {
                pubkey: Some(f.feed_pk.to_string()),
                code: None,
                exchange: None,
                name: None,
                groups: vec![g1.to_string()],
                force_unsubscribe: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("--force-unsubscribe"),
            "unexpected error: {err}"
        );
        let output = String::from_utf8(output).unwrap();
        assert!(
            output.contains(&f.user_pk.to_string()),
            "plan does not name the user: {output}"
        );
    }

    #[test]
    fn test_cli_feed_update_force_unsubscribes_then_updates() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let f = GuardFixture::new(2);
        let (g1, g2) = (f.groups[0], f.groups[1]);
        let signature = Signature::new_unique();
        f.expect_get_feed(&mut client, vec![g1, g2]);
        f.expect_get_groups(&mut client);
        f.expect_scan(&mut client, vec![g2]);
        // The mock does not mutate state, so the post-unsubscribe re-scan needs its own snapshot
        // with the membership gone.
        f.expect_scan(&mut client, vec![]);
        client
            .expect_update_multicastgroup_roles()
            .with(predicate::eq(UpdateMulticastGroupRolesCommand {
                user_pk: f.user_pk,
                group_pks: vec![g2],
                client_ip: f.client_ip,
                publisher: false,
                subscriber: false,
                device_pk: None,
                feed_pk: None,
            }))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));
        client
            .expect_update_feed()
            .with(predicate::eq(UpdateFeedCommand {
                pubkey: f.feed_pk,
                name: None,
                groups: Some(vec![g1]),
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            UpdateFeedCliCommand {
                pubkey: Some(f.feed_pk.to_string()),
                code: None,
                exchange: None,
                name: None,
                groups: vec![g1.to_string()],
                force_unsubscribe: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        assert!(String::from_utf8(output)
            .unwrap()
            .contains(&format!("Signature: {signature}")));
    }

    /// A user holding several dropped groups is stripped with one batched role update, not one
    /// transaction per group.
    #[test]
    fn test_cli_feed_update_force_unsubscribes_a_user_in_one_batch() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let f = GuardFixture::new(3);
        let (g1, g2, g3) = (f.groups[0], f.groups[1], f.groups[2]);
        let signature = Signature::new_unique();
        f.expect_get_feed(&mut client, vec![g1, g2, g3]);
        f.expect_get_groups(&mut client);
        f.expect_scan(&mut client, vec![g2, g3]);
        // The mock does not mutate state, so the post-unsubscribe re-scan needs its own snapshot
        // with the memberships gone.
        f.expect_scan(&mut client, vec![]);
        // The plan sorts by (user, group), so the batch carries the dropped groups in pubkey
        // order.
        let mut dropped = vec![g2, g3];
        dropped.sort();
        client
            .expect_update_multicastgroup_roles()
            .with(predicate::eq(UpdateMulticastGroupRolesCommand {
                user_pk: f.user_pk,
                group_pks: dropped,
                client_ip: f.client_ip,
                publisher: false,
                subscriber: false,
                device_pk: None,
                feed_pk: None,
            }))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));
        client
            .expect_update_feed()
            .with(predicate::eq(UpdateFeedCommand {
                pubkey: f.feed_pk,
                name: None,
                groups: Some(vec![g1]),
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            UpdateFeedCliCommand {
                pubkey: Some(f.feed_pk.to_string()),
                code: None,
                exchange: None,
                name: None,
                groups: vec![g1.to_string()],
                force_unsubscribe: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
    }

    /// An additive change scans (the guard re-derives the dropped set from its own snapshot) but
    /// finds nothing dropped, so an existing subscriber needs no flag and no removals happen.
    #[test]
    fn test_cli_feed_update_additive_change_needs_no_flag() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let f = GuardFixture::new(2);
        let (g1, g2) = (f.groups[0], f.groups[1]);
        let signature = Signature::new_unique();
        f.expect_get_feed(&mut client, vec![g1]);
        f.expect_get_groups(&mut client);
        // The scanned feed carries [g1, g2] (the fixture's full set); the new set is a superset,
        // so nothing is dropped even though the user subscribes to g2.
        f.expect_scan(&mut client, vec![g2]);
        client.expect_update_multicastgroup_roles().times(0);
        client
            .expect_update_feed()
            .with(predicate::eq(UpdateFeedCommand {
                pubkey: f.feed_pk,
                name: None,
                groups: Some(vec![g1, g2]),
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            UpdateFeedCliCommand {
                pubkey: Some(f.feed_pk.to_string()),
                code: None,
                exchange: None,
                name: None,
                groups: vec![g1.to_string(), g2.to_string()],
                force_unsubscribe: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
    }

    #[test]
    fn test_cli_feed_update_aborts_when_orphans_keep_appearing() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let f = GuardFixture::new(2);
        let (g1, g2) = (f.groups[0], f.groups[1]);
        f.expect_get_feed(&mut client, vec![g1, g2]);
        f.expect_get_groups(&mut client);
        // Every scan sees the same still-subscribed user, as if a new subscription raced each
        // unsubscribe pass. Deliberately not `expect_scan` (capped per call): uncapped scans let
        // the guard run until its own round limit trips, which the removal count below then pins
        // at one removal per round.
        let user_pk = f.user_pk;
        let user_acct = user(f.owner, f.device_pk, f.client_ip, vec![g2]);
        client
            .expect_list_user()
            .returning(move |_| Ok(HashMap::from([(user_pk, user_acct.clone())])));
        let pass_acct = pass(
            f.owner,
            f.client_ip,
            AccessPassType::EdgeSeat(vec![seat(f.feed_pk)]),
        );
        client
            .expect_list_accesspass()
            .returning(move |_| Ok(HashMap::from([(Pubkey::new_unique(), pass_acct.clone())])));
        let device_pk = f.device_pk;
        let device_acct = device(f.exchange_pk);
        client
            .expect_list_device()
            .returning(move |_| Ok(HashMap::from([(device_pk, device_acct.clone())])));
        let feed_pk = f.feed_pk;
        let feed_acct = feed_account(f.exchange_pk, vec![g1, g2]);
        client
            .expect_list_feed()
            .returning(move |_| Ok(HashMap::from([(feed_pk, feed_acct.clone())])));
        client
            .expect_update_multicastgroup_roles()
            .times(3)
            .returning(|_| Ok(Signature::new_unique()));
        client.expect_update_feed().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            UpdateFeedCliCommand {
                pubkey: Some(f.feed_pk.to_string()),
                code: None,
                exchange: None,
                name: None,
                groups: vec![g1.to_string()],
                force_unsubscribe: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("kept appearing"),
            "expected the round-limit error, got: {err}"
        );
    }

    /// A mixed-role orphan (one role must go, the other still allowlist-authorized) cannot be
    /// expressed as the removal-only update the automated path issues, so even `--force` fails
    /// closed before submitting anything.
    #[test]
    fn test_cli_feed_update_force_fails_closed_on_a_mixed_role_orphan() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let f = GuardFixture::new(2);
        let (g1, g2) = (f.groups[0], f.groups[1]);
        f.expect_get_feed(&mut client, vec![g1, g2]);
        f.expect_get_groups(&mut client);

        // One scan: the user publishes and subscribes g2; the sub allowlist still authorizes the
        // subscriber role, so only the publisher role is removable.
        let user_pk = f.user_pk;
        let mut user_acct = user(f.owner, f.device_pk, f.client_ip, vec![g2]);
        user_acct.publishers.push(g2);
        client
            .expect_list_user()
            .times(1)
            .returning(move |_| Ok(HashMap::from([(user_pk, user_acct.clone())])));
        let mut pass_acct = pass(
            f.owner,
            f.client_ip,
            AccessPassType::EdgeSeat(vec![seat(f.feed_pk)]),
        );
        pass_acct.mgroup_sub_allowlist.push(g2);
        client
            .expect_list_accesspass()
            .times(1)
            .returning(move |_| Ok(HashMap::from([(Pubkey::new_unique(), pass_acct.clone())])));
        let device_pk = f.device_pk;
        let device_acct = device(f.exchange_pk);
        client
            .expect_list_device()
            .times(1)
            .returning(move |_| Ok(HashMap::from([(device_pk, device_acct.clone())])));
        let feed_pk = f.feed_pk;
        let feed_acct = feed_account(f.exchange_pk, vec![g1, g2]);
        client
            .expect_list_feed()
            .times(1)
            .returning(move |_| Ok(HashMap::from([(feed_pk, feed_acct.clone())])));

        client.expect_update_multicastgroup_roles().times(0);
        client.expect_update_feed().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            UpdateFeedCliCommand {
                pubkey: Some(f.feed_pk.to_string()),
                code: None,
                exchange: None,
                name: None,
                groups: vec![g1.to_string()],
                force_unsubscribe: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("manually"),
            "expected the mixed-role error, got: {err}"
        );
        let output = String::from_utf8(output).unwrap();
        assert!(
            output.contains("still allowlisted"),
            "plan does not flag the kept role: {output}"
        );
    }

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
            ..Default::default()
        };
        let feed_for_get = feed.clone();
        client
            .expect_get_feed()
            .with(predicate::eq(GetFeedCommand {
                pubkey_or_code: "feed01".to_string(),
                exchange: Some(exchange_pk),
            }))
            .times(1)
            .returning(move |_| Ok((feed_pk, feed_for_get.clone())));

        // `--group` always runs the guard; with no users the scan finds nothing to drop.
        client.expect_list_user().returning(|_| Ok(HashMap::new()));
        client
            .expect_list_accesspass()
            .returning(|_| Ok(HashMap::new()));
        client
            .expect_list_device()
            .returning(|_| Ok(HashMap::new()));
        client
            .expect_list_feed()
            .returning(move |_| Ok(HashMap::from([(feed_pk, feed.clone())])));

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
                pubkey: None,
                code: Some("feed01".to_string()),
                exchange: Some("xchi".to_string()),
                name: Some("Feed v2".to_string()),
                groups: vec!["mg01".to_string()],
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
