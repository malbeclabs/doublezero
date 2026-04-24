use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::{
    commands::{device::list::ListDeviceCommand, metro::list::ListMetroCommand},
    MetroStatus,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListMetroCliCommand {
    /// Output in JSON format
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output in compact JSON format
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct MetroDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub name: String,
    pub device1: String,
    pub device2: String,
    pub lat: f64,
    pub lng: f64,
    pub bgp_community: u16,
    pub status: MetroStatus,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListMetroCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let metros = client.list_metro(ListMetroCommand)?;

        let devices = client.list_device(ListDeviceCommand)?;

        let mut metro_displays: Vec<MetroDisplay> = metros
            .into_iter()
            .map(|(pubkey, tunnel)| MetroDisplay {
                account: pubkey,
                code: tunnel.code,
                name: tunnel.name,
                device1: {
                    if tunnel.device1_pk == Pubkey::default() {
                        "(none)".to_string()
                    } else {
                        devices
                            .get(&tunnel.device1_pk)
                            .map_or_else(|| tunnel.device1_pk.to_string(), |d| d.code.clone())
                    }
                },
                device2: {
                    if tunnel.device2_pk == Pubkey::default() {
                        "(none)".to_string()
                    } else {
                        devices
                            .get(&tunnel.device2_pk)
                            .map_or_else(|| tunnel.device2_pk.to_string(), |d| d.code.clone())
                    }
                },
                lat: tunnel.lat,
                lng: tunnel.lng,
                bgp_community: tunnel.bgp_community,
                status: tunnel.status,
                owner: tunnel.owner,
            })
            .collect();

        metro_displays.sort_by(|a, b| a.code.cmp(&b.code));

        let res = if self.json {
            serde_json::to_string_pretty(&metro_displays)?
        } else if self.json_compact {
            serde_json::to_string(&metro_displays)?
        } else {
            Table::new(metro_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        metro::list::{ListMetroCliCommand, MetroStatus::Activated},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{AccountType, Device, DeviceStatus, DeviceType, Metro};
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_cli_metro_list() {
        let mut client = create_test_client();

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let facility1_pubkey = Pubkey::new_unique();
        let facility2_pubkey = Pubkey::new_unique();
        let metro1_pubkey = Pubkey::new_unique();
        let metro2_pubkey = Pubkey::new_unique();

        let device1_pubkey = Pubkey::new_unique();
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "device1_code".to_string(),
            contributor_pk,
            facility_pk: facility1_pubkey,
            metro_pk: metro1_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
        };
        let device2_pubkey = Pubkey::new_unique();
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "device2_code".to_string(),
            contributor_pk,
            facility_pk: facility2_pubkey,
            metro_pk: metro2_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
        };

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            devices.insert(device2_pubkey, device2.clone());
            Ok(devices)
        });

        let metro1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let metro1 = Metro {
            account_type: AccountType::Metro,
            owner: metro1_pubkey,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 15.00,
            lng: 15.00,
            bgp_community: 6,
            unused: 0,
            status: Activated,
            code: "some code".to_string(),
            name: "some name".to_string(),
        };

        client.expect_list_metro().returning(move |_| {
            let mut metros = HashMap::new();
            metros.insert(metro1_pubkey, metro1.clone());
            Ok(metros)
        });

        let mut output = Vec::new();
        let res = ListMetroCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code      | name      | device1 | device2 | lat | lng | bgp_community | status    | owner                                     \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | some code | some name | (none)  | (none)  | 15  | 15  | 6             | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n");

        let mut output = Vec::new();
        let res = ListMetroCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\",\"code\":\"some code\",\"name\":\"some name\",\"device1\":\"(none)\",\"device2\":\"(none)\",\"lat\":15.0,\"lng\":15.0,\"bgp_community\":6,\"status\":\"Activated\",\"owner\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\"}]\n");
    }
}
