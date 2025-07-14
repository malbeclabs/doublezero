use crate::{
    activator::DeviceMap, idallocator::IDAllocator, ipblockallocator::IPBlockAllocator,
    states::devicestate::DeviceState,
};
use doublezero_sdk::{
    commands::{
        device::get::GetDeviceCommand,
        user::{
            activate::ActivateUserCommand, ban::BanUserCommand,
            closeaccount::CloseAccountUserCommand, reject::RejectUserCommand,
        },
    },
    DoubleZeroClient, NetworkV4, User, UserStatus, UserType,
};
use log::info;
use solana_client::rpc_response::RpcContactInfo;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::{
    collections::{hash_map::Entry, HashMap},
    fmt::Write,
    net::{IpAddr, Ipv4Addr},
    str::FromStr,
};

#[allow(clippy::too_many_arguments)]
pub fn process_user_event(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    devices: &mut DeviceMap,
    user_tunnel_ips: &mut IPBlockAllocator,
    tunnel_tunnel_ids: &mut IDAllocator,
    user: &User,
    clusters: Vec<RpcContactInfo>,
    state_transitions: &mut HashMap<&'static str, usize>,
) {
    match user.status {
        // Create User
        UserStatus::Pending => {
            let mut log_msg = String::new();
            write!(
                &mut log_msg,
                "Event:User(Pending) {} ({}) ",
                pubkey, user.client_ip
            )
            .unwrap();

            let device_state = match get_or_insert_device_state(
                client,
                pubkey,
                devices,
                user,
                state_transitions,
            ) {
                Some(ds) => ds,
                None => {
                    // Reject user since we couldn't get their user block
                    let res =
                        reject_user(client, pubkey, "Error: Device not found", state_transitions);

                    match res {
                        Ok(signature) => {
                            write!(
                                &mut log_msg,
                                " Reject(Device not found) Rejected {signature}"
                            )
                            .unwrap();
                        }
                        Err(e) => {
                            write!(&mut log_msg, " Reject(Device not found) Error: {e}").unwrap();
                        }
                    }
                    info!("{log_msg}");
                    return;
                }
            };

            write!(&mut log_msg, " for: {}", device_state.device.code).unwrap();

            let ip: IpAddr = user.client_ip.into();
            let cluster = clusters.iter().find(|c| match c.gossip {
                Some(addr) => addr.ip() == ip,
                None => false,
            });

            write!(
                &mut log_msg,
                " ValidatorPubkey: {} ",
                &cluster
                    .map(|c| c.pubkey.to_string())
                    .unwrap_or_else(|| "None".to_string())
            )
            .unwrap();

            // Try to get tunnel network
            let tunnel_net = match user_tunnel_ips.next_available_block(0, 2) {
                Some(net) => net,
                None => {
                    // Reject user since we couldn't get their user block
                    let res = reject_user(
                        client,
                        pubkey,
                        "Error: No available user block",
                        state_transitions,
                    );

                    match res {
                        Ok(signature) => {
                            write!(
                                &mut log_msg,
                                " Reject(No available user block) Rejected {signature}"
                            )
                            .unwrap();
                        }
                        Err(e) => {
                            write!(&mut log_msg, " Reject(No available user block) Error: {e}")
                                .unwrap();
                        }
                    }
                    info!("{log_msg}");
                    return;
                }
            };

            write!(&mut log_msg, " tunnel_net: {} ", &tunnel_net).unwrap();

            let tunnel_id = device_state.get_next_tunnel_id();

            let need_dz_ip = match user.user_type {
                UserType::IBRLWithAllocatedIP | UserType::EdgeFiltering => true,
                UserType::IBRL => false,
                UserType::Multicast => !user.publishers.is_empty(),
            };

            let dz_ip = if need_dz_ip {
                match device_state.get_next_dz_ip() {
                    Some(ip) => ip,
                    None => {
                        let res = reject_user(
                            client,
                            pubkey,
                            "Error: No available dz_ip to allocate",
                            state_transitions,
                        );

                        match res {
                            Ok(signature) => {
                                write!(
                                    &mut log_msg,
                                    " Reject(No available dz_ip to allocate) Rejected {signature}"
                                )
                                .unwrap();
                            }
                            Err(e) => {
                                write!(
                                    &mut log_msg,
                                    " Reject(No available dz_ip to allocate) Error: {e}"
                                )
                                .unwrap();
                            }
                        }
                        info!("{log_msg}");
                        return;
                    }
                }
            } else {
                user.client_ip
            };

            write!(&mut log_msg, " tunnel_id: {} dz_ip: {} ", tunnel_id, &dz_ip).unwrap();

            let validator_pubkey = if let Some(v) = &cluster {
                Pubkey::from_str(&v.pubkey).ok()
            } else {
                None
            };

            // Activate the user
            let res = ActivateUserCommand {
                user_pubkey: *pubkey,
                tunnel_id,
                tunnel_net: tunnel_net.into(),
                dz_ip,
                validator_pubkey,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    write!(&mut log_msg, " Activated   {signature}").unwrap();
                    *state_transitions
                        .entry("user-pending-to-activated")
                        .or_insert(0) += 1;
                }
                Err(e) => {
                    write!(&mut log_msg, " Error: {e}").unwrap();
                }
            }

            info!("{log_msg}");
        }
        UserStatus::Updating => {
            let mut log_msg = String::new();

            write!(
                &mut log_msg,
                "Event:User(Updating) {} ({}) ",
                pubkey, user.client_ip
            )
            .unwrap();

            let device_state = match get_or_insert_device_state(
                client,
                pubkey,
                devices,
                user,
                state_transitions,
            ) {
                Some(ds) => ds,
                None => return,
            };

            write!(
                &mut log_msg,
                " Activating User: {}, for: {}",
                &user.client_ip, device_state.device.code
            )
            .unwrap();

            let need_dz_ip = match user.user_type {
                UserType::IBRLWithAllocatedIP | UserType::EdgeFiltering => true,
                UserType::IBRL => false,
                UserType::Multicast => !user.publishers.is_empty(),
            };

            let dz_ip = if need_dz_ip && user.dz_ip == user.client_ip {
                match device_state.get_next_dz_ip() {
                    Some(ip) => ip,
                    None => {
                        let res = reject_user(
                            client,
                            pubkey,
                            "Error: No available dz_ip to allocate",
                            state_transitions,
                        );

                        match res {
                            Ok(signature) => {
                                write!(
                                    &mut log_msg,
                                    " Reject(No available user block) Rejected {signature}"
                                )
                                .unwrap();
                            }
                            Err(e) => {
                                write!(&mut log_msg, " Reject(No available user block) Error: {e}")
                                    .unwrap();
                            }
                        }
                        info!("{log_msg}");
                        return;
                    }
                }
            } else {
                user.dz_ip
            };

            write!(
                &mut log_msg,
                " tunnel_net: {} tunnel_id: {} dz_ip: {} ",
                &user.tunnel_net, user.tunnel_id, &dz_ip
            )
            .unwrap();

            // Activate the user
            let res = ActivateUserCommand {
                user_pubkey: *pubkey,
                tunnel_id: user.tunnel_id,
                tunnel_net: user.tunnel_net,
                dz_ip,
                validator_pubkey: Some(user.validator_pubkey),
            }
            .execute(client);
            match res {
                Ok(signature) => {
                    write!(&mut log_msg, "Reactivated   {signature}").unwrap();

                    *state_transitions
                        .entry("user-updating-to-activated")
                        .or_insert(0) += 1;
                }
                Err(e) => {
                    write!(&mut log_msg, " Error {e}").unwrap();
                }
            }

            info!("{log_msg}");
        }

        // Delete User
        UserStatus::Deleting | UserStatus::PendingBan => {
            let mut log_msg = String::new();

            write!(
                &mut log_msg,
                "Event:User(Deleting) {} ({}) ",
                pubkey, user.client_ip
            )
            .unwrap();

            if let Some(device_state) = devices.get_mut(&user.device_pk) {
                write!(
                    &mut log_msg,
                    "for {} tunnel_net: {} tunnel_id: {} dz_ip: {}",
                    device_state.device.code, &user.tunnel_net, user.tunnel_id, &user.dz_ip
                )
                .unwrap();

                if user.tunnel_id != 0 {
                    tunnel_tunnel_ids.unassign(user.tunnel_id);
                }
                if user.tunnel_net != NetworkV4::default() {
                    user_tunnel_ips.unassign_block(user.tunnel_net.into());
                }
                if user.dz_ip != Ipv4Addr::UNSPECIFIED {
                    device_state.release(user.dz_ip, user.tunnel_id).unwrap();
                }

                if user.status == UserStatus::Deleting {
                    let res = CloseAccountUserCommand {
                        pubkey: *pubkey,
                        owner: user.owner,
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => {
                            write!(&mut log_msg, " Deactivated {signature}").unwrap();

                            *state_transitions
                                .entry("user-deleting-to-deactivated")
                                .or_insert(0) += 1;
                        }
                        Err(e) => info!("Error: {e}"),
                    }
                } else if user.status == UserStatus::PendingBan {
                    let res = BanUserCommand { pubkey: *pubkey }.execute(client);

                    match res {
                        Ok(signature) => {
                            write!(&mut log_msg, " Banned {signature}").unwrap();

                            *state_transitions
                                .entry("user-pending-ban-to-banned")
                                .or_insert(0) += 1;
                        }
                        Err(e) => {
                            write!(&mut log_msg, " Error {e}").unwrap();
                        }
                    }
                }
            }

            info!("{log_msg}");
        }
        _ => {}
    }
}

fn reject_user(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    reason: &str,
    state_transitions: &mut HashMap<&str, usize>,
) -> eyre::Result<Signature> {
    let signature = RejectUserCommand {
        pubkey: *pubkey,
        reason: reason.to_string(),
    }
    .execute(client)?;

    *state_transitions
        .entry("user-pending-to-rejected")
        .or_insert(0) += 1;

    Ok(signature)
}

fn get_or_insert_device_state<'a>(
    client: &dyn DoubleZeroClient,
    _pubkey: &Pubkey,
    devices: &'a mut DeviceMap,
    user: &User,
    _state_transitions: &mut HashMap<&'static str, usize>,
) -> Option<&'a mut DeviceState> {
    match devices.entry(user.device_pk) {
        Entry::Occupied(entry) => Some(entry.into_mut()),
        Entry::Vacant(entry) => {
            let res = GetDeviceCommand {
                pubkey_or_code: user.device_pk.to_string(),
            }
            .execute(client);

            match res {
                Ok((_, device)) => {
                    info!(
                        "Add Device: {} public_ip: {} dz_prefixes: {} ",
                        device.code, &device.public_ip, &device.dz_prefixes,
                    );
                    Some(entry.insert(DeviceState::new(&device)))
                }
                Err(_) => None,
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        idallocator::IDAllocator,
        ipblockallocator::IPBlockAllocator,
        process::user::process_user_event,
        states::devicestate::DeviceState,
        tests::utils::{create_test_client, get_device_bump_seed, get_user_bump_seed},
    };
    use doublezero_sdk::{
        AccountType, Device, DeviceStatus, DeviceType, MockDoubleZeroClient, User, UserCYOA,
        UserStatus, UserType,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        processors::user::{
            activate::UserActivateArgs, ban::UserBanArgs, closeaccount::UserCloseAccountArgs,
            reject::UserRejectArgs,
        },
        types::NetworkV4,
    };
    use mockall::{predicate, Sequence};
    use solana_client::rpc_response::RpcContactInfo;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::{collections::HashMap, net::Ipv4Addr};

    fn do_test_process_user_event_pending_to_activated(
        user_type: UserType,
        expected_dz_ip: Option<Ipv4Addr>,
    ) {
        let mut seq = Sequence::new();
        let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let validator_pubkey = Pubkey::new_unique();
        let cluster = RpcContactInfo {
            gossip: Some("192.168.1.1:5000".parse().unwrap()),
            pubkey: validator_pubkey.to_string(),
            version: Some("1.2.3".to_string()),
            feature_set: None,
            rpc: Some("192.168.1.1:8899".parse().unwrap()),
            tvu: Some("192.168.1.1:8899".parse().unwrap()),
            tpu: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_quic: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_forwards: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_forwards_quic: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_vote: Some("192.168.1.1:8899".parse().unwrap()),
            serve_repair: Some("192.168.1.1:8899".parse().unwrap()),
            pubsub: Some("192.168.1.1:8899".parse().unwrap()),
            shred_version: None,
        };
        let clusters: Vec<RpcContactInfo> = vec![cluster];

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
        };

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_user_bump_seed(&client),
            user_type,
            tenant_pk: Pubkey::new_unique(),
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1].into(),
            dz_ip: Ipv4Addr::UNSPECIFIED,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Pending,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                    tunnel_id: 500,
                    tunnel_net: "10.0.0.0/31".parse().unwrap(),
                    dz_ip: expected_dz_ip.unwrap_or(Ipv4Addr::UNSPECIFIED),
                    validator_pubkey: Some(validator_pubkey),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        let mut devices = HashMap::new();
        devices.insert(device_pubkey, DeviceState::new(&device));

        process_user_event(
            &client,
            &user_pubkey,
            &mut devices,
            &mut user_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &user,
            clusters,
            &mut state_transitions,
        );

        assert!(!user_tunnel_ips.assigned_ips.is_empty());
        assert!(!tunnel_tunnel_ids.assigned.is_empty());

        assert_eq!(state_transitions.len(), 1);
        assert_eq!(state_transitions["user-pending-to-activated"], 1);
    }

    #[test]
    fn test_process_user_event_pending_to_activated_ibrl() {
        do_test_process_user_event_pending_to_activated(
            UserType::IBRL,
            Some([192, 168, 1, 1].into()),
        );
    }

    #[test]
    fn test_process_user_event_pending_to_activated_ibrl_with_allocated_ip() {
        do_test_process_user_event_pending_to_activated(
            UserType::IBRLWithAllocatedIP,
            Some([10, 0, 0, 1].into()),
        );
    }

    #[test]
    fn test_process_user_event_pending_to_activated_edge_filtering() {
        do_test_process_user_event_pending_to_activated(
            UserType::EdgeFiltering,
            Some([10, 0, 0, 1].into()),
        );
    }

    #[test]
    fn test_process_user_event_update_to_activated() {
        let mut seq = Sequence::new();
        let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let validator_pubkey = Pubkey::new_unique();
        let cluster = RpcContactInfo {
            gossip: Some("192.168.1.1:5000".parse().unwrap()),
            pubkey: validator_pubkey.to_string(),
            version: Some("1.2.3".to_string()),
            feature_set: None,
            rpc: Some("192.168.1.1:8899".parse().unwrap()),
            tvu: Some("192.168.1.1:8899".parse().unwrap()),
            tpu: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_quic: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_forwards: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_forwards_quic: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_vote: Some("192.168.1.1:8899".parse().unwrap()),
            serve_repair: Some("192.168.1.1:8899".parse().unwrap()),
            pubsub: Some("192.168.1.1:8899".parse().unwrap()),
            shred_version: None,
        };
        let clusters: Vec<RpcContactInfo> = vec![cluster];

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
        };

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_user_bump_seed(&client),
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::new_unique(),
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1].into(),
            dz_ip: [192, 168, 1, 1].into(),
            tunnel_id: 500,
            tunnel_net: "10.0.0.1/29".parse().unwrap(),
            status: UserStatus::Updating,
            publishers: vec![Pubkey::default()],
            subscribers: vec![Pubkey::default()],
            validator_pubkey,
        };

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                    tunnel_id: 500,
                    tunnel_net: "10.0.0.1/29".parse().unwrap(),
                    dz_ip: [10, 0, 0, 1].into(),
                    validator_pubkey: Some(validator_pubkey),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        let mut devices = HashMap::new();
        devices.insert(device_pubkey, DeviceState::new(&device));

        process_user_event(
            &client,
            &user_pubkey,
            &mut devices,
            &mut user_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &user,
            clusters,
            &mut state_transitions,
        );

        assert!(!user_tunnel_ips.assigned_ips.is_empty());
        assert!(!tunnel_tunnel_ids.assigned.is_empty());

        assert_eq!(state_transitions.len(), 1);
        assert_eq!(state_transitions["user-updating-to-activated"], 1);
    }

    #[test]
    fn test_process_user_event_pending_to_rejected_by_get_device() {
        let mut seq = Sequence::new();
        let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/32".parse().unwrap());
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let validator_pubkey = Pubkey::new_unique();
        let cluster = RpcContactInfo {
            gossip: Some("192.168.1.1:5000".parse().unwrap()),
            pubkey: validator_pubkey.to_string(),
            version: Some("1.2.3".to_string()),
            feature_set: None,
            rpc: Some("192.168.1.1:8899".parse().unwrap()),
            tvu: Some("192.168.1.1:8899".parse().unwrap()),
            tpu: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_quic: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_forwards: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_forwards_quic: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_vote: Some("192.168.1.1:8899".parse().unwrap()),
            serve_repair: Some("192.168.1.1:8899".parse().unwrap()),
            pubsub: Some("192.168.1.1:8899".parse().unwrap()),
            shred_version: None,
        };
        let clusters: Vec<RpcContactInfo> = vec![cluster];

        let device_pubkey = Pubkey::new_unique();

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_user_bump_seed(&client),
            user_type: UserType::IBRLWithAllocatedIP,
            tenant_pk: Pubkey::new_unique(),
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1].into(),
            dz_ip: Ipv4Addr::UNSPECIFIED,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Pending,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(device_pubkey))
            .returning(|_| Err(eyre::eyre!("Device not found")));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                    reason: "Error: Device not found".to_string(),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        let mut devices = HashMap::new();

        process_user_event(
            &client,
            &user_pubkey,
            &mut devices,
            &mut user_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &user,
            clusters,
            &mut state_transitions,
        );

        assert_eq!(state_transitions.len(), 1);
        assert_eq!(state_transitions["user-pending-to-rejected"], 1);
    }

    #[test]
    fn test_process_user_event_pending_to_rejected_by_no_tunnel_block() {
        let mut seq = Sequence::new();
        let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/32".parse().unwrap());
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let validator_pubkey = Pubkey::new_unique();
        let cluster = RpcContactInfo {
            gossip: Some("192.168.1.1:5000".parse().unwrap()),
            pubkey: validator_pubkey.to_string(),
            version: Some("1.2.3".to_string()),
            feature_set: None,
            rpc: Some("192.168.1.1:8899".parse().unwrap()),
            tvu: Some("192.168.1.1:8899".parse().unwrap()),
            tpu: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_quic: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_forwards: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_forwards_quic: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_vote: Some("192.168.1.1:8899".parse().unwrap()),
            serve_repair: Some("192.168.1.1:8899".parse().unwrap()),
            pubsub: Some("192.168.1.1:8899".parse().unwrap()),
            shred_version: None,
        };
        let clusters: Vec<RpcContactInfo> = vec![cluster];

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2].into(),
            status: DeviceStatus::Activated,
            code: "TestDevice".to_string(),
            metrics_publisher_pk: Pubkey::default(),
            dz_prefixes: "10.0.0.0/32".parse().unwrap(),
        };

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_user_bump_seed(&client),
            user_type: UserType::IBRLWithAllocatedIP,
            tenant_pk: Pubkey::new_unique(),
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1].into(),
            dz_ip: Ipv4Addr::UNSPECIFIED,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Pending,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                    reason: "Error: No available dz_ip to allocate".to_string(),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        let mut devices = HashMap::new();
        let device2 = device.clone();
        devices.insert(device_pubkey, DeviceState::new(&device2));

        // allocate the only ip
        assert_ne!(
            devices.get_mut(&device_pubkey).unwrap().dz_ips[0].next_available_block(1, 1),
            None
        );

        process_user_event(
            &client,
            &user_pubkey,
            &mut devices,
            &mut user_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &user,
            clusters,
            &mut state_transitions,
        );

        assert_eq!(state_transitions.len(), 1);
        assert_eq!(state_transitions["user-pending-to-rejected"], 1);
    }

    #[test]
    fn test_process_user_event_pending_to_rejected_by_no_user_block() {
        let mut seq = Sequence::new();
        let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/32".parse().unwrap());
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let validator_pubkey = Pubkey::new_unique();
        let cluster = RpcContactInfo {
            gossip: Some("192.168.1.1:5000".parse().unwrap()),
            pubkey: validator_pubkey.to_string(),
            version: Some("1.2.3".to_string()),
            feature_set: None,
            rpc: Some("192.168.1.1:8899".parse().unwrap()),
            tvu: Some("192.168.1.1:8899".parse().unwrap()),
            tpu: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_quic: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_forwards: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_forwards_quic: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_vote: Some("192.168.1.1:8899".parse().unwrap()),
            serve_repair: Some("192.168.1.1:8899".parse().unwrap()),
            pubsub: Some("192.168.1.1:8899".parse().unwrap()),
            shred_version: None,
        };
        let clusters: Vec<RpcContactInfo> = vec![cluster];

        // eat a blocok
        let _ = user_tunnel_ips.next_available_block(0, 2);

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
        };

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_user_bump_seed(&client),
            user_type: UserType::IBRLWithAllocatedIP,
            tenant_pk: Pubkey::new_unique(),
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1].into(),
            dz_ip: Ipv4Addr::UNSPECIFIED,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Pending,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                    reason: "Error: No available user block".to_string(),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        let mut devices = HashMap::new();
        let device2 = device.clone();
        devices.insert(device_pubkey, DeviceState::new(&device2));

        process_user_event(
            &client,
            &user_pubkey,
            &mut devices,
            &mut user_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &user,
            clusters,
            &mut state_transitions,
        );

        assert_eq!(state_transitions.len(), 1);
        assert_eq!(state_transitions["user-pending-to-rejected"], 1);
    }

    fn do_test_process_user_event_deleting_or_pending_ban<F>(
        user_status: UserStatus,
        func: F,
        state_transition: &'static str,
    ) where
        F: Fn(&mut MockDoubleZeroClient, &User, &mut Sequence),
    {
        assert!(user_status == UserStatus::Deleting || user_status == UserStatus::PendingBan);

        let mut seq = Sequence::new();
        let mut devices = HashMap::new();
        let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let validator_pubkey = Pubkey::new_unique();
        let cluster = RpcContactInfo {
            gossip: Some("192.168.1.1:5000".parse().unwrap()),
            pubkey: validator_pubkey.to_string(),
            version: Some("1.2.3".to_string()),
            feature_set: None,
            rpc: Some("192.168.1.1:8899".parse().unwrap()),
            tvu: Some("192.168.1.1:8899".parse().unwrap()),
            tpu: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_quic: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_forwards: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_forwards_quic: Some("192.168.1.1:8899".parse().unwrap()),
            tpu_vote: Some("192.168.1.1:8899".parse().unwrap()),
            serve_repair: Some("192.168.1.1:8899".parse().unwrap()),
            pubsub: Some("192.168.1.1:8899".parse().unwrap()),
            shred_version: None,
        };
        let clusters: Vec<RpcContactInfo> = vec![cluster];

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        let device_pubkey = Pubkey::new_unique();
        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_user_bump_seed(&client),
            user_type: UserType::IBRLWithAllocatedIP,
            tenant_pk: Pubkey::new_unique(),
            device_pk: device_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1].into(),
            dz_ip: Ipv4Addr::UNSPECIFIED,
            tunnel_id: 102,
            tunnel_net: "10.0.0.0/31".parse().unwrap(),
            status: user_status,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2].into(),
            status: DeviceStatus::Activated,
            code: "TestDevice".to_string(),
            metrics_publisher_pk: Pubkey::default(),
            dz_prefixes: "11.0.0.0/16".parse().unwrap(),
        };

        devices.insert(device_pubkey, DeviceState::new(&device));

        func(&mut client, &user, &mut seq);

        assert!(tunnel_tunnel_ids.assigned.contains(&102));

        process_user_event(
            &client,
            &user_pubkey,
            &mut devices,
            &mut user_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &user,
            clusters,
            &mut state_transitions,
        );

        assert!(!tunnel_tunnel_ids.assigned.contains(&102));

        assert_eq!(state_transitions.len(), 1);
        assert_eq!(state_transitions[state_transition], 1);
    }

    #[test]
    fn test_process_user_event_deleting() {
        do_test_process_user_event_deleting_or_pending_ban(
            UserStatus::Deleting,
            |user_service, _, seq| {
                user_service
                    .expect_execute_transaction()
                    .times(1)
                    .in_sequence(seq)
                    .with(
                        predicate::eq(DoubleZeroInstruction::CloseAccountUser(
                            UserCloseAccountArgs {},
                        )),
                        predicate::always(),
                    )
                    .returning(|_, _| Ok(Signature::new_unique()));
            },
            "user-deleting-to-deactivated",
        );
    }

    #[test]
    fn test_process_user_event_pending_ban() {
        do_test_process_user_event_deleting_or_pending_ban(
            UserStatus::PendingBan,
            |user_service, _, seq| {
                user_service
                    .expect_execute_transaction()
                    .times(1)
                    .in_sequence(seq)
                    .with(
                        predicate::eq(DoubleZeroInstruction::BanUser(UserBanArgs {})),
                        predicate::always(),
                    )
                    .returning(|_, _| Ok(Signature::new_unique()));
            },
            "user-pending-ban-to-banned",
        );
    }
}
