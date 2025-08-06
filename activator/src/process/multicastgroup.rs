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
    state_transitions: &mut HashMap<&'static str, usize>,
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

            let res = multicastgroup_tunnel_ips.next_available_block(0, 1);
            match res {
                Some(multicast_group) => {
                    let multicast_ip = multicast_group.ip();
                    write!(&mut log_msg, "multicast_ip: {multicast_ip} ",).unwrap();

                    let res = ActivateMulticastGroupCommand {
                        mgroup_pubkey: *pubkey,
                        multicast_ip,
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => {
                            write!(&mut log_msg, "Activated: {signature} ",).unwrap();

                            multicastgroups.insert(*pubkey, multicastgroup.clone());
                            *state_transitions
                                .entry("multicastgroup-pending-to-activated")
                                .or_insert(0) += 1;
                        }
                        Err(e) => {
                            write!(&mut log_msg, "Error: {e} ",).unwrap();
                        }
                    }
                }
                None => {
                    write!(&mut log_msg, "Error: No available multicast block",).unwrap();
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
                    *state_transitions
                        .entry("multicastgroup-deleting-to-deactivated")
                        .or_insert(0) += 1;
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
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::{collections::HashMap, net::Ipv4Addr};

    #[test]
    fn test_process_multicastgroup_event() {
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
            pub_allowlist: vec![client.get_payer()],
            sub_allowlist: vec![client.get_payer()],
            publishers: vec![],
            subscribers: vec![],
            status: MulticastGroupStatus::Pending,
            code: "test".to_string(),
            tenant_pk: Pubkey::default(),
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

        let mut multicastgroup_tunnel_ips = IPBlockAllocator::new("224.0.0.0/4".parse().unwrap());
        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        process_multicastgroup_event(
            &client,
            &pubkey,
            &multicastgroup,
            &mut multicastgroups,
            &mut multicastgroup_tunnel_ips,
            &mut state_transitions,
        )
        .expect("Failed to process multicastgroup event");

        assert!(multicastgroups.contains_key(&pubkey));
        assert_eq!(*multicastgroups.get(&pubkey).unwrap(), multicastgroup);
        assert_eq!(state_transitions["multicastgroup-pending-to-activated"], 1);
    }
}
