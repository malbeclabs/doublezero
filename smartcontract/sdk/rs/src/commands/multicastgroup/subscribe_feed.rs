use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, device::get::GetDeviceCommand,
        user::get::GetUserCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    processors::multicastgroup::subscribe_feed::MAX_USER_FEEDS,
    state::{
        accesspass::AccessPassType,
        accountdata::AccountData,
        feed::Feed,
        user::{UserStatus, UserType},
    },
};
use doublezero_serviceability_instruction::multicastgroup::subscribe_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

/// A legacy transaction behind `send_transaction`'s compute-budget prelude fits 25 variable
/// feed+group accounts.
pub(crate) const MAX_FEED_TX_ACCOUNTS: usize = 25;

/// Join whole feeds on an EdgeSeat access pass.
///
/// The group list the program demands (exactly the groups these feeds add) is derived here, so a
/// retry against a user that already holds some of the feeds sends the right list and succeeds.
/// A join too large for one transaction is split into several, each carrying whole feeds.
#[derive(Debug, PartialEq, Clone)]
pub struct SubscribeFeedCommand {
    pub user_pk: Pubkey,
    pub feed_pks: Vec<Pubkey>,
}

impl SubscribeFeedCommand {
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
                "user {} is a {} user; only a Multicast user can join feeds",
                self.user_pk,
                user.user_type
            );
        }
        if user.status != UserStatus::Activated {
            eyre::bail!("user {} is {}, not Activated", self.user_pk, user.status);
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

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: user.device_pk.to_string(),
        }
        .execute(client)
        .map_err(|err| eyre::eyre!("failed to fetch device {}: {err}", user.device_pk))?;

        let mut feeds: Vec<(Pubkey, Feed)> = Vec::with_capacity(self.feed_pks.len());
        for feed_pk in &self.feed_pks {
            if feeds.iter().any(|(pk, _)| pk == feed_pk) {
                eyre::bail!("feed {} given more than once", feed_pk);
            }
            let feed = match client.get(*feed_pk)? {
                AccountData::Feed(feed) => feed,
                _ => eyre::bail!("account {} is not a Feed", feed_pk),
            };
            if !accesspass
                .feed_seats()
                .iter()
                .any(|seat| seat.feed_key == *feed_pk)
            {
                eyre::bail!(
                    "feed {} ({}) is not provisioned on the access pass",
                    feed.code,
                    feed_pk
                );
            }
            if feed.exchange != device.exchange_pk {
                eyre::bail!(
                    "feed {} serves exchange {}, but the user's device {} is in exchange {}",
                    feed.code,
                    feed.exchange,
                    device.code,
                    device.exchange_pk
                );
            }
            feeds.push((*feed_pk, feed));
        }

        let new_feeds = feeds
            .iter()
            .filter(|(pk, _)| !user.feed_pks.contains(pk))
            .count();
        if user.feed_pks.len() + new_feeds > MAX_USER_FEEDS {
            eyre::bail!(
                "user holds {} feeds and this join adds {}; a user may hold at most {}",
                user.feed_pks.len(),
                new_feeds,
                MAX_USER_FEEDS
            );
        }

        // Exactly the groups each transaction adds, mirroring the processor's derivation.
        let mut subscribed: Vec<Pubkey> = user.subscribers.clone();
        let mut chunks: Vec<(Vec<Pubkey>, Vec<Pubkey>)> = Vec::new();
        let mut chunk_feeds: Vec<Pubkey> = Vec::new();
        let mut chunk_groups: Vec<Pubkey> = Vec::new();
        for (feed_pk, feed) in &feeds {
            let new_groups: Vec<Pubkey> = feed
                .groups
                .iter()
                .filter(|group| !subscribed.contains(group) && !chunk_groups.contains(group))
                .copied()
                .collect();
            if !chunk_feeds.is_empty()
                && chunk_feeds.len() + 1 + chunk_groups.len() + new_groups.len()
                    > MAX_FEED_TX_ACCOUNTS
            {
                subscribed.extend(chunk_groups.iter().copied());
                chunks.push((
                    std::mem::take(&mut chunk_feeds),
                    std::mem::take(&mut chunk_groups),
                ));
            }
            chunk_feeds.push(*feed_pk);
            chunk_groups.extend(new_groups);
        }
        chunks.push((chunk_feeds, chunk_groups));

        let mut signature = Signature::default();
        for (index, (chunk_feed_pks, chunk_group_pks)) in chunks.iter().enumerate() {
            let result = client.send_transaction(subscribe_feed(
                &client.get_program_id(),
                &client.get_payer(),
                &accesspass_pubkey,
                &self.user_pk,
                &user.device_pk,
                chunk_feed_pks,
                chunk_group_pks,
            ));
            // A failure mid-sequence leaves earlier chunks applied; name them so the operator
            // knows where the join stands. The join is retry-safe, so rerunning finishes it.
            signature = if index == 0 {
                result?
            } else {
                result.map_err(|err| {
                    let applied: Vec<String> = chunks[..index]
                        .iter()
                        .flat_map(|(feed_pks, _)| feed_pks)
                        .map(|pk| pk.to_string())
                        .collect();
                    let failing: Vec<String> =
                        chunk_feed_pks.iter().map(|pk| pk.to_string()).collect();
                    eyre::eyre!(
                        "feeds {} already joined; joining {} failed: {err}; rerunning the command finishes the rest",
                        applied.join(", "),
                        failing.join(", ")
                    )
                })?
            };
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
            device::Device,
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
        exchange_pk: Pubkey,
    }

    /// Mock an activated Multicast user on `device_pk`, a dynamic EdgeSeat pass seating `feeds`,
    /// and one Feed account per entry.
    fn setup(
        client: &mut crate::MockDoubleZeroClient,
        subscribers: Vec<Pubkey>,
        held_feeds: Vec<Pubkey>,
        feeds: Vec<(Pubkey, Feed)>,
    ) -> Fixture {
        let payer = client.get_payer();
        let program_id = client.get_program_id();
        let client_ip = Ipv4Addr::new(100, 0, 0, 1);
        let user_pk = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let exchange_pk = feeds
            .first()
            .map(|(_, feed)| feed.exchange)
            .unwrap_or_else(Pubkey::new_unique);

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

        let device = Device {
            account_type: AccountType::Device,
            code: "dz1".to_string(),
            exchange_pk,
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        let (accesspass_pk, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            accesspass_type: AccessPassType::EdgeSeat(
                feeds
                    .iter()
                    .map(|(pk, _)| FeedSeat {
                        feed_key: *pk,
                        max_users: 2,
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
            exchange_pk,
        }
    }

    fn feed_with(code: &str, exchange: Pubkey, groups: Vec<Pubkey>) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: code.to_string(),
            name: code.to_string(),
            exchange,
            groups,
            ..Default::default()
        }
    }

    #[test]
    fn test_commands_subscribe_feed_expands_to_new_groups() {
        let mut client = create_test_client();
        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let exchange = Pubkey::new_unique();
        let (g0, g1) = (Pubkey::new_unique(), Pubkey::new_unique());
        let feed_pk = Pubkey::new_unique();
        // The user already subscribes g0, so only g1 is in the derived group list.
        let f = setup(
            &mut client,
            vec![g0],
            vec![],
            vec![(feed_pk, feed_with("shreds", exchange, vec![g0, g1]))],
        );

        let expected = subscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[feed_pk],
            &[g1],
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        SubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![feed_pk],
        }
        .execute(&client)
        .unwrap();
    }

    #[test]
    fn test_commands_subscribe_feed_shared_group_joined_once() {
        let mut client = create_test_client();
        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let exchange = Pubkey::new_unique();
        let (g0, g1, g2) = (
            Pubkey::new_unique(),
            Pubkey::new_unique(),
            Pubkey::new_unique(),
        );
        let (feed1_pk, feed2_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        let f = setup(
            &mut client,
            vec![],
            vec![],
            vec![
                (feed1_pk, feed_with("f1", exchange, vec![g0, g1])),
                (feed2_pk, feed_with("f2", exchange, vec![g0, g2])),
            ],
        );

        let expected = subscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[feed1_pk, feed2_pk],
            &[g0, g1, g2],
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        SubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![feed1_pk, feed2_pk],
        }
        .execute(&client)
        .unwrap();
    }

    #[test]
    fn test_commands_subscribe_feed_already_held_feed_is_a_noop_retry() {
        // The user already holds every feed at the cap, one of them being the request: the retry
        // adds nothing to the count (no cap error) and sends only the groups not yet subscribed.
        let mut client = create_test_client();
        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let exchange = Pubkey::new_unique();
        let (g0, g1) = (Pubkey::new_unique(), Pubkey::new_unique());
        let feed_pk = Pubkey::new_unique();
        let mut held: Vec<Pubkey> = (0..MAX_USER_FEEDS - 1)
            .map(|_| Pubkey::new_unique())
            .collect();
        held.push(feed_pk);
        let f = setup(
            &mut client,
            vec![g0],
            held,
            vec![(feed_pk, feed_with("held", exchange, vec![g0, g1]))],
        );

        let expected = subscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[feed_pk],
            &[g1],
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        SubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![feed_pk],
        }
        .execute(&client)
        .unwrap();
    }

    #[test]
    fn test_commands_subscribe_feed_splits_over_the_transaction_limit() {
        // Two 14-group feeds cannot share one transaction (2 feeds + 28 groups), so each goes in
        // its own, carrying exactly its groups.
        let mut client = create_test_client();
        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let exchange = Pubkey::new_unique();
        let groups1: Vec<Pubkey> = (0..14).map(|_| Pubkey::new_unique()).collect();
        let groups2: Vec<Pubkey> = (0..14).map(|_| Pubkey::new_unique()).collect();
        let (feed1_pk, feed2_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        let f = setup(
            &mut client,
            vec![],
            vec![],
            vec![
                (feed1_pk, feed_with("f1", exchange, groups1.clone())),
                (feed2_pk, feed_with("f2", exchange, groups2.clone())),
            ],
        );

        let expected1 = subscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[feed1_pk],
            &groups1,
        );
        let expected2 = subscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[feed2_pk],
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

        SubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![feed1_pk, feed2_pk],
        }
        .execute(&client)
        .unwrap();
    }

    #[test]
    fn test_commands_subscribe_feed_split_failure_names_the_applied_chunk() {
        let mut client = create_test_client();
        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let exchange = Pubkey::new_unique();
        let groups1: Vec<Pubkey> = (0..14).map(|_| Pubkey::new_unique()).collect();
        let groups2: Vec<Pubkey> = (0..14).map(|_| Pubkey::new_unique()).collect();
        let (feed1_pk, feed2_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        let f = setup(
            &mut client,
            vec![],
            vec![],
            vec![
                (feed1_pk, feed_with("f1", exchange, groups1.clone())),
                (feed2_pk, feed_with("f2", exchange, groups2.clone())),
            ],
        );

        let expected1 = subscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[feed1_pk],
            &groups1,
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected1))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));
        let expected2 = subscribe_feed(
            &program_id,
            &payer,
            &f.accesspass_pk,
            &f.user_pk,
            &f.device_pk,
            &[feed2_pk],
            &groups2,
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected2))
            .times(1)
            .returning(|_| Err(eyre::eyre!("blockhash expired")));

        let err = SubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![feed1_pk, feed2_pk],
        }
        .execute(&client)
        .unwrap_err();
        assert_eq!(
            err.to_string(),
            format!(
                "feeds {feed1_pk} already joined; joining {feed2_pk} failed: blockhash expired; rerunning the command finishes the rest"
            )
        );
    }

    #[test]
    fn test_commands_subscribe_feed_wrong_metro_rejected() {
        let mut client = create_test_client();

        let exchange = Pubkey::new_unique();
        let other_exchange = Pubkey::new_unique();
        let (feed1_pk, feed2_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        // The device's exchange comes from the first feed; the second serves another metro.
        let f = setup(
            &mut client,
            vec![],
            vec![],
            vec![
                (
                    feed1_pk,
                    feed_with("home", exchange, vec![Pubkey::new_unique()]),
                ),
                (
                    feed2_pk,
                    feed_with("away", other_exchange, vec![Pubkey::new_unique()]),
                ),
            ],
        );
        assert_eq!(f.exchange_pk, exchange);

        // No send_transaction expectation: the command must fail before any transaction.
        let err = SubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![feed1_pk, feed2_pk],
        }
        .execute(&client)
        .unwrap_err();
        assert_eq!(
            err.to_string(),
            format!(
                "feed away serves exchange {other_exchange}, but the user's device dz1 is in exchange {exchange}"
            )
        );
    }

    #[test]
    fn test_commands_subscribe_feed_past_user_cap_rejected() {
        let mut client = create_test_client();

        let exchange = Pubkey::new_unique();
        let feed_pk = Pubkey::new_unique();
        let held: Vec<Pubkey> = (0..MAX_USER_FEEDS).map(|_| Pubkey::new_unique()).collect();
        let f = setup(
            &mut client,
            vec![],
            held,
            vec![(
                feed_pk,
                feed_with("one-too-many", exchange, vec![Pubkey::new_unique()]),
            )],
        );

        let err = SubscribeFeedCommand {
            user_pk: f.user_pk,
            feed_pks: vec![feed_pk],
        }
        .execute(&client)
        .unwrap_err();
        assert_eq!(
            err.to_string(),
            format!(
                "user holds {MAX_USER_FEEDS} feeds and this join adds 1; a user may hold at most {MAX_USER_FEEDS}"
            )
        );
    }
}
