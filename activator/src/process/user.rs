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
    ipv4_to_string, networkv4_list_to_string, networkv4_to_string, DoubleZeroClient, User,
    UserStatus, UserType,
};
use solana_sdk::pubkey::Pubkey;
use std::collections::{hash_map::Entry, HashMap};

pub fn process_user_event(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    devices: &mut DeviceMap,
    user_tunnel_ips: &mut IPBlockAllocator,
    tunnel_tunnel_ids: &mut IDAllocator,
    user: &User,
    state_transitions: &mut HashMap<&'static str, usize>,
) {
    match user.status {
        // Create User
        UserStatus::Pending => {
            let device_state =
                match get_or_insert_device_state(client, devices, user, state_transitions) {
                    Some(ds) => ds,
                    None => return,
                };

            println!(
                "Activating User: {}, for: {}",
                ipv4_to_string(&user.client_ip),
                device_state.device.code
            );

            // Try to get tunnel network
            let tunnel_net = match user_tunnel_ips.next_available_block(0, 2) {
                Some(net) => net,
                None => {
                    // Reject user since we couldn't get their user block
                    reject_user(
                        client,
                        user,
                        "Error: No available user block",
                        state_transitions,
                    );
                    return;
                }
            };

            print!("tunnel_net: {} ", networkv4_to_string(&tunnel_net));

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
                        eprintln!("Error: No available dz_ip to allocate");
                        reject_user(
                            client,
                            user,
                            "Error: No available dz_ip to allocate",
                            state_transitions,
                        );
                        return;
                    }
                }
            } else {
                user.client_ip
            };

            print!(
                "tunnel_id: {} dz_ip: {} ",
                tunnel_id,
                ipv4_to_string(&dz_ip)
            );

            // Activate the user
            let res = ActivateUserCommand {
                pubkey: *pubkey,
                tunnel_id,
                tunnel_net,
                dz_ip,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    println!("Activated   {signature}");
                    *state_transitions
                        .entry("user-pending-to-activated")
                        .or_insert(0) += 1;
                }
                Err(e) => println!("Error: {e}"),
            }
        }

        UserStatus::Updating => {
            let device_state =
                match get_or_insert_device_state(client, devices, user, state_transitions) {
                    Some(ds) => ds,
                    None => return,
                };

            println!(
                "Activating User: {}, for: {}",
                ipv4_to_string(&user.client_ip),
                device_state.device.code
            );

            let need_dz_ip = match user.user_type {
                UserType::IBRLWithAllocatedIP | UserType::EdgeFiltering => true,
                UserType::IBRL => false,
                UserType::Multicast => !user.publishers.is_empty(),
            };

            let dz_ip = if need_dz_ip && user.dz_ip == user.client_ip {
                match device_state.get_next_dz_ip() {
                    Some(ip) => ip,
                    None => {
                        eprintln!("Error: No available dz_ip to allocate");
                        reject_user(
                            client,
                            user,
                            "Error: No available dz_ip to allocate",
                            state_transitions,
                        );
                        return;
                    }
                }
            } else {
                user.dz_ip
            };

            print!(
                "tunnel_net: {} tunnel_id: {} dz_ip: {} ",
                networkv4_to_string(&user.tunnel_net),
                user.tunnel_id,
                ipv4_to_string(&dz_ip)
            );

            // Activate the user
            let res = ActivateUserCommand {
                pubkey: *pubkey,
                tunnel_id: user.tunnel_id,
                tunnel_net: user.tunnel_net,
                dz_ip,
            }
            .execute(client);
            match res {
                Ok(signature) => {
                    println!("Reactivated   {signature}");
                    *state_transitions
                        .entry("user-updating-to-activated")
                        .or_insert(0) += 1;
                }
                Err(e) => println!("Error: {e}"),
            }
        }

        // Delete User
        UserStatus::Deleting | UserStatus::PendingBan => {
            print!("Deactivating User {} ", ipv4_to_string(&user.client_ip));

            if let Some(device_state) = devices.get_mut(&user.device_pk) {
                print!("for {} ", device_state.device.code);

                print!(
                    "tunnel_net: {} tunnel_id: {} dz_ip: {} ",
                    networkv4_to_string(&user.tunnel_net),
                    user.tunnel_id,
                    ipv4_to_string(&user.dz_ip)
                );

                if user.tunnel_id != 0 {
                    tunnel_tunnel_ids.unassign(user.tunnel_id);
                }
                if user.tunnel_net != ([0, 0, 0, 0], 0) {
                    user_tunnel_ips.unassign_block(user.tunnel_net);
                }
                if user.dz_ip != [0, 0, 0, 0] {
                    device_state.release(user.dz_ip, user.tunnel_id);
                }

                if user.status == UserStatus::Deleting {
                    let res = CloseAccountUserCommand {
                        index: user.index,
                        owner: user.owner,
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => {
                            println!("Deactivated {signature}");
                            *state_transitions
                                .entry("user-deleting-to-deactivated")
                                .or_insert(0) += 1;
                        }
                        Err(e) => println!("Error: {e}"),
                    }
                } else if user.status == UserStatus::PendingBan {
                    let res = BanUserCommand { index: user.index }.execute(client);

                    match res {
                        Ok(signature) => {
                            println!("Banned {signature}");
                            *state_transitions
                                .entry("user-pending-ban-to-banned")
                                .or_insert(0) += 1;
                        }
                        Err(e) => println!("Error: {e}"),
                    }
                }
            }
        }
        _ => {}
    }
}

fn reject_user(
    client: &dyn DoubleZeroClient,
    user: &User,
    reason: &str,
    state_transitions: &mut HashMap<&str, usize>,
) {
    let res = RejectUserCommand {
        index: user.index,
        reason: reason.to_string(),
    }
    .execute(client);

    match res {
        Ok(signature) => {
            println!("Rejected {signature}");
            *state_transitions
                .entry("user-pending-to-rejected")
                .or_insert(0) += 1;
        }
        Err(e) => println!("Error: {e}"),
    }
}

fn get_or_insert_device_state<'a>(
    client: &dyn DoubleZeroClient,
    devices: &'a mut DeviceMap,
    user: &User,
    state_transitions: &mut HashMap<&'static str, usize>,
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
                    println!(
                        "Add Device: {} public_ip: {} dz_prefixes: {} ",
                        device.code,
                        ipv4_to_string(&device.public_ip),
                        networkv4_list_to_string(&device.dz_prefixes)
                    );
                    Some(entry.insert(DeviceState::new(&device)))
                }
                Err(_) => {
                    // Reject user since we couldn't load the device
                    reject_user(client, user, "Error: Device not found", state_transitions);
                    None
                }
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
        AccountType, Device, DeviceStatus, DeviceType, IpV4, MockDoubleZeroClient, User, UserCYOA,
        UserStatus, UserType,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        processors::user::{
            activate::UserActivateArgs, ban::UserBanArgs, closeaccount::UserCloseAccountArgs,
            reject::UserRejectArgs,
        },
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::collections::HashMap;

    fn do_test_process_user_event_pending_to_activated(
        user_type: UserType,
        expected_dz_ip: Option<IpV4>,
    ) {
        let mut seq = Sequence::new();
        let mut user_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 16));
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: vec![([10, 0, 0, 1], 24)],
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
            client_ip: [192, 168, 1, 1],
            dz_ip: [0, 0, 0, 0],
            tunnel_id: 0,
            tunnel_net: ([0, 0, 0, 0], 0),
            status: UserStatus::Pending,
            publishers: vec![],
            subscribers: vec![],
        };

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                    tunnel_id: 500,
                    tunnel_net: ([10, 0, 0, 0], 31),
                    dz_ip: expected_dz_ip.unwrap_or([0, 0, 0, 0]),
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
            &mut state_transitions,
        );

        assert!(!user_tunnel_ips.assigned_ips.is_empty());
        assert!(!tunnel_tunnel_ids.assigned.is_empty());

        assert_eq!(state_transitions.len(), 1);
        assert_eq!(state_transitions["user-pending-to-activated"], 1);
    }

    #[test]
    fn test_process_user_event_pending_to_activated_ibrl() {
        do_test_process_user_event_pending_to_activated(UserType::IBRL, Some([192, 168, 1, 1]));
    }

    #[test]
    fn test_process_user_event_pending_to_activated_ibrl_with_allocated_ip() {
        do_test_process_user_event_pending_to_activated(
            UserType::IBRLWithAllocatedIP,
            Some([10, 0, 0, 1]),
        );
    }

    #[test]
    fn test_process_user_event_pending_to_activated_edge_filtering() {
        do_test_process_user_event_pending_to_activated(
            UserType::EdgeFiltering,
            Some([10, 0, 0, 1]),
        );
    }

    #[test]
    fn test_process_user_event_update_to_activated() {
        let mut seq = Sequence::new();
        let mut user_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 16));
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: vec![([10, 0, 0, 1], 24)],
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
            client_ip: [192, 168, 1, 1],
            dz_ip: [192, 168, 1, 1],
            tunnel_id: 500,
            tunnel_net: ([10, 0, 0, 1], 29),
            status: UserStatus::Updating,
            publishers: vec![Pubkey::default()],
            subscribers: vec![Pubkey::default()],
        };

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                    tunnel_id: 500,
                    tunnel_net: ([10, 0, 0, 1], 29),
                    dz_ip: [10, 0, 0, 1],
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
        let mut user_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 32));
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

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
            client_ip: [192, 168, 1, 1],
            dz_ip: [0, 0, 0, 0],
            tunnel_id: 0,
            tunnel_net: ([0, 0, 0, 0], 0),
            status: UserStatus::Pending,
            publishers: vec![],
            subscribers: vec![],
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
                    index: user.index,
                    bump_seed: user.bump_seed,
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
            &mut state_transitions,
        );

        assert_eq!(state_transitions.len(), 1);
        assert_eq!(state_transitions["user-pending-to-rejected"], 1);
    }

    #[test]
    fn test_process_user_event_pending_to_rejected_by_no_tunnel_block() {
        let mut seq = Sequence::new();
        let mut user_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 32));
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2],
            status: DeviceStatus::Activated,
            code: "TestDevice".to_string(),
            metrics_publisher_pk: Pubkey::default(),
            dz_prefixes: vec![([10, 0, 0, 0], 32)],
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
            client_ip: [192, 168, 1, 1],
            dz_ip: [0, 0, 0, 0],
            tunnel_id: 0,
            tunnel_net: ([0, 0, 0, 0], 0),
            status: UserStatus::Pending,
            publishers: vec![],
            subscribers: vec![],
        };

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                    index: user.index,
                    bump_seed: user.bump_seed,
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
            &mut state_transitions,
        );

        assert_eq!(state_transitions.len(), 1);
        assert_eq!(state_transitions["user-pending-to-rejected"], 1);
    }

    #[test]
    fn test_process_user_event_pending_to_rejected_by_no_user_block() {
        let mut seq = Sequence::new();
        let mut user_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 32));
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

        // eat a blocok
        let _ = user_tunnel_ips.next_available_block(0, 2);

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: vec![([10, 0, 0, 1], 24)],
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
            client_ip: [192, 168, 1, 1],
            dz_ip: [0, 0, 0, 0],
            tunnel_id: 0,
            tunnel_net: ([0, 0, 0, 0], 0),
            status: UserStatus::Pending,
            publishers: vec![],
            subscribers: vec![],
        };

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                    index: user.index,
                    bump_seed: user.bump_seed,
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
        let mut user_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 16));
        let mut tunnel_tunnel_ids = IDAllocator::new(100, vec![100, 101, 102]);
        let mut client = create_test_client();

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
            client_ip: [192, 168, 1, 1],
            dz_ip: [0, 0, 0, 0],
            tunnel_id: 102,
            tunnel_net: ([10, 0, 0, 0], 31),
            status: user_status,
            publishers: vec![],
            subscribers: vec![],
        };

        let device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 2],
            status: DeviceStatus::Activated,
            code: "TestDevice".to_string(),
            metrics_publisher_pk: Pubkey::default(),
            dz_prefixes: vec![([11, 0, 0, 0], 16)],
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
            |user_service, user, seq| {
                user_service
                    .expect_execute_transaction()
                    .times(1)
                    .in_sequence(seq)
                    .with(
                        predicate::eq(DoubleZeroInstruction::CloseAccountUser(
                            UserCloseAccountArgs {
                                index: user.index,
                                bump_seed: user.bump_seed,
                            },
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
            |user_service, user, seq| {
                user_service
                    .expect_execute_transaction()
                    .times(1)
                    .in_sequence(seq)
                    .with(
                        predicate::eq(DoubleZeroInstruction::BanUser(UserBanArgs {
                            index: user.index,
                            bump_seed: user.bump_seed,
                        })),
                        predicate::always(),
                    )
                    .returning(|_, _| Ok(Signature::new_unique()));
            },
            "user-pending-ban-to-banned",
        );
    }
}
