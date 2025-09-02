use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::{serializer, types::NetworkV4};
use doublezero_sdk::{
    commands::{
        accesspass::list::ListAccessPassCommand, device::list::ListDeviceCommand,
        location::list::ListLocationCommand, multicastgroup::list::ListMulticastGroupCommand,
        user::list::ListUserCommand,
    },
    MulticastGroup, User, UserCYOA, UserStatus, UserType,
};
use doublezero_serviceability::pda::get_accesspass_pda;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, io::Write, net::Ipv4Addr};
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListUserCliCommand {
    /// List prepaid access passes
    #[arg(long, default_value_t = false)]
    pub prepaid: bool,
    /// List Solana validator access passes
    #[arg(long, default_value_t = false)]
    pub solana_validator: bool,
    /// Solana identity public key
    #[arg(long)]
    pub solana_identity: Option<Pubkey>,

    /// Output as pretty JSON.
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON.
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct UserDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub user_type: UserType,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub device_pk: Pubkey,
    #[tabled(rename = "groups")]
    pub multicast: String,
    #[tabled(skip)]
    #[serde(serialize_with = "serializer::serialize_pubkeylist_as_string")]
    pub publishers: Vec<Pubkey>,
    #[tabled(skip)]
    #[serde(serialize_with = "serializer::serialize_pubkeylist_as_string")]
    pub subscribers: Vec<Pubkey>,
    #[tabled(rename = "device")]
    pub device_name: String,
    #[tabled(skip)]
    pub location_code: String,
    #[tabled(rename = "location")]
    pub location_name: String,
    pub cyoa_type: UserCYOA,
    pub client_ip: Ipv4Addr,
    pub dz_ip: Ipv4Addr,
    pub accesspass: String,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    pub status: UserStatus,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListUserCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let devices = client.list_device(ListDeviceCommand)?;
        let locations = client.list_location(ListLocationCommand)?;
        let mgroups = client.list_multicastgroup(ListMulticastGroupCommand)?;
        let accesspasses = client.list_accesspass(ListAccessPassCommand {})?;
        let binding = client.list_user(ListUserCommand)?;

        let mut users = binding
            .iter()
            .map(|(pk, user)| {
                let (accesspass_pk, _) =
                    get_accesspass_pda(&client.get_program_id(), &user.client_ip, &user.owner);
                let accesspass = accesspasses.get(&accesspass_pk);

                (*pk, user.clone(), accesspass.cloned())
            })
            .collect::<Vec<_>>();

        // Filter users by access pass type
        if self.prepaid {
            users.retain(|(_, _, accesspass)| {
                if let Some(accesspass) = accesspass {
                    accesspass.accesspass_type
                        == doublezero_serviceability::state::accesspass::AccessPassType::Prepaid
                } else {
                    false
                }
            });
        }
        // Filter users by access pass type
        if self.solana_validator {
            users.retain(|(_, _, accesspass)| {
                if let Some(accesspass) = accesspass {
                    matches!(
                        accesspass.accesspass_type,
                        doublezero_serviceability::state::accesspass::AccessPassType::SolanaValidator(_)
                    )
                } else {
                    false
                }
            });
        }

        if let Some(solana_identity) = self.solana_identity {
            users.retain(|(_, _, accesspass)| {
                if let Some(accesspass) = accesspass {
                    accesspass.accesspass_type ==
                        doublezero_serviceability::state::accesspass::AccessPassType::SolanaValidator(solana_identity)
                } else {
                    false
                }
            });
        }

        let mut users_displays: Vec<UserDisplay> = users
            .into_iter()
            .map(|(pubkey, user, accesspass)| {
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
                    device_pk: user.device_pk,
                    multicast: format_multicast_group_names(&user, &mgroups),
                    publishers: user.publishers,
                    subscribers: user.subscribers,
                    device_name,
                    location_code,
                    location_name,
                    cyoa_type: user.cyoa_type,
                    client_ip: user.client_ip,
                    dz_ip: user.dz_ip,
                    accesspass: if let Some(accesspass) = accesspass {
                        accesspass.to_string()
                    } else {
                        "none".to_string()
                    },
                    tunnel_id: user.tunnel_id,
                    tunnel_net: user.tunnel_net,
                    status: user.status,
                    owner: user.owner,
                }
            })
            .collect();

        users_displays.sort_by(|a, b| {
            a.device_name
                .cmp(&b.device_name)
                .then(a.tunnel_id.cmp(&b.tunnel_id))
        });

        let res = if self.json {
            serde_json::to_string_pretty(&users_displays)?
        } else if self.json_compact {
            serde_json::to_string(&users_displays)?
        } else {
            Table::new(users_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

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

    use crate::{
        doublezerocommand::CliCommand,
        tests::utils::create_test_client,
        user::list::{
            ListUserCliCommand, UserCYOA::GREOverDIA, UserStatus::Activated, UserType::IBRL,
        },
    };
    use doublezero_sdk::{
        AccountType, Device, DeviceStatus, DeviceType, Exchange, ExchangeStatus, Location,
        LocationStatus, MulticastGroup, MulticastGroupStatus, User, UserType,
    };
    use doublezero_serviceability::{
        pda::get_accesspass_pda,
        state::accesspass::{AccessPass, AccessPassStatus, AccessPassType},
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
            reference_count: 0,
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
            reference_count: 0,
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
            reference_count: 0,
            code: "exchange1_code".to_string(),
            name: "exchange1_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
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
            reference_count: 0,
            code: "exchange2_code".to_string(),
            name: "exchange2_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 15.0,
            lng: 15.0,
            loc_id: 6,
            status: ExchangeStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo4"),
        };

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "device1_code".to_string(),
            contributor_pk,
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
        };
        let device2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "device2_code".to_string(),
            contributor_pk,
            location_pk: location2_pubkey,
            exchange_pk: exchange2_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8"),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
        };
        let mgroup1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8");
        let mgroup1 = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 2,
            tenant_pk: Pubkey::default(),
            code: "m_code".to_string(),
            multicast_ip: [1, 2, 3, 4].into(),
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
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [2, 3, 4, 5].into(),
            tunnel_id: 500,
            tunnel_net: "1.2.3.5/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

        let (accesspass1_pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &user1.client_ip, &user1.owner);
        let accesspass1 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user1.client_ip,
            user_payer: user1.owner,
            last_access_epoch: 10,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            owner: client.get_payer(),
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
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [2, 3, 4, 5].into(),
            tunnel_id: 500,
            tunnel_net: "1.2.3.5/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![mgroup1_pubkey],
            validator_pubkey: Pubkey::default(),
        };

        let (accesspass2_pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &user2.client_ip, &user2.owner);
        let accesspass2 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user2.client_ip,
            user_payer: user2.owner,
            last_access_epoch: 10,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            owner: client.get_payer(),
        };

        client.expect_list_user().returning(move |_| {
            let mut users = HashMap::new();
            users.insert(user1_pubkey, user1.clone());
            users.insert(user2_pubkey, user2.clone());
            Ok(users)
        });

        client.expect_list_accesspass().returning(move |_| {
            let mut accesspasses = HashMap::new();
            accesspasses.insert(accesspass1_pubkey, accesspass1.clone());
            accesspasses.insert(accesspass2_pubkey, accesspass2.clone());
            Ok(accesspasses)
        });

        /*****************************************************************************************************/

        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | user_type | groups      | device       | location       | cyoa_type  | client_ip | dz_ip   | accesspass                  | tunnel_id | tunnel_net | status    | owner                                     \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | Multicast | m_code (Rx) | device1_code | location1_name | GREOverDIA | 1.2.3.4   | 2.3.4.5 | Prepaid: (expires epoch 10) | 500       | 1.2.3.5/32 | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n");

        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\",\"user_type\":\"Multicast\",\"device_pk\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"multicast\":\"m_code (Rx)\",\"publishers\":\"\",\"subscribers\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8\",\"device_name\":\"device1_code\",\"location_code\":\"location1_code\",\"location_name\":\"location1_name\",\"cyoa_type\":\"GREOverDIA\",\"client_ip\":\"1.2.3.4\",\"dz_ip\":\"2.3.4.5\",\"accesspass\":\"Prepaid: (expires epoch 10)\",\"tunnel_id\":500,\"tunnel_net\":\"1.2.3.5/32\",\"status\":\"Activated\",\"owner\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\"}]\n");
    }
}
