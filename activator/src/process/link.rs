use crate::{idallocator::IDAllocator, ipblockallocator::IPBlockAllocator};
use doublezero_sdk::{
    commands::link::{
        activate::ActivateLinkCommand, closeaccount::CloseAccountLinkCommand,
        reject::RejectLinkCommand,
    },
    DoubleZeroClient, Link, LinkStatus,
};
use log::info;
use solana_sdk::pubkey::Pubkey;
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

            let res = CloseAccountLinkCommand {
                pubkey: *pubkey,
                owner: link.owner,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    write!(&mut log_msg, " Deactivated {signature}").unwrap();

                    link_ids.unassign(link.tunnel_id);
                    link_ips.unassign_block(link.tunnel_net.into());

                    *state_transitions
                        .entry("tunnel-deleting-to-deactivated")
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
    use crate::{
        idallocator::IDAllocator,
        ipblockallocator::IPBlockAllocator,
        process::link::process_tunnel_event,
        tests::utils::{create_test_client, get_tunnel_bump_seed},
    };
    use doublezero_sdk::{AccountData, AccountType, Link, LinkLinkType, LinkStatus};
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        processors::link::{
            activate::LinkActivateArgs, closeaccount::LinkCloseAccountArgs, reject::LinkRejectArgs,
        },
        types::NetworkV4,
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
            .with(predicate::eq(tunnel_pubkey))
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
