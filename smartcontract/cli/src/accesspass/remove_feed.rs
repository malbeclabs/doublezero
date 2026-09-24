use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::{
    commands::{
        accesspass::{
            get::{GetAccessPassCommand, GetExactAccessPassCommand},
            set::SetAccessPassCommand,
            set_feeds::{FeedSeatProvision, SetAccessPassFeedsCommand},
        },
        feed::list::ListFeedCommand,
        multicastgroup::unsubscribe_feed::UnsubscribeFeedCommand,
        user::list::ListUserCommand,
    },
    Feed,
};
use doublezero_serviceability::state::accesspass::{AccessPassType, FeedSeat};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, io::Write, net::Ipv4Addr, str::FromStr};

/// Remove one feed from an EdgeSeat access pass while its other feeds stay active. Users holding
/// the feed are unsubscribed from it, and the multicast user cap drops to the remaining seats.
/// Requires `ACCESS_PASS_ADMIN` or foundation allowlist membership.
#[derive(Args, Debug)]
pub struct RemoveFeedAccessPassCliCommand {
    #[arg(long)]
    pub client_ip: Option<Ipv4Addr>,
    #[arg(long)]
    pub user_payer: String,
    #[arg(long)]
    pub feed: String,
}

impl RemoveFeedAccessPassCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let user_payer = if self.user_payer.eq_ignore_ascii_case("me") {
            client.get_payer()
        } else {
            Pubkey::from_str(&self.user_payer)?
        };
        let client_ip = self.client_ip.unwrap_or(Ipv4Addr::UNSPECIFIED);

        // GetExactAccessPassCommand refuses 0.0.0.0, and GetAccessPassCommand swaps any other IP
        // for the 0.0.0.0 pass when one exists.
        let found = if client_ip == Ipv4Addr::UNSPECIFIED {
            client.get_accesspass(GetAccessPassCommand {
                client_ip,
                user_payer,
            })?
        } else {
            client.get_accesspass_exact(GetExactAccessPassCommand {
                client_ip,
                user_payer,
            })?
        };
        let (accesspass_pk, accesspass) = found.ok_or_else(|| {
            eyre::eyre!("no access pass for client_ip {client_ip} and user_payer {user_payer}")
        })?;
        let AccessPassType::EdgeSeat(seats) = &accesspass.accesspass_type else {
            eyre::bail!(
                "access pass {accesspass_pk} is {}; only an EdgeSeat pass carries feeds",
                accesspass.accesspass_type
            );
        };

        let feeds = client.list_feed(ListFeedCommand {})?;
        let feed_pk = resolve_seated_feed(&self.feed, seats, &feeds)?;
        let remaining: Vec<&FeedSeat> = seats.iter().filter(|s| s.feed_key != feed_pk).collect();
        let max_multicast_users = u16::try_from(
            remaining
                .iter()
                .map(|s| u32::from(s.max_users))
                .sum::<u32>(),
        )?;

        writeln!(out, "AccessPass PDA: {accesspass_pk}")?;
        let feed_name = feeds.get(&feed_pk).map_or("", |f| f.name.as_str());
        writeln!(out, "Removing feed {feed_name} ({feed_pk})")?;

        let mut holders: Vec<Pubkey> = client
            .list_user(ListUserCommand {})?
            .into_iter()
            .filter(|(_, user)| {
                user.owner == user_payer
                    && user.feed_pks.contains(&feed_pk)
                    && (user.accesspass_pk == accesspass_pk
                        || (user.accesspass_pk == Pubkey::default()
                            && (client_ip == Ipv4Addr::UNSPECIFIED || user.client_ip == client_ip)))
            })
            .map(|(user_pk, _)| user_pk)
            .collect();
        holders.sort();
        for user_pk in holders {
            let signature = client.unsubscribe_feed(UnsubscribeFeedCommand {
                user_pk,
                feed_pks: vec![feed_pk],
                accesspass_pk: Some(accesspass_pk),
            })?;
            writeln!(out, "Unsubscribed user {user_pk}: {signature}")?;
        }

        if max_multicast_users != accesspass.max_multicast_users {
            let signature = client.set_accesspass(SetAccessPassCommand {
                accesspass_type: AccessPassType::EdgeSeat(vec![]),
                client_ip,
                user_payer,
                last_access_epoch: accesspass.last_access_epoch,
                allow_multiple_ip: accesspass.allow_multiple_ip(),
                tenant: accesspass
                    .tenant_allowlist
                    .first()
                    .copied()
                    .unwrap_or_default(),
                max_unicast_users: accesspass.max_unicast_users,
                max_multicast_users,
            })?;
            writeln!(
                out,
                "Set max multicast users from {} to {max_multicast_users}: {signature}",
                accesspass.max_multicast_users
            )?;
        }

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

        Ok(())
    }
}

fn resolve_seated_feed(
    feed: &str,
    seats: &[FeedSeat],
    feeds: &HashMap<Pubkey, Feed>,
) -> eyre::Result<Pubkey> {
    if let Ok(feed_pk) = Pubkey::from_str(feed) {
        if seats.iter().any(|s| s.feed_key == feed_pk) {
            return Ok(feed_pk);
        }
        eyre::bail!("feed {feed_pk} is not on the access pass");
    }
    let matches: Vec<Pubkey> = seats
        .iter()
        .map(|s| s.feed_key)
        .filter(|feed_pk| feeds.get(feed_pk).is_some_and(|f| f.name == feed))
        .collect();
    match matches.as_slice() {
        [feed_pk] => Ok(*feed_pk),
        [] => eyre::bail!("no feed named {feed} is on the access pass"),
        _ => eyre::bail!(
            "{} feeds named {feed} are on the access pass; pass the feed pubkey instead",
            matches.len()
        ),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{AccountType, User, UserType};
    use doublezero_serviceability::state::accesspass::{AccessPass, AccessPassStatus};
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

    fn feed(code: &str, name: &str) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            code: code.to_string(),
            name: name.to_string(),
            exchange: Pubkey::new_unique(),
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

    #[test]
    fn test_cli_accesspass_remove_feed_by_name_keeps_the_other_metro() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let accesspass_pk = Pubkey::new_unique();
        let (fra_pk, lon_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        let (holder_pk, other_owner_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
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
        client.expect_list_user().returning(move |_| {
            let holder = User {
                account_type: AccountType::User,
                owner: user_payer,
                user_type: UserType::Multicast,
                feed_pks: vec![fra_pk, lon_pk],
                accesspass_pk,
                ..Default::default()
            };
            let other_owner = User {
                owner: Pubkey::new_unique(),
                ..holder.clone()
            };
            Ok(HashMap::from([
                (holder_pk, holder),
                (other_owner_pk, other_owner),
            ]))
        });

        client
            .expect_unsubscribe_feed()
            .with(predicate::eq(UnsubscribeFeedCommand {
                user_pk: holder_pk,
                feed_pks: vec![fra_pk],
                accesspass_pk: Some(accesspass_pk),
            }))
            .times(1)
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
            .returning(|_| Ok(Signature::new_unique()));
        client
            .expect_set_accesspass_feeds()
            .with(predicate::eq(SetAccessPassFeedsCommand {
                client_ip: Ipv4Addr::UNSPECIFIED,
                user_payer,
                feeds: vec![FeedSeatProvision {
                    feed_key: lon_pk,
                    max_users: lon_seat.max_users,
                    max_future_users: lon_seat.max_future_users,
                    anniversary_day: lon_seat.anniversary_day,
                    window_end: lon_seat.window_end,
                    terminates_at: lon_seat.terminates_at,
                }],
            }))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        block_on(
            RemoveFeedAccessPassCliCommand {
                client_ip: None,
                user_payer: user_payer.to_string(),
                feed: "Solana Shreds FRA".to_string(),
            }
            .execute(&ctx, &client, &mut output),
        )
        .unwrap();
    }

    #[test]
    fn test_cli_accesspass_remove_feed_rejects_a_feed_not_on_the_pass() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let seated_pk = Pubkey::new_unique();
        let unseated_pk = Pubkey::new_unique();

        client.expect_check_requirements().returning(|_| Ok(()));
        let client_ip: Ipv4Addr = [100, 0, 0, 1].into();
        let pass = edge_seat_pass(user_payer, vec![seat(seated_pk, 2)]);
        client
            .expect_get_accesspass_exact()
            .with(predicate::eq(GetExactAccessPassCommand {
                client_ip,
                user_payer,
            }))
            .returning(move |_| Ok(Some((Pubkey::new_unique(), pass.clone()))));
        client
            .expect_list_feed()
            .returning(move |_| Ok(HashMap::from([(seated_pk, feed("seated", "Seated"))])));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let err = block_on(
            RemoveFeedAccessPassCliCommand {
                client_ip: Some(client_ip),
                user_payer: user_payer.to_string(),
                feed: unseated_pk.to_string(),
            }
            .execute(&ctx, &client, &mut output),
        )
        .unwrap_err();
        assert_eq!(
            err.to_string(),
            format!("feed {unseated_pk} is not on the access pass")
        );
    }
}
