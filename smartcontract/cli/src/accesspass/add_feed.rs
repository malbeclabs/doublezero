use crate::{
    accesspass::remove_feed::{
        kept_seat, refuse_cap_below_public_users, refuse_ended_or_deleted_feed, resolve_feed,
        tenant_for_cap_change,
    },
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use chrono::{Datelike, NaiveDate, Utc};
use clap::Args;
use doublezero_cli_core::{validators::validate_pubkey, CliContext};
use doublezero_sdk::commands::{
    accesspass::{
        get::GetAccessPassCommand,
        set::SetAccessPassCommand,
        set_feeds::{FeedSeatProvision, SetAccessPassFeedsCommand},
    },
    feed::list::ListFeedCommand,
};
use doublezero_serviceability::state::{accesspass::AccessPassType, feed::FeedStatus};
use eyre::WrapErr;
use solana_sdk::pubkey::Pubkey;
use std::{
    io::{BufRead, Write},
    net::Ipv4Addr,
    str::FromStr,
};

/// Add one feed to an EdgeSeat access pass, or change the seat of a feed it already holds, and
/// set the multicast user cap to the total of its seats. Requires `ACCESS_PASS_ADMIN`, or
/// foundation allowlist membership.
///
/// Use this command only for testing, such as in the e2e suite or on devnet. On mainnet-beta, a
/// customer gets a feed through the frontend, with a normal subscription or a trial, and the
/// oracle writes the feed onto the access pass.
#[derive(Args, Debug)]
pub struct AddFeedAccessPassCliCommand {
    /// Client IP of the access pass. Defaults to 0.0.0.0, the pass every host of the user payer shares
    #[arg(long)]
    pub client_ip: Option<Ipv4Addr>,
    /// User payer of the access pass, or "me"
    #[arg(long, value_parser = validate_pubkey)]
    pub user_payer: String,
    /// Feed name or pubkey
    #[arg(long)]
    pub feed: String,
    /// Number of users the feed admits on this access pass
    #[arg(long, value_parser = clap::value_parser!(u8).range(1..))]
    pub max_users: u8,
    /// Day the seat ends, as YYYY-MM-DD in UTC. The seat ends at the start of that day
    #[arg(long)]
    pub terminates_at: NaiveDate,
    /// Print the plan without sending any transaction
    #[arg(long, default_value_t = false)]
    pub dry_run: bool,
    /// Do not ask for confirmation
    #[arg(short, long, default_value_t = false)]
    pub yes: bool,
}

impl AddFeedAccessPassCliCommand {
    pub async fn execute<C: CliCommand, W: Write, R: BufRead>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
        input: &mut R,
    ) -> eyre::Result<()> {
        tracing::debug!(env = %ctx.env, user_payer = %self.user_payer, feed = %self.feed, "access-pass add-feed");
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let now = Utc::now().timestamp();
        let terminates_at = self
            .terminates_at
            .and_hms_opt(0, 0, 0)
            .expect("midnight is a valid time")
            .and_utc()
            .timestamp();
        if terminates_at <= now {
            eyre::bail!(
                "--terminates-at {} is not in the future",
                self.terminates_at
            );
        }

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
        let feed = feeds
            .get(&feed_pk)
            .ok_or_else(|| eyre::eyre!("feed {feed_pk} not found"))?;
        let seated = seats.iter().find(|s| s.feed_key == feed_pk);
        match seated {
            Some(seat) if self.max_users < seat.current_users => eyre::bail!(
                "feed {} has {} user(s) on access pass {accesspass_pk}, so --max-users must be at least {}",
                feed.name,
                seat.current_users,
                seat.current_users
            ),
            None if feed.status != FeedStatus::Active => eyre::bail!(
                "feed {} is {}, so it takes no new seat",
                feed.name,
                feed.status
            ),
            _ => {}
        }
        for seat in seats.iter().filter(|s| s.feed_key != feed_pk) {
            refuse_ended_or_deleted_feed(accesspass_pk, seat, &feeds, now)?;
        }
        let new_seat = FeedSeatProvision {
            feed_key: feed_pk,
            max_users: self.max_users,
            max_future_users: self.max_users,
            anniversary_day: self.terminates_at.day() as u8,
            window_end: terminates_at,
            terminates_at,
        };
        let seated = seated.is_some();
        let mut provisions: Vec<FeedSeatProvision> = seats
            .iter()
            .map(|seat| {
                if seat.feed_key == feed_pk {
                    new_seat.clone()
                } else {
                    kept_seat(seat, now)
                }
            })
            .collect();
        if !seated {
            provisions.push(new_seat);
        }
        let max_public_multicast_users = u16::try_from(
            provisions
                .iter()
                .map(|seat| u32::from(seat.max_users))
                .sum::<u32>(),
        )?;
        let cap_changes = max_public_multicast_users != accesspass.max_public_multicast_users;
        if cap_changes {
            refuse_cap_below_public_users(
                accesspass_pk,
                max_public_multicast_users,
                u32::from(accesspass.public_multicast_user_count),
            )?;
        }
        let tenant = tenant_for_cap_change(client, &accesspass, accesspass_pk, cap_changes)?;

        let feed_name = feed.name.as_str();
        writeln!(out, "AccessPass PDA: {accesspass_pk}")?;
        if seated {
            writeln!(out, "Changing feed {feed_name} ({feed_pk})")?;
        } else {
            writeln!(out, "Adding feed {feed_name} ({feed_pk})")?;
        }
        writeln!(
            out,
            "  Admit {} user(s) until {}",
            self.max_users, self.terminates_at
        )?;
        if cap_changes {
            writeln!(
                out,
                "  Set max multicast users from {} to {max_public_multicast_users}",
                accesspass.max_public_multicast_users
            )?;
        }

        if self.dry_run {
            writeln!(out, "[dry-run] no transactions sent.")?;
            return Ok(());
        }
        if !self.yes {
            write!(out, "Proceed? [y/N]: ")?;
            out.flush()?;
            let mut answer = String::new();
            input.read_line(&mut answer)?;
            if !answer.trim().eq_ignore_ascii_case("y") {
                writeln!(out)?;
                eyre::bail!("the answer was not y, so nothing was changed");
            }
        }

        let feed_count = provisions.len();
        let signature = client.set_accesspass_feeds(SetAccessPassFeedsCommand {
            client_ip,
            user_payer,
            feeds: provisions,
        })?;
        writeln!(
            out,
            "Set {feed_count} feed(s) on the access pass: {signature}"
        )?;

        if cap_changes {
            let signature = client.set_accesspass(SetAccessPassCommand {
                accesspass_type: AccessPassType::EdgeSeat(vec![]),
                client_ip,
                user_payer,
                last_access_epoch: accesspass.last_access_epoch,
                allow_multiple_ip: accesspass.allow_multiple_ip(),
                tenant,
                max_unicast_users: accesspass.max_unicast_users,
                max_public_multicast_users,
            })
            .wrap_err("add-feed set the feeds on the access pass but not the multicast user cap. Run add-feed again with the same arguments to set the cap")?;
            writeln!(
                out,
                "Set max multicast users from {} to {max_public_multicast_users}: {signature}",
                accesspass.max_public_multicast_users
            )?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{AccountType, Feed};
    use doublezero_serviceability::state::accesspass::{AccessPass, AccessPassStatus, FeedSeat};
    use mockall::predicate;
    use solana_sdk::signature::Signature;
    use std::collections::HashMap;

    // 2099-01-15 00:00:00 UTC.
    const TERMINATES_AT: i64 = 4_072_118_400;

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

    fn new_provision(feed_key: Pubkey, max_users: u8) -> FeedSeatProvision {
        FeedSeatProvision {
            feed_key,
            max_users,
            max_future_users: max_users,
            anniversary_day: 15,
            window_end: TERMINATES_AT,
            terminates_at: TERMINATES_AT,
        }
    }

    fn feed(name: &str) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            code: "solana-shreds-full".to_string(),
            name: name.to_string(),
            exchange: Pubkey::new_unique(),
            status: FeedStatus::Active,
            ..Default::default()
        }
    }

    fn pass(
        user_payer: Pubkey,
        accesspass_type: AccessPassType,
        max_public_multicast_users: u16,
    ) -> AccessPass {
        AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            accesspass_type,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer,
            last_access_epoch: u64::MAX,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
            tenant_allowlist: vec![],
            unicast_user_count: 0,
            max_unicast_users: 0,
            public_multicast_user_count: 0,
            max_public_multicast_users,
        }
    }

    fn add(
        user_payer: Pubkey,
        feed: &str,
        max_users: u8,
        dry_run: bool,
    ) -> AddFeedAccessPassCliCommand {
        AddFeedAccessPassCliCommand {
            client_ip: None,
            user_payer: user_payer.to_string(),
            feed: feed.to_string(),
            max_users,
            terminates_at: NaiveDate::from_ymd_opt(2099, 1, 15).unwrap(),
            dry_run,
            yes: false,
        }
    }

    fn run<C: CliCommand>(client: &C, cmd: AddFeedAccessPassCliCommand) -> eyre::Result<String> {
        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        block_on(cmd.execute(&ctx, client, &mut output, &mut "y\n".as_bytes()))?;
        Ok(String::from_utf8(output).unwrap())
    }

    fn expect_pass_and_feeds(
        client: &mut crate::doublezerocommand::MockCliCommand,
        user_payer: Pubkey,
        accesspass_pk: Pubkey,
        accesspass: AccessPass,
        feeds: HashMap<Pubkey, Feed>,
    ) {
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_accesspass()
            .with(predicate::eq(GetAccessPassCommand {
                client_ip: Ipv4Addr::UNSPECIFIED,
                user_payer,
            }))
            .returning(move |_| Ok(Some((accesspass_pk, accesspass.clone()))));
        client
            .expect_list_feed()
            .returning(move |_| Ok(feeds.clone()));
    }

    fn expect_cap(
        client: &mut crate::doublezerocommand::MockCliCommand,
        user_payer: Pubkey,
        max_public_multicast_users: u16,
        seq: &mut mockall::Sequence,
    ) {
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
                max_public_multicast_users,
            }))
            .times(1)
            .in_sequence(seq)
            .returning(|_| Ok(Signature::new_unique()));
    }

    #[test]
    fn test_cli_accesspass_add_feed_adds_a_feed_and_raises_the_cap() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let accesspass_pk = Pubkey::new_unique();
        let (ams_pk, lon_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        let lon_seat = seat(lon_pk, 3);
        expect_pass_and_feeds(
            &mut client,
            user_payer,
            accesspass_pk,
            pass(
                user_payer,
                AccessPassType::EdgeSeat(vec![lon_seat.clone()]),
                3,
            ),
            HashMap::from([
                (ams_pk, feed("solana-shreds-full-ams")),
                (lon_pk, feed("solana-shreds-full-lon")),
            ]),
        );

        let mut seq = mockall::Sequence::new();
        client
            .expect_set_accesspass_feeds()
            .with(predicate::eq(SetAccessPassFeedsCommand {
                client_ip: Ipv4Addr::UNSPECIFIED,
                user_payer,
                feeds: vec![provision(&lon_seat), new_provision(ams_pk, 2)],
            }))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));
        expect_cap(&mut client, user_payer, 5, &mut seq);

        let output = run(&client, add(user_payer, "solana-shreds-full-ams", 2, false)).unwrap();
        assert!(
            output.contains(&format!("Adding feed solana-shreds-full-ams ({ams_pk})")),
            "{output}"
        );
        assert!(
            output.contains("Set max multicast users from 3 to 5"),
            "{output}"
        );
    }

    #[test]
    fn test_cli_accesspass_add_feed_changes_the_seat_of_a_held_feed() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let accesspass_pk = Pubkey::new_unique();
        let ams_pk = Pubkey::new_unique();
        expect_pass_and_feeds(
            &mut client,
            user_payer,
            accesspass_pk,
            pass(
                user_payer,
                AccessPassType::EdgeSeat(vec![seat(ams_pk, 2)]),
                2,
            ),
            HashMap::from([(ams_pk, feed("solana-shreds-full-ams"))]),
        );

        let mut seq = mockall::Sequence::new();
        client
            .expect_set_accesspass_feeds()
            .with(predicate::eq(SetAccessPassFeedsCommand {
                client_ip: Ipv4Addr::UNSPECIFIED,
                user_payer,
                feeds: vec![new_provision(ams_pk, 4)],
            }))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));
        expect_cap(&mut client, user_payer, 4, &mut seq);

        let output = run(&client, add(user_payer, &ams_pk.to_string(), 4, false)).unwrap();
        assert!(
            output.contains(&format!("Changing feed solana-shreds-full-ams ({ams_pk})")),
            "{output}"
        );
    }

    #[test]
    fn test_cli_accesspass_add_feed_dry_run_sends_nothing() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let ams_pk = Pubkey::new_unique();
        expect_pass_and_feeds(
            &mut client,
            user_payer,
            Pubkey::new_unique(),
            pass(user_payer, AccessPassType::EdgeSeat(vec![]), 0),
            HashMap::from([(ams_pk, feed("solana-shreds-full-ams"))]),
        );

        let output = run(&client, add(user_payer, "solana-shreds-full-ams", 2, true)).unwrap();
        assert!(
            output.contains("Set max multicast users from 0 to 2"),
            "{output}"
        );
        assert!(
            output.contains("[dry-run] no transactions sent."),
            "{output}"
        );
    }

    #[test]
    fn test_cli_accesspass_add_feed_refuses_a_pass_that_is_not_edge_seat() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let ams_pk = Pubkey::new_unique();
        expect_pass_and_feeds(
            &mut client,
            user_payer,
            Pubkey::new_unique(),
            pass(user_payer, AccessPassType::Prepaid, 1),
            HashMap::from([(ams_pk, feed("solana-shreds-full-ams"))]),
        );

        let err = run(&client, add(user_payer, "solana-shreds-full-ams", 2, false)).unwrap_err();
        assert!(
            err.to_string()
                .contains("only an EdgeSeat pass carries feeds"),
            "{err}"
        );
    }

    #[test]
    fn test_cli_accesspass_add_feed_refuses_a_name_that_two_feeds_share() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let (ams_pk, other_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        expect_pass_and_feeds(
            &mut client,
            user_payer,
            Pubkey::new_unique(),
            pass(user_payer, AccessPassType::EdgeSeat(vec![]), 0),
            HashMap::from([
                (ams_pk, feed("solana-shreds-full-ams")),
                (other_pk, feed("solana-shreds-full-ams")),
            ]),
        );

        let err = run(&client, add(user_payer, "solana-shreds-full-ams", 2, false)).unwrap_err();
        let mut both = [ams_pk, other_pk];
        both.sort();
        assert_eq!(
            err.to_string(),
            format!(
                "2 feeds are named solana-shreds-full-ams, so pass the pubkey of one of them: {}, {}",
                both[0], both[1]
            )
        );

        // The pubkey picks one of them.
        let output = run(&client, add(user_payer, &ams_pk.to_string(), 2, true)).unwrap();
        assert!(
            output.contains(&format!("Adding feed solana-shreds-full-ams ({ams_pk})")),
            "{output}"
        );
    }

    #[test]
    fn test_cli_accesspass_add_feed_refuses_a_past_date() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));
        let user_payer = Pubkey::new_unique();

        let mut past = add(user_payer, "solana-shreds-full-ams", 2, false);
        past.terminates_at = NaiveDate::from_ymd_opt(2020, 1, 1).unwrap();
        let err = run(&client, past).unwrap_err();
        assert_eq!(
            err.to_string(),
            "--terminates-at 2020-01-01 is not in the future"
        );
    }

    #[test]
    fn test_cli_accesspass_add_feed_sends_a_seat_past_its_window_end_with_its_future_cap() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let (ams_pk, lon_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        // The LON seat admitted 3 users until 2026-11-01 and admits 4 from then until 2030.
        let lon_seat = FeedSeat {
            max_future_users: 4,
            window_end: 1_793_491_200,
            ..seat(lon_pk, 3)
        };
        expect_pass_and_feeds(
            &mut client,
            user_payer,
            Pubkey::new_unique(),
            pass(
                user_payer,
                AccessPassType::EdgeSeat(vec![lon_seat.clone()]),
                3,
            ),
            HashMap::from([
                (ams_pk, feed("solana-shreds-full-ams")),
                (lon_pk, feed("solana-shreds-full-lon")),
            ]),
        );

        let mut seq = mockall::Sequence::new();
        client
            .expect_set_accesspass_feeds()
            .with(predicate::eq(SetAccessPassFeedsCommand {
                client_ip: Ipv4Addr::UNSPECIFIED,
                user_payer,
                feeds: vec![
                    FeedSeatProvision {
                        max_users: 4,
                        window_end: lon_seat.terminates_at,
                        ..provision(&lon_seat)
                    },
                    new_provision(ams_pk, 2),
                ],
            }))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));
        expect_cap(&mut client, user_payer, 6, &mut seq);

        let output = run(&client, add(user_payer, "solana-shreds-full-ams", 2, false)).unwrap();
        assert!(
            output.contains("Set max multicast users from 3 to 6"),
            "{output}"
        );
    }

    #[test]
    fn test_cli_accesspass_add_feed_refuses_a_seat_that_ended_or_a_deleted_feed() {
        let user_payer = Pubkey::new_unique();
        let accesspass_pk = Pubkey::new_unique();
        let (ams_pk, lon_pk) = (Pubkey::new_unique(), Pubkey::new_unique());

        let mut client = create_test_client();
        let ended = FeedSeat {
            window_end: 1_700_000_000,
            terminates_at: 1_700_000_000,
            ..seat(lon_pk, 3)
        };
        expect_pass_and_feeds(
            &mut client,
            user_payer,
            accesspass_pk,
            pass(user_payer, AccessPassType::EdgeSeat(vec![ended]), 3),
            HashMap::from([
                (ams_pk, feed("solana-shreds-full-ams")),
                (lon_pk, feed("solana-shreds-full-lon")),
            ]),
        );
        let err = run(&client, add(user_payer, "solana-shreds-full-ams", 2, true)).unwrap_err();
        assert_eq!(
            err.to_string(),
            format!("the seat for feed {lon_pk} on access pass {accesspass_pk} ended at 2023-11-14T22:13:20+00:00. Remove it first with `doublezero access-pass remove-feed --feed {lon_pk}`")
        );

        let mut client = create_test_client();
        expect_pass_and_feeds(
            &mut client,
            user_payer,
            accesspass_pk,
            pass(
                user_payer,
                AccessPassType::EdgeSeat(vec![seat(lon_pk, 3)]),
                3,
            ),
            HashMap::from([(ams_pk, feed("solana-shreds-full-ams"))]),
        );
        let err = run(&client, add(user_payer, "solana-shreds-full-ams", 2, true)).unwrap_err();
        assert_eq!(
            err.to_string(),
            format!("the seat for feed {lon_pk} on access pass {accesspass_pk} names a feed that no longer exists. Remove it first with `doublezero access-pass remove-feed --feed {lon_pk}`")
        );
    }

    #[test]
    fn test_cli_accesspass_add_feed_refuses_a_feed_that_is_not_active_or_a_cap_below_its_users() {
        let user_payer = Pubkey::new_unique();
        let accesspass_pk = Pubkey::new_unique();
        let ams_pk = Pubkey::new_unique();

        let mut client = create_test_client();
        expect_pass_and_feeds(
            &mut client,
            user_payer,
            accesspass_pk,
            pass(user_payer, AccessPassType::EdgeSeat(vec![]), 0),
            HashMap::from([(
                ams_pk,
                Feed {
                    status: FeedStatus::Retiring,
                    ..feed("solana-shreds-full-ams")
                },
            )]),
        );
        let err = run(&client, add(user_payer, "solana-shreds-full-ams", 2, true)).unwrap_err();
        assert_eq!(
            err.to_string(),
            format!(
                "feed solana-shreds-full-ams is {}, so it takes no new seat",
                FeedStatus::Retiring
            )
        );

        let mut client = create_test_client();
        let held = FeedSeat {
            current_users: 3,
            ..seat(ams_pk, 4)
        };
        expect_pass_and_feeds(
            &mut client,
            user_payer,
            accesspass_pk,
            pass(user_payer, AccessPassType::EdgeSeat(vec![held]), 4),
            HashMap::from([(ams_pk, feed("solana-shreds-full-ams"))]),
        );
        let err = run(&client, add(user_payer, "solana-shreds-full-ams", 2, true)).unwrap_err();
        assert_eq!(
            err.to_string(),
            format!("feed solana-shreds-full-ams has 3 user(s) on access pass {accesspass_pk}, so --max-users must be at least 3")
        );
    }

    #[test]
    fn test_cli_accesspass_add_feed_refuses_a_cap_below_the_public_users() {
        let user_payer = Pubkey::new_unique();
        let accesspass_pk = Pubkey::new_unique();
        let ams_pk = Pubkey::new_unique();

        let mut client = create_test_client();
        let crowded = AccessPass {
            public_multicast_user_count: 4,
            ..pass(user_payer, AccessPassType::EdgeSeat(vec![]), 5)
        };
        expect_pass_and_feeds(
            &mut client,
            user_payer,
            accesspass_pk,
            crowded,
            HashMap::from([(ams_pk, feed("solana-shreds-full-ams"))]),
        );

        let err = run(&client, add(user_payer, "solana-shreds-full-ams", 2, true)).unwrap_err();

        assert_eq!(
            err.to_string(),
            format!("access pass {accesspass_pk} has 4 public multicast users, which is more than the new cap of 2. Reduce the users through the client portal first")
        );
    }

    #[test]
    fn test_cli_accesspass_add_feed_fails_when_the_answer_is_not_y() {
        let user_payer = Pubkey::new_unique();
        let accesspass_pk = Pubkey::new_unique();
        let ams_pk = Pubkey::new_unique();

        let mut client = create_test_client();
        expect_pass_and_feeds(
            &mut client,
            user_payer,
            accesspass_pk,
            pass(user_payer, AccessPassType::EdgeSeat(vec![]), 0),
            HashMap::from([(ams_pk, feed("solana-shreds-full-ams"))]),
        );
        client.expect_set_accesspass_feeds().never();
        client.expect_set_accesspass().never();

        let mut output = Vec::new();
        let err = block_on(add(user_payer, "solana-shreds-full-ams", 2, false).execute(
            &cli_context_default_for_tests(),
            &client,
            &mut output,
            &mut "n\n".as_bytes(),
        ))
        .unwrap_err();

        assert_eq!(
            err.to_string(),
            "the answer was not y, so nothing was changed"
        );
    }
}
