use std::collections::HashSet;

use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand,
        common::append_payer_permission_account,
        device::get::GetDeviceCommand,
        multicastgroup::{
            list::ListMulticastGroupCommand,
            subscribe::{UpdateMulticastGroupRolesCommand, MAX_GROUPS_PER_TRANSACTION},
        },
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    processors::user::delete::UserDeleteArgs, state::accesspass::AccessPassKind,
};
use doublezero_serviceability_instruction::user::delete_user;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

/// Deletes a user. `kind` names the kind of access pass the caller means to remove; the
/// program refuses the call when the stored pass is a different kind, so `kind` must carry
/// the caller's intent rather than a value read back from the pass.
#[derive(Debug, PartialEq, Clone)]
pub struct DeleteUserCommand {
    pub pubkey: Pubkey,
    pub kind: AccessPassKind,
}

impl DeleteUserCommand {
    pub fn new(pubkey: Pubkey, kind: AccessPassKind) -> Self {
        Self { pubkey, kind }
    }
}

impl DeleteUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let user = client
            .get(self.pubkey)
            .map_err(|_| eyre::eyre!("User not found ({})", self.pubkey))?
            .get_user()
            .map_err(|e| eyre::eyre!(e))?;

        let unique_mgroup_pks: Vec<Pubkey> = user
            .publishers
            .iter()
            .chain(user.subscribers.iter())
            .copied()
            .collect::<HashSet<_>>()
            .into_iter()
            .collect();
        let multicastgroups = ListMulticastGroupCommand {}.execute(client)?;
        // Strip every remaining multicast role, batched atomically per chunk (one
        // transaction each, bounded by the transaction size limit).
        let group_pks: Vec<Pubkey> = unique_mgroup_pks
            .into_iter()
            .filter(|pk| multicastgroups.contains_key(pk))
            .collect();
        for chunk in group_pks.chunks(MAX_GROUPS_PER_TRANSACTION) {
            UpdateMulticastGroupRolesCommand {
                group_pks: chunk.to_vec(),
                user_pk: self.pubkey,
                client_ip: user.client_ip,
                publisher: false,
                subscriber: false,
                device_pk: None,
                feed_pk: None,
            }
            .execute(client)?;
        }

        // GetAccessPassCommand prefers a shared dynamic (UNSPECIFIED) pass and falls
        // back to the exact client-IP pass.
        let (accesspass_pk, _) = GetAccessPassCommand {
            client_ip: user.client_ip,
            user_payer: user.owner,
        }
        .execute(client)?
        .ok_or_else(|| eyre::eyre!("You have no Access Pass"))?;

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: user.device_pk.to_string(),
        }
        .execute(client)
        .map_err(|_| eyre::eyre!("Device not found"))?;
        let dz_prefix_count = device.dz_prefixes.len();
        if dz_prefix_count == 0 {
            return Err(eyre::eyre!(
                "Device {} has no dz_prefixes; cannot delete user",
                user.device_pk
            ));
        }
        let dz_prefix_count_u8 = u8::try_from(dz_prefix_count).map_err(|_| {
            eyre::eyre!(
                "Device {} has {} dz_prefixes, exceeds u8::MAX",
                user.device_pk,
                dz_prefix_count
            )
        })?;

        // The builder derives globalstate + all resource-extension PDAs and the
        // dz_prefix block loop. The optional tenant account is appended only when
        // the user carries a non-default tenant. The on-chain DeleteUser releases
        // the EdgeSeat feed seat from the feed recorded on the User, so no trailing
        // Feed account is needed here.
        let tenant = (user.tenant_pk != Pubkey::default()).then_some(user.tenant_pk);
        let mut ix = delete_user(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            &accesspass_pk,
            &user.device_pk,
            dz_prefix_count_u8,
            tenant,
            &user.owner,
            self.kind,
            UserDeleteArgs {
                dz_prefix_count: dz_prefix_count_u8,
                multicast_publisher_count: 1,
            },
        );
        append_payer_permission_account(client, &mut ix)?;
        client.send_transaction(ix)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::delete::DeleteUserCommand,
        tests::utils::{create_test_client, expect_missing_permission_account},
        DoubleZeroClient, MockDoubleZeroClient,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        pda::{
            get_accesspass_pda, get_globalstate_pda, get_multicastgroup_pda, get_permission_pda,
        },
        processors::{
            multicastgroup::subscribe::UpdateMulticastGroupRolesArgs, user::delete::UserDeleteArgs,
        },
        state::{
            accesspass::{AccessPass, AccessPassKind, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            globalstate::GlobalState,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use doublezero_serviceability_instruction::{
        multicastgroup::update_multicast_group_roles, user::delete_user,
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::{
        account::Account, message::AccountMeta, pubkey::Pubkey, signature::Signature,
    };
    use std::net::Ipv4Addr;

    #[test]
    fn test_delete_multicast_user_unsubscribes_then_deletes() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, 1);
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        // User with one subscriber - delete must unsubscribe first.
        let user_activated_with_sub = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![mgroup_pubkey],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            ..Default::default()
        };

        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            code: "test".to_string(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            tenant_pk: Pubkey::default(),
            multicast_ip: "223.0.0.1".parse().unwrap(),
            publisher_count: 0,
            subscriber_count: 1,
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(
            &client.get_program_id(),
            &Ipv4Addr::UNSPECIFIED,
            &client.get_payer(),
        );
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: client.get_payer(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: client.get_payer(),
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        let mut seq = Sequence::new();

        // Call 1: Initial user fetch in DeleteUserCommand - Activated with subscriber
        let user_clone1 = user_activated_with_sub.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone1.clone())));

        // Call 2: ListMulticastGroupCommand - gets all multicast groups
        let mgroup_for_list = mgroup.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(
                    mgroup_pubkey,
                    AccountData::MulticastGroup(mgroup_for_list.clone()),
                );
                Ok(map)
            });

        // Call 3: MulticastGroup fetch in UpdateMulticastGroupRolesCommand
        let mgroup_clone = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(mgroup_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup_clone.clone())));

        // Call 4: User fetch inside UpdateMulticastGroupRolesCommand - needs Activated
        let user_clone2 = user_activated_with_sub.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone2.clone())));

        // Call 5: AccessPass fetch in UpdateMulticastGroupRolesCommand
        let accesspass_clone1 = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone1.clone())));

        // Execute transaction for UpdateMulticastGroupRolesCommand (unsubscribe):
        // assert the exact instruction the composed cascade emits.
        client
            .expect_send_transaction()
            .with(predicate::eq(update_multicast_group_roles(
                &program_id,
                &payer,
                &mgroup_pubkey,
                &accesspass_pubkey,
                &user_pubkey,
                &[],
                UpdateMulticastGroupRolesArgs {
                    publisher: false,
                    subscriber: false,
                    client_ip,
                    use_onchain_allocation: true,
                    extra_group_count: 0,
                },
            )))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));

        // Call 6: AccessPass fetch for DeleteUserCommand
        let accesspass_clone2 = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone2.clone())));

        // Call 7: Device fetch for DeleteUserCommand
        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        // Execute transaction for DeleteUser: assert the exact instruction (the builder
        // derives globalstate + every resource-extension PDA; tenant is None, owner is
        // the payer, and the device advertises one dz_prefix).
        client
            .expect_send_transaction()
            .with(predicate::eq(delete_user(
                &program_id,
                &payer,
                &user_pubkey,
                &accesspass_pubkey,
                &device_pk,
                1,
                None,
                &payer,
                AccessPassKind::Prepaid,
                UserDeleteArgs {
                    dz_prefix_count: 1,
                    multicast_publisher_count: 1,
                },
            )))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));

        expect_missing_permission_account(&mut client);

        let res = DeleteUserCommand {
            pubkey: user_pubkey,
            kind: AccessPassKind::Prepaid,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_delete_multicast_user_pub_and_sub_same_group_deduplicates() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, 1);
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        // User is both publisher and subscriber of the same group
        let user_activated = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![mgroup_pubkey],
            subscribers: vec![mgroup_pubkey],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            ..Default::default()
        };

        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            code: "test".to_string(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            tenant_pk: Pubkey::default(),
            multicast_ip: "223.0.0.1".parse().unwrap(),
            publisher_count: 1,
            subscriber_count: 1,
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(
            &client.get_program_id(),
            &Ipv4Addr::UNSPECIFIED,
            &client.get_payer(),
        );
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: client.get_payer(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: client.get_payer(),
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        let mut seq = Sequence::new();

        // Call 1: Initial user fetch - has same group in both publishers and subscribers
        let user_clone1 = user_activated.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone1.clone())));

        // Call 2: ListMulticastGroupCommand - gets all multicast groups
        let mgroup_for_list = mgroup.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(
                    mgroup_pubkey,
                    AccountData::MulticastGroup(mgroup_for_list.clone()),
                );
                Ok(map)
            });

        // Only ONE unsubscribe call should happen (deduplication)
        let mgroup_clone = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(mgroup_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup_clone.clone())));

        let user_clone2 = user_activated.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone2.clone())));

        let accesspass_clone1 = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone1.clone())));

        // The single (deduplicated) unsubscribe: assert the exact instruction.
        client
            .expect_send_transaction()
            .with(predicate::eq(update_multicast_group_roles(
                &program_id,
                &payer,
                &mgroup_pubkey,
                &accesspass_pubkey,
                &user_pubkey,
                &[],
                UpdateMulticastGroupRolesArgs {
                    publisher: false,
                    subscriber: false,
                    client_ip,
                    use_onchain_allocation: true,
                    extra_group_count: 0,
                },
            )))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));

        // AccessPass fetch for DeleteUser
        let accesspass_clone2 = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone2.clone())));

        // Device fetch for DeleteUser
        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        // DeleteUser transaction: assert the exact instruction.
        client
            .expect_send_transaction()
            .with(predicate::eq(delete_user(
                &program_id,
                &payer,
                &user_pubkey,
                &accesspass_pubkey,
                &device_pk,
                1,
                None,
                &payer,
                AccessPassKind::Prepaid,
                UserDeleteArgs {
                    dz_prefix_count: 1,
                    multicast_publisher_count: 1,
                },
            )))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));

        expect_missing_permission_account(&mut client);

        let res = DeleteUserCommand {
            pubkey: user_pubkey,
            kind: AccessPassKind::Prepaid,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_delete_user_with_foundation_key_clears_subscriptions() {
        let mut client = MockDoubleZeroClient::new();

        let foundation_key = Pubkey::new_unique();
        let user_owner = Pubkey::new_unique();
        client.expect_get_payer().returning(move || foundation_key);
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let (globalstate_pubkey, bump_seed) = get_globalstate_pda(&program_id);
        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed,
            account_index: 0,
            foundation_allowlist: vec![foundation_key],
            _device_allowlist: vec![],
            _user_allowlist: vec![],
            activator_authority_pk: Pubkey::new_unique(),
            sentinel_authority_pk: Pubkey::new_unique(),
            contributor_airdrop_lamports: 1_000_000_000,
            user_airdrop_lamports: 40_000,
            health_oracle_pk: Pubkey::new_unique(),
            qa_allowlist: vec![],
            feature_flags: 0,
            feed_authority_pk: Pubkey::default(),
            ip_verifier_authority_pk: Pubkey::default(),
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        let user_pubkey = Pubkey::new_unique();
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, 1);
        let client_ip = Ipv4Addr::new(100, 0, 0, 1);

        // AccessPass is keyed to (client_ip, user_owner) — not foundation_key
        let (unspecified_accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &user_owner);
        let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_owner);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer: user_owner,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: user_owner,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        let device_pk = Pubkey::new_unique();
        let user_with_sub = User {
            account_type: AccountType::User,
            owner: user_owner,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![mgroup_pubkey],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            ..Default::default()
        };

        let user_activated_final = User {
            status: UserStatus::Activated,
            subscribers: vec![],
            ..user_with_sub.clone()
        };

        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: user_owner,
            bump_seed: 0,
            index: 1,
            code: "test".to_string(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            tenant_pk: Pubkey::default(),
            multicast_ip: "223.0.0.1".parse().unwrap(),
            publisher_count: 0,
            subscriber_count: 1,
        };

        let mut seq = Sequence::new();

        // Call 1: Initial user fetch in DeleteUserCommand
        let user_clone1 = user_with_sub.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone1.clone())));

        // Call 2: ListMulticastGroupCommand
        let mgroup_for_list = mgroup.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(
                    mgroup_pubkey,
                    AccountData::MulticastGroup(mgroup_for_list.clone()),
                );
                Ok(map)
            });

        // Call 3: MulticastGroup fetch in UpdateMulticastGroupRolesCommand
        let mgroup_clone = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(mgroup_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup_clone.clone())));

        // Call 4: User fetch inside UpdateMulticastGroupRolesCommand
        let user_clone2 = user_with_sub.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone2.clone())));

        // Call 5a: UNSPECIFIED AccessPass lookup fails (fallback path) — UpdateMulticastGroupRolesCommand
        let user_clone_fallback1 = user_with_sub.clone();
        client
            .expect_get()
            .with(predicate::eq(unspecified_accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone_fallback1.clone())));

        // Call 5b: AccessPass fetch via client_ip fallback — keyed to (client_ip, user_owner)
        let accesspass_clone1 = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone1.clone())));

        // Call 6: Execute unsubscribe transaction — the foundation key is the payer, but
        // the access pass and the user's client_ip come from the user's owner.
        client
            .expect_send_transaction()
            .with(predicate::eq(update_multicast_group_roles(
                &program_id,
                &foundation_key,
                &mgroup_pubkey,
                &accesspass_pubkey,
                &user_pubkey,
                &[],
                UpdateMulticastGroupRolesArgs {
                    publisher: false,
                    subscriber: false,
                    client_ip,
                    use_onchain_allocation: true,
                    extra_group_count: 0,
                },
            )))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));

        // Call 7a: UNSPECIFIED AccessPass lookup fails (fallback path) — DeleteUserCommand
        let user_clone_fallback2 = user_activated_final.clone();
        client
            .expect_get()
            .with(predicate::eq(unspecified_accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone_fallback2.clone())));

        // Call 7b: AccessPass fetch via client_ip fallback — keyed to (client_ip, user_owner)
        let accesspass_clone2 = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone2.clone())));

        // Call 7c: Device fetch for DeleteUserCommand
        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        // Call 8: Execute DeleteUser transaction — payer is the foundation key, but the
        // trailing owner account is the user's own owner, not the payer.
        client
            .expect_send_transaction()
            .with(predicate::eq(delete_user(
                &program_id,
                &foundation_key,
                &user_pubkey,
                &accesspass_pubkey,
                &device_pk,
                1,
                None,
                &user_owner,
                AccessPassKind::Prepaid,
                UserDeleteArgs {
                    dz_prefix_count: 1,
                    multicast_publisher_count: 1,
                },
            )))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));

        expect_missing_permission_account(&mut client);

        let res = DeleteUserCommand {
            pubkey: user_pubkey,
            kind: AccessPassKind::Prepaid,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_delete_user_with_onchain_deallocation() {
        let mut client = create_test_client();

        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let user = User {
            account_type: AccountType::User,
            owner: payer,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: "10.1.0.0/31".parse().unwrap(),
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

        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        // Mock AccessPass fetch (UNSPECIFIED IP path)
        let (accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
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
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        // Mock Device fetch (1 dz_prefix)
        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        // ListMulticastGroupCommand — no groups
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .returning(|_| Ok(std::collections::HashMap::new()));

        // Single-send path (no multicast subscriptions): assert the exact DeleteUser
        // instruction the command hands to send_transaction. tenant is None (default),
        // owner is the payer, and the device advertises one dz_prefix.
        let expected = delete_user(
            &program_id,
            &payer,
            &user_pubkey,
            &accesspass_pubkey,
            &device_pk,
            1,
            None,
            &payer,
            AccessPassKind::Prepaid,
            UserDeleteArgs {
                dz_prefix_count: 1,
                multicast_publisher_count: 1,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        expect_missing_permission_account(&mut client);

        let res = DeleteUserCommand {
            pubkey: user_pubkey,
            kind: AccessPassKind::Prepaid,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_delete_user_threads_a_different_kind() {
        // A second, distinct kind from the other tests in this file: proves the command
        // reads self.kind rather than passing through a hardcoded value. The stored pass's
        // accesspass_type is irrelevant to this SDK-level test (the mock doesn't enforce
        // the program's guard); only the kind carried on the command matters here.
        let mut client = create_test_client();

        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let user = User {
            account_type: AccountType::User,
            owner: payer,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: "10.1.0.0/31".parse().unwrap(),
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

        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        let (accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
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
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .returning(|_| Ok(std::collections::HashMap::new()));

        let expected = delete_user(
            &program_id,
            &payer,
            &user_pubkey,
            &accesspass_pubkey,
            &device_pk,
            1,
            None,
            &payer,
            AccessPassKind::SolanaValidator,
            UserDeleteArgs {
                dz_prefix_count: 1,
                multicast_publisher_count: 1,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        expect_missing_permission_account(&mut client);

        let res = DeleteUserCommand {
            pubkey: user_pubkey,
            kind: AccessPassKind::SolanaValidator,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_delete_user_with_permission_pda() {
        let mut client = create_test_client();

        let payer = client.get_payer();
        let program_id = client.get_program_id();

        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let user = User {
            account_type: AccountType::User,
            owner: payer,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: "10.1.0.0/31".parse().unwrap(),
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

        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        let (accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
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
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .returning(|_| Ok(std::collections::HashMap::new()));

        let mut expected = delete_user(
            &program_id,
            &payer,
            &user_pubkey,
            &accesspass_pubkey,
            &device_pk,
            1,
            None,
            &payer,
            AccessPassKind::Prepaid,
            UserDeleteArgs {
                dz_prefix_count: 1,
                multicast_publisher_count: 1,
            },
        );
        let (permission_pda_pubkey, _) = get_permission_pda(&program_id, &payer);
        expected
            .accounts
            .push(AccountMeta::new_readonly(permission_pda_pubkey, false));

        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![permission_pda_pubkey]))
            .returning(move |_| Ok(vec![Some(Account::new(0, 0, &program_id))]));

        let res = DeleteUserCommand {
            pubkey: user_pubkey,
            kind: AccessPassKind::Prepaid,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
