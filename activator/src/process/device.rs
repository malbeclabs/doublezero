use crate::{
    idallocator::IDAllocator, ipblockallocator::IPBlockAllocator, process::iface_mgr::InterfaceMgr,
};
use doublezero_sdk::{
    commands::{
        device::{activate::ActivateDeviceCommand, closeaccount::CloseAccountDeviceCommand},
        resource::closeaccount::CloseResourceCommand,
    },
    Device, DeviceStatus, DoubleZeroClient, ResourceType,
};
use log::info;
use solana_sdk::pubkey::Pubkey;
use std::{collections::hash_map::Entry, fmt::Write};

use crate::{processor::DeviceMap, states::devicestate::DeviceState};

pub fn process_device_event(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    devices: &mut DeviceMap,
    device: &Device,
    segment_routing_ids: &mut IDAllocator,
    link_ips: &mut IPBlockAllocator,
    use_onchain_allocation: bool,
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
                    write!(&mut log_msg, " DeviceProvisioning {signature}").unwrap();

                    devices.insert(*pubkey, DeviceState::new(device));
                    metrics::counter!(
                        "doublezero_activator_state_transition",
                        "state_transition" => "device-pending-to-activated",
                        "device-pubkey" => pubkey.to_string(),
                    )
                    .increment(1);
                }
                Err(e) => write!(&mut log_msg, " Error {e}").unwrap(),
            }
            info!("{log_msg}");
        }
        DeviceStatus::DeviceProvisioning | DeviceStatus::LinkProvisioning => {
            let mut mgr = InterfaceMgr::new(
                client,
                Some(segment_routing_ids),
                link_ips,
                use_onchain_allocation,
            );
            mgr.process_device_interfaces(pubkey, device);

            match devices.entry(*pubkey) {
                Entry::Occupied(mut entry) => {
                    close_orphaned_dz_prefix_blocks(client, pubkey, &entry.get().device, device);
                    entry.get_mut().update(device);
                }
                Entry::Vacant(entry) => {
                    info!(
                        "Add Device: {} public_ip: {} dz_prefixes: {} ",
                        device.code, &device.public_ip, &device.dz_prefixes,
                    );
                    entry.insert(DeviceState::new(device));
                }
            }
        }
        DeviceStatus::Activated => {
            let mut mgr = InterfaceMgr::new(
                client,
                Some(segment_routing_ids),
                link_ips,
                use_onchain_allocation,
            );
            mgr.process_device_interfaces(pubkey, device);

            match devices.entry(*pubkey) {
                Entry::Occupied(mut entry) => {
                    close_orphaned_dz_prefix_blocks(client, pubkey, &entry.get().device, device);
                    entry.get_mut().update(device);
                }
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
                    metrics::counter!(
                        "doublezero_activator_state_transition",
                        "state_transition" => "device-deleting-to-deactivated",
                        "device-pubkey" => pubkey.to_string(),
                    )
                    .increment(1);
                }
                Err(e) => write!(&mut log_msg, " Error {e}").unwrap(),
            }
            info!("{log_msg}");
        }
        _ => {}
    }
}

/// Close orphaned DzPrefixBlock resource accounts when a device's dz_prefixes shrink.
fn close_orphaned_dz_prefix_blocks(
    client: &dyn DoubleZeroClient,
    device_pubkey: &Pubkey,
    old_device: &Device,
    new_device: &Device,
) {
    let old_count = old_device.dz_prefixes.len();
    let new_count = new_device.dz_prefixes.len();

    if new_count >= old_count {
        return;
    }

    for idx in new_count..old_count {
        let res = CloseResourceCommand {
            resource_type: ResourceType::DzPrefixBlock(*device_pubkey, idx),
        }
        .execute(client);

        match res {
            Ok(sig) => info!(
                "Closed orphaned DzPrefixBlock({}) for device {}: {}",
                idx, new_device.code, sig
            ),
            Err(e) => info!(
                "Error closing orphaned DzPrefixBlock({}) for device {}: {}",
                idx, new_device.code, e
            ),
        }
    }
}

#[cfg(test)]
mod tests {
    use crate::tests::utils::{create_test_client, get_device_bump_seed};

    use super::*;
    use doublezero_program_common::types::NetworkV4;
    use doublezero_sdk::{
        AccountData, AccountType, CurrentInterfaceVersion, DeviceType, InterfaceStatus,
        InterfaceType, LoopbackType, ResourceExtensionOwned, ResourceType,
    };
    use doublezero_serviceability::{
        id_allocator::IdAllocator,
        instructions::DoubleZeroInstruction,
        ip_allocator::IpAllocator,
        pda::get_resource_extension_pda,
        processors::device::{
            activate::DeviceActivateArgs,
            closeaccount::DeviceCloseAccountArgs,
            interface::{activate::DeviceInterfaceActivateArgs, unlink::DeviceInterfaceUnlinkArgs},
        },
        state::resource_extension::Allocator,
    };
    use metrics_util::debugging::DebuggingRecorder;
    use mockall::{predicate, Sequence};
    use solana_sdk::signature::Signature;
    use std::collections::HashMap;

    #[test]
    fn test_process_device_event_pending_to_deleted() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut devices = HashMap::new();
            let mut client = create_test_client();
            let mut segment_ids = IDAllocator::new(1, vec![]);

            let device_pubkey =
                Pubkey::from_str_const("8KvLQiyKgrK3KyVGVVyT1Pmg7ahPVFsvHUVPg97oYynV");
            let mut device = Device {
                account_type: AccountType::Device,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: get_device_bump_seed(&client),
                reference_count: 0,
                contributor_pk: Pubkey::new_unique(),
                location_pk: Pubkey::new_unique(),
                exchange_pk: Pubkey::new_unique(),
                device_type: DeviceType::Hybrid,
                public_ip: [192, 168, 1, 1].into(),
                status: DeviceStatus::Pending,
                metrics_publisher_pk: Pubkey::default(),
                code: "TestDevice".to_string(),
                dz_prefixes: "10.0.0.1/24,10.0.1.1/24".parse().unwrap(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![
                    CurrentInterfaceVersion {
                        status: InterfaceStatus::Pending,
                        name: "Ethernet0".to_string(),
                        interface_type: InterfaceType::Physical,
                        loopback_type: LoopbackType::None,
                        vlan_id: 0,
                        ip_net: NetworkV4::default(),
                        node_segment_idx: 0,
                        user_tunnel_endpoint: false,
                        ..Default::default()
                    }
                    .to_interface(),
                    CurrentInterfaceVersion {
                        status: InterfaceStatus::Pending,
                        name: "Loopback0".to_string(),
                        interface_type: InterfaceType::Loopback,
                        loopback_type: LoopbackType::Vpnv4,
                        vlan_id: 0,
                        ip_net: NetworkV4::default(),
                        node_segment_idx: 0,
                        user_tunnel_endpoint: false,
                        ..Default::default()
                    }
                    .to_interface(),
                ],
                max_users: 255,
                users_count: 0,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
                unicast_users_count: 0,
                multicast_users_count: 0,
                max_unicast_users: 0,
                max_multicast_users: 0,
            };

            let mut expected_interfaces = [
                device.interfaces[0].into_current_version().clone(),
                device.interfaces[1].into_current_version().clone(),
            ];
            expected_interfaces[1].ip_net = "1.1.1.1/32".parse().unwrap();
            expected_interfaces[1].node_segment_idx = 1;

            let device_clone = device.clone();
            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(device_pubkey))
                .returning(move |_| Ok(AccountData::Device(device_clone.clone())));

            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs {
                        resource_count: 3,
                    })),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut ip_block_allocator = IPBlockAllocator::new("1.1.1.0/24".parse().unwrap());

            process_device_event(
                &client,
                &device_pubkey,
                &mut devices,
                &device,
                &mut segment_ids,
                &mut ip_block_allocator,
                false,
            );

            assert!(devices.contains_key(&device_pubkey));
            assert_eq!(devices.get(&device_pubkey).unwrap().device, device);

            // UnlinkDeviceInterfaceCommand now looks up links to discover associated accounts
            client
                .expect_gets()
                .with(predicate::eq(AccountType::Link))
                .returning(|_| Ok(HashMap::new()));

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
                &mut segment_ids,
                &mut ip_block_allocator,
                false,
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
                        DeviceCloseAccountArgs { resource_count: 3 },
                    )),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let (resource1_pk, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::TunnelIds(device_pubkey, 0),
            );
            let resource1 = ResourceExtensionOwned {
                account_type: AccountType::ResourceExtension,
                owner: Pubkey::default(),
                bump_seed: 0,
                associated_with: device_pubkey,
                allocator: Allocator::Id(IdAllocator::new((1, 100)).unwrap()),
                storage: vec![],
            };

            let (resource2_pk, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::DzPrefixBlock(device_pubkey, 0),
            );
            let resource2 = ResourceExtensionOwned {
                account_type: AccountType::ResourceExtension,
                owner: Pubkey::default(),
                bump_seed: 0,
                associated_with: device_pubkey,
                allocator: Allocator::Ip(IpAllocator::new("0.0.0.0/0".parse().unwrap())),
                storage: vec![],
            };

            let (resource3_pk, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::DzPrefixBlock(device_pubkey, 1),
            );
            let resource3 = ResourceExtensionOwned {
                account_type: AccountType::ResourceExtension,
                owner: Pubkey::default(),
                bump_seed: 0,
                associated_with: device_pubkey,
                allocator: Allocator::Ip(IpAllocator::new("0.0.0.0/0".parse().unwrap())),
                storage: vec![],
            };

            client
                .expect_get()
                .with(predicate::in_hash(vec![
                    resource1_pk,
                    resource2_pk,
                    resource3_pk,
                ]))
                .returning(move |pk| {
                    if pk == resource1_pk {
                        Ok(AccountData::ResourceExtension(resource1.clone()))
                    } else if pk == resource2_pk {
                        Ok(AccountData::ResourceExtension(resource2.clone()))
                    } else if pk == resource3_pk {
                        Ok(AccountData::ResourceExtension(resource3.clone()))
                    } else {
                        Err(eyre::eyre!("Unexpected resource pk"))
                    }
                });

            process_device_event(
                &client,
                &device_pubkey,
                &mut devices,
                &device,
                &mut segment_ids,
                &mut ip_block_allocator,
                false,
            );
            assert!(!devices.contains_key(&device_pubkey));

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "device-pending-to-activated"),
                        ("device-pubkey", device_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "device-deleting-to-deactivated"),
                        ("device-pubkey", device_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
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
            device_type: DeviceType::Hybrid,
            public_ip: [192, 168, 1, 1].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![
                CurrentInterfaceVersion {
                    status: InterfaceStatus::Pending,
                    name: "Ethernet0".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    vlan_id: 0,
                    ip_net: NetworkV4::default(),
                    node_segment_idx: 0,
                    user_tunnel_endpoint: false,
                    ..Default::default()
                }
                .to_interface(),
                CurrentInterfaceVersion {
                    status: InterfaceStatus::Pending,
                    name: "Loopback0".to_string(),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::Vpnv4,
                    vlan_id: 0,
                    ip_net: NetworkV4::default(),
                    node_segment_idx: 0,
                    user_tunnel_endpoint: false,
                    ..Default::default()
                }
                .to_interface(),
            ],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_users_count: 0,
            max_unicast_users: 0,
            max_multicast_users: 0,
        };

        let mut ip_block_allocator = IPBlockAllocator::new("1.1.1.0/24".parse().unwrap());

        // UnlinkDeviceInterfaceCommand now looks up links to discover associated accounts
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Link))
            .returning(|_| Ok(HashMap::new()));

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
            &mut segment_ids,
            &mut ip_block_allocator,
            false,
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
            .map(|iface| iface.to_interface())
            .collect::<Vec<_>>();
        process_device_event(
            &client,
            &pubkey,
            &mut devices,
            &device,
            &mut segment_ids,
            &mut ip_block_allocator,
            false,
        );

        assert!(devices.contains_key(&pubkey));
        assert_eq!(devices.get(&pubkey).unwrap().device, device);
    }
}
