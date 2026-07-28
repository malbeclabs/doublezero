use std::net::Ipv4Addr;

use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, device::get::GetDeviceCommand,
        feed::get::GetFeedCommand, multicastgroup::get::GetMulticastGroupCommand,
        user::get::GetUserCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    processors::multicastgroup::subscribe::UpdateMulticastGroupRolesArgs,
    state::{accesspass::AccessPassType, multicastgroup::MulticastGroupStatus, user::UserType},
};
use doublezero_serviceability_instruction::multicastgroup::update_multicast_group_roles;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateMulticastGroupRolesCommand {
    pub group_pk: Pubkey,
    pub client_ip: Ipv4Addr,
    pub user_pk: Pubkey,
    pub publisher: bool,
    pub subscriber: bool,
    /// EdgeSeat feed metro gate. Left as `None`, this command resolves the device from
    /// `user.device_pk` and the feed from the pass's seats; set either to override.
    pub device_pk: Option<Pubkey>,
    pub feed_pk: Option<Pubkey>,
}

impl UpdateMulticastGroupRolesCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (_, mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.group_pk.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("MulticastGroup not found"))?;

        if mgroup.status != MulticastGroupStatus::Activated {
            eyre::bail!("MulticastGroup not active");
        }

        let (_, user) = GetUserCommand {
            pubkey: self.user_pk,
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("User not found"))?;

        // GetAccessPassCommand prefers a shared dynamic (UNSPECIFIED) pass and falls
        // back to the exact client-IP pass.
        let (accesspass_pubkey, accesspass) = GetAccessPassCommand {
            client_ip: self.client_ip,
            user_payer: user.owner,
        }
        .execute(client)?
        .ok_or_else(|| eyre::eyre!("AccessPass not found"))?;

        // Only a multicast user can hold multicast roles; the processor rejects an add otherwise.
        if (self.publisher || self.subscriber) && user.user_type != UserType::Multicast {
            eyre::bail!(
                "User {} is a {} user; multicast roles require a Multicast user",
                self.user_pk,
                user.user_type
            );
        }

        // An EdgeSeat pass bypasses the mgroup allowlists onchain (the feed metro gate below admits
        // it instead), so checking them here would reject every EdgeSeat role change.
        let is_edge_seat = matches!(accesspass.accesspass_type, AccessPassType::EdgeSeat(_));
        if !is_edge_seat {
            if self.publisher && !accesspass.mgroup_pub_allowlist.contains(&self.group_pk) {
                eyre::bail!("User not allowed to publish multicast group");
            }
            if self.subscriber && !accesspass.mgroup_sub_allowlist.contains(&self.group_pk) {
                eyre::bail!("User not allowed to subscribe multicast group");
            }
        }

        let metro_gate = if is_edge_seat {
            let device_pk = self.device_pk.unwrap_or(user.device_pk);
            let (_, device) = GetDeviceCommand {
                pubkey_or_code: device_pk.to_string(),
            }
            .execute(client)?;

            // Pick the pass's feed that serves the device's metro and carries the target group. A
            // role add needs one; a removal does not, since the group may already have been dropped
            // from its feed's group set.
            //
            // A feed the user already holds wins over an equally-eligible one it does not: the
            // processor skips the tick for a feed in `feed_pks`, so preferring the held feed keeps an
            // add free (and cannot fail `FeedSeatFull` against an unheld seat at capacity) and lets a
            // removal release the seat actually consumed.
            let feed_pk = match self.feed_pk {
                Some(feed_pk) => Some(feed_pk),
                None => {
                    let mut fallback = None;
                    let mut held = None;
                    for seat in accesspass.feed_seats() {
                        let (candidate_pk, feed) = GetFeedCommand {
                            pubkey_or_code: seat.feed_key.to_string(),
                            exchange: None,
                        }
                        .execute(client)?;
                        if feed.exchange != device.exchange_pk
                            || !feed.groups.contains(&self.group_pk)
                        {
                            continue;
                        }
                        if user.feed_pks.contains(&candidate_pk) {
                            held = Some(candidate_pk);
                            break;
                        }
                        fallback = fallback.or(Some(candidate_pk));
                    }
                    held.or(fallback)
                }
            };
            match feed_pk {
                Some(feed_pk) => Some((device_pk, feed_pk)),
                None if self.publisher || self.subscriber => eyre::bail!(
                    "No feed on the access pass serves device {} with multicast group {}",
                    device_pk,
                    self.group_pk
                ),
                None => None,
            }
        } else {
            None
        };

        client.send_transaction(update_multicast_group_roles(
            &client.get_program_id(),
            &client.get_payer(),
            &self.group_pk,
            &accesspass_pubkey,
            &self.user_pk,
            metro_gate.as_ref().map(|(device, feed)| (device, feed)),
            UpdateMulticastGroupRolesArgs {
                publisher: self.publisher,
                subscriber: self.subscriber,
                client_ip: user.client_ip,
                use_onchain_allocation: true,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::subscribe::UpdateMulticastGroupRolesCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        pda::{get_accesspass_pda, get_multicastgroup_pda},
        processors::multicastgroup::subscribe::UpdateMulticastGroupRolesArgs,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType, FeedSeat},
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            feed::Feed,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use doublezero_serviceability_instruction::multicastgroup::update_multicast_group_roles;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_commands_multicastgroup_subscribe_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (mgroup_pubkey, _bump_seed) = get_multicastgroup_pda(&program_id, 1);
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: payer,
            bump_seed: 0,
            index: 1,
            code: "test".to_string(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            tenant_pk: Pubkey::default(),
            multicast_ip: "223.0.0.1".parse().unwrap(),
            publisher_count: 0,
            subscriber_count: 0,
        };

        client
            .expect_get()
            .with(predicate::eq(mgroup_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup.clone())));

        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: payer,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk: mgroup_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            ..Default::default()
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user.client_ip, &payer);
        let accesspass = doublezero_serviceability::state::accesspass::AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: doublezero_serviceability::state::accesspass::AccessPassType::Prepaid,
            client_ip: user.client_ip,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: doublezero_serviceability::state::accesspass::AccessPassStatus::Requested,
            owner: payer,
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        // First call in UpdateMulticastGroupRolesCommand::execute tries the dynamic (UNSPECIFIED) PDA,
        // which should fail with a non-AccessPass to trigger the fallback to the fixed client_ip PDA.
        let (dynamic_accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let user_clone_for_dynamic = user.clone();
        client
            .expect_get()
            .with(predicate::eq(dynamic_accesspass_pubkey))
            .returning(move |_| Ok(AccountData::User(user_clone_for_dynamic.clone())));

        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        let expected = update_multicast_group_roles(
            &program_id,
            &payer,
            &mgroup_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
            None,
            UpdateMulticastGroupRolesArgs {
                client_ip,
                publisher: true,
                subscriber: false,
                use_onchain_allocation: true,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = UpdateMulticastGroupRolesCommand {
            group_pk: mgroup_pubkey,
            user_pk: user_pubkey,
            client_ip,
            publisher: true,
            subscriber: false,
            device_pk: None,
            feed_pk: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    /// An EdgeSeat pass is admitted by its feeds, not by the mgroup allowlists (both empty here), and
    /// the command resolves the metro-gate pair itself.
    #[test]
    fn test_commands_multicastgroup_subscribe_resolves_edge_seat_metro_gate() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, 1);
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: payer,
            bump_seed: 0,
            index: 1,
            code: "test".to_string(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            tenant_pk: Pubkey::default(),
            multicast_ip: "223.0.0.1".parse().unwrap(),
            publisher_count: 0,
            subscriber_count: 0,
        };

        let exchange_pubkey = Pubkey::new_unique();
        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            exchange_pk: exchange_pubkey,
            ..Default::default()
        };

        // The pass carries the group's feed in another metro first, so resolution has to skip it.
        let other_metro_feed_pubkey = Pubkey::new_unique();
        let other_metro_feed = Feed {
            account_type: AccountType::Feed,
            code: "shreds".to_string(),
            exchange: Pubkey::new_unique(),
            groups: vec![mgroup_pubkey],
            ..Default::default()
        };
        let feed_pubkey = Pubkey::new_unique();
        let feed = Feed {
            account_type: AccountType::Feed,
            code: "shreds".to_string(),
            exchange: exchange_pubkey,
            groups: vec![mgroup_pubkey],
            ..Default::default()
        };

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: payer,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            ..Default::default()
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user.client_ip, &payer);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::EdgeSeat(vec![
                FeedSeat {
                    feed_key: other_metro_feed_pubkey,
                    max_users: 2,
                    ..Default::default()
                },
                FeedSeat {
                    feed_key: feed_pubkey,
                    max_users: 2,
                    ..Default::default()
                },
            ]),
            client_ip: user.client_ip,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            owner: payer,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        client
            .expect_get()
            .with(predicate::eq(mgroup_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup.clone())));
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning({
                let user = user.clone();
                move |_| Ok(AccountData::User(user.clone()))
            });
        // The dynamic (UNSPECIFIED) pass must miss so the lookup falls back to the client-IP pass.
        let (dynamic_accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        client
            .expect_get()
            .with(predicate::eq(dynamic_accesspass_pubkey))
            .returning({
                let user = user.clone();
                move |_| Ok(AccountData::User(user.clone()))
            });
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));
        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device.clone())));
        client
            .expect_get()
            .with(predicate::eq(other_metro_feed_pubkey))
            .returning(move |_| Ok(AccountData::Feed(other_metro_feed.clone())));
        client
            .expect_get()
            .with(predicate::eq(feed_pubkey))
            .returning(move |_| Ok(AccountData::Feed(feed.clone())));

        let expected = update_multicast_group_roles(
            &program_id,
            &payer,
            &mgroup_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
            Some((&device_pubkey, &feed_pubkey)),
            UpdateMulticastGroupRolesArgs {
                client_ip,
                publisher: false,
                subscriber: true,
                use_onchain_allocation: true,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = UpdateMulticastGroupRolesCommand {
            group_pk: mgroup_pubkey,
            user_pk: user_pubkey,
            client_ip,
            publisher: false,
            subscriber: true,
            device_pk: None,
            feed_pk: None,
        }
        .execute(&client);

        assert!(res.is_ok(), "{:?}", res.unwrap_err());
    }

    /// With two equally-eligible feeds on the pass (same metro, both carrying the group), resolution
    /// picks the one the user already holds, even though the other is listed first. Picking the
    /// unheld feed would tick a second seat on an add and release nothing on a removal.
    #[test]
    fn test_commands_multicastgroup_subscribe_prefers_the_feed_the_user_holds() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, 1);
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: payer,
            bump_seed: 0,
            index: 1,
            code: "test".to_string(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            tenant_pk: Pubkey::default(),
            multicast_ip: "223.0.0.1".parse().unwrap(),
            publisher_count: 0,
            subscriber_count: 0,
        };

        let exchange_pubkey = Pubkey::new_unique();
        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            exchange_pk: exchange_pubkey,
            ..Default::default()
        };

        // Both feeds serve the device's metro and carry the group; only the second is held.
        let unheld_feed_pubkey = Pubkey::new_unique();
        let held_feed_pubkey = Pubkey::new_unique();
        let eligible_feed = Feed {
            account_type: AccountType::Feed,
            code: "shreds".to_string(),
            exchange: exchange_pubkey,
            groups: vec![mgroup_pubkey],
            ..Default::default()
        };

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: payer,
            user_type: UserType::Multicast,
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            feed_pks: vec![held_feed_pubkey],
            ..Default::default()
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user.client_ip, &payer);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::EdgeSeat(vec![
                FeedSeat {
                    feed_key: unheld_feed_pubkey,
                    max_users: 2,
                    ..Default::default()
                },
                FeedSeat {
                    feed_key: held_feed_pubkey,
                    max_users: 2,
                    current_users: 1,
                    ..Default::default()
                },
            ]),
            client_ip: user.client_ip,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            owner: payer,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 1,
            max_multicast_users: 1,
        };

        client
            .expect_get()
            .with(predicate::eq(mgroup_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup.clone())));
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning({
                let user = user.clone();
                move |_| Ok(AccountData::User(user.clone()))
            });
        let (dynamic_accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        client
            .expect_get()
            .with(predicate::eq(dynamic_accesspass_pubkey))
            .returning({
                let user = user.clone();
                move |_| Ok(AccountData::User(user.clone()))
            });
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));
        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device.clone())));
        client
            .expect_get()
            .with(predicate::eq(unheld_feed_pubkey))
            .returning({
                let eligible_feed = eligible_feed.clone();
                move |_| Ok(AccountData::Feed(eligible_feed.clone()))
            });
        client
            .expect_get()
            .with(predicate::eq(held_feed_pubkey))
            .returning(move |_| Ok(AccountData::Feed(eligible_feed.clone())));

        // The held feed is the one that reaches the instruction, not the first eligible seat.
        let expected = update_multicast_group_roles(
            &program_id,
            &payer,
            &mgroup_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
            Some((&device_pubkey, &held_feed_pubkey)),
            UpdateMulticastGroupRolesArgs {
                client_ip,
                publisher: false,
                subscriber: true,
                use_onchain_allocation: true,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        let res = UpdateMulticastGroupRolesCommand {
            group_pk: mgroup_pubkey,
            user_pk: user_pubkey,
            client_ip,
            publisher: false,
            subscriber: true,
            device_pk: None,
            feed_pk: None,
        }
        .execute(&client);

        assert!(res.is_ok(), "{:?}", res.unwrap_err());
    }
}
