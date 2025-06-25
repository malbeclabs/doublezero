use crate::{idallocator::IDAllocator, ipblockallocator::IPBlockAllocator};
use doublezero_sdk::{
    commands::link::{
        activate::ActivateLinkCommand, closeaccount::CloseAccountLinkCommand,
        reject::RejectLinkCommand,
    },
    DoubleZeroClient, Link, LinkStatus,
};
use std::collections::HashMap;

pub fn process_tunnel_event(
    client: &dyn DoubleZeroClient,
    tunnel_tunnel_ips: &mut IPBlockAllocator,
    tunnel_tunnel_ids: &mut IDAllocator,
    tunnel: &Link,
    state_transitions: &mut HashMap<&'static str, usize>,
) {
    match tunnel.status {
        LinkStatus::Pending => {
            print!("New Link {} ", tunnel.code);

            match tunnel_tunnel_ips.next_available_block(0, 2) {
                Some(tunnel_net) => {
                    let tunnel_id = tunnel_tunnel_ids.next_available();

                    let res = ActivateLinkCommand {
                        index: tunnel.index,
                        tunnel_id,
                        tunnel_net,
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => {
                            println!("Activated {signature}");
                            *state_transitions
                                .entry("tunnel-pending-to-activated")
                                .or_insert(0) += 1;
                        }
                        Err(e) => println!("Error: activate_tunnel: {e}"),
                    }
                }
                None => {
                    println!("Error: No available tunnel block");

                    let res = RejectLinkCommand {
                        index: tunnel.index,
                        reason: "Error: No available tunnel block".to_string(),
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => {
                            println!("Rejected {signature}");
                            *state_transitions
                                .entry("tunnel-pending-to-rejected")
                                .or_insert(0) += 1;
                        }
                        Err(e) => println!("Error: reject_tunnel: {e}"),
                    }
                }
            }
        }
        LinkStatus::Deleting => {
            print!("Deleting Link {} ", tunnel.code);

            tunnel_tunnel_ids.unassign(tunnel.tunnel_id);
            tunnel_tunnel_ips.unassign_block(tunnel.tunnel_net);

            let res = CloseAccountLinkCommand {
                index: tunnel.index,
                owner: tunnel.owner,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    println!("Deactivated {signature}");
                    *state_transitions
                        .entry("tunnel-deleting-to-deactivated")
                        .or_insert(0) += 1;
                }
                Err(e) => println!("Error: deactivate_tunnel: {e}"),
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
    use doublezero_sdk::{AccountType, Link, LinkLinkType, LinkStatus};
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        processors::link::{
            activate::LinkActivateArgs, closeaccount::LinkCloseAccountArgs, reject::LinkRejectArgs,
        },
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::collections::HashMap;

    #[test]
    fn test_process_tunnel_event_pending_to_deleted() {
        let mut seq = Sequence::new();
        let mut tunnel_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 16));
        let mut tunnel_tunnel_ids = IDAllocator::new(500, vec![500, 501, 503]);
        let mut client = create_test_client();

        let tunnel = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_tunnel_bump_seed(&client),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::L3,
            bandwidth: 10_000_000_000,
            mtu: 1500,
            delay_ns: 100,
            jitter_ns: 100,
            tunnel_id: 1,
            tunnel_net: ([0, 0, 0, 0], 0),
            status: LinkStatus::Pending,
            ata_reward_owner_pk: Pubkey::default(),
            code: "TestLink".to_string(),
        };

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                    index: tunnel.index,
                    bump_seed: tunnel.bump_seed,
                    tunnel_id: 502,
                    tunnel_net: ([10, 0, 0, 0], 31),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        process_tunnel_event(
            &client,
            &mut tunnel_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &tunnel,
            &mut state_transitions,
        );

        assert!(tunnel_tunnel_ids.assigned.contains(&502_u16));
        assert!(tunnel_tunnel_ips.contains([10, 0, 0, 42]));

        let mut tunnel = tunnel.clone();
        tunnel.status = LinkStatus::Deleting;
        tunnel.tunnel_id = 502;
        tunnel.tunnel_net = ([10, 0, 0, 0], 31);

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::CloseAccountLink(
                    LinkCloseAccountArgs {
                        index: tunnel.index,
                        bump_seed: tunnel.bump_seed,
                    },
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let assigned_ips = tunnel_tunnel_ips.assigned_ips.clone();

        process_tunnel_event(
            &client,
            &mut tunnel_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &tunnel,
            &mut state_transitions,
        );

        assert!(!tunnel_tunnel_ids.assigned.contains(&502_u16));
        assert_ne!(tunnel_tunnel_ips.assigned_ips, assigned_ips);

        assert_eq!(state_transitions.len(), 2);
        assert_eq!(state_transitions["tunnel-pending-to-activated"], 1);
        assert_eq!(state_transitions["tunnel-deleting-to-deactivated"], 1);
    }

    #[test]
    fn test_process_tunnel_event_rejected() {
        let mut seq = Sequence::new();
        let mut tunnel_tunnel_ips = IPBlockAllocator::new(([10, 0, 0, 0], 32));
        let mut tunnel_tunnel_ids = IDAllocator::new(500, vec![500, 501, 503]);
        let mut client = create_test_client();

        let tunnel = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_tunnel_bump_seed(&client),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::L3,
            bandwidth: 10_000_000_000,
            mtu: 1500,
            delay_ns: 100,
            jitter_ns: 100,
            tunnel_id: 1,
            tunnel_net: ([0, 0, 0, 0], 0),
            ata_reward_owner_pk: Pubkey::default(),
            status: LinkStatus::Pending,
            code: "TestLink".to_string(),
        };

        let _ = tunnel_tunnel_ips.next_available_block(0, 2);

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::RejectLink(LinkRejectArgs {
                    index: tunnel.index,
                    bump_seed: tunnel.bump_seed,
                    reason: "Error: No available tunnel block".to_string(),
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        process_tunnel_event(
            &client,
            &mut tunnel_tunnel_ips,
            &mut tunnel_tunnel_ids,
            &tunnel,
            &mut state_transitions,
        );

        assert_eq!(state_transitions.len(), 1);
        assert_eq!(state_transitions["tunnel-pending-to-rejected"], 1);
    }
}
