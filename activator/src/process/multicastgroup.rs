use crate::ipblockallocator::IPBlockAllocator;
use doublezero_sdk::{
    commands::multicastgroup::{
        activate::ActivateMulticastGroupCommand, deactivate::DeactivateMulticastGroupCommand,
    },
    DoubleZeroClient, MulticastGroup, MulticastGroupStatus,
};
use eyre;
use ipnetwork::Ipv4Network;
use log::info;
use solana_sdk::pubkey::Pubkey;
use std::{
    collections::{hash_map::Entry, HashMap},
    fmt::Write,
};

pub fn process_multicastgroup_event(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    multicastgroup: &MulticastGroup,
    multicastgroups: &mut HashMap<Pubkey, MulticastGroup>,
    multicastgroup_tunnel_ips: &mut IPBlockAllocator,
    use_onchain_allocation: bool,
) -> eyre::Result<()> {
    match multicastgroup.status {
        MulticastGroupStatus::Pending => {
            let mut log_msg = String::new();
            write!(
                &mut log_msg,
                "Event:MulticastGroup(Pending) {} ({}) ",
                pubkey, multicastgroup.code
            )
            .unwrap();

            let multicast_ip = if use_onchain_allocation {
                // On-chain allocation: pass UNSPECIFIED, program allocates
                write!(&mut log_msg, "using on-chain allocation ").unwrap();
                std::net::Ipv4Addr::UNSPECIFIED
            } else {
                // Legacy: allocate locally
                match multicastgroup_tunnel_ips.next_available_block(0, 1) {
                    Some(block) => {
                        let ip = block.ip();
                        write!(&mut log_msg, "multicast_ip: {ip} ").unwrap();
                        ip
                    }
                    None => {
                        write!(&mut log_msg, "Error: No available multicast block").unwrap();
                        info!("{log_msg}");
                        return Ok(());
                    }
                }
            };

            let res = ActivateMulticastGroupCommand {
                mgroup_pubkey: *pubkey,
                multicast_ip,
                use_onchain_allocation,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    write!(&mut log_msg, "Activated: {signature} ").unwrap();

                    multicastgroups.insert(*pubkey, multicastgroup.clone());
                    metrics::counter!("doublezero_activator_state_transition", "state_transition" => "multicastgroup-pending-to-activated").increment(1);
                }
                Err(e) => {
                    write!(&mut log_msg, "Error: {e} ").unwrap();
                }
            }
            info!("{log_msg}");
        }
        MulticastGroupStatus::Activated => {
            if let Entry::Vacant(entry) = multicastgroups.entry(*pubkey) {
                info!("Add MulticastGroup: {} ", multicastgroup.code);

                entry.insert(multicastgroup.clone());
                multicastgroup_tunnel_ips
                    .assign_block(Ipv4Network::new(multicastgroup.multicast_ip, 32)?);
            }
        }
        MulticastGroupStatus::Deleting => {
            let mut log_msg = String::new();
            write!(
                &mut log_msg,
                "Event:MulticastGroup(Deleting) {} ({}) ",
                pubkey, multicastgroup.code
            )
            .unwrap();

            let res = DeactivateMulticastGroupCommand {
                pubkey: *pubkey,
                owner: multicastgroup.owner,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    write!(&mut log_msg, " Deactivated {signature}",).unwrap();

                    multicastgroup_tunnel_ips
                        .unassign_block(Ipv4Network::new(multicastgroup.multicast_ip, 32)?);

                    multicastgroups.remove(pubkey);
                    metrics::counter!("doublezero_activator_state_transition", "state_transition" => "multicastgroup-deleting-to-deactivated").increment(1);
                }
                Err(e) => {
                    write!(&mut log_msg, " Error {e}",).unwrap();
                }
            }
            info!("{log_msg}");
        }
        _ => {}
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use crate::{
        ipblockallocator::IPBlockAllocator, process::multicastgroup::process_multicastgroup_event,
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        get_multicastgroup_pda, AccountData, AccountType, DoubleZeroClient, MulticastGroup,
        MulticastGroupStatus,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        processors::multicastgroup::activate::MulticastGroupActivateArgs,
    };
    use metrics_util::debugging::DebuggingRecorder;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::{collections::HashMap, net::Ipv4Addr};

    #[test]
    fn test_process_multicastgroup_event() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut client = create_test_client();

            let (_, bump_seed) = get_multicastgroup_pda(&client.get_program_id(), 1);
            client
                .expect_execute_transaction()
                .with(
                    predicate::eq(DoubleZeroInstruction::ActivateMulticastGroup(
                        MulticastGroupActivateArgs {
                            multicast_ip: [224, 0, 0, 0].into(),
                        },
                    )),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut multicastgroups = HashMap::new();
            let pubkey = Pubkey::new_unique();
            let multicastgroup = MulticastGroup {
                account_type: AccountType::MulticastGroup,
                owner: Pubkey::new_unique(),
                index: 1,
                bump_seed,
                multicast_ip: Ipv4Addr::UNSPECIFIED,
                max_bandwidth: 10000,
                status: MulticastGroupStatus::Pending,
                code: "test".to_string(),
                tenant_pk: Pubkey::default(),
                publisher_count: 0,
                subscriber_count: 0,
            };

            let mgroup = multicastgroup.clone();
            client
                .expect_get()
                .with(predicate::eq(pubkey))
                .returning(move |_| Ok(AccountData::MulticastGroup(mgroup.clone())));

            client
                .expect_execute_transaction()
                .with(
                    predicate::eq(DoubleZeroInstruction::ActivateMulticastGroup(
                        MulticastGroupActivateArgs {
                            multicast_ip: [224, 0, 0, 0].into(),
                        },
                    )),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut multicastgroup_tunnel_ips =
                IPBlockAllocator::new("224.0.0.0/4".parse().unwrap());

            process_multicastgroup_event(
                &client,
                &pubkey,
                &multicastgroup,
                &mut multicastgroups,
                &mut multicastgroup_tunnel_ips,
                false, // legacy allocation mode for this test
            )
            .expect("Failed to process multicastgroup event");

            assert!(multicastgroups.contains_key(&pubkey));
            assert_eq!(*multicastgroups.get(&pubkey).unwrap(), multicastgroup);

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![("state_transition", "multicastgroup-pending-to-activated")],
                    1,
                )
                .verify();
        });
    }
}
