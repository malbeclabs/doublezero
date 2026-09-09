use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand,
        multicastgroup::subscribe_feed::MAX_FEED_TX_ACCOUNTS, user::get::GetUserCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    pda::get_accesspass_pda,
    state::{
        accesspass::{AccessPass, AccessPassType},
        accountdata::AccountData,
        feed::Feed,
        user::UserType,
    },
};
use doublezero_serviceability_instruction::multicastgroup::unsubscribe_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

/// Leave whole feeds on an EdgeSeat access pass with one `UnsubscribeFeed` transaction.
///
/// The caller names only the feeds to leave. The program additionally demands every other held
/// feed still provisioned on the pass (as `retained`, so a group two feeds share is kept) and
/// exactly the departing group list; both are derived here. A held feed the pass has dropped is
/// left out of `retained`: the program prunes those itself and rejects them as retained.
#[derive(Debug, PartialEq, Clone)]
pub struct UnsubscribeFeedCommand {
    pub user_pk: Pubkey,
    pub feed_pks: Vec<Pubkey>,
}

impl UnsubscribeFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        if self.feed_pks.is_empty() {
            eyre::bail!("no feeds given");
        }

        let (_, user) = GetUserCommand {
            pubkey: self.user_pk,
        }
        .execute(client)
        .map_err(|err| eyre::eyre!("failed to fetch user {}: {err}", self.user_pk))?;
        if user.user_type != UserType::Multicast {
            eyre::bail!(
                "user {} is a {} user; only a Multicast user holds feeds",
                self.user_pk,
                user.user_type
            );
        }
        let mut seen: Vec<Pubkey> = Vec::with_capacity(self.feed_pks.len());
        for feed_pk in &self.feed_pks {
            if seen.contains(feed_pk) {
                eyre::bail!("feed {} given more than once", feed_pk);
            }
            seen.push(*feed_pk);
            if !user.feed_pks.contains(feed_pk) {
                eyre::bail!("user does not hold feed {}", feed_pk);
            }
        }

        // The user's own IP, not a caller-supplied one: the pass lookup must match the pass the
        // user was created against.
        let (accesspass_pubkey, accesspass) = GetAccessPassCommand {
            client_ip: user.client_ip,
            user_payer: user.owner,
        }
        .execute(client)?
        .ok_or_else(|| eyre::eyre!("AccessPass not found"))?;
        if !matches!(accesspass.accesspass_type, AccessPassType::EdgeSeat(_)) {
            eyre::bail!(
                "the access pass is {}; only an EdgeSeat pass carries feeds",
                accesspass.accesspass_type
            );
        }
        // A resolved pass seating none of the held feeds is either the wrong pass (a second pass
        // exists for the same payer, and sending would make the program prune every held feed as
        // stale while the real pass keeps its seats charged) or every held feed has been
        // de-provisioned, which is a legitimate cleanup the program handles by pruning. Only the
        // sibling PDA tells the two apart.
        let seats_any_held = |pass: &AccessPass| {
            user.feed_pks
                .iter()
                .any(|held| pass.feed_seats().iter().any(|seat| seat.feed_key == *held))
        };
        if !seats_any_held(&accesspass) {
            let program_id = client.get_program_id();
            let (dynamic_pk, _) =
                get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &user.owner);
            let (exact_pk, _) = get_accesspass_pda(&program_id, &user.client_ip, &user.owner);
            let sibling_pk = if accesspass_pubkey == dynamic_pk {
                exact_pk
            } else {
                dynamic_pk
            };
            if let Ok(AccountData::AccessPass(sibling)) = client.get(sibling_pk) {
                if seats_any_held(&sibling) {
                    eyre::bail!(
                        "access pass {} seats the feeds user {} holds, but the lookup resolved {}; refusing to leave against the wrong pass",
                        sibling_pk,
                        self.user_pk,
                        accesspass_pubkey
                    );
                }
            }
        }

        let retained_pks: Vec<Pubkey> = user
            .feed_pks
            .iter()
            .filter(|held| !self.feed_pks.contains(held))
            .filter(|held| {
                accesspass
                    .feed_seats()
                    .iter()
                    .any(|seat| seat.feed_key == **held)
            })
            .copied()
            .collect();

        let read_feed = |feed_pk: &Pubkey| -> eyre::Result<Feed> {
            match client.get(*feed_pk)? {
                AccountData::Feed(feed) => Ok(feed),
                _ => eyre::bail!("account {} is not a Feed", feed_pk),
            }
        };
        let mut targets: Vec<Feed> = Vec::with_capacity(self.feed_pks.len());
        for feed_pk in &self.feed_pks {
            targets.push(read_feed(feed_pk)?);
        }
        let mut retained: Vec<Feed> = Vec::with_capacity(retained_pks.len());
        for feed_pk in &retained_pks {
            retained.push(read_feed(feed_pk)?);
        }

        // Exactly the groups this call drops, mirroring the processor: subscribed, carried by a
        // departing feed, and not covered by any retained feed.
        let mut groups: Vec<Pubkey> = Vec::new();
        for feed in &targets {
            for group in &feed.groups {
                let still_covered = retained.iter().any(|r| r.groups.contains(group));
                if user.subscribers.contains(group) && !still_covered && !groups.contains(group) {
                    groups.push(*group);
                }
            }
        }

        if self.feed_pks.len() + retained_pks.len() + groups.len() <= MAX_FEED_TX_ACCOUNTS {
            return client.send_transaction(unsubscribe_feed(
                &client.get_program_id(),
                &client.get_payer(),
                &accesspass_pubkey,
                &self.user_pk,
                &user.device_pk,
                &self.feed_pks,
                &retained_pks,
                &groups,
            ));
        }

        // Too many accounts for one transaction: leave one feed per transaction. A target not yet
        // left counts as retained in the meantime (only on-pass feeds can be named as retained),
        // so a group two departing feeds share stays covered until the last one carrying it
        // leaves; the end state matches the single call.
        let mut subscribers = user.subscribers.clone();
        let mut signature = Signature::default();
        for index in 0..self.feed_pks.len() {
            let target_pk = self.feed_pks[index];
            let target_feed = &targets[index];

            let not_yet_left: Vec<usize> = (index + 1..self.feed_pks.len())
                .filter(|later| {
                    accesspass
                        .feed_seats()
                        .iter()
                        .any(|seat| seat.feed_key == self.feed_pks[*later])
                })
                .collect();
            let tx_retained_pks: Vec<Pubkey> = not_yet_left
                .iter()
                .map(|later| self.feed_pks[*later])
                .chain(retained_pks.iter().copied())
                .collect();

            let tx_groups: Vec<Pubkey> = target_feed
                .groups
                .iter()
                .filter(|group| {
                    subscribers.contains(group)
                        && !not_yet_left
                            .iter()
                            .any(|later| targets[*later].groups.contains(group))
                        && !retained.iter().any(|r| r.groups.contains(group))
                })
                .copied()
                .collect();

            let result = client.send_transaction(unsubscribe_feed(
                &client.get_program_id(),
                &client.get_payer(),
                &accesspass_pubkey,
                &self.user_pk,
                &user.device_pk,
                &[target_pk],
                &tx_retained_pks,
                &tx_groups,
            ));
            // A failure mid-sequence leaves earlier targets already left; name them so the
            // operator knows to rerun with only the remaining feeds.
            signature = if index == 0 {
                result?
            } else {
                result.map_err(|err| {
                    let applied: Vec<String> = self.feed_pks[..index]
                        .iter()
                        .map(|pk| pk.to_string())
                        .collect();
                    eyre::eyre!(
                        "feeds {} already left; leaving {target_pk} failed: {err}; rerun with only the remaining feeds",
                        applied.join(", ")
                    )
                })?
            };
            subscribers.retain(|group| !tx_groups.contains(group));
        }
        Ok(signature)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_serviceability::{
        pda::get_accesspass_pda,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType, FeedSeat},
            accountdata::AccountData,
            accounttype::AccountType,
            feed::Feed,
            user::{User, UserStatus, UserType},
        },
    };
    use mockall::predicate;
    use std::net::Ipv4Addr;

    struct Fixture {
        user_pk: Pubkey,
        device_pk: Pubkey,
        accesspass_pk: Pubkey,
    }

    /// Mock a Multicast user holding `held_feeds` and subscribing `subscribers`, a dynamic
    /// EdgeSeat pass whose seats are `seated_feeds`, and one Feed account per `feeds` entry.
    fn setup(
        client: &mut crate::MockDoubleZeroClient,
        subscribers: Vec<Pubkey>,
        held_feeds: Vec<Pubkey>,
        seated_feeds: Vec<Pubkey>,
        feeds: Vec<(Pubkey, Feed)>,
    ) -> Fixture {
        let payer = client.get_payer();
        let program_id = client.get_program_id();
        let client_ip = Ipv4Addr::new(100, 0, 0, 1);
        let user_pk = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();

        let user = User {
            account_type: AccountType::User,
            owner: payer,
            user_type: UserType::Multicast,
            device_pk,
            client_ip,
            status: UserStatus::Activated,
            subscribers,
            feed_pks: held_feeds,
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(user_pk))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        let (accesspass_pk, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            accesspass_type: AccessPassType::EdgeSeat(
                seated_feeds
                    .iter()
                    .map(|pk| FeedSeat {
                        feed_key: *pk,
                        max_users: 2,
                        current_users: 1,
                        ..Default::default()
                    })
                    .collect(),
            ),
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: payer,
            owner: payer,
            status: AccessPassStatus::Connected,
            last_access_epoch: u64::MAX,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            bump_seed: 0,
            connection_count: 1,
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 0,
            multicast_user_count: 1,
            max_multicast_users: 4,
        };
        client
            .expect_get()
            .with(predicate::eq(accesspass_pk))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        for (feed_pk, feed) in feeds {
            client
                .expect_get()
                .with(predicate::eq(feed_pk))
                .returning(move |_| Ok(AccountData::Feed(feed.clone())));
        }

        Fixture {
            user_pk,
            device_pk,
            accesspass_pk,
        }
    }

    fn feed_with(code: &str, groups: Vec<Pubkey>) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: code.to_string(),
            name: code.to_string(),
            exchange: Pubkey::new_unique(),
            groups,
            ..Default::default()
        }
    }

    #[test]
    fn test_commands_unsubscribe_feed_keeps_group_a_retained_feed_covers() {
        let mut client = create_test_client();
        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let (g0, g1) = (Pubkey::new_unique(), Pubkey::new_unique());
        let (feed1_pk, feed2_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        // feed1 carries g0; feed2 carries g0 and g1. Leaving feed2 keeps g0 via retained feed1.
        let f = setup(
            &mut client,
            vec![g0, g1],
            vec![feed1_pk, feed2_pk],
            vec![feed1_pk, feed2_pk],
            vec![
                (feed1_pk, feed_with("f1", vec![g0])),
                (feed2_pk, feed_with("f2", vec![g0, g1])),
            ],
        );

        let expected = unsubscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[feed2_pk],
            &[feed1_pk],
            &[g1],
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        UnsubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![feed2_pk],
        }
        .execute(&client)
        .unwrap();
    }

    #[test]
    fn test_commands_unsubscribe_feed_last_feed_drops_all_groups() {
        let mut client = create_test_client();
        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let (g0, g1) = (Pubkey::new_unique(), Pubkey::new_unique());
        let feed_pk = Pubkey::new_unique();
        let f = setup(
            &mut client,
            vec![g0, g1],
            vec![feed_pk],
            vec![feed_pk],
            vec![(feed_pk, feed_with("only", vec![g0, g1]))],
        );

        let expected = unsubscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[feed_pk],
            &[],
            &[g0, g1],
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        UnsubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![feed_pk],
        }
        .execute(&client)
        .unwrap();
    }

    #[test]
    fn test_commands_unsubscribe_feed_splits_over_the_transaction_limit() {
        // Leaving two 13-group feeds while a third is retained is 29 combined accounts, so the
        // command leaves one feed per transaction: the not-yet-left target counts as retained in
        // the first, and the second's retained list shrinks to the kept feed.
        let mut client = create_test_client();
        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let groups1: Vec<Pubkey> = (0..13).map(|_| Pubkey::new_unique()).collect();
        let groups2: Vec<Pubkey> = (0..13).map(|_| Pubkey::new_unique()).collect();
        let g_kept = Pubkey::new_unique();
        let (feed1_pk, feed2_pk, kept_pk) = (
            Pubkey::new_unique(),
            Pubkey::new_unique(),
            Pubkey::new_unique(),
        );
        let all_subscribed: Vec<Pubkey> = groups1
            .iter()
            .chain(groups2.iter())
            .copied()
            .chain(std::iter::once(g_kept))
            .collect();
        let f = setup(
            &mut client,
            all_subscribed,
            vec![feed1_pk, feed2_pk, kept_pk],
            vec![feed1_pk, feed2_pk, kept_pk],
            vec![
                (feed1_pk, feed_with("f1", groups1.clone())),
                (feed2_pk, feed_with("f2", groups2.clone())),
                (kept_pk, feed_with("kept", vec![g_kept])),
            ],
        );

        let expected1 = unsubscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[feed1_pk],
            &[feed2_pk, kept_pk],
            &groups1,
        );
        let expected2 = unsubscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[feed2_pk],
            &[kept_pk],
            &groups2,
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected1))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));
        client
            .expect_send_transaction()
            .with(predicate::eq(expected2))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        UnsubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![feed1_pk, feed2_pk],
        }
        .execute(&client)
        .unwrap();
    }

    #[test]
    fn test_commands_unsubscribe_feed_duplicate_target_rejected() {
        let mut client = create_test_client();

        let feed_pk = Pubkey::new_unique();
        let f = setup(
            &mut client,
            vec![],
            vec![feed_pk],
            vec![feed_pk],
            vec![(feed_pk, feed_with("f1", vec![]))],
        );

        let err = UnsubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![feed_pk, feed_pk],
        }
        .execute(&client)
        .unwrap_err();
        assert_eq!(
            err.to_string(),
            format!("feed {feed_pk} given more than once")
        );
    }

    #[test]
    fn test_commands_unsubscribe_feed_wrong_pass_rejected() {
        // The resolved (dynamic) accesspass seats a different feed, while the sibling exact-IP
        // accesspass seats the held one: the lookup picked the wrong pass, so the command refuses rather
        // than let the program prune every held feed.
        let mut client = create_test_client();
        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let held_pk = Pubkey::new_unique();
        let other_pk = Pubkey::new_unique();
        let f = setup(
            &mut client,
            vec![],
            vec![held_pk],
            vec![other_pk],
            vec![(held_pk, feed_with("held", vec![]))],
        );

        let (exact_pk, _) = get_accesspass_pda(&program_id, &Ipv4Addr::new(100, 0, 0, 1), &payer);
        let sibling = AccessPass {
            account_type: AccountType::AccessPass,
            accesspass_type: AccessPassType::EdgeSeat(vec![FeedSeat {
                feed_key: held_pk,
                max_users: 2,
                current_users: 1,
                ..Default::default()
            }]),
            client_ip: Ipv4Addr::new(100, 0, 0, 1),
            user_payer: payer,
            owner: payer,
            status: AccessPassStatus::Connected,
            last_access_epoch: u64::MAX,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            bump_seed: 0,
            connection_count: 1,
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 0,
            multicast_user_count: 1,
            max_multicast_users: 4,
        };
        client
            .expect_get()
            .with(predicate::eq(exact_pk))
            .returning(move |_| Ok(AccountData::AccessPass(sibling.clone())));

        let err = UnsubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![held_pk],
        }
        .execute(&client)
        .unwrap_err();
        assert_eq!(
            err.to_string(),
            format!(
                "access pass {exact_pk} seats the feeds user {} holds, but the lookup resolved {}; refusing to leave against the wrong pass",
                f.user_pk, f.accesspass_pk
            )
        );
    }

    #[test]
    fn test_commands_unsubscribe_feed_all_stale_feeds_still_leavable() {
        // Every held feed was de-provisioned and no sibling pass seats them: the leave goes
        // through with nothing retained, and the program prunes the stale entries.
        let mut client = create_test_client();
        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let g0 = Pubkey::new_unique();
        let stale_pk = Pubkey::new_unique();
        let f = setup(
            &mut client,
            vec![g0],
            vec![stale_pk],
            vec![],
            vec![(stale_pk, feed_with("stale", vec![g0]))],
        );

        let (exact_pk, _) = get_accesspass_pda(&program_id, &Ipv4Addr::new(100, 0, 0, 1), &payer);
        client
            .expect_get()
            .with(predicate::eq(exact_pk))
            .returning(|_| Err(eyre::eyre!("account not found")));

        let expected = unsubscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[stale_pk],
            &[],
            &[g0],
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        UnsubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![stale_pk],
        }
        .execute(&client)
        .unwrap();
    }

    #[test]
    fn test_commands_unsubscribe_feed_not_held_rejected() {
        let mut client = create_test_client();

        let held_pk = Pubkey::new_unique();
        let unheld_pk = Pubkey::new_unique();
        let f = setup(
            &mut client,
            vec![],
            vec![held_pk],
            vec![held_pk],
            vec![(held_pk, feed_with("held", vec![]))],
        );

        // No send_transaction expectation: the command must fail before any transaction.
        let err = UnsubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![unheld_pk],
        }
        .execute(&client)
        .unwrap_err();
        assert_eq!(
            err.to_string(),
            format!("user does not hold feed {unheld_pk}")
        );
    }

    #[test]
    fn test_commands_unsubscribe_feed_stale_held_feed_left_out_of_retained() {
        let mut client = create_test_client();
        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let g0 = Pubkey::new_unique();
        let (live_pk, stale_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        // The user holds both feeds, but the pass only seats the live one: the stale feed must not
        // appear as retained (the program rejects that) and covers nothing.
        let f = setup(
            &mut client,
            vec![g0],
            vec![live_pk, stale_pk],
            vec![live_pk],
            vec![(live_pk, feed_with("live", vec![g0]))],
        );

        let expected = unsubscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[live_pk],
            &[],
            &[g0],
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        UnsubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![live_pk],
        }
        .execute(&client)
        .unwrap();
    }
}
