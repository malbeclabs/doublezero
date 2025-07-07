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
        DeviceStatus::Activated => match devices.entry(*pubkey) {
            Entry::Occupied(mut entry) => entry.get_mut().update(device),
            Entry::Vacant(entry) => {
                info!(
                    "Add Device: {} public_ip: {} dz_prefixes: {} ",
                    device.code, &device.public_ip, &device.dz_prefixes,
                );
                entry.insert(DeviceState::new(device));
            }
        },
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
    use doublezero_sdk::{AccountType, DeviceType};
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        processors::device::{activate::DeviceActivateArgs, closeaccount::DeviceCloseAccountArgs},
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::signature::Signature;
    use std::collections::HashMap;

    #[test]
    fn test_process_device_event_pending_to_deleted() {
        let mut seq = Sequence::new();
        let mut devices = HashMap::new();
        let mut client = create_test_client();

        let device_pubkey = Pubkey::new_unique();
        let mut device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 1].into(),
            status: DeviceStatus::Pending,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: "10.0.0.1/24,10.0.1.1/24".parse().unwrap(),
            bgp_asn: 0,
            dia_bgp_asn: 0,
            mgmt_vrf: "default".to_string(),
            dns_servers: vec![[8, 8, 8, 8].into(), [8, 8, 4, 4].into()],
            ntp_servers: vec![[192, 168, 1, 1].into(), [192, 168, 1, 2].into()],
            interfaces: vec![],
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

        process_device_event(
            &client,
            &device_pubkey,
            &mut devices,
            &device,
            &mut state_transitions,
        );

        assert!(devices.contains_key(&device_pubkey));
        assert_eq!(devices.get(&device_pubkey).unwrap().device, device);

        device.status = DeviceStatus::Deleting;

        let mut client = create_test_client();
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
        );
        assert!(!devices.contains_key(&device_pubkey));
        assert_eq!(state_transitions.len(), 2);
        assert_eq!(state_transitions["device-pending-to-activated"], 1);
        assert_eq!(state_transitions["device-deleting-to-deactivated"], 1);
    }

    #[test]
    fn test_process_device_event_activated() {
        let mut devices = HashMap::new();
        let client = create_test_client();
        let pubkey = Pubkey::new_unique();

        let mut device = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: get_device_bump_seed(&client),
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [192, 168, 1, 1].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            bgp_asn: 0,
            dia_bgp_asn: 0,
            mgmt_vrf: "default".to_string(),
            dns_servers: vec![[8, 8, 8, 8].into(), [8, 8, 4, 4].into()],
            ntp_servers: vec![[192, 168, 1, 1].into(), [192, 168, 1, 2].into()],
            interfaces: vec![],
        };

        let mut state_transitions: HashMap<&'static str, usize> = HashMap::new();

        process_device_event(
            &client,
            &pubkey,
            &mut devices,
            &device,
            &mut state_transitions,
        );

        assert!(devices.contains_key(&pubkey));
        assert_eq!(devices.get(&pubkey).unwrap().device, device);

        device.dz_prefixes.push("10.0.1.1/24".parse().unwrap());
        process_device_event(
            &client,
            &pubkey,
            &mut devices,
            &device,
            &mut state_transitions,
        );

        assert!(devices.contains_key(&pubkey));
        assert_eq!(devices.get(&pubkey).unwrap().device, device);

        assert_eq!(state_transitions.len(), 0);
    }
}
