use crate::{idallocator::IDAllocator, ipblockallocator::IPBlockAllocator};
use doublezero_sdk::{
    commands::{
        device::{get::GetDeviceCommand, update::UpdateDeviceCommand},
        link::{
            activate::ActivateLinkCommand, closeaccount::CloseAccountLinkCommand,
            reject::RejectLinkCommand,
        },
    },
    DoubleZeroClient, Link, LinkStatus,
};
use ipnetwork::Ipv4Network;
use log::info;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::{collections::HashMap, fmt::Write};

pub fn process_tunnel_event(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    link_ips: &mut IPBlockAllocator,
    link_ids: &mut IDAllocator,
    link: &Link,
    state_transitions: &mut HashMap<&'static str, usize>,
) {
    match link.status {
        LinkStatus::Pending => {
            let mut log_msg = String::new();
            write!(
                &mut log_msg,
                "Event:Link(Pending) {} ({}) ",
                pubkey, link.code
            )
            .unwrap();

            match link_ips.next_available_block(0, 2) {
                Some(tunnel_net) => {
                    let tunnel_id = link_ids.next_available();

                    let res = ActivateLinkCommand {
                        link_pubkey: *pubkey,
                        tunnel_id,
                        tunnel_net: tunnel_net.into(),
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => {
                            write!(&mut log_msg, " Activated {signature}").unwrap();

                            *state_transitions
                                .entry("tunnel-pending-to-activated")
                                .or_insert(0) += 1;

                            // get the first and second ips in the network, but don't throw away
                            // the netmask/prefix. unwraps are safe below because we already have
                            // checked that we have a valid tunnel_net
                            let side_a_ip =
                                Ipv4Network::new(tunnel_net.nth(0).unwrap(), tunnel_net.prefix())
                                    .unwrap();
                            let side_z_ip =
                                Ipv4Network::new(tunnel_net.nth(1).unwrap(), tunnel_net.prefix())
                                    .unwrap();

                            if let Err(e) = assign_ip_to_dev_interface(
                                client,
                                side_a_ip,
                                &link.side_a_pk,
                                &link.side_a_iface_name,
                            ) {
                                write!(&mut log_msg, " Error assigning side A IP: {e}").unwrap();
                            } else {
                                write!(&mut log_msg, " Assigned side A IP: {side_a_ip}").unwrap();
                            }

                            if let Err(e) = assign_ip_to_dev_interface(
                                client,
                                side_z_ip,
                                &link.side_z_pk,
                                &link.side_z_iface_name,
                            ) {
                                write!(&mut log_msg, " Error assigning side Z IP: {e}").unwrap();
                            } else {
                                write!(&mut log_msg, " Assigned side Z IP: {side_z_ip}").unwrap();
                            }
                        }
                        Err(e) => write!(&mut log_msg, " Error {e}").unwrap(),
                    }
                }
                None => {
                    write!(&mut log_msg, " Error: No available tunnel block").unwrap();

                    let res = RejectLinkCommand {
                        pubkey: *pubkey,
                        reason: "Error: No available tunnel block".to_string(),
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => {
                            write!(&mut log_msg, " Rejected {signature}").unwrap();

                            *state_transitions
                                .entry("tunnel-pending-to-rejected")
                                .or_insert(0) += 1;
                        }
                        Err(e) => write!(&mut log_msg, " Error {e}").unwrap(),
                    }
                }
            }
            info!("{log_msg}");
        }
        LinkStatus::Deleting => {
            let mut log_msg = String::new();
            write!(
                &mut log_msg,
                "Event:Link(Deleting) {} ({}) ",
                pubkey, link.code
            )
            .unwrap();

            link_ids.unassign(link.tunnel_id);
            link_ips.unassign_block(link.tunnel_net.into());

            let res = CloseAccountLinkCommand {
                pubkey: *pubkey,
                owner: link.owner,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    write!(&mut log_msg, " Deactivated {signature}").unwrap();

                    *state_transitions
                        .entry("tunnel-deleting-to-deactivated")
                        .or_insert(0) += 1;

                    let zero_ip = "0.0.0.0/0".parse().unwrap();

                    if let Err(e) = assign_ip_to_dev_interface(
                        client,
                        zero_ip,
                        &link.side_a_pk,
                        &link.side_a_iface_name,
                    ) {
                        write!(&mut log_msg, " Error assigning side A IP to {zero_ip}: {e}")
                            .unwrap();
                    }

                    if let Err(e) = assign_ip_to_dev_interface(
                        client,
                        zero_ip,
                        &link.side_z_pk,
                        &link.side_z_iface_name,
                    ) {
                        write!(&mut log_msg, " Error assigning side Z IP to {zero_ip}: {e}")
                            .unwrap();
                    }
                }
                Err(e) => write!(&mut log_msg, " Error {e}").unwrap(),
            }
        }
        _ => {}
    }
}

fn assign_ip_to_dev_interface(
    client: &dyn DoubleZeroClient,
    ip_net: Ipv4Network,
    dev: &Pubkey,
    iface_name: &str,
) -> eyre::Result<Signature> {
    let (pubkey, device) = GetDeviceCommand {
        pubkey_or_code: dev.to_string(),
    }
    .execute(client)?;

    let mut interfaces = device.interfaces.clone();
    if let Some(iface) = interfaces.iter_mut().find(|i| i.name == iface_name) {
        iface.ip_net = ip_net.into();
    } else {
        return Err(eyre::eyre!(
            "Interface {} not found on device {}",
            iface_name,
            pubkey
        ));
    }

    UpdateDeviceCommand {
        pubkey: *dev,
        code: None,
        device_type: None,
        public_ip: None,
        dz_prefixes: None,
        metrics_publisher: None,
        contributor_pk: None,
        bgp_asn: None,
        dia_bgp_asn: None,
        mgmt_vrf: None,
        dns_servers: None,
        ntp_servers: None,
        interfaces: Some(interfaces),
    }
    .execute(client)
}

#[cfg(test)]
mod tests {
    use crate::{
        idallocator::IDAllocator,
        ipblockallocator::IPBlockAllocator,
        process::link::process_tunnel_event,
        tests::utils::{create_test_client, get_device_bump_seed, get_tunnel_bump_seed},
    };
    use doublezero_sdk::{
        AccountData, AccountType, Device, DeviceStatus, DeviceType, Interface, InterfaceType, Link,
        LinkLinkType, LinkStatus, LoopbackType, NetworkV4, NetworkV4List,
        CURRENT_INTERFACE_VERSION,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        processors::{
            device::update::DeviceUpdateArgs,
            link::{
                activate::LinkActivateArgs, closeaccount::LinkCloseAccountArgs,
                reject::LinkRejectArgs,
            },
        },
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::collections::HashMap;

    #[test]
    fn test_process_tunnel_event_pending_to_deleted() {
        let mut seq = Sequence::new();
        let mut link_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
        let mut link_ids = IDAllocator::new(500, vec![500, 501, 503]);
        let mut client = create_test_client();

        let tunnel_pubkey = Pubkey::new_unique();
        let tunnel = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_tunnel_bump_seed(&client),
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::L3,
            bandwidth: 10_000_000_000,
            mtu: 1500,
            delay_ns: 100,
            jitter_ns: 100,
            tunnel_id: 1,
            tunnel_net: NetworkV4::default(),
            status: LinkStatus::Pending,
            code: "TestLink".to_string(),
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
        };

        let device1 = Device {
            account_type: AccountType::Device,
            owner: tunnel.owner,
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: "1.2.3.4".parse().unwrap(),
            status: DeviceStatus::Activated,
            code: "Device1".to_string(),
            dz_prefixes: NetworkV4List::default(),
            metrics_publisher_pk: Pubkey::new_unique(),
            contributor_pk: tunnel.contributor_pk,
            bgp_asn: 65001,
            dia_bgp_asn: 65002,
            mgmt_vrf: "mgmt".to_string(),
            dns_servers: vec![],
            ntp_servers: vec![],
            interfaces: vec![
                Interface {
                    version: CURRENT_INTERFACE_VERSION,
                    name: tunnel.side_a_iface_name.clone(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    vlan_id: 0,
                    ip_net: NetworkV4::default(),
                    node_segment_idx: 0,
                    user_tunnel_endpoint: false,
                },
                Interface {
                    version: CURRENT_INTERFACE_VERSION,
                    name: "lo0".to_string(),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::Vpnv4,
                    vlan_id: 0,
                    ip_net: NetworkV4::default(),
                    node_segment_idx: 0,
                    user_tunnel_endpoint: false,
                },
            ],
            reference_count: 0,
        };

        let mut device2 = device1.clone();
        device2.public_ip = "1.2.3.5".parse().unwrap();
        device2.code = "Device2".to_string();
        device2.interfaces[0].name = tunnel.side_z_iface_name.clone();

        let mut expected_interfaces1 = device1.interfaces.clone();
        let mut expected_interfaces2 = device2.interfaces.clone();
        expected_interfaces1[0].ip_net = "10.0.0.0/31".parse().unwrap();
        expected_interfaces2[0].ip_net = "10.0.0.1/31".parse().unwrap();

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                    tunnel_id: 502,
                    tunnel_net: "10.0.0.0/31".parse().unwrap(),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let dev1 = device1.clone();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(tunnel.side_a_pk))
            .returning(move |_| Ok(AccountData::Device(dev1.clone())));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                    code: None,
                    device_type: None,
                    public_ip: None,
                    dz_prefixes: None,
                    metrics_publisher_pk: None,
                    contributor_pk: None,
                    bgp_asn: None,
                    dia_bgp_asn: None,
                    mgmt_vrf: None,
                    dns_servers: None,
                    ntp_servers: None,
                    interfaces: Some(expected_interfaces1.clone()),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let dev2 = device2.clone();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(tunnel.side_z_pk))
            .returning(move |_| Ok(AccountData::Device(dev2.clone())));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                    code: None,
                    device_type: None,
                    public_ip: None,
                    dz_prefixes: None,
                    metrics_publisher_pk: None,
                    contributor_pk: None,
                    bgp_asn: None,
                    dia_bgp_asn: None,
                    mgmt_vrf: None,
                    dns_servers: None,
                    ntp_servers: None,
                    interfaces: Some(expected_interfaces2.clone()),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        process_tunnel_event(
            &client,
            &tunnel_pubkey,
            &mut link_ips,
            &mut link_ids,
            &tunnel,
            &mut state_transitions,
        );

        assert!(link_ids.assigned.contains(&502_u16));
        assert!(link_ips.contains("10.0.0.42".parse().unwrap()));

        let mut tunnel = tunnel.clone();
        tunnel.status = LinkStatus::Deleting;
        tunnel.tunnel_id = 502;
        tunnel.tunnel_net = "10.0.0.0/31".parse().unwrap();

        let tunnel2 = tunnel.clone();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .withf(move |pk| *pk == tunnel_pubkey)
            .returning(move |_| Ok(AccountData::Link(tunnel2.clone())));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::CloseAccountLink(
                    LinkCloseAccountArgs {},
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        expected_interfaces1[0].ip_net = "0.0.0.0/0".parse().unwrap();
        expected_interfaces2[0].ip_net = "0.0.0.0/0".parse().unwrap();

        let dev1 = device1.clone();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(tunnel.side_a_pk))
            .returning(move |_| Ok(AccountData::Device(dev1.clone())));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                    code: None,
                    device_type: None,
                    public_ip: None,
                    dz_prefixes: None,
                    metrics_publisher_pk: None,
                    contributor_pk: None,
                    bgp_asn: None,
                    dia_bgp_asn: None,
                    mgmt_vrf: None,
                    dns_servers: None,
                    ntp_servers: None,
                    interfaces: Some(expected_interfaces1.clone()),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let dev2 = device2.clone();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(tunnel.side_z_pk))
            .returning(move |_| Ok(AccountData::Device(dev2.clone())));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                    code: None,
                    device_type: None,
                    public_ip: None,
                    dz_prefixes: None,
                    metrics_publisher_pk: None,
                    contributor_pk: None,
                    bgp_asn: None,
                    dia_bgp_asn: None,
                    mgmt_vrf: None,
                    dns_servers: None,
                    ntp_servers: None,
                    interfaces: Some(expected_interfaces2.clone()),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let assigned_ips = link_ips.assigned_ips.clone();

        process_tunnel_event(
            &client,
            &tunnel_pubkey,
            &mut link_ips,
            &mut link_ids,
            &tunnel,
            &mut state_transitions,
        );

        assert!(!link_ids.assigned.contains(&502_u16));
        assert_ne!(link_ips.assigned_ips, assigned_ips);

        assert_eq!(state_transitions.len(), 2);
        assert_eq!(state_transitions["tunnel-pending-to-activated"], 1);
        assert_eq!(state_transitions["tunnel-deleting-to-deactivated"], 1);
    }

    #[test]
    fn test_process_tunnel_event_rejected() {
        let mut seq = Sequence::new();
        let mut link_ips = IPBlockAllocator::new("10.0.0.0/32".parse().unwrap());
        let mut link_ids = IDAllocator::new(500, vec![500, 501, 503]);
        let mut client = create_test_client();

        let tunnel_pubkey = Pubkey::new_unique();
        let tunnel = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_tunnel_bump_seed(&client),
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::L3,
            bandwidth: 10_000_000_000,
            mtu: 1500,
            delay_ns: 100,
            jitter_ns: 100,
            tunnel_id: 1,
            tunnel_net: NetworkV4::default(),
            status: LinkStatus::Pending,
            code: "TestLink".to_string(),
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
        };

        let _ = link_ips.next_available_block(0, 2);

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::RejectLink(LinkRejectArgs {
                    reason: "Error: No available tunnel block".to_string(),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        process_tunnel_event(
            &client,
            &tunnel_pubkey,
            &mut link_ips,
            &mut link_ids,
            &tunnel,
            &mut state_transitions,
        );

        assert_eq!(state_transitions.len(), 1);
        assert_eq!(state_transitions["tunnel-pending-to-rejected"], 1);
    }
}
