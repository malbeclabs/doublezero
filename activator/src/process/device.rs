use crate::{
    idallocator::IDAllocator, ipblockallocator::IPBlockAllocator, process::iface_mgr::InterfaceMgr,
};
use doublezero_sdk::{
    commands::device::{activate::ActivateDeviceCommand, closeaccount::CloseAccountDeviceCommand},
    Device, DeviceStatus, DoubleZeroClient,
};
use log::info;
use solana_sdk::pubkey::Pubkey;
use std::{
    collections::{hash_map::Entry, HashMap},
    fmt::Write,
};

use crate::{activator::DeviceMap, states::devicestate::DeviceState};

pub fn process_device_event(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    devices: &mut DeviceMap,
    device: &Device,
    state_transitions: &mut HashMap<&'static str, usize>,
    segment_routing_ids: &mut IDAllocator,
    link_ips: &mut IPBlockAllocator,
) {
    match device.status {
        DeviceStatus::Pending => {
            let mut log_msg = String::new();
            write!(
                &mut log_msg,
                "Event:Device(Pending) {} ({}) public_ip: {} dz_prefixes: {} ",
                pubkey, device.code, &device.public_ip, &device.dz_prefixes,
            )
            .unwrap();

            let res = ActivateDeviceCommand {
                device_pubkey: *pubkey,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    write!(&mut log_msg, " Activated {signature}").unwrap();

                    devices.insert(*pubkey, DeviceState::new(device));
                    *state_transitions
                        .entry("device-pending-to-activated")
                        .or_insert(0) += 1;
                }
                Err(e) => write!(&mut log_msg, " Error {e}").unwrap(),
            }
            info!("{log_msg}");
        }
        DeviceStatus::Activated => {
            let mut mgr = InterfaceMgr::new(client, Some(segment_routing_ids), link_ips);
            mgr.process_device_interfaces(pubkey, device);

            match devices.entry(*pubkey) {
                Entry::Occupied(mut entry) => entry.get_mut().update(device),
                Entry::Vacant(entry) => {
                    info!(
                        "Add Device: {} public_ip: {} dz_prefixes: {} ",
                        device.code, &device.public_ip, &device.dz_prefixes,
                    );
                    entry.insert(DeviceState::new(device));
                }
            }
        }
        DeviceStatus::Deleting => {
            let mut log_msg = String::new();
            write!(
                &mut log_msg,
                "Event:Device(Deleting) {} ({}) ",
                pubkey, device.code
            )
            .unwrap();

            let res = CloseAccountDeviceCommand {
                pubkey: *pubkey,
                owner: device.owner,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    write!(&mut log_msg, " Deactivated {signature}").unwrap();
                    devices.remove(pubkey);
                    *state_transitions
                        .entry("device-deleting-to-deactivated")
                        .or_insert(0) += 1;
                }
                Err(e) => write!(&mut log_msg, " Error {e}").unwrap(),
            }
        }
        _ => {}
    }
}

#[cfg(test)]
mod tests {
    use crate::tests::utils::{create_test_client, get_device_bump_seed};

    use super::*;
    use doublezero_program_common::types::NetworkV4;
    use doublezero_sdk::{
        AccountData, AccountType, CurrentInterfaceVersion, DeviceType, Interface, InterfaceStatus,
        InterfaceType, LoopbackType,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        processors::device::{
            activate::DeviceActivateArgs,
            closeaccount::DeviceCloseAccountArgs,
            interface::{activate::DeviceInterfaceActivateArgs, unlink::DeviceInterfaceUnlinkArgs},
        },
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::signature::Signature;
    use std::collections::HashMap;

    #[test]
    fn test_process_device_event_pending_to_deleted() {
        let mut seq = Sequence::new();
        let mut devices = HashMap::new();
        let mut client = create_test_client();
        let mut segment_ids = IDAllocator::new(1, vec![]);

        let device_pubkey = Pubkey::from_str_const("8KvLQiyKgrK3KyVGVVyT1Pmg7ahPVFsvHUVPg97oYynV");
        let mut device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 1].into(),
            status: DeviceStatus::Pending,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: "10.0.0.1/24,10.0.1.1/24".parse().unwrap(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![
                Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Pending,
                    name: "Ethernet0".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    vlan_id: 0,
                    ip_net: NetworkV4::default(),
                    node_segment_idx: 0,
                    user_tunnel_endpoint: false,
                }),
                Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Pending,
                    name: "Loopback0".to_string(),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::Vpnv4,
                    vlan_id: 0,
                    ip_net: NetworkV4::default(),
                    node_segment_idx: 0,
                    user_tunnel_endpoint: false,
                }),
            ],
            max_users: 255,
            users_count: 0,
        };

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs)),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut ip_block_allocator = IPBlockAllocator::new("1.1.1.0/24".parse().unwrap());

        process_device_event(
            &client,
            &device_pubkey,
            &mut devices,
            &device,
            &mut state_transitions,
            &mut segment_ids,
            &mut ip_block_allocator,
        );

        assert!(devices.contains_key(&device_pubkey));
        assert_eq!(devices.get(&device_pubkey).unwrap().device, device);

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::UnlinkDeviceInterface(
                    DeviceInterfaceUnlinkArgs {
                        name: "Ethernet0".to_string(),
                    },
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateDeviceInterface(
                    DeviceInterfaceActivateArgs {
                        name: "Loopback0".to_string(),
                        ip_net: "1.1.1.1/32".parse().unwrap(),
                        node_segment_idx: 1,
                    },
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        // interfaces get checked on activated devices
        device.status = DeviceStatus::Activated;

        process_device_event(
            &client,
            &device_pubkey,
            &mut devices,
            &device,
            &mut state_transitions,
            &mut segment_ids,
            &mut ip_block_allocator,
        );

        device.status = DeviceStatus::Deleting;

        let mut client = create_test_client();

        let device2 = device.clone();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device2.clone())));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::CloseAccountDevice(
                    DeviceCloseAccountArgs {},
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        process_device_event(
            &client,
            &device_pubkey,
            &mut devices,
            &device,
            &mut state_transitions,
            &mut segment_ids,
            &mut ip_block_allocator,
        );
        assert!(!devices.contains_key(&device_pubkey));
        assert_eq!(state_transitions.len(), 2);
        assert_eq!(state_transitions["device-pending-to-activated"], 1);
        assert_eq!(state_transitions["device-deleting-to-deactivated"], 1);
    }

    #[test]
    fn test_process_device_event_activated() {
        let mut devices = HashMap::new();
        let mut client = create_test_client();
        let pubkey = Pubkey::new_unique();
        let mut segment_ids = IDAllocator::new(1, vec![]);

        let mut device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            reference_count: 0,
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 1].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![
                Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Pending,
                    name: "Ethernet0".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    vlan_id: 0,
                    ip_net: NetworkV4::default(),
                    node_segment_idx: 0,
                    user_tunnel_endpoint: false,
                }),
                Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Pending,
                    name: "Loopback0".to_string(),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::Vpnv4,
                    vlan_id: 0,
                    ip_net: NetworkV4::default(),
                    node_segment_idx: 0,
                    user_tunnel_endpoint: false,
                }),
            ],
            max_users: 255,
            users_count: 0,
        };

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();
        let mut ip_block_allocator = IPBlockAllocator::new("1.1.1.0/24".parse().unwrap());

        client
            .expect_execute_transaction()
            .times(1)
            .with(
                predicate::eq(DoubleZeroInstruction::UnlinkDeviceInterface(
                    DeviceInterfaceUnlinkArgs {
                        name: "Ethernet0".to_string(),
                    },
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        client
            .expect_execute_transaction()
            .times(1)
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateDeviceInterface(
                    DeviceInterfaceActivateArgs {
                        name: "Loopback0".to_string(),
                        ip_net: "1.1.1.1/32".parse().unwrap(),
                        node_segment_idx: 1,
                    },
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        process_device_event(
            &client,
            &pubkey,
            &mut devices,
            &device,
            &mut state_transitions,
            &mut segment_ids,
            &mut ip_block_allocator,
        );

        assert!(devices.contains_key(&pubkey));
        assert_eq!(devices.get(&pubkey).unwrap().device, device);

        device.dz_prefixes.push("10.0.1.1/24".parse().unwrap());

        let mut expected_interfaces = [
            device.interfaces[0].into_current_version().clone(),
            device.interfaces[1].into_current_version().clone(),
        ];

        expected_interfaces[0].status = InterfaceStatus::Unlinked;
        expected_interfaces[1].status = InterfaceStatus::Activated;
        expected_interfaces[1].ip_net = "1.1.1.1/32".parse().unwrap();
        expected_interfaces[1].node_segment_idx = 1;

        device.interfaces = expected_interfaces
            .iter()
            .map(|iface| Interface::V1(iface.clone()))
            .collect::<Vec<_>>();
        process_device_event(
            &client,
            &pubkey,
            &mut devices,
            &device,
            &mut state_transitions,
            &mut segment_ids,
            &mut ip_block_allocator,
        );

        assert!(devices.contains_key(&pubkey));
        assert_eq!(devices.get(&pubkey).unwrap().device, device);

        assert_eq!(state_transitions.len(), 0);
    }
}
