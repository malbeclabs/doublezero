use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::{
    commands::{
        accesspass::{
            get::GetAccessPassCommand,
            set::SetAccessPassCommand,
            set_feeds::{FeedSeatProvision, SetAccessPassFeedsCommand},
        },
        feed::list::ListFeedCommand,
        multicastgroup::unsubscribe_feed::UnsubscribeFeedCommand,
        tenant::get::GetTenantCommand,
        user::{delete::DeleteUserCommand, list::ListUserCommand},
    },
    Feed, User,
};
use doublezero_serviceability::state::accesspass::{AccessPassType, FeedSeat};
use solana_sdk::pubkey::Pubkey;
use std::{
    collections::HashMap,
    io::{BufRead, Write},
    net::Ipv4Addr,
    str::FromStr,
};

/// Remove one feed from an EdgeSeat access pass while its other feeds stay active. Users holding
/// the feed are unsubscribed from it, users left with no feed are deleted, and the multicast user
/// cap drops to the remaining seats. Requires `ACCESS_PASS_ADMIN` and `USER_ADMIN`, or foundation
/// allowlist membership.
#[derive(Args, Debug)]
pub struct RemoveFeedAccessPassCliCommand {
    #[arg(long)]
    pub client_ip: Option<Ipv4Addr>,
    #[arg(long)]
    pub user_payer: String,
    #[arg(long)]
    pub feed: String,
    /// Print the plan without sending any transaction
    #[arg(long, default_value_t = false)]
    pub dry_run: bool,
}

impl RemoveFeedAccessPassCliCommand {
    pub async fn execute<C: CliCommand, W: Write, R: BufRead>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
        input: &mut R,
    ) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let user_payer = if self.user_payer.eq_ignore_ascii_case("me") {
            client.get_payer()
        } else {
            Pubkey::from_str(&self.user_payer)?
        };
        let client_ip = self.client_ip.unwrap_or(Ipv4Addr::UNSPECIFIED);

        let (accesspass_pk, accesspass) = client
            .get_accesspass(GetAccessPassCommand {
                client_ip,
                user_payer,
            })?
            .ok_or_else(|| {
                eyre::eyre!("no access pass for client_ip {client_ip} and user_payer {user_payer}")
            })?;
        let AccessPassType::EdgeSeat(seats) = &accesspass.accesspass_type else {
            eyre::bail!(
                "access pass {accesspass_pk} is {}; only an EdgeSeat pass carries feeds",
                accesspass.accesspass_type
            );
        };

        let feeds = client.list_feed(ListFeedCommand {})?;
        let feed_pk = resolve_feed(&self.feed, seats, &feeds)?;
        let seated = seats.iter().any(|s| s.feed_key == feed_pk);
        let remaining: Vec<&FeedSeat> = seats.iter().filter(|s| s.feed_key != feed_pk).collect();
        let max_multicast_users = u16::try_from(
            remaining
                .iter()
                .map(|s| u32::from(s.max_users))
                .sum::<u32>(),
        )?;
        let cap_changes = max_multicast_users != accesspass.max_multicast_users;

        // SetAccessPass re-adds the pass's tenant, and the program requires the signer to
        // administer it even for foundation signers.
        let tenant = accesspass
            .tenant_allowlist
            .first()
            .copied()
            .unwrap_or_default();
        if cap_changes && tenant != Pubkey::default() {
            let (_, t) = client.get_tenant(GetTenantCommand {
                pubkey_or_code: tenant.to_string(),
            })?;
            let signer = client.get_payer();
            if !t.administrators.contains(&signer) {
                eyre::bail!(
                    "{signer} is not an administrator of tenant {tenant} on access pass {accesspass_pk}, so it cannot lower the multicast user cap"
                );
            }
        }

        let feed_name = feeds.get(&feed_pk).map_or("", |f| f.name.as_str());
        writeln!(out, "AccessPass PDA: {accesspass_pk}")?;
        writeln!(out, "Removing feed {feed_name} ({feed_pk})")?;
        if seated {
            writeln!(
                out,
                "  Drop the seat and keep {} other feed(s)",
                remaining.len()
            )?;
        } else {
            writeln!(out, "  The seat is already off the access pass")?;
        }
        let holders = find_holders(client, user_payer, client_ip, accesspass_pk, feed_pk)?;
        for (user_pk, user) in &holders {
            if left_with_no_feed(user, feed_pk, &remaining) {
                writeln!(out, "  Unsubscribe and delete user {user_pk}")?;
            } else {
                writeln!(out, "  Unsubscribe user {user_pk}")?;
            }
        }
        if cap_changes {
            writeln!(
                out,
                "  Set max multicast users from {} to {max_multicast_users}",
                accesspass.max_multicast_users
            )?;
        }

        if self.dry_run {
            writeln!(out, "[dry-run] no transactions sent.")?;
            return Ok(());
        }
        write!(out, "Proceed? [y/N]: ")?;
        out.flush()?;
        let mut answer = String::new();
        input.read_line(&mut answer)?;
        if !answer.trim().eq_ignore_ascii_case("y") {
            writeln!(out, "Aborted.")?;
            return Ok(());
        }

        // Drop the seat before reading the holders, so no user can subscribe to the feed after
        // the read.
        if seated {
            let signature = client.set_accesspass_feeds(SetAccessPassFeedsCommand {
                client_ip,
                user_payer,
                feeds: remaining
                    .iter()
                    .map(|seat| FeedSeatProvision {
                        feed_key: seat.feed_key,
                        max_users: seat.max_users,
                        max_future_users: seat.max_future_users,
                        anniversary_day: seat.anniversary_day,
                        window_end: seat.window_end,
                        terminates_at: seat.terminates_at,
                    })
                    .collect(),
            })?;
            writeln!(
                out,
                "Set {} remaining feed(s) on the access pass: {signature}",
                remaining.len()
            )?;
        }

        for (user_pk, user) in find_holders(client, user_payer, client_ip, accesspass_pk, feed_pk)?
        {
            let signature = client.unsubscribe_feed(UnsubscribeFeedCommand {
                user_pk,
                feed_pks: vec![feed_pk],
                accesspass_pk: Some(accesspass_pk),
            })?;
            writeln!(out, "Unsubscribed user {user_pk}: {signature}")?;
            if left_with_no_feed(&user, feed_pk, &remaining) {
                let signature = client.delete_user(DeleteUserCommand {
                    pubkey: user_pk,
                    accesspass_pk: Some(accesspass_pk),
                    kind: None,
                })?;
                writeln!(out, "Deleted user {user_pk}: {signature}")?;
            }
        }

        if cap_changes {
            let signature = client.set_accesspass(SetAccessPassCommand {
                accesspass_type: AccessPassType::EdgeSeat(vec![]),
                client_ip,
                user_payer,
                last_access_epoch: accesspass.last_access_epoch,
                allow_multiple_ip: accesspass.allow_multiple_ip(),
                tenant,
                max_unicast_users: accesspass.max_unicast_users,
                max_multicast_users,
            })?;
            writeln!(
                out,
                "Set max multicast users from {} to {max_multicast_users}: {signature}",
                accesspass.max_multicast_users
            )?;
        }

        Ok(())
    }
}

fn find_holders<C: CliCommand>(
    client: &C,
    user_payer: Pubkey,
    client_ip: Ipv4Addr,
    accesspass_pk: Pubkey,
    feed_pk: Pubkey,
) -> eyre::Result<Vec<(Pubkey, User)>> {
    let mut holders: Vec<(Pubkey, User)> = client
        .list_user(ListUserCommand {})?
        .into_iter()
        .filter(|(_, user)| {
            user.owner == user_payer
                && user.feed_pks.contains(&feed_pk)
                && (user.accesspass_pk == accesspass_pk
                    || (user.accesspass_pk == Pubkey::default()
                        && (client_ip == Ipv4Addr::UNSPECIFIED || user.client_ip == client_ip)))
        })
        .collect();
    holders.sort_by_key(|(user_pk, _)| *user_pk);
    Ok(holders)
}

fn left_with_no_feed(user: &User, feed_pk: Pubkey, remaining: &[&FeedSeat]) -> bool {
    user.publishers.is_empty()
        && !user
            .feed_pks
            .iter()
            .any(|held| *held != feed_pk && remaining.iter().any(|s| s.feed_key == *held))
}

fn resolve_feed(
    feed: &str,
    seats: &[FeedSeat],
    feeds: &HashMap<Pubkey, Feed>,
) -> eyre::Result<Pubkey> {
    if let Ok(feed_pk) = Pubkey::from_str(feed) {
        if feeds.contains_key(&feed_pk) || seats.iter().any(|s| s.feed_key == feed_pk) {
            return Ok(feed_pk);
        }
        eyre::bail!("feed {feed_pk} not found");
    }
    feeds
        .iter()
        .find(|(_, f)| f.name == feed)
        .map(|(feed_pk, _)| *feed_pk)
        .ok_or_else(|| eyre::eyre!("no feed named {feed}"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{AccountType, UserType};
    use doublezero_serviceability::state::{
        accesspass::{AccessPass, AccessPassStatus},
        tenant::{Tenant, TenantBillingConfig, TenantPaymentStatus},
    };
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    fn seat(feed_key: Pubkey, max_users: u8) -> FeedSeat {
        FeedSeat {
            feed_key,
            max_users,
            max_future_users: max_users,
            current_users: 1,
            anniversary_day: 15,
            window_end: 1_900_000_000,
            terminates_at: 1_900_000_000,
        }
    }

    fn provision(seat: &FeedSeat) -> FeedSeatProvision {
        FeedSeatProvision {
            feed_key: seat.feed_key,
            max_users: seat.max_users,
            max_future_users: seat.max_future_users,
            anniversary_day: seat.anniversary_day,
            window_end: seat.window_end,
            terminates_at: seat.terminates_at,
        }
    }

    fn feed(code: &str, name: &str) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            code: code.to_string(),
            name: name.to_string(),
            exchange: Pubkey::new_unique(),
            ..Default::default()
        }
    }

    fn user(owner: Pubkey, accesspass_pk: Pubkey, feed_pks: Vec<Pubkey>) -> User {
        User {
            account_type: AccountType::User,
            owner,
            user_type: UserType::Multicast,
            feed_pks,
            accesspass_pk,
            ..Default::default()
        }
    }

    fn edge_seat_pass(user_payer: Pubkey, seats: Vec<FeedSeat>) -> AccessPass {
        AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            accesspass_type: AccessPassType::EdgeSeat(seats),
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer,
            last_access_epoch: u64::MAX,
            connection_count: 1,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
            tenant_allowlist: vec![],
            unicast_user_count: 0,
            max_unicast_users: 0,
            multicast_user_count: 1,
            max_multicast_users: 5,
        }
    }

    fn run<C: CliCommand>(client: &C, cmd: RemoveFeedAccessPassCliCommand) -> eyre::Result<String> {
        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        block_on(cmd.execute(&ctx, client, &mut output, &mut "y\n".as_bytes()))?;
        Ok(String::from_utf8(output).unwrap())
    }

    fn remove(user_payer: Pubkey, feed: &str, dry_run: bool) -> RemoveFeedAccessPassCliCommand {
        RemoveFeedAccessPassCliCommand {
            client_ip: None,
            user_payer: user_payer.to_string(),
            feed: feed.to_string(),
            dry_run,
        }
    }

    #[test]
    fn test_cli_accesspass_remove_feed_by_name_keeps_the_other_metro() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let accesspass_pk = Pubkey::new_unique();
        let (fra_pk, lon_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        let (both_pk, fra_only_pk, other_owner_pk) = (
            Pubkey::new_unique(),
            Pubkey::new_unique(),
            Pubkey::new_unique(),
        );
        let lon_seat = seat(lon_pk, 3);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        let pass = edge_seat_pass(user_payer, vec![seat(fra_pk, 2), lon_seat.clone()]);
        client
            .expect_get_accesspass()
            .with(predicate::eq(GetAccessPassCommand {
                client_ip: Ipv4Addr::UNSPECIFIED,
                user_payer,
            }))
            .returning(move |_| Ok(Some((accesspass_pk, pass.clone()))));
        client.expect_list_feed().returning(move |_| {
            Ok(HashMap::from([
                (fra_pk, feed("solana-shreds-full", "Solana Shreds FRA")),
                (lon_pk, feed("solana-shreds-full", "Solana Shreds LON")),
            ]))
        });
        client.expect_list_user().times(2).returning(move |_| {
            Ok(HashMap::from([
                (
                    both_pk,
                    user(user_payer, accesspass_pk, vec![fra_pk, lon_pk]),
                ),
                (fra_only_pk, user(user_payer, accesspass_pk, vec![fra_pk])),
                (
                    other_owner_pk,
                    user(Pubkey::new_unique(), accesspass_pk, vec![fra_pk]),
                ),
            ]))
        });

        let mut seq = mockall::Sequence::new();
        client
            .expect_set_accesspass_feeds()
            .with(predicate::eq(SetAccessPassFeedsCommand {
                client_ip: Ipv4Addr::UNSPECIFIED,
                user_payer,
                feeds: vec![provision(&lon_seat)],
            }))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));
        for user_pk in [both_pk, fra_only_pk] {
            client
                .expect_unsubscribe_feed()
                .with(predicate::eq(UnsubscribeFeedCommand {
                    user_pk,
                    feed_pks: vec![fra_pk],
                    accesspass_pk: Some(accesspass_pk),
                }))
                .times(1)
                .in_sequence(&mut seq)
                .returning(|_| Ok(Signature::new_unique()));
        }
        client
            .expect_delete_user()
            .with(predicate::eq(DeleteUserCommand {
                pubkey: fra_only_pk,
                accesspass_pk: Some(accesspass_pk),
                kind: None,
            }))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));
        client
            .expect_set_accesspass()
            .with(predicate::eq(SetAccessPassCommand {
                accesspass_type: AccessPassType::EdgeSeat(vec![]),
                client_ip: Ipv4Addr::UNSPECIFIED,
                user_payer,
                last_access_epoch: u64::MAX,
                allow_multiple_ip: false,
                tenant: Pubkey::default(),
                max_unicast_users: 0,
                max_multicast_users: 3,
            }))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));

        run(&client, remove(user_payer, "Solana Shreds FRA", false)).unwrap();
    }

    #[test]
    fn test_cli_accesspass_remove_feed_dry_run_sends_nothing() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let accesspass_pk = Pubkey::new_unique();
        let (fra_pk, lon_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        let holder_pk = Pubkey::new_unique();

        client.expect_check_requirements().returning(|_| Ok(()));
        let pass = edge_seat_pass(user_payer, vec![seat(fra_pk, 2), seat(lon_pk, 3)]);
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((accesspass_pk, pass.clone()))));
        client
            .expect_list_feed()
            .returning(move |_| Ok(HashMap::from([(fra_pk, feed("fra", "FRA"))])));
        client.expect_list_user().times(1).returning(move |_| {
            Ok(HashMap::from([(
                holder_pk,
                user(user_payer, accesspass_pk, vec![fra_pk]),
            )]))
        });

        let output = run(&client, remove(user_payer, &fra_pk.to_string(), true)).unwrap();
        assert!(output.contains(&format!("Unsubscribe and delete user {holder_pk}")));
        assert!(output.contains("Set max multicast users from 5 to 3"));
        assert!(output.contains("[dry-run] no transactions sent."));
    }

    #[test]
    fn test_cli_accesspass_remove_feed_refuses_before_writing_without_tenant_admin() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let (fra_pk, tenant_pk) = (Pubkey::new_unique(), Pubkey::new_unique());

        client.expect_check_requirements().returning(|_| Ok(()));
        client.expect_get_payer().returning(Pubkey::new_unique);
        let mut pass = edge_seat_pass(user_payer, vec![seat(fra_pk, 2)]);
        pass.tenant_allowlist = vec![tenant_pk];
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), pass.clone()))));
        client
            .expect_list_feed()
            .returning(move |_| Ok(HashMap::from([(fra_pk, feed("fra", "FRA"))])));
        client
            .expect_get_tenant()
            .with(predicate::eq(GetTenantCommand {
                pubkey_or_code: tenant_pk.to_string(),
            }))
            .returning(move |_| {
                Ok((
                    tenant_pk,
                    Tenant {
                        account_type: AccountType::Tenant,
                        owner: Pubkey::new_unique(),
                        bump_seed: 0,
                        code: "tenant".to_string(),
                        vrf_id: 100,
                        reference_count: 1,
                        administrators: vec![Pubkey::new_unique()],
                        token_account: Pubkey::default(),
                        payment_status: TenantPaymentStatus::Paid,
                        metro_routing: true,
                        route_liveness: false,
                        billing: TenantBillingConfig::default(),
                        include_topologies: vec![],
                    },
                ))
            });

        let err = run(&client, remove(user_payer, "FRA", false)).unwrap_err();
        assert!(err
            .to_string()
            .contains(&format!("is not an administrator of tenant {tenant_pk}")));
    }

    #[test]
    fn test_cli_accesspass_remove_feed_rejects_an_unknown_feed() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let seated_pk = Pubkey::new_unique();
        let unknown_pk = Pubkey::new_unique();

        client.expect_check_requirements().returning(|_| Ok(()));
        let client_ip: Ipv4Addr = [100, 0, 0, 1].into();
        let pass = edge_seat_pass(user_payer, vec![seat(seated_pk, 2)]);
        client
            .expect_get_accesspass()
            .with(predicate::eq(GetAccessPassCommand {
                client_ip,
                user_payer,
            }))
            .returning(move |_| Ok(Some((Pubkey::new_unique(), pass.clone()))));
        client
            .expect_list_feed()
            .returning(move |_| Ok(HashMap::from([(seated_pk, feed("seated", "Seated"))])));

        let err = run(
            &client,
            RemoveFeedAccessPassCliCommand {
                client_ip: Some(client_ip),
                ..remove(user_payer, &unknown_pk.to_string(), false)
            },
        )
        .unwrap_err();
        assert_eq!(err.to_string(), format!("feed {unknown_pk} not found"));
    }
}
