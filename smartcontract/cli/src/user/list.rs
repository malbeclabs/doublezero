use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::multicastgroup::list::ListMulticastGroupCommand;
use doublezero_sdk::commands::{
    device::list::ListDeviceCommand, location::list::ListLocationCommand,
    user::list::ListUserCommand,
};
use doublezero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListUserCliCommand {
    #[arg(long, default_value_t = false)]
    pub json: bool,
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Serialize)]
pub struct UserDisplay {
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub user_type: UserType,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub device_pk: Pubkey,
    pub multicast: String,
    pub publishers: Vec<String>,
    pub subscribers: Vec<String>,
    pub device_name: String,
    pub location_code: String,
    pub location_name: String,
    pub cyoa_type: UserCYOA,
    #[serde(serialize_with = "crate::serializer::serialize_ipv4_as_string")]
    pub client_ip: IpV4,
    #[serde(serialize_with = "crate::serializer::serialize_ipv4_as_string")]
    pub dz_ip: IpV4,
    pub tunnel_id: u16,
    #[serde(serialize_with = "crate::serializer::serialize_networkv4_as_string")]
    pub tunnel_net: NetworkV4,
    pub status: UserStatus,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListUserCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let devices = client.list_device(ListDeviceCommand {})?;
        let locations = client.list_location(ListLocationCommand {})?;
        let mgroups = client.list_multicastgroup(ListMulticastGroupCommand {})?;
        let users = client.list_user(ListUserCommand {})?;

        let mut users: Vec<(Pubkey, User)> = users.into_iter().collect();
        users.sort_by(|(_, a), (_, b)| {
            a.device_pk
                .cmp(&b.device_pk)
                .then(a.tunnel_id.cmp(&b.tunnel_id))
        });

        if self.json || self.json_compact {
            let users = users
                .into_iter()
                .map(|(pubkey, user)| {
                    let device = devices.get(&user.device_pk);
                    let location = match device {
                        Some(device) => locations.get(&device.location_pk),
                        None => None,
                    };
                    let device_name = match device {
                        Some(device) => device.code.clone(),
                        None => user.device_pk.to_string(),
                    };
                    let location_code = match device {
                        Some(device) => match location {
                            Some(location) => location.code.clone(),
                            None => device.location_pk.to_string(),
                        },
                        None => "".to_string(),
                    };
                    let location_name = match device {
                        Some(device) => match location {
                            Some(location) => location.name.clone(),
                            None => device.location_pk.to_string(),
                        },
                        None => "".to_string(),
                    };

                    UserDisplay {
                        account: pubkey,
                        user_type: user.user_type,
                        multicast: format_multicast_group_names(&user, &mgroups),
                        publishers: user
                            .publishers
                            .into_iter()
                            .map(|pk| pk.to_string())
                            .collect(),
                        subscribers: user
                            .subscribers
                            .into_iter()
                            .map(|pk| pk.to_string())
                            .collect(),
                        device_pk: user.device_pk,
                        device_name,
                        location_code,
                        location_name,
                        cyoa_type: user.cyoa_type,
                        client_ip: user.client_ip,
                        dz_ip: user.dz_ip,
                        tunnel_id: user.tunnel_id,
                        tunnel_net: user.tunnel_net,
                        status: user.status,
                        owner: user.owner,
                    }
                })
                .collect::<Vec<_>>();

            let json = {
                if self.json_compact {
                    serde_json::to_string(&users)?
                } else {
                    serde_json::to_string_pretty(&users)?
                }
            };
            writeln!(out, "{}", json)?;
        } else {
            let mut table = Table::new();
            table.add_row(row![
                "account",
                "user_type",
                "groups",
                "device",
                "location",
                "cyoa_type",
                "client_ip",
                "tunnel_id",
                "tunnel_net",
                "dz_ip",
                "status",
                "owner"
            ]);

            for (pubkey, data) in users {
                let device = devices.get(&data.device_pk);
                let location = match device {
                    Some(device) => locations.get(&device.location_pk),
                    None => None,
                };

                let device_name = match device {
                    Some(device) => device.code.clone(),
                    None => data.device_pk.to_string(),
                };
                let location_name = match device {
                    Some(device) => match location {
                        Some(location) => location.name.clone(),
                        None => device.location_pk.to_string(),
                    },
                    None => "".to_string(),
                };

                table.add_row(Row::new(vec![
                    Cell::new(&pubkey.to_string()),
                    Cell::new(&data.user_type.to_string()),
                    Cell::new(&format_multicast_group_names(&data, &mgroups)),
                    Cell::new(&device_name),
                    Cell::new(&location_name),
                    Cell::new(&data.cyoa_type.to_string()),
                    Cell::new(&ipv4_to_string(&data.client_ip)),
                    Cell::new(&data.tunnel_id.to_string()),
                    Cell::new(&networkv4_to_string(&data.tunnel_net)),
                    Cell::new(&ipv4_to_string(&data.dz_ip)),
                    Cell::new(&data.status.to_string()),
                    Cell::new(&data.owner.to_string()),
                ]));
            }

            table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
            let _ = table.print(out);
        }

        Ok(())
    }
}

pub fn format_multicast_group_names(
    user: &User,
    mgroups: &HashMap<Pubkey, MulticastGroup>,
) -> String {
    user.get_multicast_groups()
        .iter()
        .map(|pk| {
            let name = mgroups
                .get(pk)
                .map_or_else(|| pk.to_string(), |group| group.code.clone());

            let mut result = name;
            if user.publishers.contains(pk) {
                result.push_str(" (Tx)");
            }
            if user.subscribers.contains(pk) {
                result.push_str(" (Rx)");
            }
            result
        })
        .collect::<Vec<_>>()
        .join(",")
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::tests::tests::create_test_client;
    use crate::user::list::ListUserCliCommand;
    use crate::user::list::UserCYOA::GREOverDIA;
    use crate::user::list::UserStatus::Activated;
    use crate::user::list::UserType::IBRL;
    use doublezero_sdk::{
        AccountType, Device, DeviceStatus, DeviceType, Exchange, ExchangeStatus, Location,
        LocationStatus, MulticastGroup, MulticastGroupStatus, User, UserType,
    };
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_user_list() {
        let mut client = create_test_client();

        let user1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let user2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");

        let location1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1");
        let location1 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 2,
            code: "location1_code".to_string(),
            name: "location1_name".to_string(),
            country: "location1_country".to_string(),
            lat: 15.0,
            lng: 15.0,
            loc_id: 6,
            status: LocationStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1"),
        };
        let location2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo2");
        let location2 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 2,
            code: "location2_code".to_string(),
            name: "location2_name".to_string(),
            country: "location2_country".to_string(),
            lat: 15.0,
            lng: 15.0,
            loc_id: 6,
            status: LocationStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo2"),
        };
        let exchange1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3");
        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 2,
            code: "exchange1_code".to_string(),
            name: "exchange1_name".to_string(),
            lat: 15.0,
            lng: 15.0,
            loc_id: 6,
            status: ExchangeStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3"),
        };
        let exchange2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo4");
        let exchange2 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 2,
            code: "exchange2_code".to_string(),
            name: "exchange2_name".to_string(),
            lat: 15.0,
            lng: 15.0,
            loc_id: 6,
            status: ExchangeStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo4"),
        };

        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            code: "device1_code".to_string(),
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
        };
        let device2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            code: "device2_code".to_string(),
            location_pk: location2_pubkey,
            exchange_pk: exchange2_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8"),
        };
        let mgroup1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8");
        let mgroup1 = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 2,
            tenant_pk: Pubkey::default(),
            code: "m_code".to_string(),
            multicast_ip: [1, 2, 3, 4],
            max_bandwidth: 1000,
            pub_allowlist: vec![],
            sub_allowlist: vec![],
            publishers: vec![],
            subscribers: vec![user2_pubkey],
            status: MulticastGroupStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
        };

        client.expect_list_location().returning(move |_| {
            let mut locations = HashMap::new();
            locations.insert(location1_pubkey, location1.clone());
            locations.insert(location2_pubkey, location2.clone());
            Ok(locations)
        });

        client.expect_list_exchange().returning(move |_| {
            let mut exchanges = HashMap::new();
            exchanges.insert(exchange1_pubkey, exchange1.clone());
            exchanges.insert(exchange2_pubkey, exchange2.clone());
            Ok(exchanges)
        });

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            devices.insert(device2_pubkey, device2.clone());
            Ok(devices)
        });

        client.expect_list_multicastgroup().returning(move |_| {
            let mut mgroups = HashMap::new();
            mgroups.insert(mgroup1_pubkey, mgroup1.clone());
            Ok(mgroups)
        });

        let user1 = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 2,
            owner: user1_pubkey,
            user_type: IBRL,
            tenant_pk: Pubkey::default(),
            device_pk: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4],
            dz_ip: [2, 3, 4, 5],
            tunnel_id: 500,
            tunnel_net: ([1, 2, 3, 5], 32).into(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
        };

        let user2 = User {
            account_type: AccountType::User,
            index: 2,
            bump_seed: 3,
            owner: user2_pubkey,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4],
            dz_ip: [2, 3, 4, 5],
            tunnel_id: 500,
            tunnel_net: ([1, 2, 3, 5], 32).into(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![mgroup1_pubkey],
        };

        client.expect_list_user().returning(move |_| {
            let mut users = HashMap::new();
            users.insert(user1_pubkey, user1.clone());
            users.insert(user2_pubkey, user2.clone());
            Ok(users)
        });

        let mut output = Vec::new();
        let res = ListUserCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();

        assert_eq!(output_str, " account                                   | user_type | groups      | device       | location       | cyoa_type  | client_ip | tunnel_id | tunnel_net | dz_ip   | status    | owner \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | Multicast | m_code (Rx) | device1_code | location1_name | GREOverDIA | 1.2.3.4   | 500       | 1.2.3.5/32 | 2.3.4.5 | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n");

        let mut output = Vec::new();
        let res = ListUserCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\",\"user_type\":\"Multicast\",\"device_pk\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"multicast\":\"m_code (Rx)\",\"publishers\":[],\"subscribers\":[\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8\"],\"device_name\":\"device1_code\",\"location_code\":\"location1_code\",\"location_name\":\"location1_name\",\"cyoa_type\":\"GREOverDIA\",\"client_ip\":\"1.2.3.4\",\"dz_ip\":\"2.3.4.5\",\"tunnel_id\":500,\"tunnel_net\":\"1.2.3.5/32\",\"status\":\"Activated\",\"owner\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\"}]\n");
    }
}
