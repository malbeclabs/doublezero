use crate::{idallocator::IDAllocator, ipblockallocator::IPBlockAllocator};
use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::{
    commands::link::{
        activate::ActivateLinkCommand, closeaccount::CloseAccountLinkCommand,
        reject::RejectLinkCommand,
    },
    DoubleZeroClient, Link, LinkStatus,
};
use log::info;
use solana_sdk::pubkey::Pubkey;
use std::fmt::Write;

/// Stateless variant of process_link_event for onchain allocation mode.
/// Does not use any local allocators - all allocation is handled by the smart contract.
pub fn process_link_event_stateless(client: &dyn DoubleZeroClient, pubkey: &Pubkey, link: &Link) {
    match link.status {
        LinkStatus::Pending => {
            let mut log_msg = String::new();
            write!(
                &mut log_msg,
                "Event:Link(Pending) {} ({}) ",
                pubkey, link.code
            )
            .unwrap();

            // On-chain allocation: pass zeros, smart contract allocates from ResourceExtension
            let res = ActivateLinkCommand {
                link_pubkey: *pubkey,
                side_a_pk: link.side_a_pk,
                side_z_pk: link.side_z_pk,
                tunnel_id: 0,
                tunnel_net: NetworkV4::default(),
                use_onchain_allocation: true,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    write!(&mut log_msg, " ReadyForService (onchain) {signature}").unwrap();

                    metrics::counter!(
                        "doublezero_activator_state_transition",
                        "state_transition" => "link-pending-to-activated",
                        "link-pubkey" => pubkey.to_string(),
                    )
                    .increment(1);
                }
                Err(e) => write!(&mut log_msg, " Error {e}").unwrap(),
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

            let res = CloseAccountLinkCommand {
                pubkey: *pubkey,
                owner: link.owner,
                use_onchain_deallocation: true,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    write!(&mut log_msg, " Deactivated (onchain) {signature}").unwrap();
                    // No local deallocation needed - onchain handles it

                    metrics::counter!(
                        "doublezero_activator_state_transition",
                        "state_transition" => "link-deleting-to-deactivated",
                        "link-pubkey" => pubkey.to_string(),
                    )
                    .increment(1);
                }
                Err(e) => write!(&mut log_msg, " Error {e}").unwrap(),
            }
        }
        _ => {}
    }
}

fn get_ip_block(link: &Link, link_ips: &mut IPBlockAllocator) -> Option<ipnetwork::Ipv4Network> {
    // if the link already has a tunnel net assigned, reuse that
    if link.tunnel_net != NetworkV4::default() {
        Some(link.tunnel_net.into())
    } else {
        link_ips.next_available_block(0, 2)
    }
}

fn get_link_id(link: &Link, link_ids: &mut IDAllocator) -> u16 {
    // if the link already has a tunnel id assigned, reuse that
    if link.tunnel_id != 0 {
        link.tunnel_id
    } else {
        link_ids.next_available()
    }
}

pub fn process_link_event(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    link_ips: &mut IPBlockAllocator,
    link_ids: &mut IDAllocator,
    link: &Link,
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

            // Off-chain allocation: allocator assigns tunnel_id and tunnel_net
            match get_ip_block(link, link_ips) {
                Some(link_net) => {
                    let link_id = get_link_id(link, link_ids);

                    let res = ActivateLinkCommand {
                        link_pubkey: *pubkey,
                        side_a_pk: link.side_a_pk,
                        side_z_pk: link.side_z_pk,
                        tunnel_id: link_id,
                        tunnel_net: link_net.into(),
                        use_onchain_allocation: false,
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => {
                            write!(&mut log_msg, " ReadyForService {signature}").unwrap();

                            metrics::counter!(
                                "doublezero_activator_state_transition",
                                "state_transition" => "link-pending-to-activated",
                                "link-pubkey" => pubkey.to_string(),
                            )
                            .increment(1);
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

                            metrics::counter!(
                                "doublezero_activator_state_transition",
                                "state_transition" => "link-pending-to-rejected",
                                "link-pubkey" => pubkey.to_string(),
                            )
                            .increment(1);
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

            let res = CloseAccountLinkCommand {
                pubkey: *pubkey,
                owner: link.owner,
                use_onchain_deallocation: false,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    write!(&mut log_msg, " Deactivated {signature}").unwrap();
                    // Off-chain: activator tracks local allocations
                    link_ids.unassign(link.tunnel_id);
                    link_ips.unassign_block(link.tunnel_net.into());

                    metrics::counter!(
                        "doublezero_activator_state_transition",
                        "state_transition" => "link-deleting-to-deactivated",
                        "link-pubkey" => pubkey.to_string(),
                    )
                    .increment(1);
                }
                Err(e) => write!(&mut log_msg, " Error {e}").unwrap(),
            }
        }
        _ => {}
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        idallocator::IDAllocator,
        ipblockallocator::IPBlockAllocator,
        tests::utils::{create_test_client, get_tunnel_bump_seed},
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_sdk::{AccountData, AccountType, Link, LinkLinkType, LinkStatus};
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        processors::link::{
            activate::LinkActivateArgs, closeaccount::LinkCloseAccountArgs, reject::LinkRejectArgs,
        },
    };
    use metrics_util::debugging::DebuggingRecorder;
    use mockall::{predicate, Sequence};
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_process_link_event_pending_to_deleted() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut link_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
            let mut link_ids = IDAllocator::new(500, vec![500, 501, 503]);
            let mut client = create_test_client();

            let owner_pubkey = Pubkey::new_unique();
            let device1_pubkey = Pubkey::new_unique();
            let device2_pubkey = Pubkey::new_unique();

            let tunnel_pubkey = Pubkey::new_unique();
            let tunnel = Link {
                account_type: AccountType::Link,
                owner: owner_pubkey,
                index: 0,
                bump_seed: get_tunnel_bump_seed(&client),
                contributor_pk: Pubkey::new_unique(),
                side_a_pk: device1_pubkey,
                side_z_pk: device2_pubkey,
                link_type: LinkLinkType::WAN,
                bandwidth: 10_000_000_000,
                mtu: 1500,
                delay_ns: 20_000,
                jitter_ns: 100,
                delay_override_ns: 0,
                tunnel_id: 0,
                tunnel_net: NetworkV4::default(),
                status: LinkStatus::Pending,
                code: "TestLink".to_string(),
                side_a_iface_name: "Ethernet0".to_string(),
                side_z_iface_name: "Ethernet1".to_string(),
                link_health: doublezero_serviceability::state::link::LinkHealth::Pending,
                desired_status:
                    doublezero_serviceability::state::link::LinkDesiredStatus::Activated,
            };

            let tunnel_cloned = tunnel.clone();
            client
                .expect_get()
                .with(predicate::eq(tunnel_pubkey))
                .times(1)
                .returning(move |_| Ok(AccountData::Link(tunnel_cloned.clone())));

            client
                .expect_execute_transaction()
                .with(
                    predicate::eq(DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                        tunnel_id: 502,
                        tunnel_net: "10.0.0.0/31".parse().unwrap(),
                        use_onchain_allocation: false,
                    })),
                    predicate::always(),
                )
                .times(1)
                .returning(|_, _| Ok(Signature::new_unique()));

            process_link_event(
                &client,
                &tunnel_pubkey,
                &mut link_ips,
                &mut link_ids,
                &tunnel,
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
                .withf(move |pk| *pk == tunnel_pubkey)
                .times(1)
                .returning(move |_| Ok(AccountData::Link(tunnel2.clone())));

            client
                .expect_execute_transaction()
                .with(
                    predicate::eq(DoubleZeroInstruction::CloseAccountLink(
                        LinkCloseAccountArgs {
                            use_onchain_deallocation: false,
                        },
                    )),
                    predicate::always(),
                )
                .times(1)
                .returning(|_, _| Ok(Signature::new_unique()));

            let assigned_ips = link_ips.assigned_ips.clone();

            process_link_event(
                &client,
                &tunnel_pubkey,
                &mut link_ips,
                &mut link_ids,
                &tunnel,
            );

            assert!(!link_ids.assigned.contains(&502_u16));
            assert_ne!(link_ips.assigned_ips, assigned_ips);

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "link-pending-to-activated"),
                        ("link-pubkey", tunnel_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "link-deleting-to-deactivated"),
                        ("link-pubkey", tunnel_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    #[test]
    fn test_process_link_event_pending_reuse_ip() {
        let mut link_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
        let mut link_ids = IDAllocator::new(500, vec![500, 501, 503]);
        let mut client = create_test_client();

        let owner_pubkey = Pubkey::new_unique();
        let device1_pubkey = Pubkey::new_unique();
        let device2_pubkey = Pubkey::new_unique();

        let link_pubkey = Pubkey::new_unique();
        let link = Link {
            account_type: AccountType::Link,
            owner: owner_pubkey,
            index: 0,
            bump_seed: get_tunnel_bump_seed(&client),
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: device1_pubkey,
            side_z_pk: device2_pubkey,
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 1500,
            delay_ns: 20_000,
            jitter_ns: 100,
            delay_override_ns: 0,
            tunnel_id: 1,
            tunnel_net: "10.1.2.0/31".parse().unwrap(),
            status: LinkStatus::Pending,
            code: "TestLink".to_string(),
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: "Ethernet1".to_string(),
            link_health: doublezero_serviceability::state::link::LinkHealth::Pending,
            desired_status: doublezero_serviceability::state::link::LinkDesiredStatus::Activated,
        };

        let link_cloned = link.clone();
        client
            .expect_get()
            .with(predicate::eq(link_pubkey))
            .times(1)
            .returning(move |_| Ok(AccountData::Link(link_cloned.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                    tunnel_id: 1,
                    tunnel_net: "10.1.2.0/31".parse().unwrap(),
                    use_onchain_allocation: false,
                })),
                predicate::always(),
            )
            .times(1)
            .returning(|_, _| Ok(Signature::new_unique()));

        process_link_event(&client, &link_pubkey, &mut link_ips, &mut link_ids, &link);
    }

    #[test]
    fn test_process_link_event_rejected() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
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
                link_type: LinkLinkType::WAN,
                bandwidth: 10_000_000_000,
                mtu: 1500,
                delay_ns: 20_000,
                jitter_ns: 100,
                delay_override_ns: 0,
                tunnel_id: 1,
                tunnel_net: NetworkV4::default(),
                status: LinkStatus::Pending,
                code: "TestLink".to_string(),
                side_a_iface_name: "Ethernet0".to_string(),
                side_z_iface_name: "Ethernet1".to_string(),
                link_health: doublezero_serviceability::state::link::LinkHealth::Pending,
                desired_status:
                    doublezero_serviceability::state::link::LinkDesiredStatus::Activated,
            };

            let tunnel_clone = tunnel.clone();
            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(tunnel_pubkey))
                .returning(move |_| Ok(AccountData::Link(tunnel_clone.clone())));

            // Note: device_z is not fetched for Pending status links (only for Requested)

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

            process_link_event(
                &client,
                &tunnel_pubkey,
                &mut link_ips,
                &mut link_ids,
                &tunnel,
            );

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "link-pending-to-rejected"),
                        ("link-pubkey", tunnel_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    // Tests for process_link_event_stateless

    #[test]
    fn test_process_link_event_stateless_pending_to_activated() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut client = create_test_client();

            let owner_pubkey = Pubkey::new_unique();
            let device1_pubkey = Pubkey::new_unique();
            let device2_pubkey = Pubkey::new_unique();

            let link_pubkey = Pubkey::new_unique();
            let link = Link {
                account_type: AccountType::Link,
                owner: owner_pubkey,
                index: 0,
                bump_seed: get_tunnel_bump_seed(&client),
                contributor_pk: Pubkey::new_unique(),
                side_a_pk: device1_pubkey,
                side_z_pk: device2_pubkey,
                link_type: LinkLinkType::WAN,
                bandwidth: 10_000_000_000,
                mtu: 1500,
                delay_ns: 20_000,
                jitter_ns: 100,
                delay_override_ns: 0,
                tunnel_id: 0,
                tunnel_net: NetworkV4::default(),
                status: LinkStatus::Pending,
                code: "TestLink".to_string(),
                side_a_iface_name: "Ethernet0".to_string(),
                side_z_iface_name: "Ethernet1".to_string(),
                link_health: doublezero_serviceability::state::link::LinkHealth::Pending,
                desired_status:
                    doublezero_serviceability::state::link::LinkDesiredStatus::Activated,
            };

            // SDK command fetches the link internally
            let link_clone = link.clone();
            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(link_pubkey))
                .returning(move |_| Ok(AccountData::Link(link_clone.clone())));

            // Stateless mode: tunnel_id=0, tunnel_net=default, use_onchain_allocation=true
            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                        tunnel_id: 0,
                        tunnel_net: NetworkV4::default(),
                        use_onchain_allocation: true,
                    })),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            super::process_link_event_stateless(&client, &link_pubkey, &link);

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "link-pending-to-activated"),
                        ("link-pubkey", link_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    #[test]
    fn test_process_link_event_stateless_deleting() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut client = create_test_client();

            let owner_pubkey = Pubkey::new_unique();
            let device1_pubkey = Pubkey::new_unique();
            let device2_pubkey = Pubkey::new_unique();

            let link_pubkey = Pubkey::new_unique();
            let link = Link {
                account_type: AccountType::Link,
                owner: owner_pubkey,
                index: 0,
                bump_seed: get_tunnel_bump_seed(&client),
                contributor_pk: Pubkey::new_unique(),
                side_a_pk: device1_pubkey,
                side_z_pk: device2_pubkey,
                link_type: LinkLinkType::WAN,
                bandwidth: 10_000_000_000,
                mtu: 1500,
                delay_ns: 20_000,
                jitter_ns: 100,
                delay_override_ns: 0,
                tunnel_id: 502,
                tunnel_net: "10.0.0.0/31".parse().unwrap(),
                status: LinkStatus::Deleting,
                code: "TestLink".to_string(),
                side_a_iface_name: "Ethernet0".to_string(),
                side_z_iface_name: "Ethernet1".to_string(),
                link_health: doublezero_serviceability::state::link::LinkHealth::Pending,
                desired_status:
                    doublezero_serviceability::state::link::LinkDesiredStatus::Activated,
            };

            // SDK command fetches the link internally
            let link_clone = link.clone();
            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(link_pubkey))
                .returning(move |_| Ok(AccountData::Link(link_clone.clone())));

            // Stateless mode: use_onchain_deallocation=true
            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::CloseAccountLink(
                        LinkCloseAccountArgs {
                            use_onchain_deallocation: true,
                        },
                    )),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            super::process_link_event_stateless(&client, &link_pubkey, &link);

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "link-deleting-to-deactivated"),
                        ("link-pubkey", link_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }
}
