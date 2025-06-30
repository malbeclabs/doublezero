use doublezero_sdk::{
    commands::device::{activate::ActivateDeviceCommand, closeaccount::CloseAccountDeviceCommand},
    ipv4_to_string, networkv4_list_to_string, Device, DeviceStatus, DoubleZeroClient,
};
use solana_sdk::pubkey::Pubkey;
use std::collections::{hash_map::Entry, HashMap};

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
            print!("New Device {} ", device.code);

            let res = ActivateDeviceCommand {
                device_pubkey: *pubkey,
            }
            .execute(client);

            match res {
                Err(e) => println!("Error: {e}"),
                Ok(signature) => {
                    println!("Activated {signature}");

                    println!(
                        "Add Device: {} public_ip: {} dz_prefixes: {} ",
                        device.code,
                        ipv4_to_string(&device.public_ip),
                        networkv4_list_to_string(&device.dz_prefixes)
                    );
                    devices.insert(*pubkey, DeviceState::new(device));
                    *state_transitions
                        .entry("device-pending-to-activated")
                        .or_insert(0) += 1;
                }
            }
        }
        DeviceStatus::Activated => match devices.entry(*pubkey) {
            Entry::Occupied(mut entry) => entry.get_mut().update(device),
            Entry::Vacant(entry) => {
                println!(
                    "Add Device: {} public_ip: {} dz_prefixes: {} ",
                    device.code,
                    ipv4_to_string(&device.public_ip),
                    networkv4_list_to_string(&device.dz_prefixes)
                );
                entry.insert(DeviceState::new(device));
            }
        },
        DeviceStatus::Deleting => {
            print!("Deleting Device {} ", device.code);

            let res = CloseAccountDeviceCommand {
                pubkey: *pubkey,
                owner: device.owner,
            }
            .execute(client);

            match res {
                Err(e) => println!("Error: {e}"),
                Ok(signature) => {
                    println!("Deactivated {signature}");
                    devices.remove(pubkey);
                    *state_transitions
                        .entry("device-deleting-to-deactivated")
                        .or_insert(0) += 1;
                }
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
            public_ip: [192, 168, 1, 1],
            status: DeviceStatus::Pending,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: vec![([10, 0, 0, 1], 24), ([10, 0, 1, 1], 24)],
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
            public_ip: [192, 168, 1, 1],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: vec![([10, 0, 0, 1], 24)],
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

        device.dz_prefixes.push(([10, 0, 1, 1], 24));
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
